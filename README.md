# Marketing OS

A local-first, evidence-grounded marketing workflow orchestrator written in Go.

Marketing OS monitors durable business events, loads pinned marketing Agent Skills, uses a configured LLM for bounded analysis and drafting, validates every structured result, and stages proposed actions in GitHub Issues for human approval. It does **not** publish, send, spend, approve, or mutate external marketing systems.

## What the first version does

- Maintains isolated workspaces and versioned, explicitly approved context for multiple products.
- Inspects product config, allowlisted repository documents, website, docs, pricing, and changelog sources.
- Loads `coreyhaines31/marketingskills` from a pinned Git submodule/commit and verifies a SHA-256 repository manifest before use.
- Runs `release-to-marketing` manually or on a cron cadence.
- Reads published GitHub releases, captures immutable evidence, scores material marketability, and exits with `no_action` for weak/irrelevant events.
- Generates exactly one release summary, changelog, LinkedIn, X, and email draft when justified.
- Validates strict JSON, evidence IDs, channel allowlists, cost/timeout limits, and approved “Words to Avoid”.
- Creates one idempotent GitHub approval issue and mirrors approval/assets under the product workspace.
- Records runs, attempts, model/provider/token/cost metadata, evidence, approvals, errors, cursors, dedupe keys, and audit events in SQLite.
- Provides a durable global scheduler kill switch.

## Explicit non-goals

No autonomous publishing, email sending, ad spending, purchase, approval, customer outreach, or destructive external mutation. GitHub Issue creation is the only enabled external write. The app is a structured workflow engine with an LLM component—not an open-ended autonomous agent.

## Requirements

- Go 1.25+
- Git 2.x (needed for pinned-skill verification/update)
- SQLite is embedded through a pure-Go driver; no local SQLite package is required
- A structured-output-capable OpenAI-compatible model endpoint
- A GitHub token for real workflow runs (public source reads can be anonymous)

## Setup

```sh
git clone --recurse-submodules <repository-url>
cd marketing-os
cp config.example.yaml config.yaml
```

Edit `config.yaml`:

- Set `llm.model` and endpoint/pricing limits.
- Set `github.approval_repository` to `owner/repository`.
- Keep all three execution capabilities under `safety` set to `false`.

Set credentials only in the environment:

```sh
export OPENAI_API_KEY='...'
export GITHUB_TOKEN='...'
```

Build and verify:

```sh
go mod download
go test ./...
go build -o ./bin/marketing-os ./cmd/marketing-os
./bin/marketing-os skills status
```

The committed lock currently pins upstream repository version `2.8.12` at commit `67264763cb107d61749f418d081c56e5bcbc0209`. `skills status` must report `pin_valid: true` before any AI workflow runs.

## First product

```sh
./bin/marketing-os product add \
  --id acme \
  --name 'Acme' \
  --repository acme/acme \
  --local-repository /absolute/path/to/acme \
  --website https://acme.example \
  --docs https://docs.acme.example \
  --pricing https://acme.example/pricing \
  --changelog https://acme.example/changelog \
  --product-type saas \
  --conversion start_trial \
  --language en

./bin/marketing-os context draft acme
./bin/marketing-os context show acme
./bin/marketing-os context approve acme 1 --actor "$USER"
```

Drafting never auto-approves context. Unknown facts remain explicit and block dependent workflows until a human approves a version.

## Release workflow

```sh
./bin/marketing-os workflow list acme
./bin/marketing-os workflow enable acme release-to-marketing

# Read and reason without DB state, dedupe, cursor, asset, or GitHub writes:
./bin/marketing-os --dry-run workflow run acme release-to-marketing

# Persist state and create an approval issue when marketable:
./bin/marketing-os workflow run acme release-to-marketing

# Run enabled cron definitions until SIGINT/SIGTERM:
./bin/marketing-os scheduler start
```

Emergency stop and recovery:

```sh
./bin/marketing-os stop-all
./bin/marketing-os start-all
```

The kill switch is stored in SQLite and survives process restarts. It prevents scheduled execution without deleting state.

## Documentation

- [Architecture and data flow](docs/architecture.md)
- [Database design and concurrency](docs/database.md)
- [Configuration](docs/configuration.md)
- [CLI reference](docs/cli.md)
- [Pinned Agent Skills](docs/skills.md)
- [Security model](docs/security.md)
- [Adding a workflow](docs/adding-a-workflow.md)
- [Troubleshooting](docs/troubleshooting.md)
- [Implementation plan and acceptance gates](docs/implementation-plan.md)

## Upstream Agent Skills

The marketing guidance is sourced from [`coreyhaines31/marketingskills`](https://github.com/coreyhaines31/marketingskills), retained as a Git submodule with its upstream files and license notices intact. The application does not copy or rewrite upstream skill content. Updates are explicit, reviewable, and lock-file controlled.
