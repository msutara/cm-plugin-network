package network

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const (
	// defaultCmdTimeout is the maximum time allowed for ifdown/ifup commands.
	defaultCmdTimeout = 30 * time.Second
	// defaultDNSCheckHost is the hostname used to verify DNS resolution.
	defaultDNSCheckHost = "dns.google"
	// defaultConnectTarget is the TCP endpoint used to verify internet reachability.
	defaultConnectTarget = "8.8.8.8:53"
)

// Interface represents a network interface and its current state.
type Interface struct {
	Name  string `json:"name"`
	MAC   string `json:"mac"`
	IP    string `json:"ip"`
	State string `json:"state"`
}

// DNSConfig holds the system DNS configuration.
type DNSConfig struct {
	Nameservers []string `json:"nameservers"`
	Search      []string `json:"search"`
}

// DryRunResult holds the result of a dry-run validation without applying changes.
type DryRunResult struct {
	Valid    bool     `json:"valid"`
	Current  string   `json:"current_config,omitempty"`
	Proposed string   `json:"proposed_config"`
	Changes  []string `json:"changes"`
}

// StaticIPRequest is the payload for setting a static IP on an interface.
type StaticIPRequest struct {
	IP      string `json:"ip"`
	Gateway string `json:"gateway"`
}

// NetworkStatus holds overall network connectivity information.
type NetworkStatus struct {
	DefaultGateway    string `json:"default_gateway"`
	DNSReachable      bool   `json:"dns_reachable"`
	InternetReachable bool   `json:"internet_reachable"`
}

var (
	errNotLinux          = errors.New("network plugin requires Linux")
	errIfaceNotFound     = errors.New("interface not found")
	errInvalidCIDR       = errors.New("invalid CIDR notation for IP (expected e.g. 192.168.1.10/24)")
	errInvalidGW         = errors.New("invalid gateway IP address")
	errEmptyIP           = errors.New("ip field is required")
	errEmptyGateway      = errors.New("gateway field is required")
	errIPv6NotSupported  = errors.New("IPv6 addresses are not supported; only IPv4 is supported")
	errInvalidNameserver = errors.New("invalid nameserver IP address")
	errInvalidSearchDom  = errors.New("search domain contains invalid characters")
	errGWNotInSubnet     = errors.New("gateway is not within the IP address subnet")
	errGWEqualsIP        = errors.New("gateway cannot be the same as the interface IP")
	errInvalidIfaceName  = errors.New("invalid interface name")
	errNoStaticConfig    = errors.New("no static configuration exists for this interface")
	errNoBackup          = errors.New("no backup configuration available to rollback")
)

// Service contains the domain logic for network management.
type Service struct {
	mu sync.RWMutex

	policy *InterfacePolicy

	// resolvPath is overridable for testing.
	resolvPath string
	// interfacesDirPath is overridable for testing.
	interfacesDirPath string
	// cmdTimeout limits how long ifdown/ifup commands may run.
	cmdTimeout time.Duration
	// dnsCheckHost is the hostname resolved to test DNS. Empty disables the check.
	dnsCheckHost string
	// connectTarget is the host:port dialed to test internet. Empty disables the check.
	connectTarget string
	// ifdownPath is the absolute path to the ifdown binary.
	ifdownPath string
	// ifupPath is the absolute path to the ifup binary.
	ifupPath string
}

