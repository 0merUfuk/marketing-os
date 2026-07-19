# Adding a workflow

A workflow is not a prompt plus cron. It is a complete, testable state machine with deterministic safety fields, durable state, evidence rules, bounded model behavior, and an approval-only output.

## Required definition fields

Every `domain.WorkflowDefinition` must specify:

- trigger and check cadence;
- activation condition and purpose;
- primary/supporting skills;
- required inputs;
- ordered steps;
- deterministic self-check;
- durable state requirements;
- dedupe-key definition;
- cooldown;
- stop condition;
- error behavior;
- output destination;
- approval policy;
- maximum cost and timeout;
- overlap policy;
- enabled flag (default must be false).

The MVP validator accepts only:

```text
output_destination = github_issue
approval_policy = human_required_no_execution
```

Do not weaken this validator to add publishing. A future execution tier requires a separate capability design and explicit operator opt-in.

## Implementation sequence

1. **Add domain constants and definition**
   - Put the ID and constructor under `internal/domain`.
   - Keep activation/stop/error semantics specific and machine-testable.
   - Add definition-validation tests.

2. **Define immutable event identity**
   - Prefer provider-native immutable IDs, not mutable titles/tags/names.
   - Include product ID, workflow ID, source account/repository identity, and event ID in a deterministic hash.
   - Add tests proving renamed mutable fields do not create duplicate work.

3. **Add a narrow source adapter**
   - Define the smallest interface the workflow needs.
   - Bound response sizes, redirects, retries, and timeouts.
   - Avoid generic arbitrary-write clients.
   - Test with `httptest.Server`, including rate limit, server failure, cancellation, malformed JSON, and redacted errors.

4. **Capture evidence before generation**
   - Persist immutable same-product evidence with content hash and external identity.
   - Never let a model-supplied evidence ID become authoritative.

5. **Select pinned skills**
   - Choose one primary skill and only necessary supporting skills/references.
   - Call `RequirePinned` before any model request.
   - Record repository/skill versions.

6. **Build a typed prompt package**
   - Separate system policy from untrusted data.
   - Include approved context version, evidence, event data, and skill versions.
   - Redact configured/common secret patterns.

7. **Define strict output schema**
   - Set `additionalProperties: false` for every object.
   - Require explicit arrays/booleans instead of relying on zero values.
   - Add deterministic semantic validators beyond JSON shape.
   - Validate same-product evidence IDs, no-action shape, allowed action/channel set, approval requirement, terminology, and limits.

8. **Add bounded repair**
   - Reuse the runtime pattern: at most configured attempts, no new evidence IDs/claims, no commentary, size-limited invalid output.
   - Record all request IDs, aggregate usage, repairs, provider/model, and cost.

9. **Implement claim/finalization state**
   - Claim a lease and dedupe key in one transaction.
   - Use fencing tokens for every finalizer.
   - Advance cursors and complete dedupe only on terminal success.
   - Release claims without cursor movement on failure.

10. **Stage one approval intent**
    - Render evidence, action, drafts, risks, warnings, estimated cost, and explicit review instructions.
    - Persist exact request + deterministic hidden marker before remote creation.
    - Reconcile by marker after crash/ambiguous error.
    - Mirror product-local output before remote write.

11. **Expose manual and scheduler routes**
    - Both must call the same workflow service.
    - Add `workflow run/enable/disable/list` support.
    - Scheduler must enforce kill switch, timeout, retry, cancellation, and live definition reconciliation.

12. **Document and test**
    - Unit: activation, dedupe, schema/evidence/policy validation, no-action, prompt selection.
    - Integration: database claim/fencing/dedupe/cursor transactions and adapter errors.
    - E2E: event → model fixture → approval issue; retry produces no duplicate issue.
    - Race: `go test -race ./...`.
    - Update CLI/config/architecture/security/troubleshooting docs.

## Workflow interface

The scheduler requires:

```go
Run(context.Context, string, workflows.RunOptions) (workflows.RunOutcome, error)
```

If multiple workflow IDs are added, introduce a registry/router that maps IDs to typed runners. Do not place a large switch with workflow internals in the CLI or scheduler.

## Approval issue requirements

Every proposal must contain:

- product, workflow, run, and trigger identity;
- evidence sources and summaries;
- exact proposed action and channel drafts;
- risks, warnings, unsupported claims;
- estimated model cost;
- explicit approve/request-changes/reject instructions;
- deterministic hidden idempotency marker.

No approval issue may claim that an action already executed.

## Review checklist

- [ ] Default disabled
- [ ] No Tier 2 external-write adapter
- [ ] Immutable event identity
- [ ] Durable dedupe and non-overlap lease
- [ ] Approved context prerequisite
- [ ] Pinned skill verification/version audit
- [ ] Same-product immutable evidence
- [ ] Strict JSON + semantic self-check
- [ ] Input/output/cost/timeout/retry bounds
- [ ] Secret redaction
- [ ] No-action path
- [ ] Write-ahead approval + crash reconciliation
- [ ] Cursor only advances on success
- [ ] Dry run has no workflow-domain writes
- [ ] Structured logs and durable run/error/audit state
- [ ] Unit, integration, E2E, and race tests
