package network

import "fmt"

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

// ListInterfaces returns all network interfaces on the system.
func ListInterfaces() ([]Interface, error) {
	// TODO: implement using net.Interfaces() and ip command parsing
	return nil, fmt.Errorf("not implemented")
}

// GetInterface returns details for a single network interface.
func GetInterface(name string) (*Interface, error) {
	// TODO: implement lookup by interface name
	return nil, fmt.Errorf("interface %q not found", name)
}

// SetStaticIP configures a static IP address on the named interface.
func SetStaticIP(name string, req StaticIPRequest) (*Interface, error) {
	// TODO: implement by writing /etc/network/interfaces.d/{name}
	return nil, fmt.Errorf("not implemented")
}

// GetDNS returns the current DNS configuration.
func GetDNS() (*DNSConfig, error) {
	// TODO: implement by reading /etc/resolv.conf
	return nil, fmt.Errorf("not implemented")
}

// SetDNS updates the system DNS configuration.
func SetDNS(cfg DNSConfig) (*DNSConfig, error) {
	// TODO: implement by writing /etc/resolv.conf
	return nil, fmt.Errorf("not implemented")
}

// GetNetworkStatus returns overall network connectivity status.
func GetNetworkStatus() (*NetworkStatus, error) {
	// TODO: implement gateway detection and reachability checks
	return nil, fmt.Errorf("not implemented")
}
