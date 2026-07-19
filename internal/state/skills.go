package state

import (
	"context"
	"encoding/json"
	"time"

	"github.com/omerufuk/marketing-os/internal/skills"
)

func (s *Store) SyncSkillSnapshot(ctx context.Context, lock skills.Lock, indexed []skills.Skill) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	now := formatTime(time.Now().UTC())
	_, err = tx.ExecContext(ctx, `INSERT INTO repository_versions(commit_sha,repository_url,pinned_ref,repository_version,manifest_sha256,installed_at)
		VALUES(?,?,?,?,?,?) ON CONFLICT(commit_sha) DO UPDATE SET repository_url=excluded.repository_url,pinned_ref=excluded.pinned_ref,repository_version=excluded.repository_version,manifest_sha256=excluded.manifest_sha256`,
		lock.Commit, lock.Repository, lock.Ref, lock.RepositoryVersion, lock.ManifestSHA256, now)
	if err != nil {
		return err
	}
	for _, skill := range indexed {
		_, err = tx.ExecContext(ctx, `INSERT INTO skills(name,description,current_version,updated_at) VALUES(?,?,?,?)
			ON CONFLICT(name) DO UPDATE SET description=excluded.description,current_version=excluded.current_version,updated_at=excluded.updated_at`, skill.Name, skill.Description, skill.Version, now)
		if err != nil {
			return err
		}
		metadata, _ := json.Marshal(map[string]any{"metadata": skill.Metadata, "license": skill.License, "references": skill.References, "scripts": skill.Scripts, "assets": skill.Assets})
		_, err = tx.ExecContext(ctx, `INSERT INTO skill_versions(skill_name,version,repository_commit,metadata_json,indexed_at) VALUES(?,?,?,?,?)
			ON CONFLICT(skill_name,version,repository_commit) DO UPDATE SET metadata_json=excluded.metadata_json,indexed_at=excluded.indexed_at`, skill.Name, skill.Version, lock.Commit, string(metadata), now)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}
