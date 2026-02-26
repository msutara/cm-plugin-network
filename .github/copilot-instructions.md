# Copilot Instructions

## Project Overview

cm-plugin-network is a Go plugin for Config Manager that provides network
interface configuration for headless Debian-based nodes. It provides endpoints
to list interfaces, configure static IPs, manage DNS nameservers and search
domains, and check connectivity status.

Target platforms: Raspbian Bookworm (ARM64), Debian Bullseye slim.

## Architecture

- **plugin.go** — `NetworkPlugin` struct implementing `plugin.Plugin` from `config-manager-core`;
  registration handled by the core (no `init()` self-registration);
  uses `sync.Once` for lazy service initialization
- **routes.go** — Chi router with HTTP handlers; input validation via regex;
  `MaxBytesReader` on PUT bodies; mounted under `/api/v1/plugins/network`
- **service.go** — domain logic: `ListInterfaces`, `GetInterface`,
  `SetStaticIP`, `GetDNS`, `SetDNS`, `GetNetworkStatus`; sentinel errors
  for typed error mapping in handlers

## Integration

The plugin is compiled into the core binary via a normal import in
`cmd/cm/main.go`:

```go
import network "github.com/msutara/cm-plugin-network"

plugin.Register(network.NewNetworkPlugin())
```

Routes are mounted under `/api/v1/plugins/network`.

## Conventions

- Main Go package is `package network` at the repo root
- Additional helper packages are allowed
- Use `github.com/go-chi/chi/v5` for HTTP routing
- Use `log/slog` for all structured logging (include `"plugin", "network"`)
- Error responses: `{"error": {"code": ..., "message": ..., "details": {}}}`
- Job IDs follow the pattern `network.{job_name}`
- Specs live in `specs/`, user docs in `docs/`
- Filenames use UPPERCASE-KEBAB-CASE (e.g., `SPEC.md`, `USAGE.md`)

## Specifications

- [specs/SPEC.md](../specs/SPEC.md) — plugin specification and scope
- [docs/USAGE.md](../docs/USAGE.md) — endpoint examples and integration

## Validation

- All Go code must pass `golangci-lint run`
- All tests must pass: `go test ./...`
- CI runs markdownlint + lint + test via `.github/workflows/ci.yml`
- Never push directly to main — always use feature branches and PRs
