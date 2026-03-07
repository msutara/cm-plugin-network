package network

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

// maxBodySize limits JSON request bodies to 1 MB.
const maxBodySize = 1 << 20

// validName matches safe interface names (alphanumeric, hyphens,
// underscores, dots for VLANs, colons for aliases).
var validName = validIfaceName

func newRouter(svc *Service) http.Handler {
	r := chi.NewRouter()
	h := &handler{svc: svc}

	r.Get("/interfaces", h.handleListInterfaces)
	r.Get("/interfaces/{name}", h.handleGetInterface)
	r.Put("/interfaces/{name}", h.handleSetStaticIP)
	r.Delete("/interfaces/{name}", h.handleDeleteStaticIP)
	r.Post("/interfaces/{name}/rollback", h.handleRollbackInterface)
	r.Get("/dns", h.handleGetDNS)
	r.Put("/dns", h.handleSetDNS)
	r.Post("/dns/rollback", h.handleRollbackDNS)
	r.Get("/status", h.handleGetNetworkStatus)

	return r
}

// handler groups HTTP handlers with a shared Service instance.
type handler struct {
	svc *Service
}

func (h *handler) handleListInterfaces(w http.ResponseWriter, _ *http.Request) {
	ifaces, err := h.svc.ListInterfaces()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, ifaces)
}

func (h *handler) handleGetInterface(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !validName.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid interface name")
		return
	}
	iface, err := h.svc.GetInterface(name)
	if err != nil {
		if errors.Is(err, errInvalidIfaceName) {
			writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		if errors.Is(err, errIfaceNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
			return
		}
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func (h *handler) handleSetStaticIP(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !validName.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid interface name")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req StaticIPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if r.URL.Query().Get("dry_run") == "true" {
		result, err := h.svc.DryRunStaticIP(name, req)
		if err != nil {
			writeStaticIPError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if r.Header.Get("X-Confirm") != "true" {
		writePreconditionRequired(w)
		return
	}

	iface, err := h.svc.SetStaticIP(name, req)
	if err != nil {
		writeStaticIPError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func (h *handler) handleDeleteStaticIP(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !validName.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid interface name")
		return
	}

	if r.URL.Query().Get("dry_run") == "true" {
		result, err := h.svc.DryRunDeleteStaticIP(name)
		if err != nil {
			writeDeleteError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if r.Header.Get("X-Confirm") != "true" {
		writePreconditionRequired(w)
		return
	}

	iface, err := h.svc.DeleteStaticIP(name)
	if err != nil {
		writeDeleteError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func (h *handler) handleRollbackInterface(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	if !validName.MatchString(name) {
		writeError(w, http.StatusBadRequest, "invalid interface name")
		return
	}

	if r.URL.Query().Get("dry_run") == "true" {
		result, err := h.svc.DryRunRollbackInterface(name)
		if err != nil {
			writeRollbackError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if r.Header.Get("X-Confirm") != "true" {
		writePreconditionRequired(w)
		return
	}

	iface, err := h.svc.RollbackInterface(name)
	if err != nil {
		writeRollbackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, iface)
}

func (h *handler) handleGetDNS(w http.ResponseWriter, _ *http.Request) {
	dns, err := h.svc.GetDNS()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, dns)
}

func (h *handler) handleSetDNS(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodySize)
	var req DNSConfig
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			writeError(w, http.StatusRequestEntityTooLarge, "request body too large")
			return
		}
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if r.URL.Query().Get("dry_run") == "true" {
		result, err := h.svc.DryRunDNS(req)
		if err != nil {
			writeDNSError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if r.Header.Get("X-Confirm") != "true" {
		writePreconditionRequired(w)
		return
	}

	dns, err := h.svc.SetDNS(req)
	if err != nil {
		writeDNSError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dns)
}

func (h *handler) handleRollbackDNS(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("dry_run") == "true" {
		result, err := h.svc.DryRunRollbackDNS()
		if err != nil {
			writeRollbackError(w, err)
			return
		}
		writeJSON(w, http.StatusOK, result)
		return
	}

	if r.Header.Get("X-Confirm") != "true" {
		writePreconditionRequired(w)
		return
	}

	dns, err := h.svc.RollbackDNS()
	if err != nil {
		writeRollbackError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, dns)
}

func (h *handler) handleGetNetworkStatus(w http.ResponseWriter, _ *http.Request) {
	status, err := h.svc.GetNetworkStatus()
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
		slog.Error("failed to write JSON response", "plugin", "network", "error", err)
	}
}

func writeError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
			"details": map[string]any{},
		},
	}); err != nil {
		slog.Error("failed to write error response", "plugin", "network", "error", err)
	}
}

func writeStaticIPError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidCIDR) || errors.Is(err, errInvalidGW) ||
		errors.Is(err, errEmptyIP) || errors.Is(err, errEmptyGateway) ||
		errors.Is(err, errIPv6NotSupported) || errors.Is(err, errGWNotInSubnet) ||
		errors.Is(err, errGWEqualsIP) || errors.Is(err, errInvalidIfaceName) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, errIfaceNotFound) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, errNotLinux) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	slog.Error("set static IP failed", "plugin", "network", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error during configuration")
}

func writeDNSError(w http.ResponseWriter, err error) {
	if errors.Is(err, errNotLinux) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, errInvalidNameserver) || errors.Is(err, errInvalidSearchDom) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	slog.Error("set DNS failed", "plugin", "network", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error during DNS configuration")
}

func writeDeleteError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidIfaceName) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, errIfaceNotFound) || errors.Is(err, errNoStaticConfig) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, errNotLinux) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	slog.Error("delete failed", "plugin", "network", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error during delete operation")
}

func writeRollbackError(w http.ResponseWriter, err error) {
	if errors.Is(err, errInvalidIfaceName) {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if errors.Is(err, errIfaceNotFound) || errors.Is(err, errNoBackup) {
		writeError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, errNotLinux) {
		writeError(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	slog.Error("rollback failed", "plugin", "network", "error", err)
	writeError(w, http.StatusInternalServerError, "internal error during rollback operation")
}

func writeErrorWithDetails(w http.ResponseWriter, status int, message string, details map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]any{
			"code":    status,
			"message": message,
			"details": details,
		},
	}); err != nil {
		slog.Error("failed to write error response", "plugin", "network", "error", err)
	}
}

func writePreconditionRequired(w http.ResponseWriter) {
	writeErrorWithDetails(w, http.StatusPreconditionRequired,
		"confirmation required: This operation will modify network configuration. Set X-Confirm: true header to proceed.",
		map[string]any{
			"dry_run_hint": "Use ?dry_run=true to preview changes first.",
		},
	)
}
