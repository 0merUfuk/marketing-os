package state

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/omerufuk/marketing-os/internal/domain"
	migrationfiles "github.com/omerufuk/marketing-os/migrations"
	_ "modernc.org/sqlite"
)

var (
	ErrNotFound          = errors.New("not found")
	ErrDuplicate         = errors.New("duplicate")
	ErrUnapprovedContext = errors.New("approved product context is required")
	ErrBusy              = errors.New("workflow is already running")
)

type Store struct {
	db  *sql.DB
	now func() time.Time
}

func Open(ctx context.Context, path string) (*Store, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("database path is required")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("absolute database path: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(absolute), 0o700); err != nil {
		return nil, fmt.Errorf("create database directory: %w", err)
	}
	u := &url.URL{Scheme: "file", Path: absolute}
	q := u.Query()
	for _, pragma := range []string{"foreign_keys(1)", "busy_timeout(5000)", "journal_mode(WAL)", "synchronous(FULL)"} {
		q.Add("_pragma", pragma)
	}
	q.Set("_txlock", "immediate")
	u.RawQuery = q.Encode()
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(4)
	store := &Store{db: db, now: func() time.Time { return time.Now().UTC() }}
	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := store.Migrate(ctx); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func (s *Store) Close() error { return s.db.Close() }

func (s *Store) Migrate(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		name TEXT PRIMARY KEY NOT NULL,
		applied_at TEXT NOT NULL
	)`); err != nil {
		return fmt.Errorf("create migrations table: %w", err)
	}
	entries, err := fs.ReadDir(migrationfiles.Files, ".")
	if err != nil {
		return fmt.Errorf("read embedded migrations: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".sql") {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		var exists int
		err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name = ?`, name).Scan(&exists)
		if err != nil {
			return fmt.Errorf("check migration %s: %w", name, err)
		}
		if exists == 1 {
			continue
		}
		sqlBytes, err := migrationfiles.Files.ReadFile(name)
		if err != nil {
			return fmt.Errorf("read migration %s: %w", name, err)
		}
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return fmt.Errorf("begin migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, string(sqlBytes)); err != nil {
			tx.Rollback()
			return fmt.Errorf("apply migration %s: %w", name, err)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO schema_migrations(name, applied_at) VALUES(?, ?)`, name, formatTime(s.now())); err != nil {
			tx.Rollback()
			return fmt.Errorf("record migration %s: %w", name, err)
		}
		if err := tx.Commit(); err != nil {
			return fmt.Errorf("commit migration %s: %w", name, err)
		}
	}
	return nil
}

func (s *Store) AddProduct(ctx context.Context, p domain.Product) error {
	if err := p.Validate(); err != nil {
		return err
	}
	now := formatTime(s.now())
	_, err := s.db.ExecContext(ctx, `INSERT INTO products(
		id, name, repository, repository_id, local_repository, website,
		documentation_url, pricing_url, changelog_url, product_type,
		primary_conversion_action, default_language, created_at, updated_at
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		p.ID, p.Name, p.Repository, p.RepositoryID, p.LocalRepository, p.Website,
		p.DocumentationURL, p.PricingURL, p.ChangelogURL, p.ProductType,
		p.PrimaryConversionAction, p.DefaultLanguage, now, now)
	if err != nil {
		if strings.Contains(strings.ToLower(err.Error()), "unique") {
			return fmt.Errorf("%w: product %s", ErrDuplicate, p.ID)
		}
		return fmt.Errorf("insert product: %w", err)
	}
	return nil
}

