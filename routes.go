package network

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"regexp"

	"github.com/go-chi/chi/v5"
)

// maxBodySize limits JSON request bodies to 1 MB.
const maxBodySize = 1 << 20

// validName matches safe interface names (alphanumeric, hyphens, underscores).
var validName = regexp.MustCompile(`^[a-zA-Z0-9_-]+$`)

func newRouter() http.Handler {
	r := chi.NewRouter()

	r.Get("/interfaces", handleListInterfaces)
	r.Get("/interfaces/{name}", handleGetInterface)
	r.Put("/interfaces/{name}", handleSetStaticIP)
	r.Get("/dns", handleGetDNS)
	r.Put("/dns", handleSetDNS)
	r.Get("/status", handleGetNetworkStatus)

	return r
}

func handleListInterfaces(w http.ResponseWriter, r *http.Request) {
	ifaces, err := ListInterfaces()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ifaces)
}

func handleGetInterface(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !validName.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid interface name")
		return
	}
	iface, err := GetInterface(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func handleSetStaticIP(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !validName.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid interface name")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req StaticIPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	iface, err := SetStaticIP(name, req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func handleGetDNS(w http.ResponseWriter, r *http.Request) {
	dns, err := GetDNS()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dns)
}

func handleSetDNS(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req DNSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	dns, err := SetDNS(req)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dns)
}

func handleGetNetworkStatus(w http.ResponseWriter, r *http.Request) {
	status, err := GetNetworkStatus()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, status)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("failed to write JSON response", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
		},
	}); err != nil {
		slog.Error("failed to write error response", "error", err)
	}
}
