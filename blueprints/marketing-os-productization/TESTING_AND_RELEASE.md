# Implementation Phases and Release Readiness

Each phase is independently reviewable. A phase is complete only when its deliverables and gate pass with real output.

## Phase 0 — Drift and artifact safety

Deliverables:

- verify branch, HEAD, remotes, status, staged state, and submodule pin;
- classify every dirty/untracked path;
- preserve blueprint/task context without committing local-only artifacts;
- prevent `.claude/` symlinks, real local configuration, generated assets, credentials, and private runtime data from accidental staging;
- delete nothing ambiguous.

Gate:

- intended product paths remain visible;
- local-only paths remain present but unstaged/excluded;
- no sensitive values are printed or staged;
- cleanup decisions are reported before product edits.

## Phase 1 — Blueprint

Deliverables are this index plus product, architecture, vendoring/provenance, onboarding, safety, and release documents.

Gate:

- D-001 through D-009 trace to requirements;
- D-010 remains CANDIDATE;
- all six OPEN questions retain fail-closed defaults;
- link/path audit and `git diff --check` pass;
- no App code, pricing/cloud/legal/branding decision, or decision-ledger rewrite occurs.

## Phase 2 — Vendored skills

Follow [`VENDORED_SKILLS.md`](VENDORED_SKILLS.md).

Required implementation surfaces:

- `third_party/marketingskills/`
- `THIRD_PARTY_NOTICES.md`
- `skills.lock.yaml`
- `internal/skills/` and tests
- `config.example.yaml` and config fixtures
- README, GUIDE, `docs/skills.md`, configuration/troubleshooting docs, CONTRIBUTING, CHANGELOG
- `.gitmodules` and gitlink removal only after replacement verification
- Makefile smoke behavior

Gate:

- ordinary clone needs no submodule;
- exact selected inventory/license/provenance is verified;
- manifest drift and unsafe paths fail closed;
- context/release prompt selection is unchanged;
- `skills status` and `skills list` work, while runtime `skills update` fails closed with offline maintainer guidance and no mutation;
- no upstream script executes;
- all phase-specific and full-suite tests pass.

## Phase 3 — Minimal CI

If `.github/workflows` remains absent, add one CI workflow that uses the repository's declared Go version and runs secret-free checks. It must not add deployment, release publishing, package publishing, Marketplace, credentials, paid services, or write permissions.

Minimum jobs:

- formatting/tidiness drift check without committing formatter output;
- `make test`;
- `make test-race`;
- `make vet`;
- `make build`;
- `make smoke`;
- `git diff --check`.

Use least GitHub permissions (`contents: read`) and no secrets. Cache is optional, not required.

Gate:

- workflow parses and runs on pull requests/branch pushes;
- smoke uses only `config.example.yaml` and vendored files;
- CI makes no external product write and requests no elevated permission.

## Phase 4 — GitHub App seam

For this execution, the required deliverables are [`ARCHITECTURE.md`](ARCHITECTURE.md) and [`GITHUB_APP_IMPLEMENTATION_PLAN.md`](GITHUB_APP_IMPLEMENTATION_PLAN.md). Do not implement, register, or deploy the App unless a later owner-approved handoff explicitly opens that scope.

A future implementation phase must follow the plan's signature/body limits, durable receipt, default installation lifecycle, authorization, scoped token, shared-runner, lease/retry/dead-letter, privacy, and staged acceptance contracts. Tests must prove a pre-readiness delivery makes no model/approval-issue call and cannot spring into execution after later approval or enablement.

Before deployment, the owner must approve a retention/deletion policy for minimal delivery metadata. Test cleanup or ephemeral fixtures are not a production retention decision.

Gate before registration/deployment:

- owner approves credentials, permissions, host, cost/public exposure, and any legal/privacy implications;
- no owner-only OPEN item is silently ratified;
- local fixture tests cover the complete webhook-to-`awaiting_approval` path, duplicate delivery, blocked preconditions, explicit identity-preserving retry, and replay prevention.

## Phase 5 — Full verification and release prep

Run from repository root:

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

After formatting/tidy, inspect the diff and rerun relevant checks if either changed files. Also verify:

- clean-clone/no-submodule setup;
- `skills status` reports valid vendored provenance/manifest;
- no stale normal-user submodule instructions remain;
- required third-party notice/license/inventory agree;
- no forbidden/sensitive/local-only path is staged;
- documentation links and referenced paths exist;
- changelog describes behavior without claiming a public release.

## Required test matrix

| Area | Required proof |
|---|---|
| Skill lock | strict parsing, exact inventory, valid manifest, drift failure |
| Skill loader | five skills load, requested refs only, traversal/symlink/size failure |
| Skills CLI | vendored `status`/`list` succeed; runtime `update` refuses with maintainer guidance and zero mutation |
| Context | bounded evidence, explicit uncertainty, no auto-approval, one canonical version |
| Release | fixture `no_action`, marketable five-draft proposal, strict evidence/policy validation |
| State | concurrent claims, fencing, dedupe, cursor, failure recovery |
| Approval | write-ahead intent, marker reconciliation, ambiguous-write retry creates one issue |
| Dry run | no durable workflow-domain or GitHub issue write |
| Kill switch | blocks scheduled attempts/retries and persists |
| CLI | setup/status/list/run controls and stable JSON output |
| Security | secret redaction, URL policy, bounded network/files, no forbidden adapter |
| Future webhook seam | complete matrix in [`GITHUB_APP_IMPLEMENTATION_PLAN.md`](GITHUB_APP_IMPLEMENTATION_PLAN.md), including authenticated raw-body verification, immutable identities, minimal deduped receipt, lifecycle disablement, blocked precondition, no model/issue call, no auto-replay, and identity-preserving explicit retry |
| Race/build | race suite, vet, trimmed binary, smoke |

## Commit and push gate

Before commit:

- review `git status`, staged paths, staged diff, and secret/path categories;
- keep task handoffs, real local config, generated output, and local symlinks unstaged;
- group the blueprint, vendoring, docs/tests, and CI intentionally;
- record actual verification results.

Use a `codex/` feature branch. Push only if authentication and repository policy allow. If CI exists after push, watch it to a terminal state and report the check URL/result. There is no approved external deployment target.

## Release blockers

- any required command fails;
- vendored source/license/provenance cannot be proven;
- manifest/path/reference verification is weaker than before;
- ordinary setup still needs a submodule;
- context can be bypassed or auto-approved;
- a replay can duplicate an approval issue;
- CI needs credentials or writes;
- a forbidden capability or owner-only decision enters the diff;
- secret/private/local-only material is staged;
- D-010 or an OPEN item is presented as ratified.

## Definition of done

Productization is verified when Option A works from an ordinary clone, current CLI behavior and trust invariants regress neither functionally nor under race tests, minimal CI is green, provenance is auditable, the feature branch is safely pushed when possible, and the report includes exact commands/results and any remaining owner gates. The future GitHub App remains an implementation-ready seam, not a claimed deployment.
