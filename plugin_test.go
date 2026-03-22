package network

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/msutara/config-manager-core/plugin"
)

var _ plugin.Plugin = (*NetworkPlugin)(nil)

func TestNewNetworkPlugin(t *testing.T) {
	p := NewNetworkPlugin()
	if p == nil {
		t.Fatal("NewNetworkPlugin returned nil")
	}
	if p.svc == nil {
		t.Fatal("NewNetworkPlugin().svc is nil")
	}
}

func TestNetworkPlugin_Metadata(t *testing.T) {
	p := NewNetworkPlugin()

	if got := p.Name(); got != "network" {
		t.Errorf("Name: got %q, want %q", got, "network")
	}
	if got := p.Version(); got == "" {
		t.Error("Version: got empty string")
	}
	if got := p.Description(); got == "" {
		t.Error("Description: got empty string")
	}
}

func TestNetworkPlugin_Routes(t *testing.T) {
	p := NewNetworkPlugin()
	h := p.Routes()
	if h == nil {
		t.Fatal("Routes returned nil handler")
	}

	// Smoke test: /interfaces should respond (uses net.Interfaces, cross-platform)
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/interfaces", nil)
	h.ServeHTTP(w, r)
	if w.Code != http.StatusOK {
		t.Errorf("/interfaces: got %d, want %d", w.Code, http.StatusOK)
	}

	// Unknown route should not be 200
	w = httptest.NewRecorder()
	r = httptest.NewRequest(http.MethodGet, "/nonexistent", nil)
	h.ServeHTTP(w, r)
	if w.Code == http.StatusOK {
		t.Error("/nonexistent: expected non-200")
	}
}

func TestNetworkPlugin_Endpoints(t *testing.T) {
	p := NewNetworkPlugin()
	eps := p.Endpoints()

	if len(eps) != 9 {
		t.Fatalf("Endpoints: got %d, want 9", len(eps))
	}

	want := []struct{ method, path string }{
		{http.MethodGet, "/interfaces"},
		{http.MethodGet, "/interfaces/{name}"},
		{http.MethodPut, "/interfaces/{name}"},
		{http.MethodDelete, "/interfaces/{name}"},
		{http.MethodPost, "/interfaces/{name}/rollback"},
		{http.MethodGet, "/dns"},
		{http.MethodPut, "/dns"},
		{http.MethodPost, "/dns/rollback"},
		{http.MethodGet, "/status"},
	}
	for i, w := range want {
		if eps[i].Method != w.method || eps[i].Path != w.path {
			t.Errorf("endpoint[%d] = %s %s, want %s %s", i, eps[i].Method, eps[i].Path, w.method, w.path)
		}
		if eps[i].Description == "" {
			t.Errorf("endpoint[%d] has empty description", i)
		}
	}
}

func TestNetworkPlugin_ScheduledJobs(t *testing.T) {
	p := NewNetworkPlugin()
	jobs := p.ScheduledJobs()
	if jobs != nil {
		t.Errorf("expected nil scheduled jobs, got %d", len(jobs))
	}
}
