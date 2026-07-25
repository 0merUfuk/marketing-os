CREATE TABLE skill_snapshots (
    id TEXT PRIMARY KEY NOT NULL,
    distribution TEXT NOT NULL,
    repository_url TEXT NOT NULL,
    upstream_ref TEXT NOT NULL,
    upstream_commit TEXT NOT NULL,
    repository_version TEXT NOT NULL,
    upstream_manifest_sha256 TEXT NOT NULL,
    vendored_manifest_sha256 TEXT NOT NULL,
    expected_inventory_sha256 TEXT NOT NULL,
    expected_inventory_count INTEGER NOT NULL CHECK(expected_inventory_count > 0),
    installed_at TEXT NOT NULL,
    UNIQUE(upstream_commit, vendored_manifest_sha256)
);

CREATE INDEX idx_skill_snapshots_upstream_commit
ON skill_snapshots(upstream_commit, installed_at);

CREATE TABLE skill_snapshot_versions (
    snapshot_id TEXT NOT NULL REFERENCES skill_snapshots(id),
    skill_name TEXT NOT NULL,
    version TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    indexed_at TEXT NOT NULL,
    PRIMARY KEY(snapshot_id, skill_name)
);

ALTER TABLE product_context_versions
ADD COLUMN skill_snapshot_id TEXT REFERENCES skill_snapshots(id);

ALTER TABLE workflow_runs
ADD COLUMN skill_snapshot_id TEXT REFERENCES skill_snapshots(id);
