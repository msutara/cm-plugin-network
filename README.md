# cm-plugin-network

Network configuration plugin for
[Config Manager](https://github.com/msutara/config-manager-core). Designed for
headless Debian-based nodes (Raspbian Bookworm ARM64, Debian Bullseye slim).

## Features

- List network interfaces and their state
- Configure static IP addresses per adapter (VLAN and alias names supported)
- View and update DNS nameservers and search domains
- Check default gateway and internet reachability
- RESTful API mounted at `/api/v1/plugins/network`
- Atomic config writes with power-loss durability
- IPv4-mapped IPv6 canonicalization and subnet validation
- Symlink-safe DNS writes (systemd-resolved compatible)
- Dry-run preview mode for safe change review
- Confirmation header requirement to prevent accidental changes
- Automatic rollback on interface activation failure
- Interface write policy (allowlist/denylist) for protecting critical interfaces
- Configurable at startup and runtime via `plugin.Configurable`

## Safety Features

### Dry-Run Mode

Preview changes before applying them by adding `?dry_run=true` to
`PUT /interfaces/{name}` or `PUT /dns`. The server validates the input and
returns a `DryRunResult` JSON object with the current config, proposed config,
and a human-readable change summary — without modifying any files or restarting
services.

```bash
curl -s -X PUT \
  http://localhost:8080/api/v1/plugins/network/interfaces/eth0?dry_run=true \
  -H 'Content-Type: application/json' \
  -d '{"ip": "192.168.1.10/24", "gateway": "192.168.1.1"}'
```

Response:

```json
{
  "valid": true,
  "current_config": "auto eth0\niface eth0 inet static\n    address 10.0.0.1\n    ...",
  "proposed_config": "auto eth0\niface eth0 inet static\n    address 192.168.1.10\n    ...",
  "changes": ["- address 10.0.0.1", "+ address 192.168.1.10", "..."]
}
```

### Confirmation Header

All mutating PUT operations require the `X-Confirm: true` header. Without
it, the server returns **428 Precondition Required** with a description of
the operation and a hint to use dry-run mode first.

```bash
curl -s -X PUT \
  http://localhost:8080/api/v1/plugins/network/interfaces/eth0 \
  -H 'Content-Type: application/json' \
  -H 'X-Confirm: true' \
  -d '{"ip": "192.168.1.10/24", "gateway": "192.168.1.1"}'
```

### Rollback Behavior

Before applying a new configuration, the current config file is backed up.
If `ifup` fails after writing the new config:

1. The backup is automatically restored.
2. `ifup` is retried with the old configuration.
3. On **successful rollback**, the backup file is cleaned up.
4. On **failed rollback**, the backup file is **preserved** on disk so an
   operator can manually recover. The error message includes the backup path.

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
