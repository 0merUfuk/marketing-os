# Marketing OS — Absolute Beginner's Guide

This guide walks you through installing, configuring, and running **Marketing OS** end to end. It is written for the first commit (`main`, `e243eee`) and covers real usage, not just theory.

---

## What this project does in one sentence

> Marketing OS reads a GitHub release, loads pinned marketing skills, calls an LLM, validates every claim, and opens a GitHub issue containing drafts for human approval. It never publishes, sends, spends, or approves anything automatically.

---

## Table of contents

1. [Quick mental model](#quick-mental-model)
2. [Prerequisites](#prerequisites)
3. [Installation](#installation)
4. [Configuration](#configuration)
5. [Your first product](#your-first-product)
6. [Building approved product context](#building-approved-product-context)
7. [Running the release-to-marketing workflow](#running-the-release-to-marketing-workflow)
8. [Understanding the output](#understanding-the-output)
9. [Scheduled operation](#scheduled-operation)
10. [Emergency controls](#emergency-controls)
11. [Updating marketing skills](#updating-marketing-skills)
12. [Troubleshooting common failures](#troubleshooting-common-failures)
13. [JSON automation](#json-automation)
14. [File layout reference](#file-layout-reference)
15. [Safety rules you should know](#safety-rules-you-should-know)

---

## Quick mental model

Marketing OS is a **state machine with an LLM in the middle**.

```
GitHub release  ──►  evidence  ──►  model  ──►  validation  ──►  approval issue
     ▲                                              │
     └────────── pinned skills + product context ───┘
```

Three durable things must exist before a real workflow run succeeds:

1. A **registered product**.
2. An **approved product context** (human in the loop).
3. A valid **skills lock** (`skills status` must show `pin_valid: true`).

The workflow then proceeds through explicit states:

```
pending → running → no_action
              └────► awaiting_approval
              └────► failed / cancelled / blocked / killed
```

This first version stops at `awaiting_approval`. A human must read the GitHub issue and decide what to do.

---

## Prerequisites

- **Go 1.25.6 or later**.
- **Git 2.x**.
- A **GitHub personal access token** with at least `repo` scope.
- An **OpenAI-compatible API key** and endpoint. Examples:
  - OpenAI: `https://api.openai.com/v1`
  - OpenRouter: `https://openrouter.ai/api/v1`
  - Local llama.cpp / vLLM / Ollama proxy (loopback only without API key)
- macOS, Linux, or any OS that runs Go. Development was done on macOS ARM64.

---

## Installation

Clone the repository **with submodules** so the pinned marketing skills are available:

```sh
git clone --recurse-submodules https://github.com/0merUfuk/marketing-os.git
cd marketing-os
```

If you already cloned without submodules:

```sh
git submodule update --init --recursive
```

Build the binary:

```sh
go build -o ./bin/marketing-os ./cmd/marketing-os
```

Smoke-test the build:

```sh
./bin/marketing-os --help
```

Verify the pinned skills:

```sh
./bin/marketing-os --config ./config.example.yaml skills status
```

Expected output:

```json
{
  "pin_valid": true,
  "commit_matches": true,
  "manifest_matches": true
}
```

If `pin_valid` is `false`, you cannot run any workflow that uses the model.

---

## Configuration

Copy the example file:

```sh
cp config.example.yaml config.yaml
```

Edit `config.yaml`. The file is **strict YAML**: unknown fields cause an error.

### Minimum required edits

```yaml
database:
  driver: sqlite
  path: ./data/marketing-os.db

workspace:
  products_path: ./products

llm:
  provider: openai-compatible
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  model: gpt-4o-mini
  timeout_seconds: 90
  max_retries: 2
  max_repair_attempts: 1
  max_input_tokens: 30000
  max_output_tokens: 5000
  max_cost_per_run_usd: 1.00
  input_cost_per_million_usd: 0.15
  output_cost_per_million_usd: 0.60

github:
  api_base_url: https://api.github.com
  token_env: GITHUB_TOKEN
  approval_repository: 0merUfuk/marketing-approvals
  approval_labels:
    - marketing-approval
  timeout_seconds: 30
  max_retries: 2

safety:
  global_kill_switch: false
  publishing_enabled: false
  sending_enabled: false
  spending_enabled: false
```

### Important rules

- **Never paste secrets into `config.yaml`.** Only write the environment variable names (`OPENAI_API_KEY`, `GITHUB_TOKEN`).
- `llm.base_url` and `github.api_base_url` must be HTTPS unless they are loopback (`localhost` or `127.x.x.x`).
- `github.approval_repository` must be `owner/repo`. This is where approval issues are opened.
- `safety.*_enabled` must all stay `false` in this version.
- Set real pricing numbers; they are used to reject runs that would exceed `max_cost_per_run_usd`.

### Environment variables

```sh
export OPENAI_API_KEY="sk-..."
export GITHUB_TOKEN="ghp_..."
```

You can put these in your shell profile, a `.env` file loaded by your shell, or pass them inline. Marketing OS only reads them at runtime.

---

## Your first product

### 1. Add the product

```sh
./bin/marketing-os product add \
  --id acme \
  --name "Acme Widget" \
  --repository acme/widget \
  --repository-id 123456789 \
  --local-repository /Users/omerufuk/code/acme-widget \
  --website https://acme.example \
  --docs https://docs.acme.example \
  --pricing https://acme.example/pricing \
  --changelog https://acme.example/changelog \
  --product-type saas \
  --conversion start_trial \
  --language en
```

Rules:

- `--id` must be lowercase kebab-case (e.g. `acme`, `my-product`). Max 64 characters.
- `--repository` is optional but required for the release workflow. Format: `owner/repo`.
- `--repository-id` is optional but strongly recommended. It detects repo renames.
- `--local-repository` is optional. If provided, the context draft reads bounded files from it.
- This command also creates a disabled `release-to-marketing` workflow definition.

### 2. List products

```sh
./bin/marketing-os product list
./bin/marketing-os product inspect acme
```

### 3. Inspect the created workflow

```sh
./bin/marketing-os workflow list acme
```

The `release-to-marketing` workflow starts **disabled**. You must enable it explicitly later.

---

## Building approved product context

The workflow cannot run until a human approves a product context version. This is the most important safety gate.

### 1. Draft context

```sh
./bin/marketing-os context draft acme
```

What happens internally:

- Loads the pinned `product-marketing` skill.
- Collects bounded evidence from:
  - product config,
  - local repository files (if configured),
  - website, docs, pricing, changelog URLs.
- Calls the LLM with a strict JSON schema.
- Validates required headings, evidence IDs, and unsupported/uncertain list.
- Saves the draft as version 1.

### 2. Review the draft

```sh
./bin/marketing-os context show acme
# or a specific version
./bin/marketing-os context show acme --version 1
```

Also open the file on disk:

```text
./products/acme/.agents/product-marketing.v1.draft.md
```

Read every heading and every unsupported statement. Do not approve if the AI invented facts.

### 3. Approve context

```sh
./bin/marketing-os context approve acme 1 --actor "Omer Ufuk Boz"
```

This:

- Supersedes any older approved version.
- Copies the approved context to `./products/acme/.agents/product-marketing.md`.
- Appends a line to `./products/acme/.agents/product-marketing.changelog.md`.

Only after this step can the release workflow proceed.

### 4. Iterate if needed

If the product changes, draft a new version and approve it:

```sh
./bin/marketing-os context draft acme
./bin/marketing-os context approve acme 2 --actor "$USER"
```

---

## Running the release-to-marketing workflow

### 1. Enable the workflow

```sh
./bin/marketing-os workflow enable acme release-to-marketing
```

### 2. Dry run first

A dry run performs all reasoning and validation but writes nothing durable:

```sh
./bin/marketing-os --dry-run workflow run acme release-to-marketing
```

You can also target a specific release:

```sh
./bin/marketing-os --dry-run workflow run acme release-to-marketing --release-id 12345678
```

Expected outcomes:

- `no_action` — release is not marketable enough.
- `awaiting_approval` — drafts were generated and would have been staged.

Dry run does **not** create workflow runs, dedupe keys, evidence rows, cursors, approval records, local approval files, or GitHub issues.

### 3. Real run

```sh
./bin/marketing-os workflow run acme release-to-marketing
```

What happens:

1. Claims a workflow lease with a fencing token.
2. Checks the global kill switch.
3. Fetches the latest published GitHub release (or the one you selected).
4. Verifies the stable `repository_id` matches the product config.
5. Captures immutable release evidence.
6. Loads approved product context and pinned skills.
7. Calls the LLM with bounded prompts and cost/token guards.
8. Validates JSON schema, evidence citations, required channels, forbidden terms.
9. If marketable, persists an approval intent, reconciles GitHub, creates an issue, and writes local approval artifacts.
10. Transitions the run to `awaiting_approval`.

### 4. View the approval

```sh
./bin/marketing-os approvals list
./bin/marketing-os approvals show <approval-id>
```

The approval record contains the GitHub issue URL, generated assets, evidence IDs, cost estimate, and model metadata.

### 5. Read the GitHub issue

Open the issue in `github.approval_repository`. It contains:

- Trigger details.
- Marketability score and classification.
- Evidence sections.
- Customer value summary.
- Drafts for release summary, changelog, LinkedIn, X, and email.
- Unsupported claims and warnings.
- Estimated model cost.
- Explicit approve / request changes / reject instructions.

Only a human can act on that issue.

---

## Understanding the output

### Workflow statuses

| Status | Meaning |
|--------|---------|
| `pending` | Run is waiting to claim a lease. |
| `running` | Run owns the lease and is executing. |
| `no_action` | Release was not marketable; nothing to approve. |
| `awaiting_approval` | Durable approval intent + GitHub issue created. |
| `failed` | Error stopped the run; lease released. |
| `cancelled` | Context cancellation or lease expiry stopped it. |
| `blocked` | Preconditions failed (e.g. no approved context). |
| `killed` | Global kill switch denied execution. |

### Run inspection

```sh
./bin/marketing-os runs list
./bin/marketing-os runs show <run-id>
```

Useful fields:

- `repository_commit` — which marketing skills version was used.
- `skill_versions` — versions of loaded skills.
- `context_version` — which approved product context.
- `input_tokens`, `output_tokens`, `estimated_cost_usd`.
- `evidence_ids` — what evidence the output cited.
- `approval_id` — links to the durable approval.
- `error_code`, `error_message` — if failed.

---

## Scheduled operation

### Start the scheduler

```sh
./bin/marketing-os scheduler start
```

It runs until you press `Ctrl+C` or send SIGTERM. It:

- Reconciles enabled workflows every 30 seconds.
- Checks the kill switch before each scheduled run.
- Respects per-workflow timeouts and retry settings.
- Gracefully waits for active jobs during shutdown.

### Enable scheduling for a product

Scheduling is automatic for any enabled workflow definition. The built-in cadence is `0 */6 * * *` (every 6 hours) for `release-to-marketing`. You can change it in the database or by re-upserting the workflow.

### Run a scheduled workflow manually for testing

```sh
./bin/marketing-os workflow run acme release-to-marketing
```

This uses the same code path as the scheduler.

---

## Emergency controls

### Stop everything

```sh
./bin/marketing-os stop-all
```

This persists the kill switch in SQLite. Running scheduler jobs finish, but no new scheduled runs start. The setting survives restarts.

### Resume

```sh
./bin/marketing-os start-all
```

### Check state

```sh
./bin/marketing-os --json runs list | jq '.[] | {id,status,product_id,workflow_id}'
```

---

## Updating marketing skills

Marketing OS uses a pinned skills submodule. Do not edit files inside `.skills/marketingskills` directly.

### Check status

```sh
./bin/marketing-os skills status
```

### Update to a new upstream commit

```sh
./bin/marketing-os skills update --ref <new-commit-or-tag>
```

This:

- Refuses if the skills repo has local changes.
- Fetches the ref.
- Checks it out detached.
- Computes the repository manifest.
- Atomically writes `skills.lock.yaml`.

After updating, review the diff, then commit the submodule and lock file. Never run workflows with an uncommitted or unmatched lock.

### List loaded skills

```sh
./bin/marketing-os skills list
```

---

## Troubleshooting common failures

### `pin_valid: false`

Run:

```sh
./bin/marketing-os skills status
```

If the commit or manifest does not match, re-run:

```sh
./bin/marketing-os skills update --ref 67264763cb107d61749f418d081c56e5bcbc0209
```

### `no approved product context`

You must draft and approve context before the release workflow runs:

```sh
./bin/marketing-os context draft <product>
./bin/marketing-os context approve <product> 1 --actor "$USER"
```

### `workflow is disabled`

Enable it:

```sh
./bin/marketing-os workflow enable <product> release-to-marketing
```

### `GitHub base URL is invalid`

Use HTTPS for `github.api_base_url` in production. HTTP is only allowed on loopback.

### `LLM token and cost limits must be positive`

Set positive values for `max_input_tokens`, `max_output_tokens`, `max_cost_per_run_usd`, and finite non-negative pricing.

### `model output remained invalid after 1 repair attempt(s)`

The model returned malformed JSON or violated the schema repeatedly. Check:

- Is the model actually structured-output capable?
- Is the prompt context too large? Reduce evidence or use a model with a bigger context window.
- Try `max_repair_attempts: 2` in config.

### `not found: approval`

The approval ID in the command does not exist. Use `approvals list` to see valid IDs.

---

## JSON automation

Add `--json` to any command for machine-readable output:

```sh
./bin/marketing-os --json product list
./bin/marketing-os --json workflow list acme
./bin/marketing-os --json runs list
./bin/marketing-os --json approvals show <id>
./bin/marketing-os --json skills status
```

This is useful for:

- CI/CD pipelines.
- Cron scripts that need the last run status.
- Custom dashboards reading SQLite directly.

Exit codes:

- `0` success.
- Non-zero on config, validation, database, model, GitHub, or workflow errors.

---

## File layout reference

```text
marketing-os/
├── bin/marketing-os              # built binary (gitignored)
├── config.yaml                   # your config (gitignored)
├── config.example.yaml           # safe example with no secrets
├── data/
│   └── marketing-os.db           # SQLite database (gitignored)
├── products/
│   └── acme/
│       ├── product.yaml
│       ├── .agents/
│       │   ├── product-marketing.md
│       │   ├── product-marketing.changelog.md
│       │   └── product-marketing.v1.draft.md
│       ├── approvals/
│       │   └── <approval-id>.md
│       └── drafts/
│           └── <approval-id>.json
├── .skills/marketingskills/      # pinned submodule
├── skills.lock.yaml              # verified lock
├── internal/                     # Go implementation
├── migrations/                   # SQLite schema
├── docs/                         # detailed documentation
└── GUIDE.md                      # this file
```

---

## Safety rules you should know

1. **No autonomous execution.** The first version stops at `awaiting_approval`.
2. **No publishing, sending, spending.** `safety.*_enabled` must stay `false`.
3. **Secrets only via environment variables.** `config.yaml` stores variable names, never values.
4. **Pinned skills.** The model only sees skills that pass manifest verification.
5. **Evidence-grounded.** Every generated asset must cite evidence IDs from the same product.
6. **Bounded.** Token limits, cost limits, timeouts, retry limits, redirect limits, and file-size limits are enforced.
7. **Idempotent.** Re-running the same release produces the same dedupe key and reconciles to one GitHub issue.
8. **Audit trail.** Every claim, transition, kill-switch action, and model call metadata is recorded.

---

## Next steps after this guide

- Run a real dry-run against your own release.
- Tune `max_cost_per_run_usd`, `max_input_tokens`, and the model choice for your budget.
- Read `docs/architecture.md` if you want to extend the system.
- Read `docs/adding-a-workflow.md` if you want to add a second workflow type.

Happy shipping.