# Contributing to Marketing OS

Thank you for your interest in contributing. This document covers setup, testing, and how to propose changes.

## Prerequisites

- Go 1.25.6 or later
- Git 2.x (for contribution source control; Marketing OS does not require Git at runtime)
- macOS, Linux, or any OS that runs Go

## Setup

```sh
git clone https://github.com/0merUfuk/marketing-os.git
cd marketing-os
make install-deps
make build
make smoke
```

`make smoke` must report `pin_valid: true`. If it does not, run:

```sh
make skills-status
```

and follow the troubleshooting guide in `docs/troubleshooting.md`.

## Development workflow

### Make targets

| Target | Description |
|--------|-------------|
| `make build` | Compile binary to `./bin/marketing-os` |
| `make test` | Run all tests |
| `make test-race` | Run tests with race detector |
| `make vet` | Run `go vet` |
| `make cover` | Generate coverage report |
| `make smoke` | Build and verify skills lock |
| `make fmt` | Format all Go files |
| `make tidy` | Run `go mod tidy` |
| `make clean` | Remove build artifacts |

### Before opening a pull request

1. `make fmt`
2. `make tidy`
3. `make test`
4. `make test-race`
5. `make vet`
6. `git diff --check` (no whitespace errors)

All six must pass with zero output or exit code 0.

### Pull request guidelines

- Branch from `main`.
- One feature or fix per PR.
- Include tests for any new logic.
- Update documentation if behavior changes.
- Reference issues in the PR description (e.g. "Closes #12").

## Architecture overview

See `docs/architecture.md` for the system design, state machine, and data flow.

## Adding a new workflow

See `docs/adding-a-workflow.md` for the state-machine, evidence, model, validation, idempotency, approval, scheduler, and test requirements.

## Updating marketing skills

Marketing OS vendors exactly five reviewed upstream skills under `third_party/marketingskills`. The runtime command intentionally refuses all updates:

```sh
go run ./cmd/marketing-os --config ./config.example.yaml skills update
```

The refusal prevents a network fetch from changing production guidance. Maintainers must instead follow the [offline reviewed procedure](docs/skills.md#maintainer-update-procedure), including synchronized updates to the vendored files, `UPSTREAM.yaml`, `THIRD_PARTY_NOTICES.md`, and both manifests in `skills.lock.yaml`. Never merge an unmatched lock or an unreviewed extra skill.

## Security constraints

- Never add auto-publishing, auto-sending, auto-spending, or auto-approval capabilities to the core engine.
- Never store secret values in configuration files. Only environment variable names.
- Every AI-generated asset must cite same-product evidence IDs.
- All network calls must enforce timeouts, retry limits, and URL policy.
- SQLite transactions must not span external network calls.

## Reporting vulnerabilities

Do not open a public issue for security vulnerabilities. Email the maintainer directly.

## License

By contributing, you agree that your contributions are licensed under the MIT license.
