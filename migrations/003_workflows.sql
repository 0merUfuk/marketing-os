CREATE TABLE IF NOT EXISTS repository_versions (
    commit_sha TEXT PRIMARY KEY NOT NULL,
    repository_url TEXT NOT NULL,
    pinned_ref TEXT NOT NULL,
    repository_version TEXT NOT NULL DEFAULT '',
    manifest_sha256 TEXT NOT NULL,
    installed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skills (
    name TEXT PRIMARY KEY NOT NULL,
    description TEXT NOT NULL,
    current_version TEXT NOT NULL DEFAULT '',
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS skill_versions (
    skill_name TEXT NOT NULL REFERENCES skills(name) ON DELETE CASCADE,
    version TEXT NOT NULL,
    repository_commit TEXT NOT NULL REFERENCES repository_versions(commit_sha),
    metadata_json TEXT NOT NULL DEFAULT '{}',
    indexed_at TEXT NOT NULL,
    PRIMARY KEY(skill_name, version, repository_commit)
);

CREATE TABLE IF NOT EXISTS workflows (
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    id TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    cadence TEXT NOT NULL,
    activation_condition TEXT NOT NULL,
    purpose TEXT NOT NULL,
    primary_skill TEXT NOT NULL,
    supporting_skills_json TEXT NOT NULL,
    required_inputs_json TEXT NOT NULL,
    ordered_steps_json TEXT NOT NULL,
    self_check TEXT NOT NULL,
    state_requirements_json TEXT NOT NULL,
    dedupe_key_template TEXT NOT NULL,
    cooldown_seconds INTEGER NOT NULL DEFAULT 0,
    stop_condition TEXT NOT NULL,
    error_behavior TEXT NOT NULL,
    output_destination TEXT NOT NULL,
    approval_policy TEXT NOT NULL,
    max_cost_usd REAL NOT NULL CHECK(max_cost_usd > 0),
    timeout_seconds INTEGER NOT NULL CHECK(timeout_seconds > 0),
    enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)),
    allow_overlap INTEGER NOT NULL DEFAULT 0 CHECK(allow_overlap IN (0,1)),
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(product_id, id)
);

