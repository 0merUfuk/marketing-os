CREATE TABLE IF NOT EXISTS products (
    id TEXT PRIMARY KEY NOT NULL,
    name TEXT NOT NULL,
    repository TEXT NOT NULL DEFAULT '',
    repository_id INTEGER NOT NULL DEFAULT 0,
    local_repository TEXT NOT NULL DEFAULT '',
    website TEXT NOT NULL DEFAULT '',
    documentation_url TEXT NOT NULL DEFAULT '',
    pricing_url TEXT NOT NULL DEFAULT '',
    changelog_url TEXT NOT NULL DEFAULT '',
    product_type TEXT NOT NULL,
    primary_conversion_action TEXT NOT NULL,
    default_language TEXT NOT NULL,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS product_context_versions (
    id TEXT PRIMARY KEY NOT NULL,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    version INTEGER NOT NULL CHECK (version > 0),
    status TEXT NOT NULL CHECK (status IN ('draft', 'approved', 'superseded')),
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    evidence_ids_json TEXT NOT NULL DEFAULT '[]',
    uncertainty_json TEXT NOT NULL DEFAULT '[]',
    created_at TEXT NOT NULL,
    approved_at TEXT,
    approved_by TEXT NOT NULL DEFAULT '',
    UNIQUE(product_id, version)
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_context_one_approved
ON product_context_versions(product_id) WHERE status = 'approved';

CREATE INDEX IF NOT EXISTS idx_context_product_version
ON product_context_versions(product_id, version DESC);
