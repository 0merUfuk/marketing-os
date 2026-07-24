package state

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/omerufuk/marketing-os/internal/skills"
	migrationfiles "github.com/omerufuk/marketing-os/migrations"
)

func TestSkillSnapshotsAllowDistinctVendoredManifestsAtOneUpstreamCommit(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	indexed := testIndexedSkills()
	first := testSnapshotMetadata(strings.Repeat("b", 64))
	second := testSnapshotMetadata(strings.Repeat("c", 64))
	if first.Commit != second.Commit || first.ID == second.ID {
		t.Fatalf("invalid test identities: first=%+v second=%+v", first, second)
	}
	if err := store.SyncSkillSnapshot(ctx, first, indexed); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncSkillSnapshot(ctx, second, indexed); err != nil {
		t.Fatal(err)
	}
	var snapshots, versions int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshots WHERE upstream_commit=?`, first.Commit).Scan(&snapshots); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshot_versions`).Scan(&versions); err != nil {
		t.Fatal(err)
	}
	if snapshots != 2 || versions != 10 {
		t.Fatalf("snapshots=%d versions=%d, want 2/10", snapshots, versions)
	}
}

func TestSkillSnapshotMetadataConflictsNeverOverwriteHistory(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	original := testSnapshotMetadata(strings.Repeat("d", 64))
	indexed := testIndexedSkills()
	if err := store.SyncSkillSnapshot(ctx, original, indexed); err != nil {
		t.Fatal(err)
	}
	conflict := original
	conflict.Repository = "https://example.test/different.git"
	if err := store.SyncSkillSnapshot(ctx, conflict, indexed); !errors.Is(err, ErrSkillSnapshotConflict) {
		t.Fatalf("conflict error = %v, want ErrSkillSnapshotConflict", err)
	}
	var repository string
	if err := store.db.QueryRowContext(ctx, `SELECT repository_url FROM skill_snapshots WHERE id=?`, original.ID).Scan(&repository); err != nil {
		t.Fatal(err)
	}
	if repository != original.Repository {
		t.Fatalf("historical repository overwritten with %q", repository)
	}
}

func TestSyncSkillSnapshotRejectsNonExactInventoryAtomically(t *testing.T) {
	t.Parallel()
	complete := testIndexedSkills()
	duplicate := append([]skills.Skill(nil), complete...)
	duplicate[len(duplicate)-1] = duplicate[0]
	wrongVersion := append([]skills.Skill(nil), complete...)
	wrongVersion[0].Version = "9.9.9"
	replacement := append([]skills.Skill(nil), complete...)
	replacement[len(replacement)-1] = skills.Skill{Name: "rogue", Version: "1.0.0"}
	tests := map[string][]skills.Skill{
		"empty":         nil,
		"partial":       append([]skills.Skill(nil), complete[:len(complete)-1]...),
		"extra":         append(append([]skills.Skill(nil), complete...), skills.Skill{Name: "rogue", Version: "1.0.0"}),
		"duplicate":     duplicate,
		"wrong version": wrongVersion,
		"replacement":   replacement,
	}
	for name, indexed := range tests {
		indexed := indexed
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := context.Background()
			store := openSkillSnapshotTestStore(t, ctx)
			snapshot := testSnapshotMetadata(strings.Repeat("e", 64))
			if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err == nil {
				t.Fatalf("SyncSkillSnapshot accepted %s inventory", name)
			}
			var parents, children int
			if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshots WHERE id=?`, snapshot.ID).Scan(&parents); err != nil {
				t.Fatal(err)
			}
			if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshot_versions WHERE snapshot_id=?`, snapshot.ID).Scan(&children); err != nil {
				t.Fatal(err)
			}
			if parents != 0 || children != 0 {
				t.Fatalf("invalid inventory persisted parents=%d children=%d", parents, children)
			}
		})
	}
}

