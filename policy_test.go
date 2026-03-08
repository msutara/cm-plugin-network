package network

import (
	"errors"
	"testing"
)

func TestIsWriteAllowed_NilPolicy(t *testing.T) {
	// Service with nil policy — all writes allowed
	svc := &Service{}
	if err := svc.checkWritePolicy("eth0"); err != nil {
		t.Errorf("nil policy should allow all writes, got %v", err)
	}
}

func TestIsWriteAllowed_EmptyMode(t *testing.T) {
	p := NewInterfacePolicy("", nil)
	if !p.IsWriteAllowed("anything") {
		t.Error("empty mode should allow all")
	}
}

func TestIsWriteAllowed_DenylistDefault(t *testing.T) {
	p := NewInterfacePolicy("denylist", nil)
	for _, name := range defaultDenylist {
		if p.IsWriteAllowed(name) {
			t.Errorf("%q should be denied by default denylist", name)
		}
	}
	if !p.IsWriteAllowed("eth0") {
		t.Error("eth0 should be allowed by default denylist")
	}
}

func TestIsWriteAllowed_DenylistCustom(t *testing.T) {
	p := NewInterfacePolicy("denylist", []string{"lo", "eth0p", "docker*"})
	tests := []struct {
		name    string
		allowed bool
	}{
		{"lo", false},
		{"eth0p", false},
		{"docker0", false},
		{"docker_gwbridge", false},
		{"eth0", true},
		{"wlan0", true},
	}
	for _, tc := range tests {
		if got := p.IsWriteAllowed(tc.name); got != tc.allowed {
			t.Errorf("IsWriteAllowed(%q) = %v, want %v", tc.name, got, tc.allowed)
		}
	}
}

func TestIsWriteAllowed_Allowlist(t *testing.T) {
	p := NewInterfacePolicy("allowlist", []string{"eth0", "eth1", "wlan*"})
	tests := []struct {
		name    string
		allowed bool
	}{
		{"eth0", true},
		{"eth1", true},
		{"wlan0", true},
		{"wlan1", true},
		{"br0", false},
		{"lo", false},
		{"docker0", false},
	}
	for _, tc := range tests {
		if got := p.IsWriteAllowed(tc.name); got != tc.allowed {
			t.Errorf("IsWriteAllowed(%q) = %v, want %v", tc.name, got, tc.allowed)
		}
	}
}

func TestIsWriteAllowed_GlobPatterns(t *testing.T) {
	p := NewInterfacePolicy("denylist", []string{"veth*", "br-*", "docker*", "ip6tnl?"})
	tests := []struct {
		name    string
		allowed bool
	}{
		{"veth1234abc", false},
		{"br-deadbeef", false},
		{"docker0", false},
		{"docker_gwbridge", false},
		{"ip6tnl0", false},
		{"ip6tnl1", false},
		{"ip6tnl", true}, // ? requires exactly one char
		{"eth0", true},
		{"wlan0", true},
	}
	for _, tc := range tests {
		if got := p.IsWriteAllowed(tc.name); got != tc.allowed {
			t.Errorf("IsWriteAllowed(%q) = %v, want %v", tc.name, got, tc.allowed)
		}
	}
}

func TestIsWriteAllowed_AllowlistEmpty(t *testing.T) {
	// Empty allowlist = nothing allowed
	p := NewInterfacePolicy("allowlist", []string{})
	if p.IsWriteAllowed("eth0") {
		t.Error("empty allowlist should deny everything")
	}
}

func TestIsWriteAllowed_DenylistEmpty(t *testing.T) {
	// Empty denylist = everything allowed
	p := NewInterfacePolicy("denylist", []string{})
	if !p.IsWriteAllowed("eth0") {
		t.Error("empty denylist should allow everything")
	}
}

func TestIsWriteAllowed_MalformedPatternDenylist(t *testing.T) {
	// Malformed glob pattern in denylist should fail closed (deny)
	p := NewInterfacePolicy("denylist", []string{"[invalid", "eth0"})
	if p.IsWriteAllowed("anything") {
		t.Error("malformed denylist pattern should deny (fail closed)")
	}
}

func TestIsWriteAllowed_MalformedPatternAllowlist(t *testing.T) {
	// Malformed glob pattern in allowlist should fail closed (not match)
	p := NewInterfacePolicy("allowlist", []string{"[invalid"})
	if p.IsWriteAllowed("anything") {
		t.Error("malformed allowlist pattern should not grant access")
	}
}

func TestPolicy_Set_UpdatesAtomically(t *testing.T) {
	p := NewInterfacePolicy("denylist", []string{"lo"})
	if p.IsWriteAllowed("lo") {
		t.Fatal("lo should be denied initially")
	}
	p.Set("allowlist", []string{"eth0"})
	if p.IsWriteAllowed("lo") {
		t.Error("lo should still be denied after switch to allowlist")
	}
	if !p.IsWriteAllowed("eth0") {
		t.Error("eth0 should be allowed in allowlist")
	}
}

