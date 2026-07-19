package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/omerufuk/marketing-os/internal/domain"
)

var evidenceIDPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._:-]{0,127}$`)

var (
	ErrImmutableConflict = errors.New("immutable evidence conflicts with existing record")
	ErrEvidenceScope     = errors.New("evidence is missing or belongs to another product")
)

func (s *Store) SaveEvidence(ctx context.Context, input domain.EvidenceInput) (domain.Evidence, error) {
	if err := domain.ValidateProductID(input.ProductID); err != nil {
		return domain.Evidence{}, err
	}
	if strings.TrimSpace(input.SourceType) == "" || strings.TrimSpace(input.ExternalID) == "" || strings.TrimSpace(input.Content) == "" {
		return domain.Evidence{}, errors.New("evidence source_type, external_id, and content are required")
	}
	if input.ID == "" {
		input.ID = uuid.NewString()
	}
	if !evidenceIDPattern.MatchString(input.ID) {
		return domain.Evidence{}, errors.New("evidence id contains unsupported characters")
	}
	metadata := input.Metadata
	if metadata == nil {
		metadata = map[string]any{}
	}
	metadataJSON, err := json.Marshal(metadata)
	if err != nil {
		return domain.Evidence{}, fmt.Errorf("encode evidence metadata: %w", err)
	}
	existing, err := s.evidenceByID(ctx, input.ID)
	if err == nil {
		storedMetadata, _ := json.Marshal(existing.Metadata)
		if existing.ProductID == input.ProductID && existing.SourceType == input.SourceType &&
			existing.SourceURL == input.SourceURL && existing.ExternalID == input.ExternalID &&
			existing.ContentHash == domain.ContentHash(input.Content) && string(storedMetadata) == string(metadataJSON) {
			return existing, nil
		}
		return domain.Evidence{}, fmt.Errorf("%w: id %s", ErrImmutableConflict, input.ID)
	}
	if !errors.Is(err, ErrNotFound) {
		return domain.Evidence{}, err
	}
	now := s.now()
	evidence := domain.Evidence{
		ID: input.ID, ProductID: input.ProductID, SourceType: input.SourceType,
		SourceURL: input.SourceURL, ExternalID: input.ExternalID, CapturedAt: now,
		Content: input.Content, ContentHash: domain.ContentHash(input.Content), Metadata: metadata,
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO evidence(
		id, product_id, source_type, source_url, external_id, captured_at,
		content, content_hash, metadata_json
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?)`, evidence.ID, evidence.ProductID,
		evidence.SourceType, evidence.SourceURL, evidence.ExternalID, formatTime(evidence.CapturedAt),
		evidence.Content, evidence.ContentHash, string(metadataJSON))
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return domain.Evidence{}, fmt.Errorf("%w: duplicate natural key or id", ErrImmutableConflict)
		}
		return domain.Evidence{}, fmt.Errorf("insert evidence: %w", err)
	}
	return evidence, nil
}

func (s *Store) evidenceByID(ctx context.Context, id string) (domain.Evidence, error) {
	return scanEvidence(s.db.QueryRowContext(ctx, evidenceSelect+` WHERE id = ?`, id))
}

func (s *Store) EvidenceByIDs(ctx context.Context, productID string, ids []string) ([]domain.Evidence, error) {
	if len(ids) == 0 {
		return []domain.Evidence{}, nil
	}
	unique := make([]string, 0, len(ids))
	seen := map[string]struct{}{}
	for _, id := range ids {
		if _, ok := seen[id]; !ok {
			seen[id] = struct{}{}
			unique = append(unique, id)
		}
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(unique)), ",")
	args := make([]any, len(unique))
	for i, id := range unique {
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx, evidenceSelect+` WHERE id IN (`+placeholders+`)`, args...)
	if err != nil {
		return nil, fmt.Errorf("query evidence: %w", err)
	}
	defer rows.Close()
	result := make([]domain.Evidence, 0, len(unique))
	for rows.Next() {
		evidence, err := scanEvidence(rows)
		if err != nil {
			return nil, err
		}
		if evidence.ProductID != productID {
			return nil, fmt.Errorf("%w: %s", ErrEvidenceScope, evidence.ID)
		}
		result = append(result, evidence)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(result) != len(unique) {
		return nil, fmt.Errorf("%w: one or more ids were not found", ErrEvidenceScope)
	}
	return result, nil
}

const evidenceSelect = `SELECT id, product_id, source_type, source_url, external_id,
	captured_at, content, content_hash, metadata_json FROM evidence`

func scanEvidence(row rowScanner) (domain.Evidence, error) {
	var evidence domain.Evidence
	var captured, metadataJSON string
	if err := row.Scan(&evidence.ID, &evidence.ProductID, &evidence.SourceType,
		&evidence.SourceURL, &evidence.ExternalID, &captured, &evidence.Content,
		&evidence.ContentHash, &metadataJSON); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Evidence{}, fmt.Errorf("%w: evidence", ErrNotFound)
		}
		return domain.Evidence{}, err
	}
	evidence.CapturedAt, _ = parseTime(captured)
	if err := json.Unmarshal([]byte(metadataJSON), &evidence.Metadata); err != nil {
		return domain.Evidence{}, fmt.Errorf("decode evidence metadata: %w", err)
	}
	return evidence, nil
}
