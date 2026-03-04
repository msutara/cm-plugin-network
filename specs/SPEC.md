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

### Query Parameters

| Parameter  | Applies To               | Description                                   |
| ---------- | ------------------------ | --------------------------------------------- |
| `dry_run`  | PUT /interfaces, PUT /dns | When `true`, validates and previews changes without applying them. Returns a `DryRunResult` with current vs proposed config and a change summary. |

### Required Headers

| Header      | Applies To               | Description                                   |
| ----------- | ------------------------ | --------------------------------------------- |
| `X-Confirm` | PUT /interfaces, PUT /dns | Must be set to `true` for mutating operations. Without it, the server returns **428 Precondition Required** with a description of what would change. This prevents accidental network disruption from scripts or UI bugs. |

---

## 6. Security

- **IPv4-only static IP** — all static IP and gateway inputs are validated
  as IPv4 addresses. IPv4-mapped IPv6 forms (e.g., `::ffff:192.168.1.1`)
  are canonicalized to pure IPv4 before use. DNS nameservers accept both
  IPv4 and IPv6.
- **CIDR validation** — static IP values must include a valid CIDR suffix.
- **Subnet validation** — the gateway must reside within the IP address
  subnet, and cannot equal the interface IP.
- **Path traversal defense** — interface names are validated via regex
  (`^[a-zA-Z0-9][a-zA-Z0-9._:-]*$`) at both the HTTP and service layers.
  VLAN names (`eth0.100`) and aliases (`br0:1`) are accepted.
- **DNS injection prevention** — nameserver and search domain values are
  validated before being written to system configuration.
- **Atomic file writes** — configuration files are written via temp-file +
  fsync + rename to prevent corruption from interrupted writes or power
  loss. The parent directory is synced for durability on embedded flash.
- **Symlink-safe DNS writes** — if `/etc/resolv.conf` is a symlink (e.g.,
  to systemd-resolved), the write targets the resolved path to preserve
  the link.
- **Command execution** — `ifdown`/`ifup` are invoked via absolute paths
  with separate per-command timeouts (default 30 s each) and no shell
  interpretation.
- **MaxBytesReader** — PUT request bodies are size-limited to prevent
  abuse.
- **Concurrency** — all mutating operations are serialized via a mutex to
  prevent interleaved config writes on the same interface.
- **Rollback on failure** — before applying a new static IP configuration,
  the current config file is backed up. If `ifup` fails after writing the
  new config, the backup is automatically restored and `ifup` is retried
  with the old configuration. On successful rollback, the backup file is
  removed. If the restore itself fails, the backup file is **preserved** on
  disk for manual recovery and the error includes the backup path. The same
  backup/restore pattern applies to DNS writes.
- **Dry-run support** — `PUT /interfaces/{name}?dry_run=true` and
  `PUT /dns?dry_run=true` validate the request and return a preview of
  what would change (current vs proposed config, human-readable diff)
  without modifying any files or running any commands.
- **Confirmation requirement** — mutating PUT operations require the
  `X-Confirm: true` header. Without it, the server responds with
  **428 Precondition Required** and a message describing the operation.
  This prevents accidental network disruption from automated clients.

---

## 7. Configuration

The network plugin currently has no plugin-specific configuration.
Global CM settings (port, auth) apply as usual via `/etc/cm/config.yaml`.
