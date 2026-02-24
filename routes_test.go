package network

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func testRouter() http.Handler {
	return newRouter(NewService())
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
}

func TestHandleSetStaticIP_InvalidName(t *testing.T) {
	body := `{"ip": "10.0.0.1/24", "gateway": "10.0.0.254"}`
	w := httptest.NewRecorder()
	r := httptest.NewRequest(http.MethodPut, "/interfaces/bad$name", bytes.NewBufferString(body))
	testRouter().ServeHTTP(w, r)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("got %d, want %d", w.Code, http.StatusBadRequest)
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
	testRouter().ServeHTTP(w, r)

	// May return OK or 500 depending on system resolv.conf access
	if w.Code != http.StatusOK && w.Code != http.StatusInternalServerError {
		t.Fatalf("got %d, want 200 or 500", w.Code)
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
}
