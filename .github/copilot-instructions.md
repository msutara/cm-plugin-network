# Copilot Instructions

## Project Overview

CM Plugin Network is a Config Manager plugin that provides network interface
configuration for headless Debian-based nodes. It implements a local
`pluginiface.Plugin` interface (mirroring the core `plugin.Plugin` contract)
for independent development; registration with the core is handled externally.

The plugin exposes REST API endpoints for listing interfaces, setting static IPs,
managing DNS, and checking connectivity status.

## Architecture

- **plugin.go** — `NetworkPlugin` struct implementing the `pluginiface.Plugin`
  interface; registration is handled by the core (no `init()` self-registration)
- **routes.go** — Chi router with HTTP handlers for all API endpoints
- **service.go** — domain logic functions (interface listing, static IP, DNS, status)

Routes are mounted by the core under `/api/v1/plugins/network`.

## Conventions

- Use `github.com/go-chi/chi/v5` for HTTP routing
- Use `log/slog` for all logging (structured, with plugin name)
- Error responses use `{"error": {"code": ..., "message": ...}}` format
- Job IDs follow the pattern `network.{job_name}`
- Domain types live in `service.go`
- Handler functions live in `routes.go`
- Keep the plugin as a single Go package at the root; additional helper
  packages (e.g., `pluginiface`) are allowed

## Validation

- All Go code must pass `golangci-lint run`
- All tests must pass: `go test ./...`
- CI runs lint + test via `.github/workflows/ci.yml`
- Never push directly to main — always use feature branches and PRs
