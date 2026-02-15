package network

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
)

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
	iface, err := GetInterface(name)
	if err != nil {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func handleSetStaticIP(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")

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
	json.NewEncoder(w).Encode(v) //nolint:errcheck
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(map[string]any{ //nolint:errcheck
		"error": map[string]any{
			"code":    status,
			"message": message,
		},
	})
}
