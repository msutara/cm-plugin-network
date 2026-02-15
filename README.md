# CM Plugin Network

Config Manager plugin for network interface configuration on headless
Debian-based nodes. Implements the `plugin.Plugin` interface from
[config-manager-core](https://github.com/msutara/config-manager-core).

## Features

- **List interfaces** — enumerate network interfaces and their state
- **Static IP** — configure static IP addresses per adapter
- **DNS management** — view and update nameservers and search domains
- **Status checks** — default gateway and reachability information
- **REST API** — all functionality exposed via JSON endpoints

## Documentation

- [Usage Guide](docs/USAGE.md) — API examples and integration

## Specifications

- [SPEC.md](specs/SPEC.md) — plugin specification and API contract

## Development

```bash
# Lint
golangci-lint run

# Test
go test ./...
```

## License

The license for this project has not yet been finalized.