func TestSyncSkillSnapshotExactRepeatIsIdempotent(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openSkillSnapshotTestStore(t, ctx)
	snapshot := testSnapshotMetadata(strings.Repeat("f", 64))
	indexed := testIndexedSkills()
	if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
		t.Fatal(err)
	}
	if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
		t.Fatalf("exact repeat failed: %v", err)
	}
	var parents, children, inventoryCount int
	var inventoryIdentity string
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshots WHERE id=?`, snapshot.ID).Scan(&parents); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshot_versions WHERE snapshot_id=?`, snapshot.ID).Scan(&children); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT expected_inventory_sha256,expected_inventory_count
		FROM skill_snapshots WHERE id=?`, snapshot.ID).Scan(&inventoryIdentity, &inventoryCount); err != nil {
		t.Fatal(err)
	}
	if parents != 1 || children != len(indexed) {
		t.Fatalf("parents=%d children=%d, want 1/%d", parents, children, len(indexed))
	}
	if inventoryIdentity != snapshot.ExpectedInventorySHA256 || inventoryCount != snapshot.ExpectedInventoryCount {
		t.Fatalf("persisted inventory identity/count = %q/%d, want %q/%d",
			inventoryIdentity, inventoryCount, snapshot.ExpectedInventorySHA256, snapshot.ExpectedInventoryCount)
	}
}

func TestSyncSkillSnapshotCannotAppendOrRepairExistingInventory(t *testing.T) {
	t.Parallel()
	t.Run("attempted append", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		store := openSkillSnapshotTestStore(t, ctx)
		snapshot := testSnapshotMetadata(strings.Repeat("1", 64))
		indexed := testIndexedSkills()
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
			t.Fatal(err)
		}
		withExtra := append(append([]skills.Skill(nil), indexed...), skills.Skill{Name: "rogue", Version: "1.0.0"})
		if err := store.SyncSkillSnapshot(ctx, snapshot, withExtra); err == nil {
			t.Fatal("repeat sync appended an unexpected skill")
		}
		assertSnapshotVersionCount(t, ctx, store, snapshot.ID, len(indexed))
	})
	t.Run("partial existing set", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		store := openSkillSnapshotTestStore(t, ctx)
		snapshot := testSnapshotMetadata(strings.Repeat("2", 64))
		indexed := testIndexedSkills()
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `DELETE FROM skill_snapshot_versions
			WHERE snapshot_id=? AND skill_name='launch'`, snapshot.ID); err != nil {
			t.Fatal(err)
		}
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); !errors.Is(err, ErrSkillSnapshotConflict) {
			t.Fatalf("partial set error = %v, want ErrSkillSnapshotConflict", err)
		}
		assertSnapshotVersionCount(t, ctx, store, snapshot.ID, len(indexed)-1)
	})
	t.Run("conflicting child metadata", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		store := openSkillSnapshotTestStore(t, ctx)
		snapshot := testSnapshotMetadata(strings.Repeat("3", 64))
		indexed := testIndexedSkills()
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `UPDATE skill_snapshot_versions SET metadata_json='{"corrupt":true}'
			WHERE snapshot_id=? AND skill_name='launch'`, snapshot.ID); err != nil {
			t.Fatal(err)
		}
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); !errors.Is(err, ErrSkillSnapshotConflict) {
			t.Fatalf("corrupt metadata error = %v, want ErrSkillSnapshotConflict", err)
		}
		var metadata string
		if err := store.db.QueryRowContext(ctx, `SELECT metadata_json FROM skill_snapshot_versions
			WHERE snapshot_id=? AND skill_name='launch'`, snapshot.ID).Scan(&metadata); err != nil {
			t.Fatal(err)
		}
		if metadata != `{"corrupt":true}` {
			t.Fatalf("corrupt immutable metadata was overwritten with %q", metadata)
		}
	})
	t.Run("unexpected existing child", func(t *testing.T) {
		t.Parallel()
		ctx := context.Background()
		store := openSkillSnapshotTestStore(t, ctx)
		snapshot := testSnapshotMetadata(strings.Repeat("5", 64))
		indexed := testIndexedSkills()
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
			t.Fatal(err)
		}
		if _, err := store.db.ExecContext(ctx, `INSERT INTO skill_snapshot_versions(
			snapshot_id,skill_name,version,metadata_json,indexed_at
		) VALUES(?,?,?,?,?)`, snapshot.ID, "rogue", "1.0.0", "{}", "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
		if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); !errors.Is(err, ErrSkillSnapshotConflict) {
			t.Fatalf("unexpected child error = %v, want ErrSkillSnapshotConflict", err)
		}
		assertSnapshotVersionCount(t, ctx, store, snapshot.ID, len(indexed)+1)
	})
}

func TestDeletingCurrentSkillCannotEraseHistoricalSnapshotVersion(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := openSkillSnapshotTestStore(t, ctx)
	snapshot := testSnapshotMetadata(strings.Repeat("4", 64))
	indexed := testIndexedSkills()
	if err := store.SyncSkillSnapshot(ctx, snapshot, indexed); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.ExecContext(ctx, `DELETE FROM skills WHERE name='launch'`); err != nil {
		t.Fatalf("delete mutable current skill: %v", err)
	}
	var historical int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshot_versions
		WHERE snapshot_id=? AND skill_name='launch'`, snapshot.ID).Scan(&historical); err != nil {
		t.Fatal(err)
	}
	if historical != 1 {
		t.Fatalf("historical launch versions=%d, want 1", historical)
	}
}

