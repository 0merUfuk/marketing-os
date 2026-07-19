# Implementation plan and acceptance gates

This plan captures the architecture-first sequence used for the production-ready MVP and the contract each milestone must satisfy. A milestone is not complete when code merely compiles; its observable acceptance gate must pass.

## Milestone 0 — Repository and constraints

Deliverables:

- Go module and conventional `cmd/`, `internal/`, `migrations/`, `docs/` layout.
- Upstream `marketingskills` retained as a Git submodule.
- Strict statement of Tier 1 (analyze/generate) versus forbidden Tier 2 capabilities.
- Architecture, database, workflow, and security choices documented.

Gate:

- No JavaScript/TypeScript application or build-script dependency.
- No publisher/sender/spending adapter exists.
- Upstream license/attribution files remain untouched.

## Milestone 1 — Configuration, products, workspace, and migrations

Deliverables:

- Strict YAML config with relative path normalization and env-only secret references.
- Product registry and isolated workspace initialization.
- Embedded ordered SQLite migrations.
- Complete disabled workflow definition seeded per product.

Gate:

- Unknown/unsafe config fails.
- Product IDs cannot escape workspace paths.
- Fresh database migrates and all constraints/indexes exist.
- Register/list/inspect CLI integration test passes.

## Milestone 2 — Pinned Agent Skill runtime

Deliverables:

- Strict `SKILL.md` frontmatter parser and metadata index.
- Explicit reference loader with path/size containment.
- Exact commit + full repository manifest lock.
- Safe in-repository tracked symlink handling.
- Explicit status/list/update CLI; no auto-update.
- Repository/skill version audit tables.

Gate:

- Actual pinned upstream commit and manifest report `pin_valid: true`.
- Any content/commit drift blocks AI execution.
- Escaping symlink/reference and invalid frontmatter tests fail closed.

## Milestone 3 — Product context onboarding

Deliverables:

- Bounded collection from product config, allowlisted local/GitHub docs, and configured pages.
- Immutable evidence snapshots and explicit source warnings.
- Pinned `product-marketing` prompt with strict result schema.
- Required-section, uncertainty, and evidence-ID validation with bounded repair.
- Draft/version/show/approve commands and canonical context mirror.

Gate:

- Drafts are never automatically approved.
- Unknown facts remain explicit.
- Workflows block before GitHub/model use without approved context.
- Only one canonical approved version exists per product.

## Milestone 4 — Bounded model and GitHub adapters

Deliverables:

- OpenAI-compatible provider abstraction selected by config.
- HTTPS/loopback policy, context cancellation, timeout, bounded retry, strict JSON Schema.
- Input/output/cost preflight and actual usage metadata.
- Configured/common secret redaction.
- Narrow GitHub repository/release/file/issue client with stable IDs and marker search.

Gate:

- Adapter tests cover success, rate/server retry, input/cost rejection before HTTP, malformed output, and secret redaction.
- No arbitrary response body or credential appears in errors/logs.
- Model has no tool interface.

## Milestone 5 — Release-to-marketing state machine

Deliverables:

- Same service used by manual and scheduled execution.
- Published release fetch, optional changelog, immutable evidence.
- Stable dedupe hash using immutable GitHub IDs.
- Primary `launch` plus supporting `copywriting`, `social`, `emails` skills.
- Marketability classification and no-action path.
- Strict channel/evidence/human-gate/terminology validation and self-check.
- Transactional run/claim/fencing/dedupe/cursor/error/audit state.

Gate:

- Low-value/maintenance event produces `no_action` with no assets or issue.
- Marketable event produces exactly five evidence-linked drafts.
- Invalid/hallucinated evidence IDs and forbidden terms fail or repair within bounds.
- Duplicate event and concurrent claims cannot duplicate work.
- Failure does not complete dedupe or advance cursor.

## Milestone 6 — Approval write-ahead and recovery

Deliverables:

- Structured GitHub Issue renderer with evidence, action, drafts, risks/warnings, cost, and review instructions.
- Durable `creating` approval intent and asset transaction before external write.
- Deterministic hidden marker and request hash.
- Product-local atomic approval/assets mirrors.
- Find-before-create and ambiguous-error recovery.

Gate:

- Crash/retry integration test converges to one remote issue.
- Run reaches `awaiting_approval` only after remote issue identity is known.
- Dedupe and cursor finalize only with the approval.
- No generated proposal executes automatically.

## Milestone 7 — Scheduler, controls, and observability

Deliverables:

- Cron scheduler with live enabled-definition reconciliation.
- Per-workflow context timeout and bounded retry.
- Durable global kill switch and explicit resume.
- Graceful cancellation/shutdown.
- Product/workflow/context/skills/approval/run/scheduler CLI.
- Structured JSON logs and machine-readable output.

Gate:

- Newly enabled workflow runs without daemon restart.
- Kill switch prevents every scheduled attempt/retry and preserves state.
- Preflight blocks/kills are durable run statuses.
- Manual and scheduled routes call the same runner.

## Milestone 8 — Quality and release verification

Deliverables:

- Unit, SQLite integration, mocked API, crash-recovery, E2E, and CLI tests.
- Full operator/developer/security documentation.
- Reproducible dependency graph and binary build.

Gate commands:

```sh
gofmt -w $(git ls-files '*.go')
go mod tidy
go test ./...
go vet ./...
go test -race ./...
go build -trimpath -o ./bin/marketing-os ./cmd/marketing-os
./bin/marketing-os --config ./config.example.yaml skills status
```

Also run a fresh temporary-config CLI smoke test for product registration, workflow default-disabled state, durable `stop-all`, and JSON output.

## Future milestones (not part of MVP)

### Human decision ingestion

- Authenticated GitHub webhook/polling of explicit approval state.
- Append-only transition history with external event dedupe.
- Still no execution until a separate reviewed Tier 2 design exists.

### PostgreSQL store

- Store interface plus PostgreSQL migrations.
- Contract suite shared with SQLite.
- PostgreSQL locking/lease semantics preserving dedupe/fencing invariants.

### Additional business events

Candidates only after durable event quality is proven: changelog merge, pricing change, case-study approval, meaningful milestone. Each must use the workflow checklist in `adding-a-workflow.md`; time-only “post every day” loops are rejected.

### Tier 2 execution

Out of scope until an explicit capability proposal defines per-channel scopes, approval identity, replay protection, dry-run parity, revocation/kill behavior, rate/spend limits, and separate credentials. Do not evolve approval issue creation into implicit publishing.
