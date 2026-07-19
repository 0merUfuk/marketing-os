# Security model

## Capability posture

Marketing OS is deny-by-default. The first version contains no adapters for publishing social posts, sending email, spending money, purchasing, approving, or editing production content. Configuration validation rejects any attempt to enable `publishing_enabled`, `sending_enabled`, or `spending_enabled`.

The only external write path is creation of a GitHub Issue in one configured approval repository. Generated assets remain proposals.

## Trust boundaries

Treat all of these as untrusted input:

- GitHub release names, bodies, tags, and repository files;
- website/docs/pricing/changelog responses;
- local repository documents;
- approved context text;
- upstream skill instructions/examples;
- model output and repaired model output.

Prompts delimit these sources as data and explicitly deny tool/state authority. The model client has no tool API. Go code owns all state transitions and external calls.

## Secrets

- Configuration stores environment-variable **names**, never secret values.
- GitHub/LLM credentials are read at runtime.
- API errors report status/category without copying arbitrary response bodies into logs.
- Model-bound prompts pass through deterministic redaction for configured credentials and common OpenAI/GitHub/bearer token patterns.
- Redaction is defense in depth, not a substitute for keeping credentials out of READMEs, release notes, context, and source pages.
- Product YAML, skill lock files, generated issues, and logs must never contain credentials.

Use separate narrowly scoped tokens. Rotate a token if evidence or output indicates accidental exposure; local SQLite/evidence may preserve the original source snapshot even though the model-bound copy was redacted.

## Filesystem safety

- Database parent directories are created with mode `0700`.
- Product IDs are validated slugs and cannot inject path separators.
- Workspace writes use temporary files plus rename and reject paths escaping the configured product root.
- Local source collection reads only an allowlist of public documentation filenames.
- Local source symlinks are canonicalized and must remain inside the configured repository.
- Agent Skill reference paths reject absolute paths, `..`, unsupported directories, oversized files, and escaping symlinks.
- Upstream tracked symlinks are accepted only when they resolve to regular files inside the pinned skills repository.
- Runtime product evidence/drafts/reports/approvals/state are ignored by default Git rules because they may be sensitive.

SQLite is not encrypted at rest. Rely on host disk encryption and account permissions, or add a reviewed encrypted-store deployment before handling data that requires application-level encryption.

## Network safety

- LLM and GitHub API base URLs require HTTPS, except explicit loopback HTTP for local development/tests.
- Product source pages require HTTPS, except loopback HTTP.
- Responses are size-bounded and transport calls have deadlines.
- Redirects are bounded and revalidated.
- Source URLs are operator-controlled configuration. Do not point them at sensitive internal HTTPS endpoints; the current version is not a general web crawler and does not perform a full DNS rebinding/private-address policy for HTTPS destinations.
- GitHub repository names must match `owner/repository`; an optional immutable repository ID detects replacement/rename mistakes.

## Prompt injection resistance

Defense is layered:

1. Only bounded, selected sources enter prompts.
2. System instructions state that context/evidence/skills are untrusted data.
3. The model cannot call tools.
4. Responses must match a strict JSON Schema with unknown fields rejected.
5. Evidence IDs must exist in the same product’s allowed set.
6. Required channels, human-approval flag, classification, marketability range, and no-action shape are deterministic.
7. Approved “Words to Avoid” are deterministic policy, not merely prompt prose.
8. Invalid output gets at most the configured bounded repair attempts; repair input is size-limited and treated as untrusted.
9. No validated model result directly executes an external marketing action.

This does not prove that marketing prose is factually perfect. Human review remains mandatory.

## Idempotent remote writes

A GitHub Issue request is stored as a durable intent before HTTP creation. Every issue body contains a deterministic hidden marker. After crashes, timeouts, or ambiguous server failures, retries search the approval repository by marker before creating. Request hashes and database uniqueness constraints provide additional detection.

The workflow never marks dedupe complete or advances its cursor until the issue identity is reconciled and final local transaction succeeds.

## Scheduler safety

- Workflows are disabled when products are registered.
- Unapproved context blocks execution before GitHub/model calls.
- A singleton durable kill switch is checked before each scheduled attempt and retry.
- `stop-all` preserves all state and records future rejected scheduled attempts as `killed` when product/workflow state exists.
- Per-workflow leases and fencing tokens prevent overlap and stale finalization.
- Context cancellation propagates to model/GitHub calls and graceful shutdown waits for active cron jobs.

## Data retention

Evidence and audit records are intentionally immutable/append-oriented for reproducibility. The first version has no automated retention or right-to-erasure workflow. Before using customer-identifiable or regulated data, define retention, deletion, backup, and access policies and implement them as reviewed deterministic operations.

## Reporting a security issue

Do not paste tokens, private evidence, database files, or full approval bodies into a public issue. Report the smallest reproducible description through the repository owner’s private security channel.
