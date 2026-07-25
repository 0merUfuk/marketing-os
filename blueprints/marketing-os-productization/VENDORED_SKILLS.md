# Option A — Vendored Marketing Skills and Provenance

## Objective

Remove `.skills/marketingskills` from the normal installation path while preserving reproducibility, bounded loading, upstream authorship, and MIT attribution. D-002 and D-009 are binding.

Current source evidence:

- upstream: `https://github.com/coreyhaines31/marketingskills.git`;
- pinned commit/ref: `67264763cb107d61749f418d081c56e5bcbc0209`;
- upstream version: `2.8.12`;
- current manifest: `dbec7d7c123f10e15a19130abe6efe469db8df1bac454b70919ccb92e2286047`;
- license source: `.skills/marketingskills/LICENSE`;
- runtime selection: `docs/skills.md`, `internal/productcontext/service.go`, and `internal/skillruntime/prompt.go`.

Those values must be re-read from live source and recorded from real tool output during migration; this blueprint does not substitute for verification.

## Vendored scope

Until Q-SKILLS-1 is resolved, vendor exactly these five complete upstream skill directories:

- `product-marketing`
- `launch`
- `copywriting`
- `social`
- `emails`

The complete selected directories keep their upstream `SKILL.md`, references, and evaluation fixtures internally consistent. Do not vendor unrelated upstream skills, tools, CLIs, integrations, or repository scripts.

Runtime prompt selection remains narrower:

- context drafting loads `product-marketing`;
- release generation loads primary `launch`;
- supporting skills are `copywriting`, `social`, and `emails`;
- only `social/references/platform-limits.md` and `emails/references/copy-guidelines.md` are explicitly loaded as references today.

Vendoring a file does not authorize loading, executing, or sending it to a model.

## Required layout

```text
third_party/marketingskills/
├── LICENSE
└── skills/
    ├── copywriting/
    ├── emails/
    ├── launch/
    ├── product-marketing/
    └── social/

THIRD_PARTY_NOTICES.md
skills.lock.yaml
```

`third_party/marketingskills/LICENSE` must be an unmodified copy of the upstream license. Root `THIRD_PARTY_NOTICES.md` is the human-readable attribution and modification ledger. `skills.lock.yaml` is the machine-enforced provenance/manifest record. The `third_party/` location avoids triggering Go's automatic top-level `vendor/` module mode; this technical path choice does not resolve Q-LEGAL-1's notice-granularity question.

## Migration procedure

### 1. Verify source

- Confirm the submodule remote, exact commit, clean state, upstream version, and license before copying.
- Produce a sorted inventory and hashes from the pinned source.
- Stop if the pin, license, or working tree contradicts the lock/handoff.

### 2. Copy without transformation

- Copy only the five selected directories and upstream license into the required layout.
- Preserve bytes, paths, and copyright notices.
- Do not run upstream scripts or content-generation tools.
- Compare source and destination inventories and hashes.

### 3. Replace lock semantics explicitly

Extend the strict lock format to distinguish a vendored distribution from a Git checkout while retaining:

- upstream repository URL;
- immutable ref and commit;
- upstream repository version;
- selected-skill inventory;
- deterministic SHA-256 manifest of the vendored root;
- update timestamp as non-reproducibility metadata.

For vendored mode, verification must require the configured root, exact required inventory, and manifest match. It must not pretend the vendored directory has a local Git `HEAD`. The upstream commit remains provenance recorded at copy time; the vendored manifest is the runtime integrity proof.

Unknown lock fields, missing required fields, duplicate skills, extra selected skills, missing files, or manifest drift must fail closed.

### 4. Preserve loader protections

Keep or strengthen all controls in `internal/skills/loader.go` and `internal/skills/lock.go`:

- strict frontmatter and skill-name validation;
- root/path containment;
- rejection of absolute and `..` references;
- bounded skill, reference, and bundle sizes;
- safe contained symlink handling and escaping/broken symlink rejection;
- explicit requested-reference allowlisting;
- no script execution and no binary-asset prompt loading;
- deterministic sorted manifest hashing;
- `RequirePinned` before every model-backed context/workflow run.

### 5. Switch defaults and tests

Only after vendored verification passes:

- change `config.example.yaml` and fixtures to the vendor path;
- keep `skills status` and `skills list` available for vendored distribution;
- make the normal runtime `skills update` command fail closed without fetching or mutating files, with guidance to the documented offline maintainer update workflow;
- remove clone/submodule instructions from README, GUIDE, CONTRIBUTING, and docs;
- update smoke checks to work in an ordinary clone;
- remove `.gitmodules` and the gitlink in the same intentional migration;
- document an explicit maintainer update procedure that stages a verified copy rather than mutating user installs.

Do not remove the submodule source before source/destination verification and replacement tests pass.

## `THIRD_PARTY_NOTICES.md` contract

The notice must contain:

1. Component name and upstream owner/project.
2. Source repository URL.
3. Exact upstream commit/ref and version.
4. Statement that the selected content is redistributed under MIT.
5. A link/path to `third_party/marketingskills/LICENSE`.
6. Sorted vendored skill/path inventory.
7. Local modification status:
   - `none; copied byte-for-byte`, or
   - per-file path, date, reason, and summary.
8. A clear statement that upstream authorship is not claimed by Marketing OS.

If any vendored upstream text changes, the notice and lock/manifest must change in the same commit. Do not mix local guidance into an upstream file without a modification entry. Prefer a separate project-authored overlay when possible.

Q-LEGAL-1 remains OPEN. The active fail-closed default is the centralized notice plus the original license copy. Stop if implementation requires a different legal strategy.

## Maintainer update transaction

Vendored upgrades are an offline, reviewed maintainer workflow, not a normal runtime self-update. The runtime `skills update` command must always refuse vendored mode with actionable maintainer guidance and must not fetch, checkout, copy, or rewrite the lock. The maintainer transaction must:

1. require an explicit immutable ref;
2. fetch into a temporary, isolated source;
3. verify license and selected inventory;
4. show upstream diff for review;
5. stage the exact reviewed upstream license and selected content under `third_party/marketingskills/` without following escaping symlinks;
6. recompute the vendored manifest;
7. regenerate the lock and notice/modification metadata atomically;
8. review the complete staged source, license, lock, and notice diff;
9. run loader, prompt-selection, manifest-drift, CLI, smoke, and full regression tests;
10. never auto-update during application startup or workflow execution.

## Required tests

- Clean ordinary clone reports `pin_valid: true` without submodules.
- Each of the five skills loads; only requested references enter prompt bundles.
- Missing, added, or modified vendored files invalidate the manifest.
- Invalid lock distribution/inventory fails strict parsing.
- Absolute/traversal/escaping/broken symlink references fail.
- Oversized skill/reference/bundle fails.
- Upstream scripts are not executed.
- Context and release workflows block before model/GitHub writes when pin verification fails.
- Source/destination inventory and hash comparison is recorded during migration.
- `skills status` and `skills list` work in vendored mode.
- A CLI acceptance test proves `skills update` in vendored mode exits nonzero with maintainer guidance and makes no filesystem or lock change.
- Documentation contains no normal-user `--recurse-submodules` requirement.

## Acceptance

Option A is complete only when a normal clone builds and passes `make smoke` with no submodule, the five-skill `third_party/marketingskills/` tree and original upstream license are present, provenance and local modifications are explicit, manifest drift blocks AI execution, current prompt selection is unchanged, runtime updates fail closed, all obsolete setup instructions are removed, and the complete gate in [`TESTING_AND_RELEASE.md`](TESTING_AND_RELEASE.md) passes.
