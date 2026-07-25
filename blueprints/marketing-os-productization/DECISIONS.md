# Marketing OS Productization — DECISIONS.md (Living Ledger)

**Created**: 2026-07-21
**Last Updated**: 2026-07-26
**Owner**: Ömer Ufuk Boz
**Status legend**: RATIFIED (binding) · CANDIDATE (proposed, awaiting ruling) · OPEN (owner + validation method + fail-closed default) · SUPERSEDED (kept, banner names successor)

> **Method:** this ledger is created BEFORE any spec. Specs are assembled FROM tracked artifacts — nothing chat-only. Append-only; supersession is recorded, never rewritten. The owner's four moves on any candidate: ratify / amend / reject / keep-open.

---

## D-001 — RATIFIED 2026-07-21 — Product model: open-source self-hosted core + installable GitHub App
**Reversibility**: 🟡 YELLOW

Marketing OS will remain an open-source self-hosted core, but the community-facing install path will be a GitHub App that hides CLI setup and triggers workflows from repository events.

**Why:** The current CLI is useful for technical operators but too manual for broad adoption. GitHub is where the target users already publish releases, review issues, and manage repositories.

**Ruled out for v1:**
- Pure SaaS before self-hosted installation is trustworthy.
- CLI-only community distribution.
- Consulting-only onboarding.

**Evidence:** research/00-synthesis.md; ADR-001; current GUIDE.md setup friction.

---

## D-002 — RATIFIED 2026-07-21 — Skills distribution: vendor selected skills, remove submodule from install path
**Reversibility**: 🟡 YELLOW

Marketing OS will vendor the exact marketing skill content it uses into the repo, preserve MIT attribution in `THIRD_PARTY_NOTICES.md`, and remove `.skills/marketingskills` as a required submodule for normal users.

**Why:** Submodules create clone/setup/CI friction and confuse users. Vendoring MIT-licensed content is legally allowed when the copyright and permission notice are preserved.

**Ruled out for v1:**
- Requiring every user to clone with `--recurse-submodules`.
- Copying third-party content without explicit notice.
- Rewriting upstream text as if it were originally authored by this project.

**Evidence:** upstream MIT license in `.skills/marketingskills/LICENSE`; ADR-002.

---

## D-003 — RATIFIED 2026-07-21 — Trust boundary: human approval remains non-negotiable
**Reversibility**: 🟢 GREEN

Marketing OS will continue to stop at `awaiting_approval` for the productized version. Publishing/sending/spending is a future post-approval plugin layer, not part of the core trigger-to-draft path.

**Why:** The most defensible product promise is safe, evidence-grounded draft staging that cannot embarrass the user by publishing autonomously.

**Ruled out for v1:**
- Auto-posting to LinkedIn/X.
- Auto-sending emails.
- Auto-approving marketing copy from model output.

**Evidence:** docs/security.md; ADR-003.

---

## D-004 — RATIFIED 2026-07-21 — Workflow trigger: GitHub release webhooks before polling
**Reversibility**: 🟢 GREEN

The first one-click integration will listen to `release.published` webhooks from a GitHub App and trigger `release-to-marketing` asynchronously.

**Why:** Webhooks reduce latency, avoid polling quotas, and map directly to the current workflow's stable release identity.

**Ruled out for v1:**
- Generic event bus.
- Multi-platform trigger system.
- Cron-only product experience.

**Evidence:** docs/architecture.md release flow; ADR-004.

---

## D-005 — RATIFIED 2026-07-21 — Deployment modes: same engine, two hosts
**Reversibility**: 🟡 YELLOW

The same Go workflow engine will support two host shells: local/self-hosted binary and web/server GitHub App receiver. The business logic must remain shared rather than duplicated.

**Why:** The CLI is already verified. Productization should wrap the engine, not fork workflow semantics.

**Ruled out for v1:**
- Separate SaaS rewrite in TypeScript.
- Calling the CLI binary from the web server as the main architecture.
- Duplicating release workflow rules in GitHub Actions YAML.

**Evidence:** docs/architecture.md component boundaries; ADR-005.

---

## D-006 — RATIFIED 2026-07-21 — Storage path: SQLite for self-hosted, PostgreSQL seam for cloud
**Reversibility**: 🟡 YELLOW

Self-hosted Marketing OS keeps SQLite. Managed/cloud mode introduces a storage interface and PostgreSQL adapter before multi-tenant release.

**Why:** SQLite is excellent for local-first but wrong for multi-tenant hosted GitHub App installs. The correct move is an adapter seam, not premature cloud rewrite.

**Ruled out for v1:**
- Forcing PostgreSQL on self-hosted users.
- Shipping multi-tenant cloud on SQLite.
- Migrating before the GitHub App proves adoption.

**Evidence:** docs/database.md; ADR-006.