func TestSkillSnapshotMigrationPreservesLegacySubmoduleAuditRows(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "legacy.db")
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `CREATE TABLE schema_migrations (name TEXT PRIMARY KEY NOT NULL, applied_at TEXT NOT NULL)`); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"001_foundation.sql", "002_evidence.sql", "003_workflows.sql"} {
		migration, readErr := migrationfiles.Files.ReadFile(name)
		if readErr != nil {
			t.Fatal(readErr)
		}
		if _, err := db.ExecContext(ctx, string(migration)); err != nil {
			t.Fatalf("apply legacy migration %s: %v", name, err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO schema_migrations(name,applied_at) VALUES(?,?)`, name, "2026-01-01T00:00:00Z"); err != nil {
			t.Fatal(err)
		}
	}
	const legacyCommit = "2222222222222222222222222222222222222222"
	if _, err := db.ExecContext(ctx, `INSERT INTO repository_versions(commit_sha,repository_url,pinned_ref,repository_version,manifest_sha256,installed_at) VALUES(?,?,?,?,?,?)`,
		legacyCommit, "https://example.test/legacy.git", legacyCommit, "1.0.0", strings.Repeat("e", 64), "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO skills(name,description,current_version,updated_at) VALUES(?,?,?,?)`,
		"launch", "Legacy launch guidance.", "1.0.0", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO skill_versions(skill_name,version,repository_commit,metadata_json,indexed_at) VALUES(?,?,?,?,?)`,
		"launch", "1.0.0", legacyCommit, "{}", "2026-01-01T00:00:00Z"); err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := Open(ctx, path)
	if err != nil {
		t.Fatalf("migrate legacy database: %v", err)
	}
	defer store.Close()
	var repository, version string
	if err := store.db.QueryRowContext(ctx, `SELECT repository_url FROM repository_versions WHERE commit_sha=?`, legacyCommit).Scan(&repository); err != nil {
		t.Fatal(err)
	}
	if err := store.db.QueryRowContext(ctx, `SELECT version FROM skill_versions WHERE skill_name='launch' AND repository_commit=?`, legacyCommit).Scan(&version); err != nil {
		t.Fatal(err)
	}
	var migrationCount int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM schema_migrations WHERE name='004_skill_snapshots.sql'`).Scan(&migrationCount); err != nil {
		t.Fatal(err)
	}
	if repository != "https://example.test/legacy.git" || version != "1.0.0" || migrationCount != 1 {
		t.Fatalf("legacy repository=%q version=%q migration_count=%d", repository, version, migrationCount)
	}
}

func testSnapshotMetadata(vendoredManifest string) skills.SnapshotMetadata {
	const commit = "1111111111111111111111111111111111111111"
	lock := skills.Lock{
		Distribution: skills.VendoredDistribution,
		Repository:   "https://example.test/skills.git",
		Ref:          commit, Commit: commit, RepositoryVersion: "1.0.0",
		SelectedSkills:         testSelectedSkills(),
		UpstreamManifestSHA256: strings.Repeat("a", 64),
	}
	return skills.NewSnapshotMetadata(lock, vendoredManifest)
}

func testSelectedSkills() []skills.SelectedSkill {
	names := []string{"copywriting", "emails", "launch", "product-marketing", "social"}
	selected := make([]skills.SelectedSkill, 0, len(names))
	for _, name := range names {
		selected = append(selected, skills.SelectedSkill{Name: name, Version: "1.0.0"})
	}
	return selected
}

func testIndexedSkills() []skills.Skill {
	selected := testSelectedSkills()
	indexed := make([]skills.Skill, 0, len(selected))
	for _, skill := range selected {
		indexed = append(indexed, skills.Skill{
			Name: skill.Name, Description: "Safe " + skill.Name + " guidance.", Version: skill.Version,
			Metadata:   map[string]any{"version": skill.Version},
			References: []string{}, Scripts: []string{}, Assets: []string{},
		})
	}
	return indexed
}

func openSkillSnapshotTestStore(t *testing.T, ctx context.Context) *Store {
	t.Helper()
	store, err := Open(ctx, filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := store.Close(); err != nil {
			t.Errorf("close store: %v", err)
		}
	})
	return store
}

func assertSnapshotVersionCount(t *testing.T, ctx context.Context, store *Store, snapshotID string, want int) {
	t.Helper()
	var count int
	if err := store.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM skill_snapshot_versions WHERE snapshot_id=?`, snapshotID).Scan(&count); err != nil {
		t.Fatal(err)
	}
	if count != want {
		t.Fatalf("snapshot versions=%d, want %d", count, want)
	}
}
