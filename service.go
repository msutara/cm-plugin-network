package network

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
)

// Service contains the domain logic for network management.
type Service struct {
	mu sync.RWMutex

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
	if !ipNet.Contains(gw) {
		return errGWNotInSubnet
	}
	if ip.Equal(gw) {
		return errGWEqualsIP
	}
	// Store canonicalized IPv4 to prevent IPv4-mapped IPv6 from reaching config files.
	req.Gateway = gw4.String()
	return nil
}

// SetStaticIP configures a static IP address on the named interface.
// On Linux, it atomically writes to /etc/network/interfaces.d/{name}
// and restarts the interface with ifdown/ifup using a timeout.
func (s *Service) SetStaticIP(name string, req StaticIPRequest) (*Interface, error) {
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
	if err := atomicWriteFile(confPath, []byte(config), 0o644); err != nil {
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
		return nil, fmt.Errorf("ifup %s failed: %w: %s", name, err, string(out))
	}

	return s.getInterfaceUnlocked(name)
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

	if err := atomicWriteFile(resolveSymlink(s.resolvPath), []byte(b.String()), 0o644); err != nil {
		return nil, fmt.Errorf("writing resolv.conf: %w", err)
	}
	slog.Info("wrote DNS config", "plugin", "network", "nameservers", cfg.Nameservers)

	return parseResolvConf(s.resolvPath)
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
