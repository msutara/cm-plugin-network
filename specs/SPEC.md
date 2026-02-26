# Network Plugin — Specification

## 1. Purpose

The Network Plugin (`cm-plugin-network`) provides network interface
configuration for Debian-based, headless nodes managed by Config Manager.

It exposes API endpoints for listing interfaces, setting static IPs,
managing DNS settings, and viewing connection status.

---

## 2. Responsibilities

- **List network interfaces** and their current status (up/down, IP, MAC).
- **Set static IP** configuration for a specific adapter.
- **Manage DNS settings** (nameservers, search domains).
- **Show connection status** (default gateway, reachability).

---

## 3. Non-responsibilities

This plugin does **not**:

- Manage Wi-Fi credentials or WPA configuration.
- Handle firewall rules (future plugin scope).
- Provide multi-node networking.
- Run a DHCP server.
- Manage VPN connections or tunnels.

---

## 4. Integration

- Implements the core `plugin.Plugin` interface from `config-manager-core`.
- Registration with the core is handled externally (e.g., by the host
  application importing the plugin and calling a registration function).
- Routes are mounted by the core under `/api/v1/plugins/network`.
- Imported in `cmd/cm/main.go`:

```go
import network "github.com/msutara/cm-plugin-network"

plugin.Register(network.NewNetworkPlugin())
```

---

## 5. API Routes

All routes are relative to the plugin mount point
(`/api/v1/plugins/network`).

| Method | Path                | Description                        |
| ------ | ------------------- | ---------------------------------- |
| GET    | /interfaces         | List all network interfaces        |
| GET    | /interfaces/{name}  | Get details for a single interface |
| PUT    | /interfaces/{name}  | Set static IP for an interface     |
| GET    | /dns                | Get current DNS configuration      |
| PUT    | /dns                | Update DNS configuration           |
| GET    | /status             | Show overall network status        |

---

## 6. Security

- **IPv4-only** — all IP inputs are validated as IPv4 addresses.
- **CIDR validation** — static IP values must include a valid CIDR suffix.
- **Path traversal defense** — interface names are validated via regex to
  prevent directory traversal in system calls.
- **DNS injection prevention** — nameserver and search domain values are
  validated before being written to system configuration.
- **MaxBytesReader** — PUT request bodies are size-limited to prevent
  abuse.

---

## 7. Configuration

The network plugin currently has no plugin-specific configuration.
Global CM settings (port, auth) apply as usual via `/etc/cm/config.yaml`.
