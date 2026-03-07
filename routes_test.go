package network

import (
	"bytes"
	"encoding/json"
	"errors"
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

// extractErrorMessage extracts the error message string from a JSON error
// response body. Fails the test with a clear message if the shape is unexpected.
func extractErrorMessage(t *testing.T, body []byte) string {
	t.Helper()
	var resp map[string]any
	if err := json.Unmarshal(body, &resp); err != nil {
		t.Fatalf("invalid JSON response: %v", err)
	}
	rawErr, ok := resp["error"]
	if !ok {
		t.Fatal("missing 'error' field in response")
	}
	errObj, ok := rawErr.(map[string]any)
	if !ok {
		t.Fatalf("'error' field has wrong type: %T", rawErr)
	}
	rawMsg, ok := errObj["message"]
	if !ok {
		t.Fatal("missing 'message' field in error object")
	}
	msg, ok := rawMsg.(string)
	if !ok {
		t.Fatalf("'message' field has wrong type: %T", rawMsg)
	}
	return msg
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
	msg := extractErrorMessage(t, w.Body.Bytes())
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
	msg := extractErrorMessage(t, w.Body.Bytes())
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
	r.Header.Set("X-Confirm", "true")
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
	r.Header.Set("X-Confirm", "true")
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
	msg := extractErrorMessage(t, w.Body.Bytes())
	if !strings.Contains(msg, "subnet") {
		t.Errorf("error message should mention subnet, got %q", msg)
	}
}

func TestHandleSetStaticIP_GWEqualsIP(t *testing.T) {
	body := `{"ip": "192.168.1.10/24", "gateway": "192.168.1.10"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(body))
	r.Header.Set("X-Confirm", "true")
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
	r.Header.Set("X-Confirm", "true")
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
	r.Header.Set("X-Confirm", "true")
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
			r.Header.Set("X-Confirm", "true")
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
	r.Header.Set("X-Confirm", "true")
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
	r.Header.Set("X-Confirm", "true")
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

func TestPutInterface_DryRun(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "eth0")
	oldConfig := "auto eth0\niface eth0 inet static\n    address 10.0.0.1\n    netmask 255.255.255.0\n    gateway 10.0.0.254\n"
	if err := os.WriteFile(confPath, []byte(oldConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	body := `{"ip": "192.168.1.10/24", "gateway": "192.168.1.1"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0?dry_run=true", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
	if result["proposed_config"] == nil || result["proposed_config"] == "" {
		t.Error("expected non-empty proposed_config")
	}
	if result["current_config"] == nil || result["current_config"] == "" {
		t.Error("expected non-empty current_config")
	}
	changes, ok := result["changes"].([]any)
	if !ok || len(changes) == 0 {
		t.Errorf("expected non-empty changes, got %v", result["changes"])
	}

	// Verify file was NOT modified
	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != oldConfig {
		t.Errorf("dry-run should not modify file: got %q, want %q", string(got), oldConfig)
	}
}

func TestPutInterface_NoConfirmHeader(t *testing.T) {
	body := `{"ip": "192.168.1.10/24", "gateway": "192.168.1.1"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/eth0", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("got %d, want %d", w.Code, http.StatusPreconditionRequired)
	}

	msg := extractErrorMessage(t, w.Body.Bytes())
	if !strings.Contains(msg, "confirmation required") {
		t.Errorf("expected error containing 'confirmation required', got %q", msg)
	}

	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	errObj := resp["error"].(map[string]any)
	details, ok := errObj["details"].(map[string]any)
	if !ok {
		t.Fatal("expected details in error envelope")
	}
	if details["dry_run_hint"] == nil {
		t.Error("expected dry_run_hint in details")
	}
}

func TestPutInterface_WithConfirmHeader(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("skipping Linux-only test")
	}
	dir := t.TempDir()
	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
		cmdTimeout:        5 * time.Second,
		ifdownPath:        "/bin/true",
		ifupPath:          "/bin/true",
	}
	router := newRouter(svc)

	body := `{"ip": "192.168.1.10/24", "gateway": "192.168.1.1"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/lo", bytes.NewBufferString(body))
	r.Header.Set("X-Confirm", "true")
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}
}

func TestPutDNS_DryRun(t *testing.T) {
	dir := t.TempDir()
	resolvPath := filepath.Join(dir, "resolv.conf")
	oldContent := "nameserver 9.9.9.9\n"
	if err := os.WriteFile(resolvPath, []byte(oldContent), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		resolvPath:        resolvPath,
		interfacesDirPath: dir,
	}
	router := newRouter(svc)

	body := `{"nameservers": ["8.8.8.8", "1.1.1.1"], "search": ["example.com"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dns?dry_run=true", bytes.NewBufferString(body))
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
	if result["proposed_config"] == nil || result["proposed_config"] == "" {
		t.Error("expected non-empty proposed_config")
	}

	// Verify file was NOT modified
	got, err := os.ReadFile(resolvPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != oldContent {
		t.Errorf("dry-run should not modify file: got %q, want %q", string(got), oldContent)
	}
}

func TestPutDNS_NoConfirmHeader(t *testing.T) {
	body := `{"nameservers": ["8.8.8.8"]}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/dns", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("got %d, want %d", w.Code, http.StatusPreconditionRequired)
	}

	msg := extractErrorMessage(t, w.Body.Bytes())
	if !strings.Contains(msg, "confirmation required") {
		t.Errorf("expected error containing 'confirmation required', got %q", msg)
	}
}

func TestDeleteInterface_DryRun(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "eth0")
	oldConfig := "auto eth0\niface eth0 inet static\n    address 10.0.0.1/24\n    gateway 10.0.0.254\n"
	if err := os.WriteFile(confPath, []byte(oldConfig), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/interfaces/eth0?dry_run=true", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
	if result["current_config"] == nil || result["current_config"] == "" {
		t.Error("expected non-empty current_config")
	}
	changes, ok := result["changes"].([]any)
	if !ok || len(changes) == 0 {
		t.Errorf("expected non-empty changes, got %v", result["changes"])
	}

	// Verify file was NOT removed.
	if _, err := os.Stat(confPath); err != nil {
		t.Errorf("dry-run should not remove file: %v", err)
	}
}

func TestDeleteInterface_NoConfirmHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/interfaces/eth0", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("got %d, want %d", w.Code, http.StatusPreconditionRequired)
	}

	msg := extractErrorMessage(t, w.Body.Bytes())
	if !strings.Contains(msg, "confirmation required") {
		t.Errorf("expected error containing 'confirmation required', got %q", msg)
	}
}

