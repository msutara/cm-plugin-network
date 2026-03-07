// Package network implements the Config Manager network plugin.
// It provides network interface configuration for Debian-based nodes.
package network

import (
	"net/http"
	"sync"

	"github.com/msutara/config-manager-core/plugin"
)

// Compile-time check: NetworkPlugin must implement plugin.Plugin.
var _ plugin.Plugin = (*NetworkPlugin)(nil)

// NetworkPlugin implements the core plugin.Plugin interface for network management.
type NetworkPlugin struct {
	svc     *Service
	svcOnce sync.Once
}

// NewNetworkPlugin creates a NetworkPlugin with a shared Service instance.
func NewNetworkPlugin() *NetworkPlugin {
	return &NetworkPlugin{svc: NewService()}
}

func (p *NetworkPlugin) Name() string {
	return "network"
}

func (p *NetworkPlugin) Version() string {
	return "0.1.0"
}

func (p *NetworkPlugin) Description() string {
	return "Network interface configuration"
}

func (p *NetworkPlugin) Routes() http.Handler {
	p.svcOnce.Do(func() {
		if p.svc == nil {
			p.svc = NewService()
		}
	})
	return newRouter(p.svc)
}

func (p *NetworkPlugin) ScheduledJobs() []plugin.JobDefinition {
	return nil
}

func (p *NetworkPlugin) Endpoints() []plugin.Endpoint {
	return []plugin.Endpoint{
		{Method: http.MethodGet, Path: "/interfaces", Description: "Network interface details"},
		{Method: http.MethodGet, Path: "/status", Description: "Connectivity and reachability status"},
		{Method: http.MethodGet, Path: "/dns", Description: "DNS configuration"},
		{Method: http.MethodDelete, Path: "/interfaces/{name}", Description: "Remove static IP config (revert to DHCP)"},
	}
}
