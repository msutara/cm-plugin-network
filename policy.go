package network

import (
	"fmt"
	"path/filepath"
	"sync"
)

// defaultDenylist contains kernel-created interfaces that should never be modified.
var defaultDenylist = []string{"lo", "gre0", "gretap0", "sit0", "ip6tnl0"}

// InterfacePolicy controls which interfaces are writable.
type InterfacePolicy struct {
	mu       sync.RWMutex
	mode     string   // "denylist", "allowlist", or "" (no policy)
	patterns []string // glob patterns (filepath.Match semantics)
}

// NewInterfacePolicy creates a policy from mode and pattern list.
// If mode is "denylist" and patterns is nil, defaultDenylist is used.
func NewInterfacePolicy(mode string, patterns []string) *InterfacePolicy {
	p := &InterfacePolicy{}
	p.Set(mode, patterns)
	return p
}

// Set updates the policy. Thread-safe.
func (p *InterfacePolicy) Set(mode string, patterns []string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	switch mode {
	case "denylist":
		p.mode = "denylist"
		if patterns == nil {
			p.patterns = append([]string(nil), defaultDenylist...)
		} else {
			p.patterns = append([]string(nil), patterns...)
		}
	case "allowlist":
		p.mode = "allowlist"
		p.patterns = append([]string(nil), patterns...)
	default:
		p.mode = ""
		p.patterns = nil
	}
}

// Mode returns the current policy mode.
func (p *InterfacePolicy) Mode() string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.mode
}

// Patterns returns a copy of the current patterns.
func (p *InterfacePolicy) Patterns() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return append([]string(nil), p.patterns...)
}

// IsWriteAllowed returns true if the given interface name is permitted for write operations.
func (p *InterfacePolicy) IsWriteAllowed(name string) bool {
	p.mu.RLock()
	defer p.mu.RUnlock()

	switch p.mode {
	case "denylist":
		for _, pat := range p.patterns {
			matched, err := filepath.Match(pat, name)
			if err != nil {
				return false // malformed pattern → fail closed (deny)
			}
			if matched {
				return false
			}
		}
		return true
	case "allowlist":
		for _, pat := range p.patterns {
			matched, err := filepath.Match(pat, name)
			if err != nil {
				continue // skip malformed patterns
			}
			if matched {
				return true
			}
		}
		return false
	default:
		return true // no policy = all allowed
	}
}

// errInterfaceDenied is returned when a write operation targets a policy-denied interface.
var errInterfaceDenied = fmt.Errorf("interface write denied by policy")

// InterfaceDeniedError wraps errInterfaceDenied with the specific interface name.
type InterfaceDeniedError struct {
	Name string
}

func (e *InterfaceDeniedError) Error() string {
	return fmt.Sprintf("interface '%s' is not allowed for write operations", e.Name)
}

func (e *InterfaceDeniedError) Unwrap() error {
	return errInterfaceDenied
}
