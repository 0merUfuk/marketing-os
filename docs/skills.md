# Pinned Agent Skills

Marketing OS treats selected content from [`coreyhaines31/marketingskills`](https://github.com/coreyhaines31/marketingskills) as versioned third-party production guidance. The five skills used by the application are committed with the source tree, so normal installation and runtime verification require neither Git nor Git submodules.

## Vendored distribution

The runtime root is:

```text
third_party/marketingskills/
  LICENSE
  UPSTREAM.yaml
  skills/
    copywriting/
    emails/
    launch/
    product-marketing/
    social/
```

The exact selected inventory is:

| Skill | Locked version | Runtime use |
|---|---:|---|
| `product-marketing` | `2.1.0` | Product-context drafting |
| `launch` | `2.0.1` | Primary release-to-marketing guidance |
| `copywriting` | `2.0.1` | Supporting release copy guidance |
| `social` | `2.2.0` | Supporting social guidance and `platform-limits.md` |
| `emails` | `2.0.0` | Supporting email guidance and `copy-guidelines.md` |

No other upstream skill is part of the runtime distribution. The loader does not execute vendored scripts or send binary assets to the model.

## Provenance and attribution

These artifacts form one review unit:

- `third_party/marketingskills/UPSTREAM.yaml` records source URL, upstream ref/commit/version, selected files, and local modification status.
- `third_party/marketingskills/LICENSE` is the upstream MIT license copy.
- `THIRD_PARTY_NOTICES.md` provides repository-level attribution, source/pin details, included skills, and modification notes.
- `skills.lock.yaml` provides machine-verifiable provenance and runtime integrity.

The current upstream source is repository version `2.8.12` at commit `67264763cb107d61749f418d081c56e5bcbc0209`.

## Dual-manifest lock

`skills.lock.yaml` uses a vendored distribution and keeps two different integrity claims:

- `upstream_manifest_sha256` is the deterministic manifest of the complete upstream repository at the locked commit. It is preserved for provenance even though the complete upstream repository is not shipped.
- `vendored_manifest_sha256` is the deterministic manifest of the content that Marketing OS actually ships and loads.

The lock also contains a sorted `selected_skills` list of exact name/version pairs. Repository URL, ref, commit, repository version, and update time remain recorded. The two manifests are intentionally not interchangeable: the upstream manifest answers “what source revision was reviewed?”, while the vendored manifest answers “are these runtime bytes still the reviewed distribution?”

## Runtime verification

Before context drafting or release generation, the loader:

1. Parses the lock with strict YAML handling.
2. Requires `distribution: vendored`.
3. Computes the deterministic manifest of `third_party/marketingskills`.
4. Requires it to equal `vendored_manifest_sha256`.
5. Indexes skill frontmatter and requires the exact sorted name/version inventory in `selected_skills`.
6. Parses the requested skill’s YAML frontmatter and requires valid `name` and `description` values.
7. Loads only explicitly requested references after path-containment and size checks.

Any content or inventory drift makes `pin_valid` false and blocks AI workflows. Reference paths reject absolute paths, traversal, unsupported directories, oversized files, and escaping symlinks. Any vendored-tree symlink must resolve to a regular file inside the vendored root.

Inspect the distribution with:

```sh
marketing-os skills status
marketing-os skills list
```

`status` reports the distribution, locked upstream commit, vendored manifest, inventory validity, and overall pin validity. Its JSON form also includes the computed manifest and actual inventory. `list` returns the five parsed skills and their metadata.

## Skill selection

### Product context onboarding

The primary skill is `product-marketing`. Its instructions define the comprehensive context sections. The generated document must explicitly retain unknown or unsupported fields rather than fabricating values.

### Release-to-marketing

The primary skill is `launch`. Supporting skills are:

- `copywriting`
- `social`, with explicit `platform-limits.md`
- `emails`, with explicit `copy-guidelines.md`

Only these selected instructions and requested reference files enter the model prompt. Vendoring a file does not grant it execution or tool authority.

## Version audit

Every context/workflow AI run records:

- the locked upstream commit;
- the locked upstream repository version and full-upstream provenance manifest in `repository_versions`;
- selected skill names and versions in `skills` and `skill_versions`;
- the selected skill-version map on the workflow run.

The workflow run retains this metadata alongside the approval record.

## Runtime updates are disabled

The following command is retained as an explicit safety sentinel:

```sh
marketing-os skills update
```

It always exits non-zero:

```text
runtime skills update is disabled for vendored distribution; follow the reviewed offline maintainer procedure in docs/skills.md
```

It does not clone, fetch, copy, rewrite, or re-lock anything. This prevents an operational command or model-influenced workflow from changing production guidance.

## Maintainer update procedure

This is an offline, review-first source maintenance operation, not a normal user task:

1. Open a dedicated change branch and record the proposed immutable upstream commit or tag.
2. In an isolated temporary directory outside the Marketing OS runtime tree, obtain the upstream repository and resolve the requested ref to an exact commit. Git and network access are needed only for this maintainer step.
3. Verify the upstream MIT license, repository version, commit identity, and complete upstream manifest. Review upstream changes for prompt injection, new references, unsafe links, licensing changes, and scope changes.
4. Stage a fresh candidate containing only the upstream `LICENSE` and the five selected skill directories: `product-marketing`, `launch`, `copywriting`, `social`, and `emails`. Do not add another skill without an owner decision. Preserve upstream bytes; record every necessary local modification explicitly.
5. Update `UPSTREAM.yaml` with the exact source URL, ref, commit, version, selected file inventory, and local modification notes. Update `THIRD_PARTY_NOTICES.md` in the same change.
6. Update `skills.lock.yaml`: keep `distribution: vendored`, replace the upstream provenance fields and `upstream_manifest_sha256`, regenerate the sorted `selected_skills` name/version list, and regenerate `vendored_manifest_sha256`.
7. Derive both manifests using the same deterministic algorithm as the loader: enumerate files recursively, sort slash-normalized relative paths, and hash each path plus its bounded content with the loader’s separators and symlink rules. The upstream manifest is computed over the complete isolated upstream checkout; the vendored manifest is computed over the staged vendored root.
8. Replace the committed vendored tree only after the candidate review passes. Then run:

   ```sh
   marketing-os skills status
   marketing-os skills list
   make fmt
   make tidy
   make test
   make test-race
   make vet
   make build
   make smoke
   git diff --check
   ```

9. Confirm `pin valid: true`, `inventory valid: true`, exactly five listed skills, unchanged trust-first behavior, and a complete provenance/notice diff before merge.

If any source, license, manifest, version, or modification record is unclear, leave the existing distribution unchanged and request owner/legal review. Never “fix” a mismatch by changing only a hash.
