# Trust and Safety Invariants

These invariants are release blockers. Productization may improve installation or hosting, but may not trade away the guarantees in `docs/security.md`, `docs/architecture.md`, or D-003.

## Capability boundary

1. The core may analyze evidence, generate drafts, persist state, and create/reconcile one GitHub approval issue.
2. The core has no publisher, sender, ad/spend, purchasing, production-edit, merge, release, or auto-approval adapter.
3. Every marketable run stops at `awaiting_approval`.
4. A human approval record is not permission to execute a marketing action.
5. Configuration must continue rejecting enabled publishing, sending, or spending capabilities.
6. The model has no tools and no direct state/network authority.

Any future execution plugin requires a separate owner-approved capability design. It is not an extension of this blueprint.

## Evidence and model boundary

- Treat releases, repository files, pages, local docs, approved context, skills, webhook bodies, and all model output as untrusted.
- Capture immutable same-product evidence before generation.
- Delimit untrusted data from system policy.
- Require strict JSON Schema with unknown fields rejected.
- Validate evidence IDs, channels, action, human-approval flag, marketability, no-action shape, terminology, token/output/cost limits, and required self-checks in Go.
- Bound repair attempts and treat invalid output as untrusted input.
- Never let model-supplied IDs, URLs, actions, or claims become authoritative without deterministic validation.

## Context and workflow gates

- Product workflows default disabled.
- Context drafts never auto-approve.
- Missing approved context blocks before GitHub/model calls.
- Vendored-skill manifest mismatch blocks before GitHub/model calls.
- The durable kill switch blocks every scheduled attempt/retry and survives restart.
- Dry run performs no workflow-domain persistence, dedupe/cursor mutation, asset write, approval write, or GitHub issue creation.

## Secret and privacy boundary

- Config, examples, lock files, docs, logs, issues, evidence, tests, and CI store only credential environment-variable names or obvious placeholders.
- Credentials are read at runtime, scoped narrowly, redacted from model prompts/errors/logs, and never printed by diagnostics.
- Do not commit real `config.yaml`, `.env`, tokens, keys, private databases, runtime product evidence/drafts, generated media, local config, or local-only symlinks.
- SQLite is not encrypted at rest; self-hosters rely on host controls until a separately reviewed encrypted-store mode exists.
- No hosted use involving customer-identifiable/regulated data is permitted before retention, deletion, backup, and access policies exist.

## Filesystem and dependency boundary

- Product IDs and skill names cannot inject paths.
- Reads/writes remain within canonical configured roots.
- Absolute paths, traversal, escaping/broken/directory symlinks, unsupported directories, and oversized files fail closed.
- Workspace writes remain atomic.
- Vendored skills are data: upstream scripts are not executed and binary assets are not sent to the model.
- Manifest inventory and hashes are deterministic; drift never produces a warning-only mode.

## Network and webhook boundary

- Non-loopback endpoints require HTTPS.
- Requests, responses, redirects, retries, and deadlines are bounded and cancellation-aware.
- Errors do not copy arbitrary response bodies.
- Repository identity uses immutable IDs where available.
- A future webhook host verifies signatures before parsing/dispatch, uses immutable delivery IDs, and accepts only the ratified event/action.
- Installation tokens are runtime-only, least-privilege, redacted, and never reused as general credentials.
- No database transaction spans an external network call.

## Durable state and remote-write boundary

- Claims, dedupe, and fencing remain transactional.
- Stale workers cannot finalize.
- Failure releases a claim without completing dedupe or advancing cursors.
- `no_action` and reconciled `awaiting_approval` finalize dedupe/cursor atomically.
- Approval request content and deterministic marker are stored before remote creation.
- Retries search by marker before create; ambiguous writes converge to one issue.
- Audit history is append-oriented and records actor/trigger/model/skill/context versions as applicable.

## Review checklist

- [ ] No new forbidden adapter, capability flag, or external mutation exists.
- [ ] Context and workflow default-disabled gates remain.
- [ ] Model has no tool/state authority.
- [ ] Same-product evidence and schema/policy validation remain deterministic.
- [ ] Skill verification fails closed.
- [ ] Secrets and private artifacts are absent from diff and CI output.
- [ ] Path, size, URL, timeout, retry, cancellation, and redaction tests pass.
- [ ] Dedupe/fencing/cursor/approval recovery tests pass.
- [ ] GitHub issue text describes proposals, never actions already executed.
- [ ] Owner-only public/cloud/pricing/legal/branding/credential gates were not crossed.

## Stop conditions

Stop for owner review if implementation would require a broader GitHub permission, persistent App credential, new external write, legal-notice scheme beyond Q-LEGAL-1's default, paid/cloud resource, public flip, pricing/entitlement, hosted model broker, branding change, or deletion of ambiguous artifacts.
