package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"time"
)

type EvidenceInput struct {
	ID         string         `json:"id"`
	ProductID  string         `json:"product_id"`
	SourceType string         `json:"source_type"`
	SourceURL  string         `json:"source_url,omitempty"`
	ExternalID string         `json:"external_id"`
	Content    string         `json:"content"`
	Metadata   map[string]any `json:"metadata,omitempty"`
}

type Evidence struct {
	ID          string         `json:"id"`
	ProductID   string         `json:"product_id"`
	SourceType  string         `json:"source_type"`
	SourceURL   string         `json:"source_url,omitempty"`
	ExternalID  string         `json:"external_id"`
	CapturedAt  time.Time      `json:"captured_at"`
	Content     string         `json:"content"`
	ContentHash string         `json:"content_hash"`
	Metadata    map[string]any `json:"metadata,omitempty"`
}

func ReleaseDedupeKey(productID, workflowID string, repositoryID, releaseID int64) string {
	return hashTuple(productID, workflowID, repositoryID, releaseID)
}

func AssetContentHash(channel, subject, content string) string {
	return hashTuple(channel, subject, content)
}

func ContentHash(content string) string { return hashTuple(content) }

func hashTuple(values ...any) string {
	encoded, _ := json.Marshal(values)
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}