func TestInterfaceDeniedError(t *testing.T) {
	err := &InterfaceDeniedError{Name: "eth0p"}
	if !errors.Is(err, errInterfaceDenied) {
		t.Error("InterfaceDeniedError should unwrap to errInterfaceDenied")
	}
	want := "interface 'eth0p' is not allowed for write operations"
	if err.Error() != want {
		t.Errorf("got %q, want %q", err.Error(), want)
	}
}

func TestService_CheckWritePolicy_Denied(t *testing.T) {
	policy := NewInterfacePolicy("denylist", []string{"lo", "eth0p"})
	svc := &Service{policy: policy}
	err := svc.checkWritePolicy("eth0p")
	if err == nil {
		t.Fatal("expected error for denied interface")
	}
	var denied *InterfaceDeniedError
	if !errors.As(err, &denied) {
		t.Fatalf("expected InterfaceDeniedError, got %T", err)
	}
	if denied.Name != "eth0p" {
		t.Errorf("denied.Name = %q, want %q", denied.Name, "eth0p")
	}
}

func TestService_CheckWritePolicy_Allowed(t *testing.T) {
	policy := NewInterfacePolicy("denylist", []string{"lo"})
	svc := &Service{policy: policy}
	if err := svc.checkWritePolicy("eth0"); err != nil {
		t.Errorf("eth0 should be allowed, got %v", err)
	}
}

func TestConfigure_NilConfig(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(nil)
	// Default denylist should still be active
	if p.policy.IsWriteAllowed("lo") {
		t.Error("lo should be denied with default denylist after nil Configure")
	}
}

func TestConfigure_DefaultDenylist(t *testing.T) {
	p := NewNetworkPlugin()
	// No explicit Configure call — default denylist active
	for _, name := range defaultDenylist {
		if p.policy.IsWriteAllowed(name) {
			t.Errorf("%q should be denied by default", name)
		}
	}
	if !p.policy.IsWriteAllowed("eth0") {
		t.Error("eth0 should be allowed by default")
	}
}

func TestConfigure_CustomPolicy(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": "allowlist",
			"list": []any{"eth0", "wlan0"},
		},
	})
	if !p.policy.IsWriteAllowed("eth0") {
		t.Error("eth0 should be allowed in custom allowlist")
	}
	if p.policy.IsWriteAllowed("br0") {
		t.Error("br0 should be denied in custom allowlist")
	}
}

func TestConfigure_InvalidPolicyType(t *testing.T) {
	p := NewNetworkPlugin()
	// Should warn and not panic
	p.Configure(map[string]any{
		"interface_policy": "not a map",
	})
	// Default denylist should remain active
	if p.policy.IsWriteAllowed("lo") {
		t.Error("lo should still be denied after invalid config")
	}
}

func TestUpdateConfig_Mode(t *testing.T) {
	p := NewNetworkPlugin()
	if err := p.UpdateConfig("interface_policy.mode", "allowlist"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.policy.Mode() != "allowlist" {
		t.Errorf("mode = %q, want %q", p.policy.Mode(), "allowlist")
	}
}

func TestUpdateConfig_List(t *testing.T) {
	p := NewNetworkPlugin()
	newList := []any{"eth0", "wlan0"}
	if err := p.UpdateConfig("interface_policy.list", newList); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	patterns := p.policy.Patterns()
	if len(patterns) != 2 || patterns[0] != "eth0" || patterns[1] != "wlan0" {
		t.Errorf("patterns = %v, want [eth0 wlan0]", patterns)
	}
}

func TestUpdateConfig_InvalidKey(t *testing.T) {
	p := NewNetworkPlugin()
	if err := p.UpdateConfig("unknown_key", "value"); err == nil {
		t.Error("expected error for unknown key")
	}
}

func TestUpdateConfig_InvalidModeValue(t *testing.T) {
	p := NewNetworkPlugin()
	if err := p.UpdateConfig("interface_policy.mode", "bogus"); err == nil {
		t.Error("expected error for invalid mode")
	}
}

func TestUpdateConfig_InvalidListType(t *testing.T) {
	p := NewNetworkPlugin()
	if err := p.UpdateConfig("interface_policy.list", "not a list"); err == nil {
		t.Error("expected error for non-list value")
	}
}

func TestConfigure_MissingModeDefaultsToDenylist(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"list": []any{"eth0", "docker*"},
		},
	})
	if p.policy.Mode() != "denylist" {
		t.Errorf("mode = %q, want denylist when mode key missing", p.policy.Mode())
	}
	if p.policy.IsWriteAllowed("eth0") {
		t.Error("eth0 should be denied by custom denylist")
	}
	if !p.policy.IsWriteAllowed("wlan0") {
		t.Error("wlan0 should be allowed by custom denylist")
	}
}

func TestConfigure_InvalidModeKeepsDefaults(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": "deny", // typo
			"list": []any{"eth0"},
		},
	})
	// Should keep default denylist, not disable policy
	if p.policy.Mode() != "denylist" {
		t.Errorf("mode = %q, want denylist after invalid mode", p.policy.Mode())
	}
	if p.policy.IsWriteAllowed("lo") {
		t.Error("lo should still be denied after invalid mode Configure")
	}
}

