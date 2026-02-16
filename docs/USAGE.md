# Usage Guide

## Overview

The network plugin is integrated into Config Manager by importing it and
registering the plugin with the core's plugin registry. In `cmd/cm/main.go`:

```go
import _ "github.com/msutara/cm-plugin-network"
```

> **Note:** In Phase 1, the plugin uses a local `pluginiface` package that
> mirrors the core's `plugin.Plugin` interface for independent development.
> The blank import pattern above describes the intended end-state when full
> integration with the core is wired.

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
