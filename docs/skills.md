# Pinned Agent Skills

Marketing OS treats [`coreyhaines31/marketingskills`](https://github.com/coreyhaines31/marketingskills) as versioned production guidance, not copied prompt text.

## Repository integration

The upstream repository is retained at:

```text
.skills/marketingskills
```

It is a Git submodule. Upstream file layout and license notices remain intact. Application code only reads it; it does not rewrite `SKILL.md`, references, templates, scripts, or assets.

The committed `skills.lock.yaml` currently records:

```yaml
repository: https://github.com/coreyhaines31/marketingskills.git
ref: 67264763cb107d61749f418d081c56e5bcbc0209
commit: 67264763cb107d61749f418d081c56e5bcbc0209
repository_version: 2.8.12
manifest_sha256: dbec7d7c123f10e15a19130abe6efe469db8df1bac454b70919ccb92e2286047
```

The update timestamp is metadata and does not affect reproducibility.

## Verification

Before context drafting or release generation, the loader:

1. Parses the lock using strict YAML.
2. Resolves the submodule’s actual `HEAD`.
3. Requires exact commit equality.
4. Computes a deterministic SHA-256 manifest over every repository file except `.git` metadata and an in-tree lock file.
5. Requires exact manifest equality.
6. Parses the requested skill’s YAML frontmatter and requires `name` and `description`.
7. Loads only explicitly requested references after path-containment and size checks.

Safe tracked symlinks are supported because upstream includes `CLAUDE.md -> AGENTS.md`. A symlink is accepted only if its fully resolved target remains inside the pinned repository and is a regular file. The manifest hashes the link path, link target, and resolved content. Escaping or directory symlinks fail closed.

## Skill selection

### Product context onboarding

Primary skill:

- `product-marketing`

Its instructions define the comprehensive context sections. The generated document must explicitly retain unknown/unsupported fields rather than fabricating values.

### Release-to-marketing

Primary skill:

- `launch`

Supporting skills:

- `copywriting`
- `social` with explicit `platform-limits.md`
- `emails` with explicit `copy-guidelines.md`

Only the primary skill, these supporting skills, and those named reference files enter the model prompt. Other repository files are indexed for inspection/version history but are not dumped into every prompt.

## Version audit

Every context/workflow AI run records:

- pinned repository commit;
- full repository manifest in `repository_versions`;
- selected skill names/versions in `skills` and `skill_versions`;
- selected skill-version map on the workflow run.

The associated workflow run retains the pinned repository commit and skill versions for audit alongside the approval record.

## Explicit update procedure

Updates are never automatic:

```sh
# Inspect current state
marketing-os skills status

# Choose a reviewed exact upstream commit or immutable tag
marketing-os skills update --ref <commit-or-tag>

# Verify
marketing-os skills status
marketing-os skills list

git diff --submodule=log -- .skills/marketingskills skills.lock.yaml
go test ./...
```

The updater refuses a dirty submodule to avoid discarding local/upstream edits. It fetches the requested ref, resolves it to a commit, checks out detached, validates the repository, then atomically replaces the lock.

If review fails, restore both the submodule commit and lock file together. A lock pointing at a different commit or manifest intentionally blocks AI execution.

## Upstream scripts and assets

The loader inventories `scripts/` and `assets/` paths as metadata. It does **not** execute upstream scripts or send binary assets to the model. Adding executable skill behavior requires a separately reviewed, allowlisted adapter in application code.
