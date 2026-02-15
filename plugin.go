// Package network implements the Config Manager network plugin.
// It provides network interface configuration for Debian-based nodes.
package network

import (
	"net/http"

	"github.com/msutara/config-manager-core/plugin"
)

// NetworkPlugin implements the plugin.Plugin interface for network management.
type NetworkPlugin struct{}

func init() {
	plugin.Register(&NetworkPlugin{})
}

func (p *NetworkPlugin) Name() string        { return "network" }
func (p *NetworkPlugin) Version() string     { return "0.1.0" }
func (p *NetworkPlugin) Description() string { return "Network interface configuration" }

func (p *NetworkPlugin) Routes() http.Handler {
	return newRouter()
}

func (p *NetworkPlugin) ScheduledJobs() []plugin.JobDefinition {
	return nil
}