func (s *Store) GetProduct(ctx context.Context, id string) (domain.Product, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id, name, repository, repository_id,
		local_repository, website, documentation_url, pricing_url, changelog_url,
		product_type, primary_conversion_action, default_language, created_at, updated_at
		FROM products WHERE id = ?`, id)
	var p domain.Product
	var created, updated string
	if err := row.Scan(&p.ID, &p.Name, &p.Repository, &p.RepositoryID, &p.LocalRepository,
		&p.Website, &p.DocumentationURL, &p.PricingURL, &p.ChangelogURL, &p.ProductType,
		&p.PrimaryConversionAction, &p.DefaultLanguage, &created, &updated); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.Product{}, fmt.Errorf("%w: product %s", ErrNotFound, id)
		}
		return domain.Product{}, fmt.Errorf("get product: %w", err)
	}
	p.CreatedAt, _ = parseTime(created)
	p.UpdatedAt, _ = parseTime(updated)
	return p, nil
}

func (s *Store) ListProducts(ctx context.Context) ([]domain.Product, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, repository, repository_id,
		local_repository, website, documentation_url, pricing_url, changelog_url,
		product_type, primary_conversion_action, default_language, created_at, updated_at
		FROM products ORDER BY id`)
	if err != nil {
		return nil, fmt.Errorf("list products: %w", err)
	}
	defer rows.Close()
	var products []domain.Product
	for rows.Next() {
		var p domain.Product
		var created, updated string
		if err := rows.Scan(&p.ID, &p.Name, &p.Repository, &p.RepositoryID, &p.LocalRepository,
			&p.Website, &p.DocumentationURL, &p.PricingURL, &p.ChangelogURL, &p.ProductType,
			&p.PrimaryConversionAction, &p.DefaultLanguage, &created, &updated); err != nil {
			return nil, fmt.Errorf("scan product: %w", err)
		}
		p.CreatedAt, _ = parseTime(created)
		p.UpdatedAt, _ = parseTime(updated)
		products = append(products, p)
	}
	return products, rows.Err()
}

func (s *Store) CreateContextDraft(ctx context.Context, productID, content string, evidenceIDs, uncertainty []string) (domain.ContextVersion, error) {
	if strings.TrimSpace(content) == "" {
		return domain.ContextVersion{}, errors.New("context content is required")
	}
	if _, err := s.GetProduct(ctx, productID); err != nil {
		return domain.ContextVersion{}, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ContextVersion{}, fmt.Errorf("begin context draft: %w", err)
	}
	defer tx.Rollback()
	var version int
	if err := tx.QueryRowContext(ctx, `SELECT COALESCE(MAX(version), 0) + 1 FROM product_context_versions WHERE product_id = ?`, productID).Scan(&version); err != nil {
		return domain.ContextVersion{}, fmt.Errorf("next context version: %w", err)
	}
	evidenceJSON, _ := json.Marshal(nonNilStrings(evidenceIDs))
	uncertaintyJSON, _ := json.Marshal(nonNilStrings(uncertainty))
	now := s.now()
	v := domain.ContextVersion{
		ID: uuid.NewString(), ProductID: productID, Version: version, Status: domain.ContextDraft,
		Content: content, ContentHash: hashString(content), EvidenceIDs: nonNilStrings(evidenceIDs),
		Uncertainty: nonNilStrings(uncertainty), CreatedAt: now,
	}
	_, err = tx.ExecContext(ctx, `INSERT INTO product_context_versions(
		id, product_id, version, status, content, content_hash, evidence_ids_json,
		uncertainty_json, created_at, approved_by
	) VALUES(?, ?, ?, ?, ?, ?, ?, ?, ?, '')`,
		v.ID, v.ProductID, v.Version, v.Status, v.Content, v.ContentHash,
		string(evidenceJSON), string(uncertaintyJSON), formatTime(now))
	if err != nil {
		return domain.ContextVersion{}, fmt.Errorf("insert context draft: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return domain.ContextVersion{}, fmt.Errorf("commit context draft: %w", err)
	}
	return v, nil
}

func (s *Store) ApproveContext(ctx context.Context, productID string, version int, actor string) (domain.ContextVersion, error) {
	if strings.TrimSpace(actor) == "" {
		return domain.ContextVersion{}, errors.New("approval actor is required")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return domain.ContextVersion{}, err
	}
	defer tx.Rollback()
	v, err := scanContext(tx.QueryRowContext(ctx, contextSelect+` WHERE product_id = ? AND version = ?`, productID, version))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return domain.ContextVersion{}, fmt.Errorf("%w: context %s v%d", ErrNotFound, productID, version)
		}
		return domain.ContextVersion{}, err
	}
	if v.Status == domain.ContextApproved {
		return v, tx.Commit()
	}
	if v.Status != domain.ContextDraft {
		return domain.ContextVersion{}, errors.New("only a draft context can be approved")
	}
	now := s.now()
	if _, err := tx.ExecContext(ctx, `UPDATE product_context_versions SET status = 'superseded' WHERE product_id = ? AND status = 'approved'`, productID); err != nil {
		return domain.ContextVersion{}, fmt.Errorf("supersede context: %w", err)
	}
	result, err := tx.ExecContext(ctx, `UPDATE product_context_versions
		SET status = 'approved', approved_at = ?, approved_by = ?
		WHERE id = ? AND status = 'draft'`, formatTime(now), actor, v.ID)
	if err != nil {
		return domain.ContextVersion{}, fmt.Errorf("approve context: %w", err)
	}
	if changed, _ := result.RowsAffected(); changed != 1 {
		return domain.ContextVersion{}, errors.New("context approval lost a concurrent update")
	}
	if err := tx.Commit(); err != nil {
		return domain.ContextVersion{}, err
	}
	v.Status, v.ApprovedAt, v.ApprovedBy = domain.ContextApproved, &now, actor
	return v, nil
}

