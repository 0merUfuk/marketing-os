# GitHub App Implementation Plan

**Status:** implementation-ready plan; App code, registration, credentials, hosting, and deployment are not authorized by this document

**Ratified constraints traced:** D-001, D-003, D-004, D-005, D-006, D-007, and D-008

**Primary safety rule:** a GitHub App delivery may cause evidence collection, bounded draft generation, and creation/reconciliation of one human-review issue. It may not publish, send, spend, purchase, merge, release, or approve.

This plan turns the target seam in [`ARCHITECTURE.md`](ARCHITECTURE.md) into implementation slices. It intentionally stops before code because App identity, credentials, public exposure, host/cost, user authentication, model-secret handling, and production data retention are owner-gated.

## Current state and target state

### Implemented now

- `internal/workflows.ReleaseWorkflow.Run` is the shared in-process release workflow. `RunOptions` already carries `TriggerType`, an immutable `ReleaseID`, and `DryRun`.
- `internal/scheduler.Runner` proves the workflow can be called through an interface rather than through Cobra.
- `internal/app/runtime.go` composes the SQLite store, workspace, vendored skill loader, model client, GitHub client, and release workflow for the CLI.
- `internal/state` owns SQLite migrations, workflow enablement, context approval, leases, fencing, release dedupe, cursor updates, approval intent, issue reconciliation, and append-oriented audit events.
- `internal/github.Client` uses a long-lived configured token and one client for repository reads and approval-issue operations.
- A product may have an immutable GitHub repository ID, but CLI registration does not require it.
- `SetWorkflowEnabled` changes workflow state without a distinct authenticated enable/disable audit record.
- There is no HTTP host, webhook verifier, delivery inbox, installation/repository authorization store, GitHub App token minter, lifecycle handler, or production retention policy.

### Target after a separately approved implementation

- A thin Go HTTP host authenticates and durably records bounded GitHub webhook metadata, then returns `202 Accepted` within GitHub's deadline.
- A durable dispatcher invokes the existing workflow in-process with `TriggerType: "github_app_webhook"` and the recorded release ID. It never shells out to the CLI.
- Installation and repository access are tracked by immutable provider IDs and checked before each job.
- Installation tokens are minted just in time for exactly the required repository and permission set, discarded after the job, and never stored or logged.
- Context approval and workflow enablement remain separate authenticated and authorized human actions.
- Pre-readiness releases become terminal `blocked_precondition` deliveries and never spring into execution after later onboarding.
- The only GitHub write remains approval-issue creation/reconciliation. Every marketable run still stops at `awaiting_approval`.

## GitHub.com platform contract

Implementation must be checked against current GitHub.com documentation again when code starts. The constraints used for this plan are:

