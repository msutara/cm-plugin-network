# cm-plugin-network

Network configuration plugin for
[Config Manager](https://github.com/msutara/config-manager-core). Designed for
headless Debian-based nodes (Raspbian Bookworm ARM64, Debian Bullseye slim).

## Features

- List network interfaces and their state
- Configure static IP addresses per adapter
- View and update DNS nameservers and search domains
- Check default gateway and internet reachability
- RESTful API mounted at `/api/v1/plugins/network`

## Documentation

- [Usage Guide](docs/USAGE.md) — endpoint examples and integration
- [Specification](specs/SPEC.md) — responsibilities, integration, API routes

## Development

```bash
# lint
golangci-lint run

# test
go test ./...
```

CI runs automatically on push/PR to `main` via GitHub Actions
(`.github/workflows/ci.yml`).

## Contributing

See [CONTRIBUTING.md](CONTRIBUTING.md) for guidelines.

## Security

See [SECURITY.md](SECURITY.md) for vulnerability reporting.

## License

See [LICENSE](LICENSE) for details.
