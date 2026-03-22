// Package network implements the Config Manager network plugin.
// It provides network interface configuration for Debian-based nodes.
package network

import (
	"fmt"
	"log/slog"
	"net/http"
	"sync"

	"github.com/msutara/config-manager-core/plugin"
)

// Compile-time check: NetworkPlugin must implement plugin.Plugin.
var _ plugin.Plugin = (*NetworkPlugin)(nil)

// Compile-time check: NetworkPlugin must implement plugin.Configurable.
var _ plugin.Configurable = (*NetworkPlugin)(nil)

// NetworkPlugin implements the core plugin.Plugin interface for network management.
type NetworkPlugin struct {
	svc     *Service
	svcOnce sync.Once
	policy  *InterfacePolicy
	mu      sync.RWMutex
}

// NewNetworkPlugin creates a NetworkPlugin with a shared Service instance.
func NewNetworkPlugin() *NetworkPlugin {
	policy := NewInterfacePolicy("denylist", nil) // default denylist
	svc := NewService()
	svc.policy = policy
	return &NetworkPlugin{svc: svc, policy: policy}
}

func (p *NetworkPlugin) Name() string {
	return "network"
}

func (p *NetworkPlugin) Version() string {
	return "0.4.3"
}

func (p *NetworkPlugin) Description() string {
	return "Network interface configuration"
}

func (p *NetworkPlugin) Routes() http.Handler {
	p.svcOnce.Do(func() {
		p.ensurePolicy()
		if p.svc == nil {
			svc := NewService()
			svc.policy = p.policy
			p.svc = svc
		}
	})
	return newRouter(p.svc)
}

func (p *NetworkPlugin) ScheduledJobs() []plugin.JobDefinition {
	return nil
}

func (p *NetworkPlugin) Endpoints() []plugin.Endpoint {
	return []plugin.Endpoint{
		{Method: http.MethodGet, Path: "/interfaces", Description: "List all network interfaces"},
		{Method: http.MethodGet, Path: "/interfaces/{name}", Description: "Get interface details"},
		{Method: http.MethodPut, Path: "/interfaces/{name}", Description: "Set static IP configuration"},
		{Method: http.MethodDelete, Path: "/interfaces/{name}", Description: "Remove static IP (revert to DHCP)"},
		{Method: http.MethodPost, Path: "/interfaces/{name}/rollback", Description: "Rollback interface to previous config"},
		{Method: http.MethodGet, Path: "/dns", Description: "Get DNS configuration"},
		{Method: http.MethodPut, Path: "/dns", Description: "Set DNS servers"},
		{Method: http.MethodPost, Path: "/dns/rollback", Description: "Rollback DNS to previous config"},
		{Method: http.MethodGet, Path: "/status", Description: "Connectivity and reachability status"},
	}
}

// ensurePolicy initialises the policy to a default denylist if nil.
// Protects against zero-value NetworkPlugin usage. Also propagates
// the policy pointer to the Service if it was initialised with nil.
func (p *NetworkPlugin) ensurePolicy() {
	if p.policy == nil {
		p.policy = NewInterfacePolicy("denylist", nil)
	}
	if p.svc != nil && p.svc.policy == nil {
		p.svc.policy = p.policy
	}
}

// Configure applies startup configuration from core's config.yaml.
func (p *NetworkPlugin) Configure(cfg map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensurePolicy()

	if cfg == nil {
		return
	}

	policyRaw, ok := cfg["interface_policy"]
	if !ok {
		return
	}
	policyMap, ok := policyRaw.(map[string]any)
	if !ok {
		slog.Warn("interface_policy must be a map, ignoring", "plugin", "network")
		return
	}

	// Determine mode — default to "denylist" when key is absent or non-string.
	mode := "denylist"
	if modeRaw, ok := policyMap["mode"]; ok {
		if s, ok := modeRaw.(string); ok {
			mode = s
		} else {
			slog.Warn("interface_policy.mode must be a string, keeping defaults", "plugin", "network")
			return
		}
	}
	if mode != "" && mode != "denylist" && mode != "allowlist" {
		slog.Warn("invalid interface_policy.mode, keeping defaults", "mode", mode, "plugin", "network")
		return
	}

	var list []string
	if rawList, ok := policyMap["list"]; ok {
		switch v := rawList.(type) {
		case []any:
			list = make([]string, 0, len(v))
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					slog.Warn("interface_policy.list items must be strings, keeping defaults", "plugin", "network")
					return
				}
				list = append(list, s)
			}
		case []string:
			list = v
		default:
			slog.Warn("interface_policy.list must be an array, keeping defaults", "plugin", "network")
			return
		}
	}

	p.policy.Set(mode, list)
}

// UpdateConfig validates and applies a single config key change at runtime.
func (p *NetworkPlugin) UpdateConfig(key string, value any) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.ensurePolicy()

	switch key {
	case "interface_policy.mode":
		mode, ok := value.(string)
		if !ok {
			return fmt.Errorf("interface_policy.mode must be a string")
		}
		if mode != "" && mode != "denylist" && mode != "allowlist" {
			return fmt.Errorf("interface_policy.mode must be 'denylist', 'allowlist', or empty")
		}
		p.policy.Set(mode, p.policy.Patterns())
		return nil
	case "interface_policy.list":
		var list []string
		switch v := value.(type) {
		case []any:
			list = make([]string, 0, len(v))
			for _, item := range v {
				s, ok := item.(string)
				if !ok {
					return fmt.Errorf("interface_policy.list items must be strings")
				}
				list = append(list, s)
			}
		case []string:
			list = v
		default:
			return fmt.Errorf("interface_policy.list must be an array of strings")
		}
		p.policy.Set(p.policy.Mode(), list)
		return nil
	default:
		return fmt.Errorf("unknown config key: %s", key)
	}
}

// CurrentConfig returns the plugin's current configuration.
func (p *NetworkPlugin) CurrentConfig() map[string]any {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.policy == nil {
		return map[string]any{
			"interface_policy": map[string]any{
				"mode": "denylist",
				"list": append([]string(nil), defaultDenylist...),
			},
		}
	}
	return map[string]any{
		"interface_policy": map[string]any{
			"mode": p.policy.Mode(),
			"list": p.policy.Patterns(),
		},
	}
}
