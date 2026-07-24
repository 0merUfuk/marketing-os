# Marketing OS Productization Blueprint

**Status:** implementation-ready within the owner-approved productization boundary

**Decision authority:** [`DECISIONS.md`](DECISIONS.md)

**Scope:** vendored skills, community-ready self-hosting, and the architecture seam for a future GitHub App

**Out of scope:** GitHub App registration/deployment, cloud selection, pricing, Marketplace publication, branding changes, credentials, and any automatic publishing/sending/spending/approval

This blueprint translates ratified decisions D-001 through D-009 into buildable requirements and acceptance gates. It does not amend or supersede the decision ledger. D-010 remains a candidate.

## Source hierarchy

When sources conflict, use this order:

1. [`DECISIONS.md`](DECISIONS.md) for ratified product decisions and owner-only questions.
2. Current code and committed repository docs for implemented behavior.
3. This blueprint for the approved implementation sequence and target seams.
4. Handoff material under `tasks/` for execution context; those files are local orchestration artifacts, not product requirements.

Important current evidence:

- `README.md` defines the product promise and explicit non-goals.
- `GUIDE.md` documents the context-first operator flow and the `awaiting_approval` stopping point.
- `docs/architecture.md` defines deterministic orchestration, bounded AI, and component boundaries.
- `docs/security.md` defines the deny-by-default capability and data-safety model.
- `docs/skills.md` and `skills.lock.yaml` define current skill selection and reproducibility.
- `internal/workflows/release.go` contains the shared release workflow service.
- `internal/productcontext/service.go` contains draft and human-approval services.
- `internal/skills/loader.go` and `internal/skills/lock.go` enforce bounded loading and manifest verification.
- `Makefile` and `CONTRIBUTING.md` define the local verification contract.

## Blueprint map

| Document | Purpose |
|---|---|
| [`PRODUCT.md`](PRODUCT.md) | Product brief, target users, positioning, requirements, and non-goals |
| [`ARCHITECTURE.md`](ARCHITECTURE.md) | Shared Go core, current CLI host, and future GitHub App host seam |
| [`VENDORED_SKILLS.md`](VENDORED_SKILLS.md) | Option A migration, exact provenance contract, and attribution requirements |
| [`ONBOARDING.md`](ONBOARDING.md) | Context-first installation and first-success flow |
| [`TRUST_AND_SAFETY.md`](TRUST_AND_SAFETY.md) | Non-negotiable runtime, data, model, and side-effect invariants |
| [`TESTING_AND_RELEASE.md`](TESTING_AND_RELEASE.md) | Implementation phases, acceptance gates, CI, and release readiness |
| [`DECISIONS.md`](DECISIONS.md) | Append-only decision ledger; not rewritten by this blueprint |

## Decision traceability

| Decision | Blueprint implementation |
|---|---|
| D-001 | [`PRODUCT.md`](PRODUCT.md) and the two-host model in [`ARCHITECTURE.md`](ARCHITECTURE.md) |
| D-002 | [`VENDORED_SKILLS.md`](VENDORED_SKILLS.md) |
| D-003 | [`TRUST_AND_SAFETY.md`](TRUST_AND_SAFETY.md) |
| D-004 | Asynchronous `release.published` target flow in [`ARCHITECTURE.md`](ARCHITECTURE.md) |
| D-005 | One shared workflow engine, two thin hosts in [`ARCHITECTURE.md`](ARCHITECTURE.md) |
| D-006 | SQLite now; PostgreSQL only behind a future contract-tested seam in [`ARCHITECTURE.md`](ARCHITECTURE.md) |
| D-007 | [`ONBOARDING.md`](ONBOARDING.md) |
| D-008 | GitHub Actions remains a fallback in [`PRODUCT.md`](PRODUCT.md) and [`ARCHITECTURE.md`](ARCHITECTURE.md) |
| D-009 | Attribution and modification ledger in [`VENDORED_SKILLS.md`](VENDORED_SKILLS.md) |
| D-010 | Candidate only; excluded from implementation and release gates |

## Execution order

1. Preserve or safely exclude local-only artifacts; do not delete ambiguous owner files.
2. Complete and review this blueprint against D-001 through D-009.
3. Execute the vendored-skills migration and its documentation/test changes.
4. Add minimal secret-free CI if this repository still lacks it.
5. Stop at the documented GitHub App seam unless a separate owner-approved implementation handoff exists.
6. Run every gate in [`TESTING_AND_RELEASE.md`](TESTING_AND_RELEASE.md), then commit and push only intentional files.

No phase may weaken a safety invariant to make a later phase pass.

## Open owner questions and active defaults

These remain OPEN in [`DECISIONS.md`](DECISIONS.md). Defaults are temporary, fail-closed implementation bounds, not ratifications.

| ID | Active fail-closed default |
|---|---|
| Q-SKILLS-1 | Vendor only `product-marketing`, `launch`, `copywriting`, `social`, and `emails`. |
| Q-LEGAL-1 | Add root `THIRD_PARTY_NOTICES.md` and retain the upstream license at `third_party/marketingskills/LICENSE`. |
| Q-CLOUD-1 | Remain self-hosted; define seams but launch no hosted tier. |
| Q-AUTH-1 | GitHub-only authentication for the future v0.2 App path. |
| Q-MODEL-1 | BYOK only; do not create a hosted model broker. |
| Q-DASHBOARD-1 | If a local prototype is separately approved, prefer Go server-rendered HTML/HTMX; no dashboard is required by this blueprint. |

Pricing has no ratified decision. D-010 stays CANDIDATE and no pricing behavior, copy, entitlement, or billing integration belongs in this work.

## Owner-only gates

Stop and request owner approval before:

- making the repository public or publishing to GitHub Marketplace;
- registering an App, changing credentials, or provisioning cloud/paid resources;
- choosing pricing, cloud, legal-notice strategy beyond Q-LEGAL-1's default, or hosted-model policy;
- changing the Marketing OS name, brand, or module path;
- adding publishing, sending, spending, purchasing, or automatic approval;
- deleting ambiguous local artifacts or committing private config, generated media, credentials, or local-only symlinks.

## Blueprint completion gate

The blueprint is complete when:

- every D-001 through D-009 decision maps to a requirement and a testable gate;
- D-010 and all OPEN items remain visibly unresolved;
- current and target architecture are distinguished;
- the vendoring file/provenance contract is deterministic;
- context approval and `awaiting_approval` remain hard boundaries;
- implementation phases name concrete repository evidence and verification commands.
