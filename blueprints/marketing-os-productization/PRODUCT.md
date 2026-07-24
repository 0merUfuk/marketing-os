# Product Brief and Requirements

## Product brief

Marketing OS is a local-first, evidence-grounded workflow engine that turns a published software release into validated marketing drafts staged in a GitHub Issue for human review. Go owns the workflow and every side effect; an LLM is a bounded drafting component. The product does not publish, send, spend, purchase, or approve.

The current CLI is the self-hosted operator surface. The target mainstream surface is an installable GitHub App that removes clone, submodule, and scheduler setup friction while invoking the same Go workflow engine. GitHub Actions remains an optional power-user/self-hosted path.

Evidence: `README.md:3-22`, `GUIDE.md:33-57`, `docs/architecture.md:3-10`, and D-001, D-003, D-005, D-008 in [`DECISIONS.md`](DECISIONS.md).

## Target users

Priority order:

1. The project owner operating Marketing OS across personal products.
2. Indie hackers, OSS maintainers, and solo developers who publish GitHub Releases.
3. Small software teams that want repeatable release marketing without handing execution authority to an agent.
4. DevRel and product-marketing collaborators who can review GitHub Issues but may not operate a Go CLI.

The common job is:

> When I publish a meaningful software release, help me produce evidence-linked channel drafts quickly, show uncertainty and risk, and give me a familiar place to review them without taking action for me.

## Positioning

Marketing OS is not a social scheduler, autonomous agent, generic content generator, or ad/email execution platform. It is the control plane between durable product evidence and human-approved marketing work:

- evidence is captured before generation;
- product context is versioned and human-approved;
- skill guidance is pinned and reproducible;
- structured model output is deterministically validated;
- durable workflow state makes retries and remote writes auditable;
- the terminal product action is a reviewable GitHub Issue.

## Required outcomes

### Functional requirements

| ID | Requirement | Evidence / decision |
|---|---|---|
| FR-01 | Preserve the existing local CLI and scheduler as supported self-hosted hosts. | `internal/app/`, `internal/scheduler/`; D-001, D-005 |
| FR-02 | A future GitHub App host must accept only authenticated `release.published` events and dispatch them asynchronously to the shared release workflow. | D-004 |
| FR-03 | Manual, scheduled, Actions, and future webhook triggers must converge on the same Go workflow implementation. | `internal/workflows/release.go`; D-005, D-008 |
| FR-04 | Registration must create an isolated product and disabled workflow; no workflow runs until product context is explicitly approved. | `GUIDE.md:190-295`; D-007 |
| FR-05 | Release generation must use immutable same-product evidence and record selected context/skill/model metadata. | `docs/architecture.md`, `docs/database.md` |
| FR-06 | A marketable release may create one idempotently reconciled approval issue and exactly the validated draft set; a weak release must end as `no_action`. | `README.md:12-17`, `docs/implementation-plan.md:86-121` |
| FR-07 | All productized installs must use repo-vendored, manifest-verified marketing skills without requiring a Git submodule. | D-002, D-009; [`VENDORED_SKILLS.md`](VENDORED_SKILLS.md) |
| FR-08 | Skill updates must remain explicit, reviewable, pinned to an immutable upstream commit, and recorded with provenance and local-modification status. | `docs/skills.md`; D-009 |
| FR-09 | Self-hosted operation must retain SQLite and its existing dedupe, fencing, cursor, approval-intent, and audit invariants. | `docs/database.md`; D-006 |
| FR-10 | GitHub Actions must remain usable as a fallback without becoming a second workflow implementation. | D-008 |
| FR-11 | Operators must retain dry run, workflow enable/disable, durable kill switch, run inspection, and approval inspection controls. | `README.md`, `GUIDE.md`, `docs/cli.md` |
| FR-12 | Machine-readable CLI output and structured logs must remain stable enough for automation and CI smoke checks. | `docs/cli.md`, `Makefile` |

### Quality requirements

| ID | Requirement |
|---|---|
| QR-01 | Deterministic behavior owns triggers, state, retries, validation, dedupe, approvals, and external writes; model output never owns control flow. |
| QR-02 | Every filesystem and network input is bounded, path-contained where applicable, cancellable, and treated as untrusted. |
| QR-03 | No real credential value is stored in config, source, docs, tests, logs, evidence, issues, lock files, or CI. |
| QR-04 | Any skill content drift, missing required file, invalid path, or manifest mismatch blocks AI execution. |
| QR-05 | Any trigger replay, concurrent claim, crash, timeout, or ambiguous GitHub write converges without duplicate approval issues. |
| QR-06 | Local installation requires no JavaScript/TypeScript application runtime, submodule checkout, paid service, or cloud account. |
| QR-07 | Changes pass the complete local and CI gate in [`TESTING_AND_RELEASE.md`](TESTING_AND_RELEASE.md). |

## Explicit non-goals

The following are excluded even if technically adjacent:

- autonomous social publishing, email sending, ad spending, purchasing, outreach, content mutation, or approval;
- ingesting an approval comment and treating it as authority to execute a downstream action;
- a TypeScript SaaS rewrite, server-to-CLI subprocess architecture, or workflow rules duplicated in Actions;
- a generic event bus, multi-platform trigger framework, dashboard, or multi-tenant cloud launch;
- PostgreSQL for self-hosted users or multi-tenancy on SQLite;
- automatic context approval, hidden uncertainties, or release generation without approved context;
- automatic skill updates, execution of upstream scripts, or loading arbitrary third-party files;
- pricing, billing, entitlement, hosted-model brokerage, Marketplace publication, or public-repository conversion;
- changing product branding, module path, or credential configuration;
- rebuilding the existing release workflow, state machine, or approval idempotency.

## Product acceptance

A productization release is acceptable only when a clean clone can build and verify vendored skills without submodules; a first product is blocked until context approval; a fixture release can deterministically reach `no_action` or one `awaiting_approval` proposal; retries cannot duplicate that proposal; and no forbidden execution adapter exists.

Detailed gates are in [`TESTING_AND_RELEASE.md`](TESTING_AND_RELEASE.md). Safety requirements are normative in [`TRUST_AND_SAFETY.md`](TRUST_AND_SAFETY.md).