func TestConfigure_NonStringModeKeepsDefaults(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": 42, // not a string
			"list": []any{"eth0"},
		},
	})
	if p.policy.Mode() != "denylist" {
		t.Errorf("mode = %q, want denylist after non-string mode", p.policy.Mode())
	}
	if p.policy.IsWriteAllowed("lo") {
		t.Error("lo should still be denied after non-string mode Configure")
	}
}

func TestConfigure_EmptyListClearsDenylist(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": "denylist",
			"list": []any{}, // explicit empty
		},
	})
	// Empty denylist = everything allowed, NOT restored to defaults
	if !p.policy.IsWriteAllowed("lo") {
		t.Error("lo should be allowed with explicit empty denylist")
	}
	if len(p.policy.Patterns()) != 0 {
		t.Errorf("patterns = %v, want empty", p.policy.Patterns())
	}
}

func TestUpdateConfig_EmptyListClearsDenylist(t *testing.T) {
	p := NewNetworkPlugin()
	if err := p.UpdateConfig("interface_policy.list", []any{}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Empty list = everything allowed, NOT restored to defaults
	if !p.policy.IsWriteAllowed("lo") {
		t.Error("lo should be allowed with explicit empty denylist")
	}
	if len(p.policy.Patterns()) != 0 {
		t.Errorf("patterns = %v, want empty", p.policy.Patterns())
	}
}

func TestConfigure_ExplicitEmptyModeDisablesPolicy(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": "",
		},
	})
	// Explicit empty mode = no policy = all allowed
	if p.policy.Mode() != "" {
		t.Errorf("mode = %q, want empty string", p.policy.Mode())
	}
	if !p.policy.IsWriteAllowed("lo") {
		t.Error("lo should be allowed when policy explicitly disabled")
	}
}

func TestConfigure_NonStringListItemKeepsDefaults(t *testing.T) {
	p := NewNetworkPlugin()
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": "denylist",
			"list": []any{"eth0", 42}, // non-string item
		},
	})
	// Should keep default denylist, not partially apply
	if p.policy.IsWriteAllowed("lo") {
		t.Error("lo should still be denied after non-string list item")
	}
	if len(p.policy.Patterns()) != len(defaultDenylist) {
		t.Errorf("patterns = %v, want default denylist", p.policy.Patterns())
	}
}

func TestZeroValuePlugin_ConfigureNil(t *testing.T) {
	p := &NetworkPlugin{}
	p.Configure(nil) // should not panic, should init policy
	if p.policy == nil {
		t.Fatal("policy should be initialized after Configure(nil)")
	}
	if p.policy.Mode() != "denylist" {
		t.Errorf("mode = %q, want denylist", p.policy.Mode())
	}
}

func TestZeroValuePlugin_UpdateConfig(t *testing.T) {
	p := &NetworkPlugin{}
	// Should not panic — ensurePolicy initializes
	if err := p.UpdateConfig("interface_policy.mode", "allowlist"); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p.policy.Mode() != "allowlist" {
		t.Errorf("mode = %q, want allowlist", p.policy.Mode())
	}
}

func TestZeroValuePlugin_CurrentConfig(t *testing.T) {
	p := &NetworkPlugin{}
	cfg := p.CurrentConfig() // should not panic
	policy, ok := cfg["interface_policy"].(map[string]any)
	if !ok {
		t.Fatal("expected interface_policy map")
	}
	if policy["mode"] != "denylist" {
		t.Errorf("mode = %v, want denylist", policy["mode"])
	}
}

func TestZeroValuePlugin_RoutesBeforeConfigure_PolicyPropagates(t *testing.T) {
	// Regression: Routes() called before Configure() on zero-value plugin
	// must still enforce policy after Configure() wires it.
	p := &NetworkPlugin{}
	_ = p.Routes() // triggers svcOnce with nil policy
	p.Configure(map[string]any{
		"interface_policy": map[string]any{
			"mode": "denylist",
			"list": []any{"lo", "eth0"},
		},
	})
	// Service should now have the policy propagated
	if p.svc.policy == nil {
		t.Fatal("svc.policy should be non-nil after Configure")
	}
	err := p.svc.checkWritePolicy("lo")
	if err == nil {
		t.Error("lo should be denied after Configure propagates policy to service")
	}
	err = p.svc.checkWritePolicy("wlan0")
	if err != nil {
		t.Errorf("wlan0 should be allowed, got %v", err)
	}
}

func TestCurrentConfig_Snapshot(t *testing.T) {
	p := NewNetworkPlugin()
	cfg := p.CurrentConfig()
	policy, ok := cfg["interface_policy"].(map[string]any)
	if !ok {
		t.Fatal("expected interface_policy map in CurrentConfig")
	}
	if policy["mode"] != "denylist" {
		t.Errorf("mode = %v, want denylist", policy["mode"])
	}
	list, ok := policy["list"].([]string)
	if !ok {
		t.Fatal("expected list to be []string")
	}
	if len(list) != len(defaultDenylist) {
		t.Errorf("list length = %d, want %d", len(list), len(defaultDenylist))
	}
}
