CREATE TABLE IF NOT EXISTS evidence (
    id TEXT PRIMARY KEY NOT NULL,
    product_id TEXT NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    source_type TEXT NOT NULL,
    source_url TEXT NOT NULL DEFAULT '',
    external_id TEXT NOT NULL,
    captured_at TEXT NOT NULL,
    content TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    metadata_json TEXT NOT NULL DEFAULT '{}',
    UNIQUE(product_id, source_type, external_id, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_evidence_product_captured
ON evidence(product_id, captured_at DESC);