const contextSelect = `SELECT id, product_id, version, status, content, content_hash,
	evidence_ids_json, uncertainty_json, created_at, approved_at, approved_by
	FROM product_context_versions`

func (s *Store) ApprovedContext(ctx context.Context, productID string) (domain.ContextVersion, error) {
	v, err := scanContext(s.db.QueryRowContext(ctx, contextSelect+` WHERE product_id = ? AND status = 'approved'`, productID))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ContextVersion{}, fmt.Errorf("%w: product %s", ErrUnapprovedContext, productID)
	}
	return v, err
}

func (s *Store) GetContextVersion(ctx context.Context, productID string, version int) (domain.ContextVersion, error) {
	v, err := scanContext(s.db.QueryRowContext(ctx, contextSelect+` WHERE product_id = ? AND version = ?`, productID, version))
	if errors.Is(err, sql.ErrNoRows) {
		return domain.ContextVersion{}, fmt.Errorf("%w: context %s v%d", ErrNotFound, productID, version)
	}
	return v, err
}

func (s *Store) ListContextVersions(ctx context.Context, productID string) ([]domain.ContextVersion, error) {
	rows, err := s.db.QueryContext(ctx, contextSelect+` WHERE product_id = ? ORDER BY version DESC`, productID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []domain.ContextVersion
	for rows.Next() {
		v, err := scanContext(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, v)
	}
	return result, rows.Err()
}

type rowScanner interface{ Scan(...any) error }

func scanContext(row rowScanner) (domain.ContextVersion, error) {
	var v domain.ContextVersion
	var status, evidenceJSON, uncertaintyJSON, created string
	var approved sql.NullString
	if err := row.Scan(&v.ID, &v.ProductID, &v.Version, &status, &v.Content, &v.ContentHash,
		&evidenceJSON, &uncertaintyJSON, &created, &approved, &v.ApprovedBy); err != nil {
		return domain.ContextVersion{}, err
	}
	v.Status = domain.ContextStatus(status)
	v.CreatedAt, _ = parseTime(created)
	_ = json.Unmarshal([]byte(evidenceJSON), &v.EvidenceIDs)
	_ = json.Unmarshal([]byte(uncertaintyJSON), &v.Uncertainty)
	if approved.Valid {
		t, err := parseTime(approved.String)
		if err == nil {
			v.ApprovedAt = &t
		}
	}
	return v, nil
}

func hashString(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}

func nonNilStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}

func formatTime(t time.Time) string             { return t.UTC().Format(time.RFC3339Nano) }
func parseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
