# Usage Guide

## Overview

The network plugin provides REST API endpoints for managing network
interfaces, static IP addresses, DNS configuration, and connectivity
status on headless Debian-based nodes.

## Integration

The plugin is compiled into the core binary via a normal import in
`cmd/cm/main.go`:

```go
import network "github.com/msutara/cm-plugin-network"

plugin.Register(network.NewNetworkPlugin())
```

> **Note:** The plugin implements the `plugin.Plugin` interface from
> `config-manager-core` directly.

Once loaded, its routes are available under `/api/v1/plugins/network`.

## API Endpoints

### List Interfaces

```bash
curl http://localhost:8080/api/v1/plugins/network/interfaces
```

### Get Interface Details

```bash
curl http://localhost:8080/api/v1/plugins/network/interfaces/eth0
```

### Set Static IP

```bash
curl -X PUT http://localhost:8080/api/v1/plugins/network/interfaces/eth0 \
  -H "Content-Type: application/json" \
  -d '{"ip": "192.168.1.100/24", "gateway": "192.168.1.1"}'
```

### Get DNS Configuration

```bash
curl http://localhost:8080/api/v1/plugins/network/dns
```

### Update DNS Configuration

```bash
curl -X PUT http://localhost:8080/api/v1/plugins/network/dns \
  -H "Content-Type: application/json" \
  -d '{"nameservers": ["8.8.8.8", "1.1.1.1"], "search": ["local"]}'
```

### Check Network Status

```bash
curl http://localhost:8080/api/v1/plugins/network/status
```

## Configuration

The network plugin currently has no plugin-specific configuration.
Global CM settings (port, auth) apply as usual via `/etc/cm/config.yaml`.
