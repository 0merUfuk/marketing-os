# Configuration

Marketing OS reads one strict YAML file (`--config`, default `./config.yaml`). Unknown keys, invalid URL schemes, unsafe execution capabilities, and out-of-range limits fail startup. Relative paths are resolved from the configuration file’s directory, not the caller’s current directory.

Start from `config.example.yaml`:

```sh
cp config.example.yaml config.yaml
```

`config.yaml` is ignored by Git. Never place credential values in it; configure only environment-variable names.

## Database

```yaml
database:
  driver: sqlite
  path: ./data/marketing-os.db
```

- `driver`: only `sqlite` is supported in the first version.
- `path`: local database path; parent directories are created with restrictive permissions.

## Workspace

```yaml
workspace:
  products_path: ./products
```

Each product receives its own tree:

```text
products/<product>/
  product.yaml
  .agents/
    product-marketing-context.md
    product-marketing-context.draft.md
  evidence/
  research/
  drafts/
  reports/
  approvals/
  state/
```

Runtime evidence/draft/report/approval/state paths are ignored by the supplied `.gitignore` because they may contain private material. The canonical context is not automatically ignored; choose repository policy deliberately.

## Agent Skills

```yaml
skills:
  repository_path: ./.skills/marketingskills
  lock_file: ./skills.lock.yaml
```

The repository path should point to the `coreyhaines31/marketingskills` Git submodule. The lock file records repository URL, requested ref, resolved commit, upstream version, full-manifest SHA-256, and update time. No AI operation proceeds when the commit or manifest differs.

## LLM

```yaml
llm:
  provider: openai-compatible
  base_url: https://api.openai.com/v1
  api_key_env: OPENAI_API_KEY
  model: configured-model
  timeout_seconds: 90
  max_retries: 2
  max_repair_attempts: 1
  max_input_tokens: 30000
  max_output_tokens: 5000
  max_cost_per_run_usd: 1.00
  input_cost_per_million_usd: 0
  output_cost_per_million_usd: 0
```

- `provider`: audit label for the configured OpenAI-compatible provider.
- `base_url`: HTTPS is required except for loopback HTTP (`localhost`, `127.0.0.1`, `::1`). `/chat/completions` is appended.
- `api_key_env`: environment variable read at runtime. Leave empty only for an unauthenticated local endpoint.
- `model`: provider model identifier; no model is hardcoded in source.
- `timeout_seconds`: transport request timeout. Workflow definitions also have an overall timeout.
- `max_retries`: bounded retries for rate limits and server failures (0–5); non-retryable 4xx responses fail immediately.
- `max_repair_attempts`: bounded structured-output repairs (0–2).
- `max_input_tokens`: conservative preflight estimate limit; requests above it fail before HTTP.
- `max_output_tokens`: response bound supplied to the provider.
- `max_cost_per_run_usd`: model-adapter preflight/actual cost ceiling.
- token prices: used for estimated cost metadata. If zero, usage is still recorded but estimated monetary cost is zero.

The adapter uses an approximate four-characters-per-token preflight estimate. Provider-reported usage is authoritative after a successful response.

## GitHub

```yaml
github:
  api_base_url: https://api.github.com
  token_env: GITHUB_TOKEN
  approval_repository: owner/marketing-approvals
  approval_labels: [marketing-approval]
  timeout_seconds: 30
  max_retries: 2
```

- `api_base_url`: HTTPS, or loopback HTTP for tests/local emulation.
- `token_env`: runtime token environment variable.
- `approval_repository`: the only remote write destination, in `owner/repository` format.
- `approval_labels`: labels requested on new approval issues. Labels must already exist if GitHub enforces that requirement.
- timeout/retry values are bounded and respect context cancellation and `Retry-After`.

Minimum practical fine-grained token permissions:

- Product repository: Metadata read, Contents read, Releases read (and access to private repository if applicable).
- Approval repository: Metadata read, Issues read/write.

Use the narrowest repository selection possible.

## Scheduler

```yaml
scheduler:
  enabled: true
  retry_delay_seconds: 30
  max_retries: 1
```

Enabled workflow definitions are reconciled every 30 seconds while the scheduler runs. Each definition owns its cron cadence, timeout, overlap policy, cooldown/stop/error behavior, and maximum cost. A scheduler retry calls the same workflow service as manual execution; durable dedupe and leases remain authoritative.

## Safety

```yaml
safety:
  global_kill_switch: false
  publishing_enabled: false
  sending_enabled: false
  spending_enabled: false
```

All three capability flags must remain `false`; configuration validation rejects the file otherwise. Setting `global_kill_switch: true` activates the durable switch at startup. Setting it back to false in YAML does **not** silently override an operator-issued `stop-all`; use `marketing-os start-all` explicitly.

## Logging

```yaml
logging:
  level: info
```

Supported levels are `debug`, `info`, `warn`, and `error`. Logs are structured JSON on stderr. `--json` controls stdout command results and remains parseable because logs use a separate stream. `--verbose` enables debug logging for that invocation.