func TestDeleteInterface_NoStaticConfig(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/interfaces/eth0?dry_run=true", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestDeleteInterface_InvalidName(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/interfaces/bad%20name", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRollbackInterface_DryRun(t *testing.T) {
	dir := t.TempDir()
	confPath := filepath.Join(dir, "eth0")
	bakPath := confPath + ".bak"

	current := "auto eth0\niface eth0 inet static\n    address 192.168.1.10/24\n    gateway 192.168.1.1\n"
	backup := "auto eth0\niface eth0 inet static\n    address 10.0.0.1/24\n    gateway 10.0.0.254\n"
	if err := os.WriteFile(confPath, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bakPath, []byte(backup), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/interfaces/eth0/rollback?dry_run=true", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
	changes, ok := result["changes"].([]any)
	if !ok || len(changes) == 0 {
		t.Errorf("expected non-empty changes, got %v", result["changes"])
	}
	got, err := os.ReadFile(confPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != current {
		t.Errorf("dry-run should not modify file")
	}
}

func TestRollbackInterface_NoConfirmHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/interfaces/eth0/rollback", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("got %d, want %d", w.Code, http.StatusPreconditionRequired)
	}

	msg := extractErrorMessage(t, w.Body.Bytes())
	if !strings.Contains(msg, "confirmation required") {
		t.Errorf("expected error containing 'confirmation required', got %q", msg)
	}
}

func TestRollbackInterface_NoBackup(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/interfaces/eth0/rollback?dry_run=true", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

func TestRollbackInterface_InvalidName(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/interfaces/bad%20name/rollback", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestRollbackDNS_DryRun(t *testing.T) {
	dir := t.TempDir()
	resolvPath := filepath.Join(dir, "resolv.conf")
	bakPath := resolvPath + ".bak"

	current := "nameserver 8.8.8.8\n"
	backup := "nameserver 1.1.1.1\n"
	if err := os.WriteFile(resolvPath, []byte(current), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(bakPath, []byte(backup), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        resolvPath,
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dns/rollback?dry_run=true", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusOK {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var result map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if result["valid"] != true {
		t.Errorf("expected valid=true, got %v", result["valid"])
	}
	changes, ok := result["changes"].([]any)
	if !ok || len(changes) == 0 {
		t.Errorf("expected non-empty changes, got %v", result["changes"])
	}
}

func TestRollbackDNS_NoConfirmHeader(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dns/rollback", nil)
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusPreconditionRequired {
		t.Fatalf("got %d, want %d", w.Code, http.StatusPreconditionRequired)
	}

	msg := extractErrorMessage(t, w.Body.Bytes())
	if !strings.Contains(msg, "confirmation required") {
		t.Errorf("expected error containing 'confirmation required', got %q", msg)
	}
}

func TestRollbackDNS_NoBackup(t *testing.T) {
	dir := t.TempDir()
	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dns/rollback?dry_run=true", nil)
	router.ServeHTTP(w, r)

	if w.Code != http.StatusNotFound {
		t.Fatalf("got %d, want %d; body: %s", w.Code, http.StatusNotFound, w.Body.String())
	}
}

// --- writeDeleteError / writeRollbackError tests ---

func TestWriteDeleteError_InvalidName(t *testing.T) {
	w := httptest.NewRecorder()
	writeDeleteError(w, errInvalidIfaceName)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWriteDeleteError_NotFound(t *testing.T) {
	w := httptest.NewRecorder()
	writeDeleteError(w, errIfaceNotFound)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestWriteDeleteError_NoStaticConfig(t *testing.T) {
	w := httptest.NewRecorder()
	writeDeleteError(w, errNoStaticConfig)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestWriteDeleteError_NotLinux(t *testing.T) {
	w := httptest.NewRecorder()
	writeDeleteError(w, errNotLinux)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestWriteDeleteError_InternalGeneric(t *testing.T) {
	w := httptest.NewRecorder()
	writeDeleteError(w, errors.New("restore read: /etc/network/interfaces.d/eth0.bak: permission denied"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	body := w.Body.String()
	if strings.Contains(body, "/etc") {
		t.Errorf("500 response leaks internal path: %s", body)
	}
	if !strings.Contains(body, "internal error during delete operation") {
		t.Errorf("expected generic message, got: %s", body)
	}
}

func TestWriteRollbackError_InvalidName(t *testing.T) {
	w := httptest.NewRecorder()
	writeRollbackError(w, errInvalidIfaceName)
	if w.Code != http.StatusBadRequest {
		t.Errorf("got %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestWriteRollbackError_NoBackup(t *testing.T) {
	w := httptest.NewRecorder()
	writeRollbackError(w, errNoBackup)
	if w.Code != http.StatusNotFound {
		t.Errorf("got %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestWriteRollbackError_NotLinux(t *testing.T) {
	w := httptest.NewRecorder()
	writeRollbackError(w, errNotLinux)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

func TestWriteRollbackError_InternalGeneric(t *testing.T) {
	w := httptest.NewRecorder()
	writeRollbackError(w, errors.New("restore read: /var/tmp/resolv.conf.bak: not a regular file"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	body := w.Body.String()
	if strings.Contains(body, "/var") {
		t.Errorf("500 response leaks internal path: %s", body)
	}
	if !strings.Contains(body, "internal error during rollback operation") {
		t.Errorf("expected generic message, got: %s", body)
	}
}

// --- Confirmed handler 503 tests (non-Linux) ---

func TestDeleteInterface_Confirmed_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("test requires non-Linux OS")
	}
	dir := t.TempDir()
	svc := &Service{interfacesDirPath: dir}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodDelete, "/interfaces/eth0", nil)
	r.Header.Set("X-Confirm", "true")
	router.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want %d; body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

func TestRollbackInterface_Confirmed_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("test requires non-Linux OS")
	}
	dir := t.TempDir()
	svc := &Service{interfacesDirPath: dir}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/interfaces/eth0/rollback", nil)
	r.Header.Set("X-Confirm", "true")
	router.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want %d; body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

func TestRollbackDNS_Confirmed_NonLinux(t *testing.T) {
	if runtime.GOOS == "linux" {
		t.Skip("test requires non-Linux OS")
	}
	dir := t.TempDir()
	svc := &Service{
		interfacesDirPath: dir,
		resolvPath:        filepath.Join(dir, "resolv.conf"),
	}
	router := newRouter(svc)

	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPost, "/dns/rollback", nil)
	r.Header.Set("X-Confirm", "true")
	router.ServeHTTP(w, r)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("got %d, want %d; body: %s", w.Code, http.StatusServiceUnavailable, w.Body.String())
	}
}

// --- Pre-existing error mapper 500 tests ---

func TestWriteStaticIPError_InternalGeneric(t *testing.T) {
	w := httptest.NewRecorder()
	writeStaticIPError(w, errors.New("ifup eth0 failed: exit status 1: /etc/network/run/ifstate"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	body := w.Body.String()
	if strings.Contains(body, "/etc") {
		t.Errorf("500 response leaks internal path: %s", body)
	}
	if !strings.Contains(body, "internal error during configuration") {
		t.Errorf("expected generic message, got: %s", body)
	}
}

func TestWriteDNSError_InternalGeneric(t *testing.T) {
	w := httptest.NewRecorder()
	writeDNSError(w, errors.New("write resolv.conf: /etc/resolv.conf: permission denied"))
	if w.Code != http.StatusInternalServerError {
		t.Errorf("got %d, want %d", w.Code, http.StatusInternalServerError)
	}
	body := w.Body.String()
	if strings.Contains(body, "/etc") {
		t.Errorf("500 response leaks internal path: %s", body)
	}
	if !strings.Contains(body, "internal error during DNS configuration") {
		t.Errorf("expected generic message, got: %s", body)
	}
}