// validIfaceName matches safe interface names (alphanumeric, hyphens,
// underscores, dots for VLANs, colons for aliases).
var validIfaceName = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._:-]*$`)

// validSearchDomain matches safe DNS search domain names.
var validSearchDomain = regexp.MustCompile(`^[a-zA-Z0-9._-]+$`)

// NewService creates a Service with default system paths.
func NewService() *Service {
	return &Service{
		resolvPath:        "/etc/resolv.conf",
		interfacesDirPath: "/etc/network/interfaces.d",
		cmdTimeout:        defaultCmdTimeout,
		dnsCheckHost:      defaultDNSCheckHost,
		connectTarget:     defaultConnectTarget,
		ifdownPath:        "/sbin/ifdown",
		ifupPath:          "/sbin/ifup",
	}
}

// ListInterfaces returns all network interfaces on the system.
func (s *Service) ListInterfaces() ([]Interface, error) {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil, fmt.Errorf("listing interfaces: %w", err)
	}

	result := make([]Interface, 0, len(ifaces))
	for _, iface := range ifaces {
		// Skip loopback
		if iface.Flags&net.FlagLoopback != 0 {
			continue
		}

		state := "down"
		if iface.Flags&net.FlagUp != 0 {
			state = "up"
		}

		ip := ""
		addrs, err := iface.Addrs()
		if err == nil {
			for _, addr := range addrs {
				if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
					ip = addr.String()
					break
				}
			}
		}

		result = append(result, Interface{
			Name:  iface.Name,
			MAC:   iface.HardwareAddr.String(),
			IP:    ip,
			State: state,
		})
	}
	return result, nil
}

// GetInterface returns details for a single network interface.
func (s *Service) GetInterface(name string) (*Interface, error) {
	if !validIfaceName.MatchString(name) {
		return nil, errInvalidIfaceName
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, errIfaceNotFound
	}

	state := "down"
	if iface.Flags&net.FlagUp != 0 {
		state = "up"
	}

	ip := ""
	addrs, err := iface.Addrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				ip = addr.String()
				break
			}
		}
	}

	return &Interface{
		Name:  iface.Name,
		MAC:   iface.HardwareAddr.String(),
		IP:    ip,
		State: state,
	}, nil
}

// validateStaticIPRequest checks that the request fields are valid.
// It canonicalizes the gateway to pure IPv4 form in place.
func validateStaticIPRequest(req *StaticIPRequest) error {
	if req.IP == "" {
		return errEmptyIP
	}
	ip, ipNet, err := net.ParseCIDR(req.IP)
	if err != nil {
		return errInvalidCIDR
	}
	if ip.To4() == nil {
		return errIPv6NotSupported
	}
	if req.Gateway == "" {
		return errEmptyGateway
	}
	gw := net.ParseIP(req.Gateway)
	if gw == nil {
		return errInvalidGW
	}
	gw4 := gw.To4()
	if gw4 == nil {
		return errIPv6NotSupported
	}
	// Canonicalize early so subnet and equality checks use the same representation.
	req.Gateway = gw4.String()
	if !ipNet.Contains(gw4) {
		return errGWNotInSubnet
	}
	if ip.To4().Equal(gw4) {
		return errGWEqualsIP
	}
	return nil
}

// checkWritePolicy returns an error if the interface is denied by policy.
func (s *Service) checkWritePolicy(name string) error {
	if s.policy != nil && !s.policy.IsWriteAllowed(name) {
		return &InterfaceDeniedError{Name: name}
	}
	return nil
}

// SetStaticIP configures a static IP address on the named interface.
// On Linux, it atomically writes to /etc/network/interfaces.d/{name}
// and restarts the interface with ifdown/ifup using a timeout.
func (s *Service) SetStaticIP(name string, req StaticIPRequest) (*Interface, error) {
	if err := s.checkWritePolicy(name); err != nil {
		return nil, err
	}
	if err := validateStaticIPRequest(&req); err != nil {
		return nil, err
	}

	if runtime.GOOS != "linux" {
		return nil, errNotLinux
	}

	// Defense-in-depth: validate interface name at the service layer
	if !validIfaceName.MatchString(name) {
		return nil, errInvalidIfaceName
	}

	// Verify interface exists
	if _, err := net.InterfaceByName(name); err != nil {
		return nil, errIfaceNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	ip, ipNet, err := net.ParseCIDR(req.IP)
	if err != nil {
		return nil, errInvalidCIDR
	}
	netmask := net.IP(ipNet.Mask).String()

	config := fmt.Sprintf("auto %s\niface %s inet static\n    address %s\n    netmask %s\n    gateway %s\n",
		name, name, ip.String(), netmask, req.Gateway)

	confPath := filepath.Join(s.interfacesDirPath, name)

	// Back up existing config so we can rollback if ifup fails.
	backupPath, err := backupConfigFile(confPath)
	if err != nil {
		return nil, fmt.Errorf("backing up interface config: %w", err)
	}

	if err := atomicWriteFile(confPath, []byte(config), 0o644); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath) // no change applied, backup unnecessary
		}
		return nil, fmt.Errorf("writing interface config: %w", err)
	}
	slog.Info("wrote static IP config", "plugin", "network", "interface", name, "path", confPath)

	// Separate timeouts for ifdown and ifup so a slow ifdown cannot steal ifup's budget.
	ctxDown, cancelDown := context.WithTimeout(context.Background(), s.cmdTimeout)
	defer cancelDown()

	if out, err := exec.CommandContext(ctxDown, s.ifdownPath, name).CombinedOutput(); err != nil {
		slog.Warn("ifdown failed (may be expected for first-time config)", "plugin", "network", "interface", name, "error", err, "output", string(out))
	}

	ctxUp, cancelUp := context.WithTimeout(context.Background(), s.cmdTimeout)
	defer cancelUp()

	if out, err := exec.CommandContext(ctxUp, s.ifupPath, name).CombinedOutput(); err != nil {
		ifupErr := fmt.Errorf("ifup %s failed: %w: %s", name, err, string(out))

		if backupPath != "" {
			if restErr := restoreConfigFile(backupPath, confPath); restErr != nil {
				slog.Error("rollback failed; backup preserved for manual recovery",
					"plugin", "network", "interface", name, "backup", backupPath)
				return nil, fmt.Errorf("%w; rollback also failed: %v; backup preserved for manual recovery", ifupErr, restErr)
			}
			slog.Info("restored config backup after ifup failure", "plugin", "network", "interface", name)

			ctxReUp, cancelReUp := context.WithTimeout(context.Background(), s.cmdTimeout)
			defer cancelReUp()

			if out2, err2 := exec.CommandContext(ctxReUp, s.ifupPath, name).CombinedOutput(); err2 != nil {
				slog.Error("rollback ifup also failed; backup preserved for manual recovery",
					"plugin", "network", "interface", name, "backup", backupPath)
				return nil, fmt.Errorf("%w; rollback ifup also failed: %v: %s; backup preserved for manual recovery", ifupErr, err2, string(out2))
			}
			// Rollback succeeded — safe to remove backup.
			_ = os.Remove(backupPath)
		}

		return nil, ifupErr
	}

	// Success — .bak preserved for rollback via POST /interfaces/{name}/rollback.

	return s.getInterfaceUnlocked(name)
}

// DeleteStaticIP removes the static IP configuration for an interface,
// reverting it to DHCP. The existing config is backed up before removal.
func (s *Service) DeleteStaticIP(name string) (*Interface, error) {
	if err := s.checkWritePolicy(name); err != nil {
		return nil, err
	}
	if runtime.GOOS != "linux" {
		return nil, errNotLinux
	}
	if !validIfaceName.MatchString(name) {
		return nil, errInvalidIfaceName
	}
	if _, err := net.InterfaceByName(name); err != nil {
		return nil, errIfaceNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	confPath := filepath.Join(s.interfacesDirPath, name)
	if _, err := os.Stat(confPath); os.IsNotExist(err) {
		return nil, errNoStaticConfig
	}

	backupPath, err := backupConfigFile(confPath)
	if err != nil {
		return nil, fmt.Errorf("backing up interface config: %w", err)
	}

	if err := os.Remove(confPath); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath)
		}
		return nil, fmt.Errorf("removing interface config: %w", err)
	}
	slog.Info("deleted static IP config", "plugin", "network", "interface", name)

	ctxDown, cancelDown := context.WithTimeout(context.Background(), s.cmdTimeout)
	defer cancelDown()

	if out, err := exec.CommandContext(ctxDown, s.ifdownPath, name).CombinedOutput(); err != nil {
		slog.Warn("ifdown failed during delete", "plugin", "network",
			"interface", name, "error", err, "output", string(out))
	}

	ctxUp, cancelUp := context.WithTimeout(context.Background(), s.cmdTimeout)
	defer cancelUp()

	// ifup may fail if the interface has no fallback config (e.g. no DHCP
	// entry in /etc/network/interfaces). This is expected after deleting
	// the only config snippet — the interface stays down until manually
	// configured or rolled back via POST /interfaces/{name}/rollback.
	if out, err := exec.CommandContext(ctxUp, s.ifupPath, name).CombinedOutput(); err != nil {
		slog.Warn("ifup failed after config removal (interface may lack fallback config)",
			"plugin", "network", "interface", name, "error", err, "output", string(out))
	}

	// .bak preserved for rollback via POST /interfaces/{name}/rollback.

	return s.getInterfaceUnlocked(name)
}

// DryRunDeleteStaticIP previews what would change if the static config were removed.
func (s *Service) DryRunDeleteStaticIP(name string) (*DryRunResult, error) {
	if err := s.checkWritePolicy(name); err != nil {
		return nil, err
	}
	if !validIfaceName.MatchString(name) {
		return nil, errInvalidIfaceName
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	confPath := filepath.Join(s.interfacesDirPath, name)
	data, err := os.ReadFile(confPath)
	if os.IsNotExist(err) {
		return nil, errNoStaticConfig
	}
	if err != nil {
		return nil, fmt.Errorf("read current config: %w", err)
	}

	return &DryRunResult{
		Valid:    true,
		Current:  string(data),
		Proposed: "",
		Changes:  []string{"static configuration will be removed; interface will revert to DHCP"},
	}, nil
}

// RollbackInterface restores the interface config from the current .bak snapshot.
// The .bak file represents the state prior to the last mutating operation
// (PUT, DELETE, or a previous rollback). On successful rollback the
// pre-rollback snapshot is promoted to .bak so the rollback itself can
// be reversed.
func (s *Service) RollbackInterface(name string) (*Interface, error) {
	if err := s.checkWritePolicy(name); err != nil {
		return nil, err
	}
	if runtime.GOOS != "linux" {
		return nil, errNotLinux
	}
	if !validIfaceName.MatchString(name) {
		return nil, errInvalidIfaceName
	}
	if _, err := net.InterfaceByName(name); err != nil {
		return nil, errIfaceNotFound
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	confPath := filepath.Join(s.interfacesDirPath, name)
	backupPath := confPath + ".bak"

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return nil, errNoBackup
	}

	// Backup current config to a DIFFERENT path so we don't overwrite the
	// .bak we're about to restore from.
	preRollbackPath, err := backupConfigFileAs(confPath, ".pre-rollback")
	if err != nil {
		return nil, fmt.Errorf("backing up current config before rollback: %w", err)
	}

	if err := restoreConfigFile(backupPath, confPath); err != nil {
		if preRollbackPath != "" {
			_ = os.Remove(preRollbackPath)
		}
		return nil, fmt.Errorf("restoring backup config: %w", err)
	}

	ctxDown, cancelDown := context.WithTimeout(context.Background(), s.cmdTimeout)
	defer cancelDown()

	if out, err := exec.CommandContext(ctxDown, s.ifdownPath, name).CombinedOutput(); err != nil {
		slog.Warn("ifdown failed during rollback", "plugin", "network",
			"interface", name, "error", err, "output", string(out))
	}

	ctxUp, cancelUp := context.WithTimeout(context.Background(), s.cmdTimeout)
	defer cancelUp()

	if out, err := exec.CommandContext(ctxUp, s.ifupPath, name).CombinedOutput(); err != nil {
		ifupErr := fmt.Errorf("ifup %s failed after rollback: %w: %s", name, err, string(out))

		// Restore pre-rollback state.
		if preRollbackPath != "" {
			if restErr := restoreConfigFile(preRollbackPath, confPath); restErr != nil {
				slog.Error("rollback reversal failed; backup preserved",
					"plugin", "network", "interface", name, "backup", preRollbackPath)
				return nil, fmt.Errorf("%w; reversal also failed: %v; backup preserved for manual recovery",
					ifupErr, restErr)
			}

			ctxReUp, cancelReUp := context.WithTimeout(context.Background(), s.cmdTimeout)
			defer cancelReUp()

			if out2, err2 := exec.CommandContext(ctxReUp, s.ifupPath, name).CombinedOutput(); err2 != nil {
				slog.Error("rollback reversal ifup failed",
					"plugin", "network", "interface", name, "backup", preRollbackPath)
				return nil, fmt.Errorf("%w; reversal ifup also failed: %v: %s; backup preserved for manual recovery",
					ifupErr, err2, string(out2))
			}
			_ = os.Remove(preRollbackPath)
		} else {
			// No prior config existed (e.g. post-delete rollback).
			// Remove the restored file to return to "no config" state.
			_ = os.Remove(confPath)
			// Rename .bak to mark it as failed so retries don't loop.
			failedPath := backupPath + ".failed"
			if renameErr := os.Rename(backupPath, failedPath); renameErr != nil {
				slog.Warn("could not rename backup to .failed",
					"plugin", "network", "interface", name, "error", renameErr)
			}
			slog.Warn("rollback ifup failed; reverted to no-config state; backup marked as failed",
				"plugin", "network", "interface", name, "failedBackup", failedPath)
		}
		return nil, ifupErr
	}

	// Success — promote .pre-rollback → .bak so the rollback itself is reversible.
	if preRollbackPath != "" {
		if err := os.Rename(preRollbackPath, backupPath); err != nil {
			slog.Warn("could not promote pre-rollback to backup",
				"plugin", "network", "interface", name)
		}
	}

	return s.getInterfaceUnlocked(name)
}

// DryRunRollbackInterface previews what would change if the .bak config were restored.
func (s *Service) DryRunRollbackInterface(name string) (*DryRunResult, error) {
	if err := s.checkWritePolicy(name); err != nil {
		return nil, err
	}
	if !validIfaceName.MatchString(name) {
		return nil, errInvalidIfaceName
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	confPath := filepath.Join(s.interfacesDirPath, name)
	backupPath := confPath + ".bak"

	backupData, err := safeReadFile(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errNoBackup
		}
		return nil, fmt.Errorf("read backup config: %w", err)
	}

	var current string
	if data, err := os.ReadFile(confPath); err == nil {
		current = string(data)
	}

	return &DryRunResult{
		Valid:    true,
		Current:  current,
		Proposed: string(backupData),
		Changes:  diffConfigs(current, string(backupData)),
	}, nil
}

// GetDNS returns the current DNS configuration by parsing resolv.conf.
func (s *Service) GetDNS() (*DNSConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return parseResolvConf(s.resolvPath)
}

// parseResolvConf reads a resolv.conf file and extracts nameservers and
// search domains.
func parseResolvConf(path string) (*DNSConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return &DNSConfig{
				Nameservers: make([]string, 0),
				Search:      make([]string, 0),
			}, nil
		}
		return nil, fmt.Errorf("reading resolv.conf: %w", err)
	}
	defer f.Close()

	cfg := &DNSConfig{
		Nameservers: make([]string, 0),
		Search:      make([]string, 0),
	}

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch fields[0] {
		case "nameserver":
			cfg.Nameservers = append(cfg.Nameservers, fields[1])
		case "search":
			cfg.Search = append(cfg.Search, fields[1:]...)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scanning resolv.conf: %w", err)
	}
	return cfg, nil
}

// SetDNS updates the system DNS configuration by writing to resolv.conf.
func (s *Service) SetDNS(cfg DNSConfig) (*DNSConfig, error) {
	if runtime.GOOS != "linux" {
		return nil, errNotLinux
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var b strings.Builder
	b.WriteString("# Generated by cm-plugin-network\n")
	for _, ns := range cfg.Nameservers {
		if net.ParseIP(ns) == nil {
			return nil, fmt.Errorf("%w: %q", errInvalidNameserver, ns)
		}
		fmt.Fprintf(&b, "nameserver %s\n", ns)
	}
	for _, dom := range cfg.Search {
		if !validSearchDomain.MatchString(dom) {
			return nil, fmt.Errorf("%w: %q", errInvalidSearchDom, dom)
		}
	}
	if len(cfg.Search) > 0 {
		fmt.Fprintf(&b, "search %s\n", strings.Join(cfg.Search, " "))
	}

	targetPath := resolveSymlink(s.resolvPath)

	// Back up existing config so we can rollback on failure.
	backupPath, err := backupConfigFile(targetPath)
	if err != nil {
		return nil, fmt.Errorf("backing up DNS config: %w", err)
	}

	if err := atomicWriteFile(targetPath, []byte(b.String()), 0o644); err != nil {
		if backupPath != "" {
			_ = os.Remove(backupPath) // no change applied, backup unnecessary
		}
		return nil, fmt.Errorf("writing resolv.conf: %w", err)
	}
	slog.Info("wrote DNS config", "plugin", "network", "nameservers", cfg.Nameservers)

	result, err := parseResolvConf(s.resolvPath)
	if err != nil {
		if backupPath != "" {
			if restErr := restoreConfigFile(backupPath, targetPath); restErr != nil {
				slog.Error("DNS rollback failed; backup preserved for manual recovery",
					"plugin", "network", "backup", backupPath)
				return nil, fmt.Errorf("verifying DNS config: %w; rollback also failed: %v; backup preserved for manual recovery", err, restErr)
			}
			slog.Info("restored DNS config backup after verification failure", "plugin", "network")
			// Rollback succeeded — safe to remove backup.
			_ = os.Remove(backupPath)
		}
		return nil, fmt.Errorf("verifying DNS config: %w", err)
	}

	// Success — .bak preserved for rollback via POST /dns/rollback.

	return result, nil
}

// GetNetworkStatus returns overall network connectivity status.
func (s *Service) GetNetworkStatus() (*NetworkStatus, error) {
	status := &NetworkStatus{}

	// Parse default gateway from /proc/net/route on Linux
	if runtime.GOOS == "linux" {
		if gw, err := defaultGatewayLinux(); err == nil {
			status.DefaultGateway = gw
		}
	}

	// DNS reachability: try to resolve a well-known domain with a timeout
	if s.dnsCheckHost != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if _, err := net.DefaultResolver.LookupHost(ctx, s.dnsCheckHost); err == nil {
			status.DNSReachable = true
		}
	}

	// Internet reachability: try TCP dial to a reliable endpoint
	if s.connectTarget != "" {
		conn, err := net.DialTimeout("tcp", s.connectTarget, 3*time.Second)
		if err == nil {
			status.InternetReachable = true
			conn.Close()
		}
	}

	return status, nil
}

// DryRunStaticIP validates a static IP request and returns what would change
// without writing any files or restarting the interface.
func (s *Service) DryRunStaticIP(ifaceName string, req StaticIPRequest) (*DryRunResult, error) {
	if err := s.checkWritePolicy(ifaceName); err != nil {
		return nil, err
	}
	if err := validateStaticIPRequest(&req); err != nil {
		return nil, err
	}
	if !validIfaceName.MatchString(ifaceName) {
		return nil, errInvalidIfaceName
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	ip, ipNet, err := net.ParseCIDR(req.IP)
	if err != nil {
		return nil, errInvalidCIDR
	}
	netmask := net.IP(ipNet.Mask).String()

	proposed := fmt.Sprintf("auto %s\niface %s inet static\n    address %s\n    netmask %s\n    gateway %s\n",
		ifaceName, ifaceName, ip.String(), netmask, req.Gateway)

	confPath := filepath.Join(s.interfacesDirPath, ifaceName)
	var current string
	if data, err := os.ReadFile(confPath); err == nil {
		current = string(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read current config: %w", err)
	}

	return &DryRunResult{
		Valid:    true,
		Current:  current,
		Proposed: proposed,
		Changes:  diffConfigs(current, proposed),
	}, nil
}

// DryRunDNS validates a DNS configuration and returns what would change
// without writing any files.
func (s *Service) DryRunDNS(req DNSConfig) (*DryRunResult, error) {
	var b strings.Builder
	b.WriteString("# Generated by cm-plugin-network\n")
	for _, ns := range req.Nameservers {
		if net.ParseIP(ns) == nil {
			return nil, fmt.Errorf("%w: %q", errInvalidNameserver, ns)
		}
		fmt.Fprintf(&b, "nameserver %s\n", ns)
	}
	for _, dom := range req.Search {
		if !validSearchDomain.MatchString(dom) {
			return nil, fmt.Errorf("%w: %q", errInvalidSearchDom, dom)
		}
	}
	if len(req.Search) > 0 {
		fmt.Fprintf(&b, "search %s\n", strings.Join(req.Search, " "))
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	proposed := b.String()
	var current string
	if data, err := os.ReadFile(s.resolvPath); err == nil {
		current = string(data)
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read current config: %w", err)
	}

	return &DryRunResult{
		Valid:    true,
		Current:  current,
		Proposed: proposed,
		Changes:  diffConfigs(current, proposed),
	}, nil
}

// RollbackDNS restores the resolv.conf from the current .bak snapshot.
// The .bak file always represents the state prior to the last mutating
// operation (either a PUT or a previous rollback), and on successful
// rollback the pre-rollback snapshot is promoted to .bak so the rollback
// itself can be reversed.
func (s *Service) RollbackDNS() (*DNSConfig, error) {
	if runtime.GOOS != "linux" {
		return nil, errNotLinux
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	targetPath := resolveSymlink(s.resolvPath)
	backupPath := targetPath + ".bak"

	if _, err := os.Stat(backupPath); os.IsNotExist(err) {
		return nil, errNoBackup
	}

	// Backup current config to a DIFFERENT path so we don't overwrite the
	// .bak we're about to restore from.
	preRollbackPath, err := backupConfigFileAs(targetPath, ".pre-rollback")
	if err != nil {
		return nil, fmt.Errorf("backing up current DNS config before rollback: %w", err)
	}

	if err := restoreConfigFile(backupPath, targetPath); err != nil {
		if preRollbackPath != "" {
			_ = os.Remove(preRollbackPath)
		}
		return nil, fmt.Errorf("restoring DNS backup: %w", err)
	}

	// Verify the restored config is parseable.
	result, err := parseResolvConf(s.resolvPath)
	if err != nil {
		// Restored file is corrupt — revert to pre-rollback state.
		if preRollbackPath != "" {
			if restErr := restoreConfigFile(preRollbackPath, targetPath); restErr != nil {
				slog.Error("DNS rollback reversal failed; backup preserved",
					"plugin", "network", "backup", preRollbackPath)
				return nil, fmt.Errorf("verifying rolled-back DNS: %w; reversal also failed: %v; backup preserved for manual recovery",
					err, restErr)
			}
			_ = os.Remove(preRollbackPath)
		}
		return nil, fmt.Errorf("verifying rolled-back DNS config: %w", err)
	}

	// Success — promote .pre-rollback → .bak so the rollback itself is reversible.
	if preRollbackPath != "" {
		if err := os.Rename(preRollbackPath, backupPath); err != nil {
			slog.Warn("could not promote pre-rollback to backup",
				"plugin", "network")
		}
	}

	slog.Info("rolled back DNS config", "plugin", "network")
	return result, nil
}

// DryRunRollbackDNS previews what would change if the .bak resolv.conf were restored.
func (s *Service) DryRunRollbackDNS() (*DryRunResult, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	targetPath := resolveSymlink(s.resolvPath)
	backupPath := targetPath + ".bak"

	backupData, err := safeReadFile(backupPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, errNoBackup
		}
		return nil, fmt.Errorf("read DNS backup: %w", err)
	}

	var current string
	if data, err := os.ReadFile(targetPath); err == nil {
		current = string(data)
	}

	return &DryRunResult{
		Valid:    true,
		Current:  current,
		Proposed: string(backupData),
		Changes:  diffConfigs(current, string(backupData)),
	}, nil
}

// diffConfigs compares current and proposed configs and returns human-readable changes.
func diffConfigs(current, proposed string) []string {
	if current == "" {
		return []string{"new file will be created"}
	}
	if current == proposed {
		return []string{"no changes"}
	}

	currentLines := strings.Split(strings.TrimSpace(current), "\n")
	proposedLines := strings.Split(strings.TrimSpace(proposed), "\n")

	currentSet := make(map[string]bool, len(currentLines))
	for _, l := range currentLines {
		currentSet[strings.TrimSpace(l)] = true
	}
	proposedSet := make(map[string]bool, len(proposedLines))
	for _, l := range proposedLines {
		proposedSet[strings.TrimSpace(l)] = true
	}

	var changes []string
	for _, l := range currentLines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" && !proposedSet[trimmed] {
			changes = append(changes, "- "+trimmed)
		}
	}
	for _, l := range proposedLines {
		trimmed := strings.TrimSpace(l)
		if trimmed != "" && !currentSet[trimmed] {
			changes = append(changes, "+ "+trimmed)
		}
	}
	if len(changes) == 0 {
		changes = []string{"no changes"}
	}
	return changes
}

// getInterfaceUnlocked reads interface info without acquiring the lock.
// Use when the caller already holds the lock.
func (s *Service) getInterfaceUnlocked(name string) (*Interface, error) {
	iface, err := net.InterfaceByName(name)
	if err != nil {
		return nil, errIfaceNotFound
	}

	state := "down"
	if iface.Flags&net.FlagUp != 0 {
		state = "up"
	}

	ip := ""
	addrs, err := iface.Addrs()
	if err == nil {
		for _, addr := range addrs {
			if ipNet, ok := addr.(*net.IPNet); ok && ipNet.IP.To4() != nil {
				ip = addr.String()
				break
			}
		}
	}

	return &Interface{
		Name:  iface.Name,
		MAC:   iface.HardwareAddr.String(),
		IP:    ip,
		State: state,
	}, nil
}

// atomicWriteFile writes data to a temporary file and renames it to path,
// preventing corruption from incomplete writes.
func atomicWriteFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	tmpName := tmp.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tmpName)
		}
	}()

	if n, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("writing temp file: %w", err)
	} else if n != len(data) {
		_ = tmp.Close()
		return fmt.Errorf("short write: %d of %d bytes", n, len(data))
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("syncing temp file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp file: %w", err)
	}
	if err := os.Chmod(tmpName, perm); err != nil {
		return fmt.Errorf("setting permissions: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	// Sync the parent directory to ensure the rename is durable on power loss.
	if d, err := os.Open(filepath.Dir(path)); err == nil {
		d.Sync()  //nolint:errcheck // best-effort durability
		d.Close() //nolint:errcheck // best-effort close
	}

	success = true
	return nil
}

// resolveSymlink follows a symlink to its real path so that atomicWriteFile
// (which uses os.Rename) writes to the actual file rather than replacing the
// symlink. Uses os.Lstat + os.Readlink to detect symlinks even when the
// target does not yet exist. Falls back to the original path if not a symlink.
func resolveSymlink(path string) string {
	fi, err := os.Lstat(path)
	if err != nil || fi.Mode()&os.ModeSymlink == 0 {
		return path
	}
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		// Target may not exist; try one level of Readlink.
		if t, err2 := os.Readlink(path); err2 == nil {
			if filepath.IsAbs(t) {
				return t
			}
			return filepath.Join(filepath.Dir(path), t)
		}
		return path
	}
	return target
}

// backupConfigFile copies the current config to a .bak file.
// Returns the backup path, or empty string if no existing config to back up.
// NOTE: This intentionally overwrites any existing .bak file. The operator
// should recover from a previous .bak before triggering a new operation.
// Timestamped backups or overwrite-refusal may be added in a future PR.
func backupConfigFile(configPath string) (string, error) {
	return backupConfigFileAs(configPath, ".bak")
}

// backupConfigFileAs copies the current config to configPath+suffix.
// Used by rollback operations to save a pre-rollback snapshot to a distinct
// path (e.g. ".pre-rollback") so it does not overwrite the .bak being restored.
func backupConfigFileAs(configPath, suffix string) (string, error) {
	backupPath := configPath + suffix
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", fmt.Errorf("backup read: %w", err)
	}
	if err := atomicWriteFile(backupPath, data, 0o644); err != nil {
		return "", fmt.Errorf("backup write: %w", err)
	}
	return backupPath, nil
}

// safeReadFile opens path, verifies the file descriptor refers to a regular
// file (not a symlink or device), and reads its contents — all through the
// same fd to eliminate TOCTOU races between check and read.
// safeReadFile opens path and reads it through a single file descriptor,
// eliminating TOCTOU races between stat and read for regular files.
// It rejects non-regular targets (directories, devices, FIFOs) after
// resolving any symlinks, but it does not attempt to defend against
// symlink substitution; callers must ensure that path is trusted if
// protection against symlink traversal is required.
func safeReadFile(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("not a regular file: %s", filepath.Base(path))
	}

	return io.ReadAll(f)
}

// restoreConfigFile restores a backup to the original config path.
// The caller is responsible for removing the backup file after
// confirming the restore was successful (e.g. ifup succeeded).
func restoreConfigFile(backupPath, configPath string) error {
	data, err := safeReadFile(backupPath)
	if err != nil {
		return fmt.Errorf("restore read: %w", err)
	}
	if err := atomicWriteFile(configPath, data, 0o644); err != nil {
		return fmt.Errorf("restore write: %w", err)
	}
	// NOTE: caller is responsible for removing the backup file.
	// In SetStaticIP the backup must survive a failed rollback ifup
	// so the operator can recover manually.
	return nil
}

// defaultGatewayLinux parses /proc/net/route for the default gateway.
func defaultGatewayLinux() (string, error) {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return "", err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Scan() // skip header
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 3 {
			continue
		}
		// Default route has Destination == "00000000"
		if fields[1] == "00000000" {
			gw := parseHexIP(fields[2])
			if gw == "" {
				continue
			}
			return gw, nil
		}
	}
	return "", errors.New("no default gateway found")
}

// parseHexIP converts an 8-character hex string from /proc/net/route into a
// dotted-decimal IPv4 address. The kernel stores these values in host byte
// order (little-endian on x86/ARM), so the octets are reversed.
func parseHexIP(hex string) string {
	if len(hex) != 8 {
		return ""
	}
	var octets [4]byte
	for i := 0; i < 4; i++ {
		var b byte
		for _, c := range hex[i*2 : i*2+2] {
			b <<= 4
			switch {
			case c >= '0' && c <= '9':
				b |= byte(c - '0')
			case c >= 'A' && c <= 'F':
				b |= byte(c-'A') + 10
			case c >= 'a' && c <= 'f':
				b |= byte(c-'a') + 10
			default:
				return ""
			}
		}
		octets[i] = b
	}
	// /proc/net/route stores in host byte order (little-endian on x86/ARM)
	return fmt.Sprintf("%d.%d.%d.%d", octets[3], octets[2], octets[1], octets[0])
}
