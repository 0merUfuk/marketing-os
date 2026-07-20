# Marketing OS Community Readiness Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Make Marketing OS community-ready by adding missing community files, a Makefile, and a Keep a Chelog formatted CHANGELOG, then verifying everything is clean and pushed.

**Architecture:** No code changes to the Go core. This plan only adds project-level files (Makefile, CONTRIBUTING.md, CHANGELOG.md) and verifies the existing codebase is healthy. All files go at the repository root or in `.github/`.

**Tech Stack:** Go 1.25.6, Git, Make, Markdown

---

## Current state

- Repo: `/Users/omerufuk/repos/marketing-os`
- Branch: `main`
- Commits: `e243eee` (initial MVP), `63d055c` (MIT LICENSE), `7a9cdf4` (GUIDE.md)
- LICENSE: MIT, already present
- No CONTRIBUTING.md, no CHANGELOG.md, no Makefile
- All tests, vet, race, build pass on current HEAD
- Pinned skills: `67264763cb107d61749f418d081c56e5bcbc0209` (v2.8.12, pin_valid: true)
- Remote: `origin` → `https://github.com/0merUfuk/marketing-os.git`

---

### Task 1: Create Makefile

**Objective:** Provide standard build/test/verify targets for contributors.

**Files:**
- Create: `Makefile`

**Content:**

```makefile
.PHONY: build test test-race vet vet-race cover smoke fmt tidy clean install-deps skills-status

## build: Compile the marketing-os binary into ./bin/
build:
	go build -trimpath -o ./bin/marketing-os ./cmd/marketing-os

## test: Run all tests
test:
	go test ./... -count=1

## test-race: Run all tests with the race detector
test-race:
	go test -race ./... -count=1

## vet: Run go vet
vet:
	go vet ./...

## cover: Generate coverage report
cover:
	go test ./... -coverprofile=coverage.out
	go tool cover -func=coverage.out | tail -n 1

## smoke: Build and run skills status smoke test
smoke: build
	./bin/marketing-os --config ./config.example.yaml --json skills status

## fmt: Format all Go files
fmt:
	gofmt -w $$(git ls-files '*.go') $$(git ls-files --others --exclude-standard '*.go')

## tidy: Run go mod tidy
tidy:
	go mod tidy

## install-deps: Download Go modules
install-deps:
	go mod download

## skills-status: Verify pinned skills lock
skills-status:
	go run ./cmd/marketing-os --config ./config.example.yaml --json skills status

## clean: Remove build artifacts
clean:
	rm -rf ./bin coverage.out
```

**Verification:**

Run: `make build && make test && make vet && make smoke`
Expected: all exit 0, `pin_valid: true` in smoke output.

**Commit:**

```bash
git add Makefile
git commit -m "build: add Makefile with standard build/test/vet/smoke targets"
```

---

### Task 2: Create CHANGELOG.md (Keep a Changelog format)

**Objective:** Seed the changelog with the initial release history.

**Files:**
- Create: `CHANGELOG.md`

**Content:**

```markdown
# Changelog

All notable changes to Marketing OS are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- CONTRIBUTING.md guide for community contributors.
- Makefile with standard build, test, vet, and smoke targets.

## [0.1.0] - 2026-07-20

### Added

- Local-first, evidence-grounded marketing workflow engine in Go.
- Modular monolith architecture with pure-Go SQLite (`modernc.org/sqlite`).
- Pinned Agent Skills submodule (`marketingskills` @ `6726476`, v2.8.12) with SHA-256 manifest verification.
- OpenAI-compatible LLM abstraction with token/cost controls, secret redaction, strict JSON output, and bounded repair retries.
- Release-to-marketing workflow: GitHub release ingestion, immutable evidence, structured generation, evidence citation validation, forbidden-term enforcement, and `no_action` for low-marketability releases.
- Idempotent GitHub approval issue creation with hidden marker reconciliation and ambiguous-write recovery.
- Multi-product support with isolated product workspaces, versioned context drafts, and human-only context approval.
- Durable SQLite state: workflow runs, claims, leases, fencing tokens, dedupe keys, cursors, audit events, and skill snapshots.
- Cron scheduler with dynamic workflow reconciliation, per-run timeouts, retry logic, and persistent global kill switch.
- Cobra CLI with JSON output, product/context/workflow/skills/scheduler/runs/approvals commands.
- Comprehensive documentation: architecture, database, configuration, CLI, skills, security, workflow extension, troubleshooting, implementation plan, and absolute beginner's guide.
- MIT license.
- Reusable GitHub Actions workflow in `0merUfuk/.github`.
- Hermes onboarding skill for one-command project registration.

### Security

- Deny-by-default: no publishing, sending, spending, or auto-approval.
- Secrets referenced only by environment variable names in configuration.
- URL policy: HTTPS required for non-loopback endpoints, redirect revalidation, private-network rejection.
- Symlink containment: safely resolved repository-contained symlinks allowed; escaping and broken links rejected.
- Bounded skill loading with reference-aware traversal prevention.
- Input-token preflight check prevents unnecessary HTTP requests.
```

