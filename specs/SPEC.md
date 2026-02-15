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
- **View network logs** (recent events from systemd journal).

---

## 3. Non-responsibilities

This plugin does **not**:

- Manage Wi-Fi credentials or WPA configuration.
- Handle firewall rules (future plugin scope).
- Provide multi-node networking or VPN management.

---

## 4. Integration

- Implements the `plugin.Plugin` interface from
  `github.com/msutara/config-manager-core/plugin`.
- Registers itself via `init()` by calling `plugin.Register()`.
- Routes are mounted by the core under `/api/v1/plugins/network`.
- Imported in `cmd/cm/main.go` with a blank import:
  `import _ "github.com/msutara/cm-plugin-network"`

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

### 5.1 GET /interfaces

Returns a JSON array of interface objects.

```json
[
  {
    "name": "eth0",
    "mac": "aa:bb:cc:dd:ee:ff",
    "ip": "192.168.1.10/24",
    "state": "up"
  }
]
```

### 5.2 GET /interfaces/{name}

Returns a single interface object (same shape as above) or 404.

### 5.3 PUT /interfaces/{name}

Request body:

```json
{
  "ip": "192.168.1.100/24",
  "gateway": "192.168.1.1"
}
```

Returns the updated interface object or an error.

### 5.4 GET /dns

```json
{
  "nameservers": ["8.8.8.8", "1.1.1.1"],
  "search": ["local"]
}
```

### 5.5 PUT /dns

Request body: same shape as GET response.

### 5.6 GET /status

```json
{
  "default_gateway": "192.168.1.1",
  "dns_reachable": true,
  "internet_reachable": true
}
```

---

## 6. Domain Types

```go
type Interface struct {
    Name  string `json:"name"`
    MAC   string `json:"mac"`
    IP    string `json:"ip"`
    State string `json:"state"`
}

type DNSConfig struct {
    Nameservers []string `json:"nameservers"`
    Search      []string `json:"search"`
}

type StaticIPRequest struct {
    IP      string `json:"ip"`
    Gateway string `json:"gateway"`
}

type NetworkStatus struct {
    DefaultGateway    string `json:"default_gateway"`
    DNSReachable      bool   `json:"dns_reachable"`
    InternetReachable bool   `json:"internet_reachable"`
}
```

---

## 7. Future Extensions

- Wi-Fi configuration (SSID scanning, WPA supplicant).
- VLAN support.
- Scheduled connectivity checks as a plugin job.
- Firewall rule management (separate plugin).
