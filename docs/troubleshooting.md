# Troubleshooting

## `configuration ... unknown field`

Configuration parsing is strict. Compare the file with `config.example.yaml`; remove misspellings or unsupported keys. Relative paths resolve from the config file directory.

## `required secret environment variable ... is not set`

Set the environment variable named by `llm.api_key_env` or `github.token_env`. Do not put the secret value in YAML.

For an unauthenticated loopback model, set `api_key_env: ""`. Real workflow runs require a GitHub token because issue creation is a write.

## `skills repository is not at locked commit`

Initialize the submodule and verify status:

```sh
git submodule update --init --recursive
marketing-os skills status --json
```

If the submodule is intentionally changing, use the explicit update command and review both submodule and lock diffs. Do not hand-edit only the lock commit.

## `skills repository manifest differs from lock`

A tracked/untracked file, modified skill, or symlink changed under `.skills/marketingskills`. Inspect:

```sh
git -C .skills/marketingskills status --short
git diff --submodule=log -- .skills/marketingskills skills.lock.yaml
```

Restore the pinned content or perform an explicit reviewed update. The workflow fails closed.

## `workflow blocked: approved product context is required`

```sh
marketing-os context versions <product>
marketing-os context draft <product>
marketing-os context show <product>
marketing-os context approve <product> <version> --actor "$USER"
```

Drafting does not approve automatically. Fix explicit unknowns before approval when they affect the workflow.

## `release-to-marketing workflow is disabled`

Product registration deliberately creates it disabled:

```sh
marketing-os workflow list <product>
marketing-os workflow enable <product> release-to-marketing
```

## `global kill switch is enabled`

Inspect why the operator stopped schedules, then resume explicitly:

```sh
marketing-os start-all
```

Changing YAML to `false` does not override a durable `stop-all` state.

## GitHub 401/403

Check token availability, repository selection, and permissions. The product repo needs metadata/contents/releases read. The approval repo needs issues read/write. For GitHub Enterprise, set the correct API base URL ending before endpoint paths.

The client intentionally does not print arbitrary response bodies because they can contain sensitive details.

## GitHub 404 for a private repository

A 404 often means the token cannot see the repository. Verify exact `owner/repository`, fine-grained token repository selection, and organization SSO authorization. If `repository_id` is configured, ensure it is the immutable ID for the same repository.

## Approval issue label error

Configured labels may need to exist in the approval repository. Create them manually or remove unavailable labels from `github.approval_labels`; Marketing OS does not mutate repository label configuration.

## Model rejects `response_format` or JSON Schema

The configured endpoint/model must support OpenAI-compatible `response_format.type=json_schema` with strict schema. Select a capable model/provider or add a reviewed provider adapter. Marketing OS does not silently fall back to unstructured text.

## `model request exceeds configured input token limit`

Reduce source/context size or raise `llm.max_input_tokens` within the actual model context window. Source collectors already cap individual files/responses; inspect approved context and selected skill references for excessive content.

## `model request exceeds configured cost limit`

Check configured per-million prices and `max_cost_per_run_usd`. The adapter estimates maximum cost before HTTP using input estimate + maximum output tokens, then verifies provider-reported actual usage. Workflow definitions also enforce their own maximum cost.

## Structured output remains invalid after repair

Inspect run error details and use `--dry-run --verbose`. Common causes:

- invented/unknown evidence IDs;
- missing required channels;
- maintenance/internal classification combined with staged assets;
- omitted email subject;
- forbidden “Words to Avoid” term;
- provider not honoring strict schema.

Do not increase repair attempts beyond the supported bound to hide a systematic prompt/model problem.

## Workflow says duplicate / creates no new issue

That is expected for the same immutable GitHub release. Dedupe is based on product, workflow, GitHub repository ID, and release ID—not mutable release title. Inspect the prior run/approval:

```sh
marketing-os --json runs list
marketing-os --json approvals list
```

## Workflow was interrupted while creating an issue

Run the same workflow again. The durable `creating` approval contains a hidden marker and exact request. Recovery searches GitHub before creating and finalizes the existing issue if found.

Do not manually delete the local approval row. If the remote issue was manually deleted, preserve evidence and investigate before any database repair.

## `workflow is already running`

Another process owns an unexpired lease. Wait for it to finish. After the configured workflow timeout, a later claim can replace the lease with a higher fencing token; the stale worker cannot finalize.

If it persists, inspect active processes and recent runs. Do not directly delete claims while a worker may still run.

## SQLite `database is locked` / busy

The store uses WAL and a five-second busy timeout. Avoid placing the DB on network filesystems and avoid opening it with tools that hold long write transactions. Stop extra scheduler instances. The single active claim still prevents workflow overlap, but multiple local daemons add needless contention.

## Scheduler does not pick up enable/disable

The running scheduler reconciles definitions every 30 seconds. Wait one interval and inspect structured stderr logs. Ensure `scheduler.enabled: true` and the global kill switch is clear.

## Graceful shutdown times out

The scheduler cancels child contexts and waits up to ten seconds for cron callbacks after cancellation. A provider/network implementation ignoring context may trigger the timeout. Verify adapter tests and endpoint responsiveness.

## Source page was skipped

Context drafting records source-fetch warnings as explicit uncertainty. Product source URLs must be HTTPS (or loopback HTTP), return a successful response, and fit size/content constraints. Fix the URL and create a new context version; existing evidence/context versions remain immutable.

## Runtime files differ from SQLite

SQLite is canonical. Approval/context mirrors are atomic but cannot share a transaction with SQLite. Re-running an idempotent approval/context command can restore a missing mirror. Do not infer workflow completion solely from local files.
