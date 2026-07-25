package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/omerufuk/marketing-os/internal/skills"
)

var ErrSkillSnapshotConflict = errors.New("immutable skill snapshot conflicts with existing record")

func (s *Store) SyncSkillSnapshot(ctx context.Context, snapshot skills.SnapshotMetadata, indexed []skills.Skill) error {
	if err := snapshot.Validate(); err != nil {
		return fmt.Errorf("validate skill snapshot: %w", err)
	}
	if err := snapshot.ValidateInventory(indexed); err != nil {
		return fmt.Errorf("validate skill snapshot inventory: %w", err)
	}
	versions, err := encodeSkillSnapshotVersions(indexed)
	if err != nil {
		return err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(s.now())
	_, err = tx.ExecContext(ctx, `INSERT INTO skill_snapshots(
		id,distribution,repository_url,upstream_ref,upstream_commit,repository_version,
		upstream_manifest_sha256,vendored_manifest_sha256,expected_inventory_sha256,
		expected_inventory_count,installed_at
	) VALUES(?,?,?,?,?,?,?,?,?,?,?)`,
		snapshot.ID, snapshot.Distribution, snapshot.Repository, snapshot.Ref, snapshot.Commit,
		snapshot.RepositoryVersion, snapshot.UpstreamManifestSHA256, snapshot.VendoredManifestSHA256,
		snapshot.ExpectedInventorySHA256, snapshot.ExpectedInventoryCount, now)
	if err != nil {
		existing, lookupErr := scanSkillSnapshot(tx.QueryRowContext(ctx, `SELECT
			id,distribution,repository_url,upstream_ref,upstream_commit,repository_version,
			upstream_manifest_sha256,vendored_manifest_sha256,expected_inventory_sha256,
			expected_inventory_count
			FROM skill_snapshots WHERE id=?`, snapshot.ID))
		if errors.Is(lookupErr, sql.ErrNoRows) {
			return err
		}
		if lookupErr != nil {
			return lookupErr
		}
		if existing != snapshot {
			return fmt.Errorf("%w: snapshot %s metadata changed", ErrSkillSnapshotConflict, snapshot.ID)
		}
		if err := verifySkillSnapshotVersions(ctx, tx, snapshot.ID, versions); err != nil {
			return err
		}
		return tx.Commit()
	}
	for i, skill := range indexed {
		_, err = tx.ExecContext(ctx, `INSERT INTO skills(name,description,current_version,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(name) DO UPDATE SET description=excluded.description,current_version=excluded.current_version,updated_at=excluded.updated_at`,
			skill.Name, skill.Description, skill.Version, now)
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO skill_snapshot_versions(
			snapshot_id,skill_name,version,metadata_json,indexed_at
		) VALUES(?,?,?,?,?)`, snapshot.ID, versions[i].name, versions[i].version, versions[i].metadata, now)
		if err != nil {
			return fmt.Errorf("insert immutable skill %s for snapshot %s: %w", skill.Name, snapshot.ID, err)
		}
	}
	return tx.Commit()
}

type encodedSkillSnapshotVersion struct {
	name     string
	version  string
	metadata string
}

func encodeSkillSnapshotVersions(indexed []skills.Skill) ([]encodedSkillSnapshotVersion, error) {
	versions := make([]encodedSkillSnapshotVersion, 0, len(indexed))
	for _, skill := range indexed {
		metadata, err := json.Marshal(map[string]any{
			"metadata": skill.Metadata, "license": skill.License, "references": skill.References,
			"scripts": skill.Scripts, "assets": skill.Assets,
		})
		if err != nil {
			return nil, fmt.Errorf("encode skill snapshot metadata for %s: %w", skill.Name, err)
		}
		versions = append(versions, encodedSkillSnapshotVersion{
			name: skill.Name, version: skill.Version, metadata: string(metadata),
		})
	}
	return versions, nil
}

func verifySkillSnapshotVersions(
	ctx context.Context,
	tx *sql.Tx,
	snapshotID string,
	expected []encodedSkillSnapshotVersion,
) error {
	expectedByName := make(map[string]encodedSkillSnapshotVersion, len(expected))
	for _, version := range expected {
		expectedByName[version.name] = version
	}
	rows, err := tx.QueryContext(ctx, `SELECT skill_name,version,metadata_json
		FROM skill_snapshot_versions WHERE snapshot_id=?`, snapshotID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var name, version, metadata string
		if err := rows.Scan(&name, &version, &metadata); err != nil {
			return err
		}
		expectedVersion, ok := expectedByName[name]
		if !ok || expectedVersion.version != version || expectedVersion.metadata != metadata {
			return fmt.Errorf("%w: skill %s metadata changed for snapshot %s", ErrSkillSnapshotConflict, name, snapshotID)
		}
		delete(expectedByName, name)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if len(expectedByName) != 0 {
		return fmt.Errorf("%w: snapshot %s has an incomplete skill inventory", ErrSkillSnapshotConflict, snapshotID)
	}
	return nil
}

func scanSkillSnapshot(row rowScanner) (skills.SnapshotMetadata, error) {
	var snapshot skills.SnapshotMetadata
	err := row.Scan(
		&snapshot.ID, &snapshot.Distribution, &snapshot.Repository, &snapshot.Ref,
		&snapshot.Commit, &snapshot.RepositoryVersion, &snapshot.UpstreamManifestSHA256,
		&snapshot.VendoredManifestSHA256, &snapshot.ExpectedInventorySHA256,
		&snapshot.ExpectedInventoryCount,
	)
	return snapshot, err
}
