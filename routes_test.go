package network

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testRouter() http.Handler {
	return newRouter(NewService())
}

func testRouterWithResolvPath(t *testing.T) http.Handler {
	t.Helper()
	tmp := t.TempDir()
	resolv := filepath.Join(tmp, "resolv.conf")
	if err := os.WriteFile(resolv, []byte("nameserver 8.8.8.8\nsearch example.com\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svc := &Service{resolvPath: resolv, interfacesDirPath: "/etc/network/interfaces.d"}
	return newRouter(svc)
}

func TestHandleListInterfaces(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/interfaces", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", w.Code, http.StatusOK)
	}
	ct := w.Header().Get("Content-Type")
	if ct != "application/json" {
		t.Fatalf("Content-Type: got %q, want application/json", ct)
	}
}

func TestHandleGetInterface_NotFound(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/interfaces/nonexistent99", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestHandleGetInterface_InvalidName(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/interfaces/bad$name", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	errObj := resp["error"].(map[string]any)
	msg := errObj["message"].(string)
	if !strings.Contains(msg, "invalid interface name") {
		t.Errorf("error message should mention 'invalid interface name', got %q", msg)
	}
}

func TestHandleSetStaticIP_InvalidName(t *testing.T) {
	body := `{"ip": "10.0.0.1/24", "gateway": "10.0.0.254"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/bad$name", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	errObj := resp["error"].(map[string]any)
	msg := errObj["message"].(string)
	if !strings.Contains(msg, "invalid interface name") {
		t.Errorf("error message should mention 'invalid interface name', got %q", msg)
	}
}

func TestHandleSetStaticIP_InvalidJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString("not json"))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetStaticIP_BadCIDR(t *testing.T) {
	body := `{"ip": "10.0.0.1", "gateway": "10.0.0.254"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetStaticIP_BodyTooLarge(t *testing.T) {
	large := `{"ip": "` + strings.Repeat("x", maxBodySize+1) + `"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(large))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestHandleGetNetworkStatus(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/status", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d", w.Code, http.StatusOK)
	}

	var status NetworkStatus
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
}

func TestHandleGetDNS(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodGet, "/dns", nil)
	testRouterWithResolvPath(t).ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want 200", w.Code)
	}
	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	ns, ok := resp["nameservers"].([]any)
	if !ok || len(ns) != 1 || ns[0] != "8.8.8.8" {
		t.Fatalf("unexpected nameservers: %v", resp["nameservers"])
	}
}

func TestHandleSetDNS_InvalidJSON(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dns", bytes.NewBufferString("bad"))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetDNS_BodyTooLarge(t *testing.T) {
	large := `{"nameservers": ["` + strings.Repeat("x", maxBodySize+1) + `"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dns", bytes.NewBufferString(large))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("got %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestWriteError(t *testing.T) {
	w := httptest.NewRecorder()
	writeError(w, http.StatusBadRequest, "test message")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	errObj, ok := resp["error"].(map[string]any)
	if !ok {
		t.Fatal("missing error object")
	}
	if errObj["message"] != "test message" {
		t.Errorf("message: got %v, want %q", errObj["message"], "test message")
	}
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatalf("details field has wrong type: %T", errObj["details"])
	}
	if len(details) != 0 {
		t.Errorf("details: got %v, want empty map", details)
	}
}

func TestHandleSetStaticIP_GWNotInSubnet(t *testing.T) {
	body := `{"ip": "192.168.1.10/24", "gateway": "10.0.0.1"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	errObj := resp["error"].(map[string]any)
	msg := errObj["message"].(string)
	if !strings.Contains(msg, "subnet") {
		t.Errorf("error message should mention subnet, got %q", msg)
	}
}

func TestHandleSetStaticIP_GWEqualsIP(t *testing.T) {
	body := `{"ip": "192.168.1.10/24", "gateway": "192.168.1.10"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleGetInterface_PathTraversal(t *testing.T) {
	traversals := []string{
		"/interfaces/..%2F..%2Fetc%2Fpasswd",
		"/interfaces/..%5C..%5Cetc%5Cshadow",
	}
	for _, path := range traversals {
		w := httptest.NewRecorder()
		r := httptest.NewRequest(http.MethodGet, path, nil)
		testRouter().ServeHTTP(w, r)

		if w.Code == http.StatusOK {
			t.Errorf("path %q should not return 200", path)
		}
	}
}

func TestHandleSetDNS_InvalidNameserver(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-only test")
	}
	svc := &Service{
		resolvPath:        filepath.Join(t.TempDir(), "resolv.conf"),
		interfacesDirPath: "/etc/network/interfaces.d",
	}
	router := newRouter(svc)

	body := `{"nameservers": ["not-an-ip", "8.8.8.8"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dns", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetDNS_InvalidSearchDomain(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-only test")
	}
	svc := &Service{
		resolvPath:        filepath.Join(t.TempDir(), "resolv.conf"),
		interfacesDirPath: "/etc/network/interfaces.d",
	}
	router := newRouter(svc)

	body := `{"nameservers": ["8.8.8.8"], "search": ["valid.com", "inval!d"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dns", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetStaticIP_EmptyFields(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"empty_ip", `{"ip": "", "gateway": "192.168.1.1"}`},
		{"empty_gw", `{"ip": "192.168.1.10/24", "gateway": ""}`},
		{"missing_fields", `{}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(tc.body))
			testRouter().ServeHTTP(w, r)

			if w.Code != http.StatusBadRequest {
				t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}

func TestHandleSetStaticIP_IPv6Rejected(t *testing.T) {
	body := `{"ip": "fe80::1/64", "gateway": "fe80::1"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleSetStaticIP_IPv4MappedIPv6Gateway(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-only test")
	}
	// Use temp dirs to avoid writing to real system paths.
	dir := t.TempDir()
	svc := &Service{
		resolvPath:        filepath.Join(dir, "resolv.conf"),
		interfacesDirPath: dir,
		cmdTimeout:        5 * time.Second,
		ifdownPath:        "/bin/true",
		ifupPath:          "/bin/true",
	}
	router := newRouter(svc)

	// Use loopback — guaranteed to exist on all Linux systems.
	body := `{"ip": "192.168.1.10/24", "gateway": "::ffff:192.168.1.1"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/lo", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	// With /bin/true stubs, config file is always written.
	confPath := filepath.Join(dir, "lo")
	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatalf("config file not written: %v", err)
	}
	if strings.Contains(string(got), "::ffff") {
		t.Error("config file contains un-canonicalized IPv4-mapped IPv6 gateway")
	}
	if !strings.Contains(string(got), "gateway 192.168.1.1") {
		t.Errorf("config file missing canonicalized gateway, got: %s", got)
	}
	// Unit tests in service_test.go directly assert canonicalization;
	// this route test verifies the full HTTP path doesn't bypass it.
}