CREATE TABLE IF NOT EXISTS workflow_runs (
    id TEXT PRIMARY KEY NOT NULL,
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    trigger_type TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    status TEXT NOT NULL CHECK(status IN ('pending','running','no_action','awaiting_approval','completed','failed','cancelled','blocked','killed')),
    attempt INTEGER NOT NULL CHECK(attempt > 0),
    fencing_token INTEGER NOT NULL,
    repository_commit TEXT NOT NULL DEFAULT '',
    skill_versions_json TEXT NOT NULL DEFAULT '{}',
    context_version INTEGER NOT NULL DEFAULT 0,
    model_provider TEXT NOT NULL DEFAULT '',
    model_name TEXT NOT NULL DEFAULT '',
    evidence_ids_json TEXT NOT NULL DEFAULT '[]',
    started_at TEXT NOT NULL,
    finished_at TEXT,
    input_tokens INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    estimated_cost_usd REAL NOT NULL DEFAULT 0,
    output_hash TEXT NOT NULL DEFAULT '',
    approval_id TEXT NOT NULL DEFAULT '',
    error_code TEXT NOT NULL DEFAULT '',
    error_message TEXT NOT NULL DEFAULT '',
    dry_run INTEGER NOT NULL DEFAULT 0 CHECK(dry_run IN (0,1)),
    FOREIGN KEY(product_id, workflow_id) REFERENCES workflows(product_id, id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_runs_product_started ON workflow_runs(product_id, started_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_workflow_status ON workflow_runs(product_id, workflow_id, status);

CREATE TABLE IF NOT EXISTS workflow_claims (
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    fencing_token INTEGER NOT NULL CHECK(fencing_token > 0),
    lease_expires_at TEXT NOT NULL,
    claimed_at TEXT NOT NULL,
    PRIMARY KEY(product_id, workflow_id),
    FOREIGN KEY(product_id, workflow_id) REFERENCES workflows(product_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS dedupe_keys (
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    input_hash TEXT NOT NULL,
    state TEXT NOT NULL CHECK(state IN ('in_flight','failed','completed')),
    run_id TEXT NOT NULL REFERENCES workflow_runs(id),
    lease_expires_at TEXT NOT NULL,
    completed_at TEXT,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(product_id, workflow_id, dedupe_key),
    FOREIGN KEY(product_id, workflow_id) REFERENCES workflows(product_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS approvals (
    id TEXT PRIMARY KEY NOT NULL,
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    dedupe_key TEXT NOT NULL,
    trigger_id TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES workflow_runs(id),
    status TEXT NOT NULL CHECK(status IN ('creating','awaiting_approval','approved','changes_requested','rejected','failed')),
    evidence_summary_json TEXT NOT NULL,
    proposed_action_json TEXT NOT NULL,
    risks_json TEXT NOT NULL DEFAULT '[]',
    warnings_json TEXT NOT NULL DEFAULT '[]',
    estimated_cost_usd REAL NOT NULL DEFAULT 0,
    issue_repository TEXT NOT NULL,
    issue_marker TEXT NOT NULL,
    issue_request_hash TEXT NOT NULL,
    issue_title TEXT NOT NULL,
    issue_body TEXT NOT NULL,
    issue_id INTEGER,
    issue_number INTEGER,
    issue_url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    UNIQUE(product_id, workflow_id, dedupe_key),
    UNIQUE(issue_repository, issue_id)
);

CREATE TABLE IF NOT EXISTS generated_assets (
    id TEXT PRIMARY KEY NOT NULL,
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    run_id TEXT NOT NULL REFERENCES workflow_runs(id),
    approval_id TEXT NOT NULL REFERENCES approvals(id) ON DELETE CASCADE,
    channel TEXT NOT NULL,
    subject TEXT NOT NULL DEFAULT '',
    content TEXT NOT NULL,
    evidence_ids_json TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(product_id, workflow_id, channel, content_hash)
);

CREATE TABLE IF NOT EXISTS approval_history (
    id TEXT PRIMARY KEY NOT NULL,
    approval_id TEXT NOT NULL REFERENCES approvals(id) ON DELETE CASCADE,
    from_status TEXT NOT NULL,
    to_status TEXT NOT NULL,
    actor TEXT NOT NULL,
    external_event_id TEXT NOT NULL DEFAULT '',
    note TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(approval_id, external_event_id)
);

CREATE TABLE IF NOT EXISTS cursors (
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL,
    name TEXT NOT NULL,
    value TEXT NOT NULL,
    updated_at TEXT NOT NULL,
    PRIMARY KEY(product_id, workflow_id, name),
    FOREIGN KEY(product_id, workflow_id) REFERENCES workflows(product_id, id) ON DELETE CASCADE
);

CREATE TABLE IF NOT EXISTS errors (
    id TEXT PRIMARY KEY NOT NULL,
    run_id TEXT NOT NULL REFERENCES workflow_runs(id) ON DELETE CASCADE,
    code TEXT NOT NULL,
    message TEXT NOT NULL,
    retryable INTEGER NOT NULL CHECK(retryable IN (0,1)),
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS audit_events (
    id TEXT PRIMARY KEY NOT NULL,
    product_id TEXT NOT NULL,
    workflow_id TEXT NOT NULL DEFAULT '',
    run_id TEXT NOT NULL DEFAULT '',
    event_type TEXT NOT NULL,
    data_json TEXT NOT NULL DEFAULT '{}',
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_product_created ON audit_events(product_id, created_at DESC);

CREATE TABLE IF NOT EXISTS scheduler_state (
    singleton INTEGER PRIMARY KEY CHECK(singleton = 1),
    kill_switch INTEGER NOT NULL DEFAULT 0 CHECK(kill_switch IN (0,1)),
    updated_at TEXT NOT NULL,
    reason TEXT NOT NULL DEFAULT ''
);

INSERT INTO scheduler_state(singleton, kill_switch, updated_at, reason)
VALUES(1, 0, strftime('%Y-%m-%dT%H:%M:%fZ','now'), '')
ON CONFLICT(singleton) DO NOTHING;