---

## D-007 — RATIFIED 2026-07-21 — Onboarding UX: context review is the first success moment
**Reversibility**: 🟢 GREEN

One-click install must guide the user to a generated product context draft and ask for approval before any release workflow can run.

**Why:** Product context is the safety and quality foundation. The first moment of value should show the user that the system understands their repo without inventing facts.

**Ruled out for v1:**
- Running release generation without context.
- Auto-approving repo-inferred product context.
- Hiding unsupported/uncertain facts.

**Evidence:** GUIDE.md context flow; ADR-007.

---

## D-008 — RATIFIED 2026-07-21 — GitHub Actions remains a fallback, not the primary product
**Reversibility**: 🟢 GREEN

The reusable GitHub Action remains available for power users and self-hosted runners, but the mainstream installation path becomes the GitHub App.

**Why:** GitHub Actions is useful for your own projects, but self-hosted runner setup is too much friction for community adoption.

**Ruled out for v1:**
- Requiring every community user to run a self-hosted runner.
- Removing the existing reusable workflow.

**Evidence:** current `.github` reusable workflow; ADR-008.

---

## D-009 — RATIFIED 2026-07-21 — License posture: third-party notices are a first-class artifact
**Reversibility**: 🟢 GREEN

Vendored skills must be accompanied by `THIRD_PARTY_NOTICES.md`, upstream commit/version metadata, and local modification notes.

**Why:** MIT allows redistribution but attribution must be explicit. Community trust improves when provenance is visible.

**Ruled out for v1:**
- Silent copying.
- Removing upstream copyright notices.
- Mixing upstream and local skill text without a modification ledger.

**Evidence:** upstream MIT license; ADR-009.

---

## D-010 — CANDIDATE 2026-07-21 — Managed cloud pricing: free public repos, paid private/team tier
**Reversibility**: 🟢 GREEN

Candidate model: self-hosted remains free; hosted cloud is free for public repos with rate limits, paid for private repos, team approvals, analytics, and managed model broker.

**Why:** Public repos create visibility; private/team use captures willingness to pay. This mirrors the adoption path of many developer tools.

**Ruled out for v1:**
- Charging for local CLI.
- Blocking public OSS users.
- Usage-based billing before cost accounting is verified.

**Evidence:** research/00-synthesis.md; OPEN Q-PRICING-1.

---

## OPEN ITEMS QUEUE

| ID | Item | Owner | Validation method | Fail-closed default | Status |
|---|---|---|---|---|---|
| Q-SKILLS-1 | Which upstream skills beyond the five required should be vendored for v0.2? | Ufuk | Audit workflow prompts + `skills.Load` calls + future roadmap | Vendor only `product-marketing`, `launch`, `copywriting`, `social`, `emails` | OPEN |
| Q-LEGAL-1 | Is centralized `THIRD_PARTY_NOTICES.md` enough, or should each vendored skill directory include a short NOTICE? | Ufuk | Review MIT license practice and GitHub community expectations | Include centralized notice + keep original upstream LICENSE copy under `third_party/marketingskills/LICENSE` | OPEN |
| Q-CLOUD-1 | First cloud host: Railway, Fly.io, Render, or Vercel+worker split? | Ufuk | Compare webhook latency, persistent storage, pricing, background worker support | Keep self-hosted only; design cloud seam but do not launch hosted tier | OPEN |
| Q-AUTH-1 | GitHub App install flow: only GitHub auth or also email/password for non-GitHub collaborators? | Ufuk | Interview 3 target users; test approval flow with repo owners vs marketers | GitHub-only auth in v0.2 | OPEN |
| Q-MODEL-1 | BYOK only, hosted model broker, or both? | Ufuk | Cost model + user trust survey + run 20 sample releases | BYOK only in v0.2 | OPEN |
| Q-DASHBOARD-1 | Do we build dashboard in Go templates, Next.js, or HTMX? | Ufuk | Prototype screens from `claude-design-prompt.md`; compare speed and maintainability | Go server-rendered HTML/HTMX for v0.2 | OPEN |

---

## CHANGELOG

- 2026-07-21 — Ledger created before spec. D-001 through D-009 RATIFIED based on owner direction and current product state. D-010 held as CANDIDATE pending pricing validation. Six OPEN items queued with fail-closed defaults.
- 2026-07-25 — Implementation constraint under Q-LEGAL-1: use `third_party/marketingskills/` instead of placing the skills beneath Go's reserved top-level `vendor/` directory, which may activate module vendor mode. This is only a technical destination-path substitution. The root `THIRD_PARTY_NOTICES.md` plus unmodified upstream `third_party/marketingskills/LICENSE` default is preserved, Q-LEGAL-1 remains OPEN, and notice granularity is not ratified by this entry.
