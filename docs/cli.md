# CLI reference

```text
marketing-os [--config path] [--json] [--dry-run] [--verbose] <command>
```

Global flags:

| Flag | Meaning |
|---|---|
| `--config` | Strict YAML configuration path (default `./config.yaml`) |
| `--json` | Machine-readable indented JSON on stdout |
| `--dry-run` | Workflow reasoning without DB/dedupe/cursor/asset/GitHub writes |
| `-v`, `--verbose` | Debug-level structured logs on stderr |

## Products

```sh
marketing-os product add \
  --id <slug> --name <name> \
  --repository <owner/repo> [--repository-id <github-id>] \
  [--local-repository /absolute/path] \
  [--website https://...] [--docs https://...] \
  [--pricing https://...] [--changelog https://...] \
  --product-type <type> --conversion <action> [--language en]

marketing-os product list
marketing-os product inspect <product>
```

`product add` initializes the isolated workspace, inserts the product, and creates a complete `release-to-marketing` definition in the **disabled** state. `repository-id` is optional but recommended: the workflow verifies it against GitHub to detect repository rename/replacement mistakes.

## Product context

```sh
marketing-os context draft <product>
marketing-os context show <product> [--version N]
marketing-os context approve <product> <version> [--actor name]
marketing-os context versions <product>
```

- `draft` collects bounded source evidence, loads the pinned `product-marketing` skill, calls the model, validates all required sections/evidence IDs, and saves an unapproved version.
- `show` prints the latest version unless `--version` is supplied.
- `approve` is the only operation that makes a version canonical. It supersedes the prior approved version and mirrors the content to `.agents/product-marketing-context.md` using atomic file replacement.
- Workflows remain blocked until an approved version exists.

## Skills

```sh
marketing-os skills status
marketing-os skills list
marketing-os skills update --ref <commit-or-tag> \
  [--repository https://github.com/coreyhaines31/marketingskills.git]
```

`status` verifies both exact Git commit and repository manifest. `list` parses and validates all skill frontmatter. `update` is explicit: it refuses a dirty skills repository, fetches the requested ref, checks it out detached, computes the manifest, and atomically writes `skills.lock.yaml`. Review the submodule diff and lock file before accepting an update.

## Workflows

```sh
marketing-os workflow list <product>
marketing-os workflow enable <product> release-to-marketing
marketing-os workflow disable <product> release-to-marketing
marketing-os workflow run <product> release-to-marketing [--release-id ID]
marketing-os --dry-run workflow run <product> release-to-marketing [--release-id ID]
```

Manual and scheduled executions call the same `ReleaseWorkflow.Run` implementation. `--release-id` selects a published release; without it the workflow requests the latest published release. Draft/prerelease filtering is enforced by the GitHub adapter.

Dry-run behavior:

- reads configured product/context/skills and GitHub evidence;
- calls the configured model and applies all validators/cost bounds;
- returns the predicted `no_action` or `awaiting_approval` outcome;
- does not create a workflow run, evidence row, claim, dedupe key, cursor, approval, asset file, or GitHub issue.

## Approvals

```sh
marketing-os approvals list
marketing-os approvals show <approval-id>
```

`show` returns the durable approval record and all generated assets. The linked GitHub issue includes evidence, proposed action, drafts, risks/warnings, estimated cost, and explicit approve/request-changes/reject instructions. This version does not ingest the human decision or execute it.

## Runs

```sh
marketing-os runs list
marketing-os runs show <run-id>
```

Run detail includes status, attempt, trigger, pinned repository commit, skill versions, approved context version, provider/model, evidence IDs, tokens, cost estimate, output hash, approval ID, and structured error fields.

Statuses are:

```text
pending running no_action awaiting_approval completed failed cancelled blocked killed
```

`completed` is reserved for future approved-action execution; first-version generation stops at `awaiting_approval`.

## Scheduler and kill switch

```sh
marketing-os scheduler start
marketing-os stop-all
marketing-os start-all
```

`scheduler start` runs until SIGINT/SIGTERM or parent context cancellation. It dynamically reconciles enabled definitions, enforces per-run timeouts/retries, checks the global kill switch before every attempt, and waits for active jobs during graceful shutdown.

`stop-all` persists the global kill switch. It does not delete definitions, runs, approvals, dedupe keys, or cursors. `start-all` explicitly clears it.

## Automation-safe output

Put `--json` before or after the command:

```sh
marketing-os --json runs list
marketing-os --json approvals show <id>
```

Command results go to stdout; JSON logs/errors go to stderr. Exit code is non-zero on validation, configuration, API, persistence, safety, or workflow failure.
