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

| Method | Path                          | Description                              |
| ------ | ----------------------------- | ---------------------------------------- |
| GET    | /interfaces                   | List all network interfaces              |
| GET    | /interfaces/{name}            | Get details for a single interface       |
| PUT    | /interfaces/{name}            | Set static IP for an interface           |
| DELETE | /interfaces/{name}            | Remove static IP config (revert to DHCP) |
| POST   | /interfaces/{name}/rollback   | Restore previous interface config (.bak) |
| GET    | /dns                          | Get current DNS configuration            |
| PUT    | /dns                          | Update DNS configuration                 |
| POST   | /dns/rollback                 | Restore previous DNS config (.bak)       |
| GET    | /status                       | Show overall network status              |

### Query Parameters

| Parameter  | Applies To               | Description                                   |
| ---------- | ------------------------ | --------------------------------------------- |
| `dry_run`  | PUT, DELETE, POST (mutating) | When `true`, validates and previews changes without applying them. Returns a `DryRunResult` with current vs proposed config and a change summary. |

### Required Headers

| Header      | Applies To               | Description                                   |
| ----------- | ------------------------ | --------------------------------------------- |
| `X-Confirm` | PUT, DELETE, POST (mutating) | Must be set to `true` for mutating operations. Without it, the server returns **428 Precondition Required** with a `dry_run` hint describing how to preview the proposed changes safely. This prevents accidental network disruption from scripts or UI bugs. |

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
- **Safe file reads** — backup files are read through `safeReadFile` which
  opens the file, verifies the file descriptor refers to a regular file
  (not a directory, device, or FIFO), and reads through the same fd. This
  eliminates TOCTOU races between validation and read.
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
  with the old configuration. The `.bak` file is **preserved** after
  successful mutations so it remains available for explicit rollback. If
  the restore itself fails, the backup file is preserved on disk for manual
  recovery. The same backup/restore pattern applies to DNS writes.
- **Explicit rollback** — `POST /interfaces/{name}/rollback` and
  `POST /dns/rollback` restore the `.bak` backup file created by the most
  recent mutating operation. Before restoring, the current config is backed
  up to a `.pre-rollback` file. On success, the `.pre-rollback` file is
  promoted to `.bak` so that rollback itself is reversible (a second
  rollback reverses the first). **Limitation:** after a DELETE→rollback
  cycle, the "no config" state has no `.bak` representation, so a second
  rollback restores the same content (effectively a no-op). If rollback
  `ifup` fails in the post-delete scenario, the `.bak` is renamed to
  `.bak.failed` to prevent infinite retry loops.
- **Delete static IP** — `DELETE /interfaces/{name}` removes the static
  config file from `/etc/network/interfaces.d/` and runs `ifdown`/`ifup`
  to revert the interface to DHCP. The deleted config is preserved as a
  `.bak` file for rollback.
- **Dry-run support** — `PUT /interfaces/{name}?dry_run=true`,
  `PUT /dns?dry_run=true`, `DELETE /interfaces/{name}?dry_run=true`,
  `POST /interfaces/{name}/rollback?dry_run=true`, and
  `POST /dns/rollback?dry_run=true` validate the request and return a
  preview of what would change (current vs proposed config, human-readable
  diff) without modifying any files or running any commands.
- **Confirmation requirement** — all mutating operations (PUT, DELETE, POST
  rollback) require the `X-Confirm: true` header. Without it, the server
  responds with **428 Precondition Required** and a message describing the
  operation. This prevents accidental network disruption from automated
  clients.

---

## 7. Configuration

Global CM settings (port, auth) apply as usual via `/etc/cm/config.yaml`.

### Interface Write Policy

Interface write operations on `/interfaces` routes (PUT, DELETE, POST
rollback) can be restricted to specific interfaces via configuration.
The plugin implements `plugin.Configurable` so the policy can be set at
startup and updated at runtime. This policy does not apply to DNS routes
(`/dns`, `/dns/rollback`).

Add to `config.yaml` under the network plugin section:

```yaml
plugins:
  network:
    interface_policy:
      mode: "denylist"
      list:
        - "lo"
        - "gre0"
        - "gretap0"
        - "sit0"
        - "ip6tnl0"
        - "docker*"
        - "veth*"
```

#### Modes

| Mode | Behavior |
| --- | --- |
| `denylist` (default) | All interfaces writable EXCEPT those matching patterns |
| `allowlist` | ONLY interfaces matching patterns are writable |
| `""` (explicit empty) | Policy disabled — all interfaces writable |

> **Note:** When no `interface_policy` configuration is provided at all, the default
> denylist is active (blocking `lo`, `gre0`, etc.). Setting `mode: ""` explicitly
> disables the policy — these are distinct behaviors.

#### Pattern Syntax

Patterns use `filepath.Match` glob semantics:

- `*` matches any sequence of characters
- `?` matches a single character
- `[abc]` matches one of the listed characters

#### Default Denylist

When no configuration is provided, the default denylist blocks:
`lo`, `gre0`, `gretap0`, `sit0`, `ip6tnl0`.

#### Error Response

When a write targets a denied interface, the API responds with HTTP 403 Forbidden:

```json
{
  "error": {
    "code": 403,
    "message": "interface 'eth0p' is not allowed for write operations",
    "details": {}
  }
}
```

GET operations are never restricted.
