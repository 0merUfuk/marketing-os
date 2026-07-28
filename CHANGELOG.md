# Changelog

All notable changes to Marketing OS are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

## [0.1.0] - 2026-07-28

### Added

- Local-first, evidence-grounded marketing workflow engine in Go.
- Modular monolith architecture with pure-Go SQLite (`modernc.org/sqlite`).
- Five vendored runtime skills (`product-marketing`, `launch`, `copywriting`, `social`, `emails`) from `coreyhaines31/marketingskills` with SHA-256 manifest verification.
- OpenAI-compatible LLM abstraction with token/cost controls, secret redaction, strict JSON output, and bounded repair retries.
- Release-to-marketing workflow: GitHub release ingestion, immutable evidence, structured generation, evidence citation validation, forbidden-term enforcement, and `no_action` for low-marketability releases.
- Idempotent GitHub approval issue creation with hidden marker reconciliation and ambiguous-write recovery.
- Multi-product support with isolated product workspaces, versioned context drafts, and human-only context approval.
- Durable SQLite state: workflow runs, claims, leases, fencing tokens, dedupe keys, cursors, audit events, and skill snapshots.
- Cron scheduler with dynamic workflow reconciliation, per-run timeouts, retry logic, and persistent global kill switch.
- Cobra CLI with JSON output, product/context/workflow/skills/scheduler/runs/approvals commands.
- CONTRIBUTING.md guide for community contributors.
- Makefile with standard build, test, vet, and smoke targets.
- First-class upstream provenance and MIT attribution through `third_party/marketingskills/UPSTREAM.yaml` and `THIRD_PARTY_NOTICES.md`.
- Minimal secret-free CI workflow.
- Comprehensive documentation: architecture, database, configuration, CLI, skills, security, workflow extension, troubleshooting, implementation plan, and absolute beginner's guide.
- MIT license.

### Security

- Deny-by-default: no publishing, sending, spending, or auto-approval.
- Secrets referenced only by environment variable names in configuration.
- URL policy: HTTPS required for non-loopback endpoints, redirect revalidation, private-network rejection.
- Symlink containment: safely resolved repository-contained symlinks allowed; escaping and broken links rejected.
- Bounded skill loading with reference-aware traversal prevention.
- Input-token preflight check prevents unnecessary HTTP requests.
- Runtime `skills update` fails closed; maintainers must follow the offline reviewed update procedure.