**Verification:**

Run: `head -5 CHANGELOG.md`
Expected: starts with `# Changelog`

**Commit:**

```bash
git add CHANGELOG.md
git commit -m "docs: add CHANGELOG.md in Keep a Changelog format"
```

---

### Task 3: Create CONTRIBUTING.md

**Objective:** Guide community contributors on how to set up, test, propose changes, and add skills.

**Files:**
- Create: `CONTRIBUTING.md`

**Content:**

```markdown
# Contributing to Marketing OS

Thank you for your interest in contributing. This document covers setup, testing, and how to propose changes.

## Prerequisites

- Go 1.25.6 or later
- Git 2.x
- macOS, Linux, or any OS that runs Go

## Setup

```sh
git clone --recurse-submodules https://github.com/0merUfuk/marketing-os.git
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

Marketing skills are a pinned Git submodule. Do not edit files inside `.skills/marketingskills` directly.

To update to a new upstream commit:

```sh
go run ./cmd/marketing-os --config ./config.example.yaml skills update --ref <new-commit-or-tag>
```

Review the submodule diff and `skills.lock.yaml` before committing. Never run workflows with an unmatched lock.

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
```

**Verification:**

Run: `head -5 CONTRIBUTING.md`
Expected: starts with `# Contributing to Marketing OS`

**Commit:**

```bash
git add CONTRIBUTING.md
git commit -m "docs: add CONTRIBUTING.md for community contributors"
```

---

### Task 4: Full verification pass

**Objective:** Verify the complete repository is healthy after all additions.

**Files:** None (read-only verification).

**Steps:**

Run each command and verify exit code 0:

```sh
make fmt
make tidy
make test
make test-race
make vet
make build
make smoke
git diff --check
git status --short
git submodule status
```

Expected:
- `make test`: all packages `ok`
- `make test-race`: all packages `ok`
- `make vet`: no output
- `make build`: binary at `./bin/marketing-os`
- `make smoke`: `pin_valid: true`
- `git diff --check`: no output
- `git status --short`: only new files if any
- `git submodule status`: `67264763cb107d61749f418d081c56e5bcbc0209`

**No commit needed** (verification only).

---

### Task 5: Push to origin

**Objective:** Push all community readiness commits to GitHub.

**Steps:**

```sh
git push origin main
```

Expected: three new commits pushed successfully.

---

## Risks and tradeoffs

- **No code changes**: This plan deliberately avoids touching Go source. No test regressions possible.
- **Makefile portability**: Uses `$$()` shell escape for git commands inside Make. Standard on macOS/Linux. Windows users would need WSL or Git Bash.
- **CHANGELOG is manual**: No automated generation yet. Acceptable for v0.1.0; can add `changeloguru` or similar later.

## Open questions

- Should we add a `.golangci.yml` lint config in a future task? (Not in this plan.)
- Should we add GitHub Actions CI workflow that runs `make test`? (Not in this plan; separate effort.)
- Should we add issue/PR templates? (Not in this plan; can add to `.github/` later.)