| Constraint | Implementation consequence | Official source |
|---|---|---|
| `X-Hub-Signature-256` is an HMAC-SHA256 digest of the unmodified request body, and comparison must be timing-safe. | Read bounded raw bytes once; authenticate those exact bytes with `crypto/hmac` and `hmac.Equal` before JSON decoding. | [Validating webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries) |
| GitHub expects a `2XX` response within 10 seconds and treats a slower response as a failed delivery. | Use an internal receipt deadline no greater than 8 seconds, commit the receipt before `202`, and do all workflow work asynchronously. | [Handling webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/handling-webhook-deliveries) |
| GitHub does not automatically redeliver failed deliveries, and only deliveries from the past three days can be redelivered. | Make the committed inbox durable, and require a failed-ingress monitor plus a bounded App-delivery listing/redelivery runbook or reconciler before deployment. Internal retry alone cannot recover a request that was never committed. | [Handling failed webhook deliveries](https://docs.github.com/en/webhooks/using-webhooks/handling-failed-webhook-deliveries) and [Redelivering webhooks](https://docs.github.com/en/webhooks/testing-and-troubleshooting-webhooks/redelivering-webhooks) |
| `X-GitHub-Delivery` is a globally unique delivery GUID; `X-GitHub-Event` identifies the event. GitHub payloads are capped at 25 MB. | Treat delivery IDs as opaque immutable keys, validate event/body agreement, and impose a smaller local request limit. | [Webhook delivery headers and payload cap](https://docs.github.com/en/webhooks/webhook-events-and-payloads#about-webhook-events-and-payloads) |
| A GitHub App needs Contents read permission to subscribe to `release`. | Register only the `release` subscription and request Contents read. Dispatch only the `published` action. | [`release` webhook](https://docs.github.com/en/webhooks/webhook-events-and-payloads#release) |
| All GitHub Apps receive `installation` and `installation_repositories` by default; neither can be manually subscribed. Apps also receive `github_app_authorization` by default and cannot unsubscribe. | The handler must support installation/repository lifecycle actions. If user authorization is later enabled, `github_app_authorization.revoked` must invalidate that user's authorization/session state. | [`installation`](https://docs.github.com/en/webhooks/webhook-events-and-payloads#installation), [`installation_repositories`](https://docs.github.com/en/webhooks/webhook-events-and-payloads#installation_repositories), and [`github_app_authorization`](https://docs.github.com/en/webhooks/webhook-events-and-payloads#github_app_authorization) |
| GitHub sends `ping` when a webhook is created. | Authenticate and durably mark `ping` complete without dispatching a workflow. | [`ping` webhook](https://docs.github.com/en/webhooks/webhook-events-and-payloads#ping) |
| User- and organization-installation access tokens expire after one hour and can be narrowed with `repository_ids` and `permissions`. Enterprise installations do not provide repository access and cannot be narrowed this way. | Mint per-job tokens for exact repository IDs and the minimum permission subset; never persist them. Reject enterprise-target installations for this repository workflow. | [Authenticating as a GitHub App installation](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/authenticating-as-a-github-app-installation) and [Installing enterprise apps](https://docs.github.com/en/enterprise-cloud@latest/enterprise-onboarding/github-apps/install-enterprise-apps) |
| App JWTs use RS256 and may expire no more than 10 minutes in the future. | Generate a short-lived JWT only when minting an installation token; do not cache or persist the JWT. | [Generating a GitHub App JWT](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-json-web-token-jwt-for-a-github-app) |
| Creating an issue requires Issues write permission; reading repository content requires Contents read. | Register only Metadata read, Contents read, and Issues write; use narrower per-operation installation tokens. | [Create an issue](https://docs.github.com/en/rest/issues/issues#create-an-issue) and [Get repository content](https://docs.github.com/en/rest/repos/contents#get-repository-content) |
| GitHub recommends minimum App permissions and allows installation tokens to be narrowed further. | Any broader permission is an owner stop condition, not an implementation convenience. | [Choosing App permissions](https://docs.github.com/en/apps/creating-github-apps/registering-a-github-app/choosing-permissions-for-a-github-app) and [GitHub App best practices](https://docs.github.com/en/apps/creating-github-apps/about-creating-github-apps/best-practices-for-creating-a-github-app) |
| REST API clients should send an explicit API version and GitHub media type. | Centralize `Accept: application/vnd.github+json`, an owner-reviewed `X-GitHub-Api-Version` value, and a Marketing OS `User-Agent`; do not rely on an implicit version. | [REST API versions](https://docs.github.com/en/rest/about-the-rest-api/api-versions) and [Getting started with REST](https://docs.github.com/en/rest/using-the-rest-api/getting-started-with-the-rest-api) |

GitHub signs the body, not the delivery/event headers. Therefore a header must never authorize a product or repository by itself. The typed body must contain the expected positive installation, repository, and release IDs; its shape must agree with `X-GitHub-Event`; authorization must come from the stored installation/repository relationship; and release/business dedupe must remain in force even if a valid body is replayed with altered headers.

## Proposed Go package and service seams

Package names are implementation targets, not permission to duplicate workflow logic.

| Package / file | Responsibility | Must not do |
|---|---|---|
| `internal/apphost/server.go` | HTTP server lifecycle, routes, timeouts, health/readiness, dependency wiring | Implement workflow rules or hold raw payloads |
| `internal/webhook/verify.go` | Body limit, signature parsing, HMAC-SHA256 verification, header bounds | Parse business fields before authentication |
| `internal/webhook/github.go` | Typed minimal GitHub envelopes and event/action validation | Trust names instead of immutable IDs or retain arbitrary payload fields |
| `internal/delivery/service.go` | Receipt acceptance, dedupe outcome, state machine, explicit retry authorization | Make network/model calls |
| `internal/delivery/worker.go` | Due-delivery claim, lease renewal, retry classification, dispatch | Implement evidence/prompt/approval semantics |
| `internal/githubapp/installations.go` | Installation/repository lifecycle, product binding, and fail-closed target-type checks | Auto-create context, auto-enable a workflow, accept enterprise targets, or reactivate on `unsuspend` |
| `internal/githubapp/authorization.go` | Current user/installation/repository authority checks and revocation handling | Treat an authenticated login as authorization |
| `internal/githubapp/token.go` | Short-lived App JWT and scoped installation-token minting | Persist/cache/log credentials or broaden requested scope |
| `internal/githubapp/client.go` | Composite read/issue adapter satisfying `workflows.GitHubAPI` | Expose generic arbitrary writes |
| `internal/state/github_app.go` | SQLite implementation of receipt, lifecycle, authorization, lease, and audit transactions | Hold a transaction open across a network call |
| `internal/workflows/release.go` | Existing release business workflow and authoritative readiness checks | Learn HTTP/webhook transport semantics |

The shared workflow interface should move to or be declared in `internal/workflows` so scheduler and dispatcher use the same contract:

```go
type Runner interface {
	Run(context.Context, string, RunOptions) (RunOutcome, error)
}

type ScopedRunnerFactory interface {
	ForInstallation(
		context.Context,
		int64, // installation ID
		int64, // source repository ID
		int64, // approval repository ID
	) (Runner, error)
}
```

`ScopedRunnerFactory` composes a job-specific `ReleaseWorkflow` with shared store, skills, model, workspace, and a scoped GitHub adapter. The CLI may continue to compose a runner with its configured token. The App host must never invoke Cobra or execute the binary as a subprocess.

Extract or expose one store/skill-only readiness checker that `ReleaseWorkflow.Run` and the dispatcher both use. The workflow must still repeat the check at execution time; the dispatcher preflight exists to avoid minting tokens or starting external work for a delivery that is already blocked.

## Webhook ingress contract

### Server limits

- Expose one exact `POST` route. Return `405` for other methods.
- Require `Content-Type: application/json`; return `415` otherwise.
- Set `http.Server` header/read/write/idle timeouts and `MaxHeaderBytes`; do not use default unlimited server settings.
- Default `max_webhook_body_bytes` to 2 MiB with a 4 MiB hard configuration ceiling. GitHub's platform cap is 25 MB, but Marketing OS needs only bounded release/lifecycle identity fields. Increasing the hard ceiling requires security review.
- Wrap the body with `http.MaxBytesReader`, read it once, and return `413` on overflow.
- Give authentication, decoding, and receipt commit a deadline no greater than 8 seconds, leaving margin under GitHub's 10-second response deadline.
- Apply bounded concurrent-handler admission. If the service cannot durably accept work, return `503`; never acknowledge an in-memory-only queue.

### Authentication and normalization order

The handler order is fixed:

1. Apply method, content-type, header, body-size, and handler-deadline limits.
2. Read the exact raw body bytes once.
3. Require one `X-Hub-Signature-256` value in exact `sha256=` plus 64-hex form.
4. Compute HMAC-SHA256 over the unchanged bytes with the runtime webhook secret. Decode both digests and compare with `hmac.Equal`; never use ordinary string equality.
5. Compute a separate SHA-256 payload hash for durable conflict detection.
6. Validate nonblank bounded `X-GitHub-Delivery` and `X-GitHub-Event`. Treat both as opaque untrusted headers.
7. Decode only the minimal typed envelope for the declared event. Unknown JSON fields remain ignored for GitHub forward compatibility, but all required identity/action fields are validated explicitly.
8. Require positive installation/repository/release IDs where the event contract needs them, a bounded `owner/repo` display name, and exact event/body shape agreement.
9. Convert the authenticated payload to a minimal receipt; discard the raw bytes after the transaction.
10. Commit the receipt/lifecycle transaction. Only then return `202`.
11. Signal the worker as a best-effort latency optimization. The worker must poll durable state, so a lost signal cannot lose work.

Do not log the body, signature, secret, authorization header, token, JWT, private key, release text, or arbitrary decode error fragments.

### Response and dedupe behavior

| Condition | Response | Durable behavior |
|---|---:|---|
| New authenticated supported delivery committed | `202` | Store minimal receipt; queue only `release.published` |
| Redelivery with same delivery ID and same payload hash/identity | `202` | Return existing disposition; do not enqueue again |
| Same delivery ID with a different hash or immutable identity | `409` | Preserve original row, increment a conflict metric, no dispatch |
| Authenticated `release` action other than `published` | `202` | Store `ignored/unsupported_action`; never dispatch |
| Authenticated unknown event caused by registration drift | `202` | Store `ignored/unsupported_event`; alert on scope drift, no dispatch |
| Authenticated `ping` | `202` | Store terminal `succeeded`, no worker/model/GitHub API call |
| Missing/bad signature | `401` | No receipt and no detailed response |
| Oversized body | `413` | No receipt |
| Invalid content type or malformed required identity | `415` / `422` | No accepted receipt |
| Receipt commit unavailable or deadline exceeded | `503` | No success acknowledgement |

The primary unique key is `X-GitHub-Delivery`. Store the payload hash and normalized immutable identity with it. The service may derive a transport fingerprint from event, action, installation ID, repository ID, release ID, and payload hash to flag a captured signed body replayed under a new header; it need not persist another payload-derived field. This is defense in depth: existing release dedupe and idempotent lifecycle transitions remain authoritative.

### Failed-ingress recovery

A `503`, timeout, network failure, or admission rejection means no durable receipt exists, so the internal worker cannot recover that delivery. Before deployment, the owner must approve and operators must test one of these bounded recovery modes:

- a monitored runbook that queries the GitHub App webhook-delivery API at least daily and redelivers reviewed transient failures; or
- a bounded reconciler that lists only the recent failure window, classifies transient failures, caps redelivery attempts, and records a recovery cursor/audit without fetching or retaining payload bodies.

Every locally observed non-2xx response increments an alerting metric, but that signal is insufficient because a request may never reach the process. The recovery check must therefore use GitHub's App-level delivery list. It authenticates with an App JWT, not a broad user or installation token. It may automatically redeliver only owner-approved transient classes; signature/validation 4xx responses, unknown scope, and repeated failures require operator review. The implementation gate must simulate an uncommitted `release.published` delivery and prove it is detected and safely redelivered within GitHub's three-day window. See [GitHub App webhook delivery endpoints](https://docs.github.com/en/rest/apps/webhooks).

## Event and lifecycle matrix

The App registration manually subscribes only to `release`. `installation`, `installation_repositories`, and `github_app_authorization` arrive automatically, and `ping` is a configuration probe.

| Event | Action | Durable result | Side-effect rule |
|---|---|---|---|
| `release` | `published` | Queue one receipt with exact installation, repository, and release IDs | Async dispatcher only; never process another release action |
| `release` | anything else | `ignored/unsupported_action` | No model, token, workflow, or issue call |
| `installation` | `created` | Installation and repository access become `pending_onboarding` / `pending_recheck` | No product/context/workflow auto-creation or enablement |
| `installation` | `suspend` | Installation becomes `suspended`; mapped workflows are atomically disabled | Cancel matching workers best-effort; no automatic recovery |
| `installation` | `unsuspend` | Installation becomes `pending_recheck`; workflows stay disabled | An authorized actor must recheck readiness and enable explicitly |
| `installation` | `deleted` | Installation becomes terminal `deleted`; repository access is revoked; mapped workflows are disabled | Never mint another token; retain audit/dedupe metadata per policy |
| `installation` | `new_permissions_accepted` | Mark `pending_recheck` and compare observed permissions with the approved set | Never treat broader permission acceptance as workflow enablement |
| `installation_repositories` | `added` | Repository becomes `pending_recheck` | No product auto-creation or workflow enablement |
| `installation_repositories` | `removed` | Repository becomes `revoked`; mapped workflow is atomically disabled | Queued work becomes `blocked_precondition` |
| `github_app_authorization` | `revoked` | Invalidate any matching user authorization, refresh token, and session state once user auth exists | Does not uninstall or reactivate an installation; no workflow dispatch |
| `ping` | none | Terminal receipt | No dispatch |

Lifecycle action strings must be re-verified against the current GitHub webhook schema when implementation begins. Unknown actions are recorded as ignored scope drift and may never activate access.

Transitions are fail-closed and order-aware:

- `deleted` cannot be reactivated by a late `created`, `unsuspend`, or repository-added delivery.
- `suspended` cannot be made active by a duplicate `created`.
- `unsuspend`, repository-added, and accepted-permission events lead only to `pending_recheck`.
- Repository removal and installation suspend/delete atomically disable the mapped workflow and append an audit event.
- Rechecks query current authenticated GitHub installation/repository state; they do not trust a stale webhook name or array.
- The current `SetWorkflowEnabled` path must gain an atomic actor/reason audit variant before App enablement is exposed.

## Durable data model

Add one append-only migration, expected to be `migrations/005_github_app.sql`. Do not modify already-applied migrations.

### `github_app_installations`

- `installation_id INTEGER PRIMARY KEY CHECK (installation_id > 0)`
- `account_id INTEGER NOT NULL CHECK (account_id > 0)`
- bounded account display name and target type for operator display only; reject `Enterprise` for this repository workflow
- `status` constrained to `pending_onboarding`, `active`, `pending_recheck`, `suspended`, or `deleted`
- timestamps for first seen, last seen, disabled, and rechecked
- approved permission-set fingerprint and last observed permission-set fingerprint; never token/key material

### `github_app_repositories`

- composite primary key `(installation_id, repository_id)`
- current bounded `full_name` as display/routing metadata, always bound to the immutable repository ID
- access state constrained to `pending_recheck`, `granted`, or `revoked`
- first-seen, last-seen, rechecked, and revoked timestamps
- foreign key to installation without cascade-deleting audit history

### `github_app_product_bindings`

- a provisional v0.2 fail-closed limit of one active binding from `(installation_id, repository_id)` to one `product_id`
- optional separately authorized approval-repository ID
- binding status, authorized actor's immutable GitHub user ID, created/updated timestamps
- foreign keys that preserve history with explicit disable/revoke behavior rather than silent cascades
- uniqueness that prevents one immutable repository from being silently mapped to multiple products

Current CLI storage permits multiple products to reference one repository. The App uniqueness rule is not a ratified product-topology decision: it prevents ambiguous webhook routing until the owner either accepts the v0.2 limitation or approves deterministic multi-product routing for monorepos.

### `webhook_deliveries`

- opaque `delivery_id TEXT PRIMARY KEY`
- 64-character lowercase `payload_sha256`
- bounded `event` and `action`
- nullable positive installation, repository, and release IDs as required by event type
- status constrained to `received`, `processing`, `retry_wait`, `blocked_precondition`, `succeeded`, `ignored`, or `dead_letter`
- bounded, allowlisted reason/error codes; no free-form diagnostic or arbitrary remote response text
- attempt count, next-attempt time, lease owner, monotonically increasing fencing token, and lease expiry
- received, started, updated, and finished timestamps
- no raw body, signature, token, repository content, release body, or model output

Add checks tying `event = 'release' AND action = 'published'` to positive installation, repository, and release IDs. Index `(status, next_attempt_at)` and installation/repository lookups.

### `webhook_delivery_repositories`

Installation payloads can name multiple repositories. Store only delivery ID, repository ID, bounded current full name, and `added`/`removed`/`observed` relation. Do not retain each repository object.

### `webhook_delivery_attempts`

Append one bounded record per claim: delivery ID, attempt number, fencing token, start/end, terminal classification, safe error code, and authenticated manual-retry actor if applicable. Do not store credentials or payload fragments.

Use the existing `audit_events` table for installation/repository lifecycle, product binding, authorized context approval, workflow enable/disable, explicit delivery retry, and dead-letter disposition. Audit insertion and the state change it describes must share one transaction and record the actor's immutable GitHub user ID where a user initiated the mutation.

## Dispatcher, lease, retry, and dead-letter rules

1. `ClaimDue` atomically changes one due `received`/`retry_wait` delivery to `processing`, increments its attempt/fencing token, and establishes a bounded lease.
2. Every renew/finalize operation compares the fencing token. Lease renewal failure cancels the job context. A stale worker cannot finish a reclaimed delivery or pass the delivery guard used by the GitHub adapter.
3. The dispatcher resolves the immutable installation/repository binding and checks installation active, repository granted, product mapped, workflow enabled, approved context present, skills pinned, and kill switch off before token minting or workflow work.
4. A failed readiness check becomes terminal `blocked_precondition` with an allowlisted reason. It is excluded from every automatic due query.
5. Only then may the job mint narrowly scoped tokens, compose a per-job GitHub adapter, and call the shared runner with the recorded release ID.
6. `no_action`, `awaiting_approval`, or existing release-dedupe completion marks the delivery `succeeded`. The workflow remains the authority for release dedupe and approval reconciliation.
7. Use typed/sentinel error classification, never error-message substring matching. Retry only transient transport, rate-limit, or lease-safe failures.
8. Default to at most five automatic attempts with bounded exponential delays of 5 seconds, 30 seconds, 2 minutes, and 10 minutes, plus bounded jitter. Honor a valid shorter/longer GitHub `Retry-After` only within an owner-reviewed maximum.
9. Exhaustion becomes terminal `dead_letter`. There is no automatic dead-letter replay.
10. Lifecycle disablement cancels active job contexts for the installation when possible. Immediately before issue search/create, the guarded GitHub adapter rechecks installation/repository authorization and the current delivery fencing token so a locally observed suspension, revocation, or lost lease cannot proceed to the sole remote write.

An explicit retry is an authenticated and authorized operator mutation:

- it is permitted only for `blocked_precondition` or `dead_letter`;
- it requires a current server-side installation/repository authority check and appends an audit/attempt record with immutable GitHub actor ID and reason;
- it reuses the original installation, repository, and release IDs;
- it never substitutes “latest release”;
- it reruns every current authorization/readiness/release-dedupe check;
- approval and workflow enablement changes never call it implicitly.

## Installation authentication and least privilege

### Registration permission ceiling

The proposed ceiling is:

- **Metadata: read** — GitHub's implicit read-only repository metadata baseline;
- **Contents: read** — release webhook availability, release reads, repository identity, and allowlisted evidence files;
- **Issues: write** — the one approval-issue write plus marker reconciliation.

No Actions, Administration, Checks, Deployments, Pull requests, Workflows, Members, Secrets, email, organization, or account permission is required by this workflow. A new endpoint that needs more permission must stop for owner review.

Manually select only the `release` event. Do not describe default `installation` or `installation_repositories` lifecycle delivery as a manual subscription.

### Token minting

- Read only environment-variable names/secret-file paths from config. Real webhook secrets and private keys remain outside the repository and database.
- Parse the private key once into protected process memory or through an owner-approved secret provider. Never log, serialize, or expose it through diagnostics.
- Create an RS256 App JWT just in time with GitHub's bounded lifetime, then call `POST /app/installations/{installation_id}/access_tokens`.
- Include exact `repository_ids` and a narrowed `permissions` body. Verify the response repository/permission scope and expiry before use.
- Reject installations whose target type is `Enterprise`; they have no repository access for this workflow and their tokens cannot satisfy this narrowing contract.
- Use `Accept: application/vnd.github+json`, an explicitly pinned `X-GitHub-Api-Version`, and an identifying `User-Agent`.
- Hold the installation token only inside the job-scoped client; never put it in SQLite, durable queues, audit events, model prompts, errors, or logs. Discard the client at job completion.
- Do not fall back from failed App authentication to the CLI's static `GITHUB_TOKEN`.

The App defaults the approval destination to the source repository. This is also the only cross-visibility-safe configuration until a separate approval-repository policy is owner-approved:

- use the source repository as the approval destination by default;
- keep a different approval repository disabled unless an authorized actor explicitly confirms it and the owner has approved a data-classification/access-equivalence policy;
- require any approved alternate to belong to the same active installation, bind its immutable ID, and verify current visibility and access before intent creation, model work, or issue work;
- block private/internal-to-public and every other destination that is not proven at least as restrictive as the source;
- mint a source token scoped to the source repository with Contents read;
- mint a separate approval token scoped to the approval repository with Issues write;
- compose both behind the existing narrow `workflows.GitHubAPI` interface;
- block if either repository is absent or belongs to a different installation.

This avoids granting Issues write to the source token or Contents read to an unrelated approval destination merely because the current CLI uses one client.

## Repository authorization and lifecycle disablement

- Signature validity proves the body came from the configured webhook secret; it does not prove that a repository is currently authorized for a product.
- Resolve by `(installation_id, repository_id)`, not `full_name`. Treat names as mutable display/API routing metadata and verify the returned repository ID.
- A source/approval repository must be present in the current granted repository set for that installation.
- Product bindings are explicit, authenticated, and authorized. A payload cannot choose arbitrary product IDs, workflow IDs, approval repositories, paths, or model configuration.
- Authentication establishes identity only. Binding products/repositories, selecting an approval repository, approving context, enabling/disabling, rechecking, and retrying each require a fresh server-side authority check against the current installation/repository and an immutable GitHub user ID in audit. Q-AUTH-1 must define the permitted roles; until then, App user mutations remain unavailable.
- Repository rename updates the bounded display name only after an authenticated payload/API response confirms the same ID.
- Repository replacement (same name, different ID) blocks and requires a new binding.
- `installation.suspend`, `installation.deleted`, and repository removal disable mapped workflows and block queued deliveries without a model or approval-issue call.
- `installation.unsuspend` never restores prior workflow enablement. It moves to `pending_recheck`; an authenticated user must revalidate access, review readiness, and enable explicitly.
- Permission changes are compared with the approved ceiling. Missing permissions block; broader permissions are reported but do not expand Marketing OS behavior.

GitHub documents that a suspended App cannot access installation resources. Marketing OS still applies its own durable disablement rather than relying only on token failure: [Suspending a GitHub App installation](https://docs.github.com/en/apps/maintaining-github-apps/suspending-a-github-app-installation).

## Context-first onboarding and approval flow

Installation is repository authorization, not product readiness.

1. An authenticated and authorized setup flow selects one granted immutable repository and creates/binds a product with a disabled `release-to-marketing` workflow.
2. The user supplies/reviews required product metadata. Webhook fields may suggest display values but cannot silently define product claims.
3. Source collection and context drafting start only after explicit user action. The existing bounded `productcontext.Service` and vendored-skill preflight remain authoritative.
4. The review surface shows evidence, uncertainties, warnings, context version, and skill snapshot.
5. Context approval requires an exact version plus a fresh authorization check for the immutable GitHub actor ID. The current caller-supplied actor string is insufficient for the App path.
6. Workflow enablement is a separate authorized action with atomic old/new state audit.
7. Recommend dry run only after enablement; dry run keeps its no-workflow-domain-write/no-issue guarantee.
8. Only then is the product ready for future `release.published` deliveries.

A release received before any readiness step commits a terminal `blocked_precondition` receipt. It makes no model or approval-issue call and is never automatically replayed. After readiness, a user may explicitly retry that exact recorded release.

For a marketable release, the existing workflow persists approval intent, reconciles by deterministic marker, and creates at most one issue. The issue is a proposal for human review. Approving context or copy never becomes authority to publish/send/spend, and the App does not subscribe to issue comments as an execution trigger.

## Safe configuration shape

Any future example remains disabled, loopback-only, and contains only placeholders/environment names:

```yaml
github_app:
  enabled: false
  listen_address: 127.0.0.1:8080
  app_id_env: MARKETING_OS_GITHUB_APP_ID
  private_key_file_env: MARKETING_OS_GITHUB_APP_PRIVATE_KEY_FILE
  webhook_secret_env: MARKETING_OS_GITHUB_APP_WEBHOOK_SECRET
  api_version: OWNER_APPROVED_GITHUB_API_VERSION
  max_webhook_body_bytes: 2097152
```

Do not add real App IDs, URLs, secrets, PEM data, tokens, installation/repository IDs, or credentials. Non-loopback exposure requires an approved HTTPS/TLS/proxy/host design; limits remain validated/capped; resolved environment values are redacted; and unrelated tenants may not share SQLite.

## Observability, privacy, and retention

### Structured events and metrics

Record safe structured events for:

- receipt accepted/duplicate/conflict/ignored;
- signature, size, decode, and commit failures;
- queue depth and oldest due age;
- claim, lease renewal/expiry, retry, blocked reason, success, and dead letter;
- installation/repository lifecycle and workflow disablement;
- token mint success/failure without token/JWT/private-key material;
- workflow terminal state and approval reconciliation using existing safe IDs.

Metrics use bounded labels such as event, disposition, and reason code. Never use installation ID, repository ID/name, delivery ID, product ID, release title, or actor as metric labels. Logs may include bounded internal IDs needed for support, but never request bodies, credentials, model input/output, repository file contents, release bodies, or issue bodies.

Health endpoints expose process liveness only. Readiness may report database migration/worker availability without configuration or tenant details.

### Data minimization and retention gate

- Raw webhook bodies and signatures are never stored by default.
- Persist only provider IDs, current bounded repository/account display metadata, payload hash, the bounded queue/lease/attempt fields required to process a dispatchable receipt, allowlisted reason codes, immutable actor IDs required for authorized mutations, and timestamps. Terminal pre-readiness receipts do not acquire queue/lease/attempt data.
- Schema must support deletion/pseudonymization without deleting workflow/approval audit accidentally.
- A cleanup job is disabled until the owner approves exact receipt, attempt, lifecycle, audit, backup, and deletion periods.
- Retention must preserve transport/business idempotency for the approved redelivery/support window. If operational details are purged earlier, retain a minimal delivery-ID/hash tombstone for the separately approved dedupe period.
- Tests use isolated ephemeral databases and immediate fixture cleanup; that is not a production retention decision.
- No hosted use involving customer-identifiable or regulated data is permitted until access, backup, deletion, incident, and privacy policies are approved.

## Interfaces to specify before implementation

| Interface | Required operations |
|---|---|
| `Acceptor` | `Accept(ctx, Receipt) (AcceptResult, error)` |
| `ReceiptStore` | `Accept`, `ClaimDue`, `Renew`, fenced `Finish`, authorized `RequestRetry` |
| `InstallationStore` | `ApplyLifecycle`, `ResolveAuthorizedProduct`, authorized `Recheck` |
| `TokenMinter` | `Mint(ctx, TokenRequest) (InstallationToken, error)` |

Contracts use typed dispositions and allowlisted reason codes. Token types have no `String`/JSON exposure. Test fakes record token/model/issue calls so ordering assertions are exact.

## Required test matrix

### Cryptography and HTTP

- GitHub's HMAC vector succeeds; mutated body, wrong secret, missing/malformed/multiple signature, and non-constant-time regressions fail.
- Exact-limit body succeeds; over-limit or misleading/chunked length fails; verification uses exact pre-decode bytes.
- Invalid method/content type/header/body shape writes no receipt.
- Only a committed new/duplicate receipt gets timely `202`; commit failure cannot.
- A simulated delivery that never commits is detected through the App delivery list and recovered under the bounded three-day redelivery procedure.
- Response/logs contain no body, signature, secret, token, or key fragment.

### Event validation

- Only `release.published` queues; other release actions are ignored; `ping` completes without dispatch.
- Default installation/repository lifecycle actions follow the matrix.
- `github_app_authorization.revoked` invalidates matching user authorization/session state once user auth exists.
- Header/body mismatch and missing/non-positive IDs fail; unknown events/actions cannot activate access.

### Receipt and worker state

- Same ID/hash is concurrent-idempotent; same ID/different hash conflicts; replay cannot duplicate release work or reverse disablement.
- Claim/renew/finalize is transactional; expired leases reclaim; stale fencing cannot finish.
- Lease loss cancels the job, and a stale delivery fence blocks issue search/create.
- Retry/attempt bounds dead-letter; blocked/dead-letter rows are not automatically due.
- Readiness changes do not mutate blocked state; authorized retry preserves release ID/audit.
- Fresh and legacy migration tests run with foreign keys enabled.

### Authorization and lifecycle

- Installation/repository/product mismatch blocks before token/model/issue; rename preserves ID while replacement blocks.
- Enterprise-target installations fail before onboarding or token minting.
- Suspend/delete/remove revoke, disable, and audit; late create/add cannot reactivate; unsuspend requires recheck.
- Permission drift cannot broaden behavior; source/approval tokens have separate exact scope.
- Cross-repository approval is disabled by default; any approved alternate is authority-checked and cannot be less restrictive than the source.
- Every user mutation checks current authority server-side and audits the immutable GitHub user ID.
- Credentials never reach DB/log/error/JSON/model; mid-job revocation prevents issue creation.

### Shared workflow and onboarding

- CLI, scheduler, and dispatcher call the same runner; App passes its trigger type and recorded release ID without subprocess/Cobra.
- Pre-readiness makes zero model/issue calls and never auto-replays.
- Context approval and enablement are separate authorized audits.
- Duplicate/redelivered/retried releases converge to one issue; fixtures end `awaiting_approval` or `no_action`.
- Forbidden execution adapters remain absent.

Run the repository's complete verification gate after every slice:

```sh
make fmt
make tidy
make test
make test-race
make vet
make build
make smoke
git diff --check
```

## Staged implementation slices and acceptance gates

Each slice requires a separate reviewed commit or isolated diff; none authorizes deployment.

| Slice | Deliverable | Acceptance gate |
|---|---|---|
| A — domain/migration/crypto | Typed receipt/lifecycle models, reason codes, migration, stores, bounded HMAC verifier | Legacy/fresh migration, official HMAC vector, bounds, concurrent conflict, and no-secret scan pass |
| B — HTTP ingress | Disabled server, exact route/timeouts, typed parsing, durable `202`, `ping`/ignored handling, failed-ingress detection/recovery contract | Commit precedes `202`; no in-memory-only loss or handler workflow/model/API call; simulated uncommitted delivery is recovered |
| C — lifecycle/authorization | Installation/repository/binding state, monotonic transitions, user-authorization revocation, atomic disable/audit, recheck | Suspend/delete/remove disable; unsuspend/add stay pending; enterprise targets and unauthorized mutations fail closed |
| D — App auth/adapter | RS256 JWT, scoped token minter, API headers, guarded composite GitHub adapter | Exact source/approval scopes, visibility guard, revocation/fence block, credential redaction, no broader write |
| E — dispatcher/runner | Fenced worker, typed retry/dead letter, authorized explicit retry, readiness, scoped runner | Fixture reaches `no_action`/one `awaiting_approval`; lease loss cancels/blocks writes; blocked never auto-replays; ambiguous issue reconciles |
| F — onboarding | Only after Q-AUTH-1 and Q-DASHBOARD-1/UI-scope approval, minimal existing-service draft/review/approve/enable surface | Identity plus current authority, CSRF/session controls, separate audits, uncertainties, owner-approved UI scope/stack |
| G — deployment | Only after owner selection, host runbook, secret rotation, HTTPS, backup/retention/alerts/incident plan | Owner approves visibility, permissions, credentials, host/cost, privacy/legal, model keys, and operations |

## Owner gates and why implementation stops here

The plan is not blocked, but code/registration/deployment is blocked on explicit rulings:

1. **App registration and visibility:** owner account/organization, private vs public, setup/callback/webhook URLs, and whether Marketplace will ever be used.
2. **Credentials:** App ID/client ID, private-key generation/storage/rotation, webhook-secret generation/rotation, and incident revocation procedure.
3. **Host and cost:** self-hosted endpoint or managed host, HTTPS/TLS/proxy, availability, queue/worker topology, database mode, backup, and paid-resource approval.
4. **Permissions and repository topology:** confirm the Metadata read, Contents read, Issues write ceiling; reject or separately design enterprise targets; decide whether one repository may map to multiple products; and decide whether approval issues may use a separately granted repository under an approved access-equivalence policy.
5. **Authenticated and authorized user flow:** Q-AUTH-1 remains OPEN. GitHub-only is the fail-closed default, but OAuth/user-token/session/CSRF details, permitted installation/repository roles, revocation behavior, and per-mutation authority checks require approval before any App user mutation is exposed.
6. **Model credentials:** Q-MODEL-1 remains OPEN. BYOK is the default, but a managed multi-install host cannot store customer keys without a reviewed secret and tenancy model.
7. **Retention/privacy/operations:** exact metadata/tombstone/audit/backup/deletion periods, support access, data export/deletion, and incident policy.
8. **Ingress recovery:** select and operate the monitored App-delivery listing/redelivery runbook or bounded reconciler inside GitHub's three-day recovery window.
9. **Dashboard/UI scope:** Q-DASHBOARD-1 remains OPEN; no onboarding web surface or stack is authorized until the owner approves it.
10. **Cloud/database boundary:** no multi-tenant cloud on SQLite; PostgreSQL and a managed host remain separate owner decisions.
11. **Legal/public product steps:** privacy terms, public flip, Marketplace, pricing, branding, and notice changes remain outside this plan.

Implementing App code before these gates would either create unused security-sensitive surfaces or silently decide credentials, authentication, tenancy, retention, and public exposure. The safe next action is owner review of this plan and its gates, followed by a narrowly authorized Slice A–E implementation handoff. Registration, credentials, onboarding UI, and deployment should remain later explicit approvals.

## Definition of implementation-ready

This plan is ready for owner review because it distinguishes current/target behavior, cites GitHub.com constraints, defines in-process package seams, specifies failed-ingress recovery plus receipt/lifecycle/lease/retry/privacy/test contracts, and limits workflow dispatch to `release.published`. Default lifecycle and authorization-revocation events plus `ping` fail closed; unsupported enterprise targets are rejected; blocked work never auto-replays; disablement is durable; credentials are never persisted/logged; user mutations require both identity and current authority; context approval and enablement remain separate; and no owner-only registration, credential, UI, host, public, paid, pricing, legal, branding, or automatic-action decision is crossed.
