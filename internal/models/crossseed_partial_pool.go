// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

const (
	CrossSeedPartialMemberModeHardlink = "hardlink"
	CrossSeedPartialMemberModeReflink  = "reflink"
)

// CrossSeedPartialFile stores the pooled member's exact target-side file path and size.
// Key is the shared normalized identity used across layout variants.
type CrossSeedPartialFile struct {
	Name string `json:"name"`
	Size int64  `json:"size"`
	Key  string `json:"key,omitempty"`
}

// CrossSeedPartialPoolMember stores the lightweight persisted marker for an active pooled member.
type CrossSeedPartialPoolMember struct {
	ID                          int64                  `json:"id"`
	SourceInstanceID            int                    `json:"sourceInstanceId"`
	SourceHash                  string                 `json:"sourceHash"`
	TargetInstanceID            int                    `json:"targetInstanceId"`
	TargetHash                  string                 `json:"targetHash"`
	TargetHashV2                string                 `json:"targetHashV2,omitempty"`
	TargetAddedOn               int64                  `json:"targetAddedOn"`
	TargetName                  string                 `json:"targetName"`
	Mode                        string                 `json:"mode"`
	ManagedRoot                 string                 `json:"managedRoot"`
	SourcePieceLength           int64                  `json:"sourcePieceLength"`
	MaxMissingBytesAfterRecheck int64                  `json:"maxMissingBytesAfterRecheck"`
	SourceFiles                 []CrossSeedPartialFile `json:"sourceFiles"`
	CreatedAt                   time.Time              `json:"createdAt"`
	UpdatedAt                   time.Time              `json:"updatedAt"`
	ExpiresAt                   time.Time              `json:"expiresAt"`
}

// CrossSeedPartialPoolMemberStore persists only active pooled members so pools can be rebuilt after restart.
type CrossSeedPartialPoolMemberStore struct {
	db dbinterface.Querier
}

func NewCrossSeedPartialPoolMemberStore(db dbinterface.Querier) *CrossSeedPartialPoolMemberStore {
	if db == nil {
		panic("db cannot be nil")
	}
	return &CrossSeedPartialPoolMemberStore{db: db}
}

func (s *CrossSeedPartialPoolMemberStore) Upsert(ctx context.Context, member *CrossSeedPartialPoolMember) (*CrossSeedPartialPoolMember, error) {
	if member == nil {
		return nil, errors.New("member cannot be nil")
	}

	normalized := *member
	normalized.SourceHash = strings.ToUpper(strings.TrimSpace(normalized.SourceHash))
	normalized.TargetHash = strings.ToUpper(strings.TrimSpace(normalized.TargetHash))
	normalized.TargetHashV2 = strings.ToUpper(strings.TrimSpace(normalized.TargetHashV2))
	normalized.TargetName = strings.TrimSpace(normalized.TargetName)
	normalized.Mode = strings.TrimSpace(normalized.Mode)
	normalized.ManagedRoot = strings.TrimSpace(normalized.ManagedRoot)

	if normalized.SourceInstanceID <= 0 || normalized.TargetInstanceID <= 0 {
		return nil, errors.New("instance ids must be positive")
	}
	if normalized.SourceHash == "" || normalized.TargetHash == "" {
		return nil, errors.New("source and target hashes are required")
	}
	if normalized.Mode != CrossSeedPartialMemberModeHardlink && normalized.Mode != CrossSeedPartialMemberModeReflink {
		return nil, fmt.Errorf("invalid pooled member mode %q", normalized.Mode)
	}
	if len(normalized.SourceFiles) == 0 {
		return nil, errors.New("source files are required")
	}
	if normalized.ExpiresAt.IsZero() {
		normalized.ExpiresAt = time.Now().UTC().Add(24 * time.Hour)
	}

	filesJSON, err := json.Marshal(normalized.SourceFiles)
	if err != nil {
		return nil, fmt.Errorf("marshal source files: %w", err)
	}

	const stmt = `
		INSERT INTO cross_seed_partial_pool_members (
			source_instance_id, source_hash, target_instance_id, target_hash, target_hash_v2,
			target_added_on, target_name, mode, managed_root, source_piece_length, max_missing_bytes_after_recheck, source_files_json, expires_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(target_instance_id, target_hash) DO UPDATE SET
			source_instance_id = excluded.source_instance_id,
			source_hash = excluded.source_hash,
			target_hash_v2 = excluded.target_hash_v2,
			target_added_on = excluded.target_added_on,
			target_name = excluded.target_name,
			mode = excluded.mode,
			managed_root = excluded.managed_root,
			source_piece_length = excluded.source_piece_length,
			max_missing_bytes_after_recheck = excluded.max_missing_bytes_after_recheck,
			source_files_json = excluded.source_files_json,
			expires_at = excluded.expires_at
	`

	if _, err := s.db.ExecContext(ctx, stmt,
		normalized.SourceInstanceID,
		normalized.SourceHash,
		normalized.TargetInstanceID,
		normalized.TargetHash,
		nullIfBlank(normalized.TargetHashV2),
		normalized.TargetAddedOn,
		normalized.TargetName,
		normalized.Mode,
		normalized.ManagedRoot,
		normalized.SourcePieceLength,
		normalized.MaxMissingBytesAfterRecheck,
		string(filesJSON),
		normalized.ExpiresAt,
	); err != nil {
		return nil, fmt.Errorf("upsert pooled member: %w", err)
	}

	return s.GetByAnyHash(ctx, normalized.TargetInstanceID, normalized.TargetHash, normalized.TargetHashV2)
}

func (s *CrossSeedPartialPoolMemberStore) ListActive(ctx context.Context, now time.Time) ([]*CrossSeedPartialPoolMember, error) {
	const query = `
		SELECT id, source_instance_id, source_hash, target_instance_id, target_hash, target_hash_v2, target_added_on,
		       target_name, mode, managed_root, source_piece_length, max_missing_bytes_after_recheck, source_files_json,
		       created_at, updated_at, expires_at
		FROM cross_seed_partial_pool_members
		WHERE expires_at > ?
		ORDER BY source_instance_id, source_hash, target_instance_id, target_hash
	`

	rows, err := s.db.QueryContext(ctx, query, now.UTC())
	if err != nil {
		return nil, fmt.Errorf("list active pooled members: %w", err)
	}
	defer rows.Close()

	var members []*CrossSeedPartialPoolMember
	for rows.Next() {
		member, scanErr := scanCrossSeedPartialPoolMember(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate active pooled members: %w", err)
	}
	return members, nil
}

func (s *CrossSeedPartialPoolMemberStore) GetByAnyHash(ctx context.Context, instanceID int, hashes ...string) (*CrossSeedPartialPoolMember, error) {
	if instanceID <= 0 {
		return nil, errors.New("instance id must be positive")
	}

	normalized := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		hash = strings.ToUpper(strings.TrimSpace(hash))
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		normalized = append(normalized, hash)
	}
	if len(normalized) == 0 {
		return nil, nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(normalized)), ",")
	query := fmt.Sprintf(`
		SELECT id, source_instance_id, source_hash, target_instance_id, target_hash, target_hash_v2, target_added_on,
		       target_name, mode, managed_root, source_piece_length, max_missing_bytes_after_recheck, source_files_json,
		       created_at, updated_at, expires_at
		FROM cross_seed_partial_pool_members
		WHERE target_instance_id = ? AND expires_at > ? AND (target_hash IN (%s) OR target_hash_v2 IN (%s))
		LIMIT 1
	`, placeholders, placeholders)

	args := make([]any, 0, 2+len(normalized)*2)
	args = append(args, instanceID)
	args = append(args, time.Now().UTC())
	for _, hash := range normalized {
		args = append(args, hash)
	}
	for _, hash := range normalized {
		args = append(args, hash)
	}

	row := s.db.QueryRowContext(ctx, query, args...)
	member, err := scanCrossSeedPartialPoolMember(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return member, nil
}

func (s *CrossSeedPartialPoolMemberStore) DeleteByAnyHash(ctx context.Context, instanceID int, hashes ...string) error {
	if instanceID <= 0 {
		return errors.New("instance id must be positive")
	}

	normalized := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		hash = strings.ToUpper(strings.TrimSpace(hash))
		if hash == "" {
			continue
		}
		if _, ok := seen[hash]; ok {
			continue
		}
		seen[hash] = struct{}{}
		normalized = append(normalized, hash)
	}
	if len(normalized) == 0 {
		return nil
	}

	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(normalized)), ",")
	stmt := fmt.Sprintf(`
		DELETE FROM cross_seed_partial_pool_members
		WHERE target_instance_id = ? AND (target_hash IN (%s) OR target_hash_v2 IN (%s))
	`, placeholders, placeholders)
	args := make([]any, 0, 1+len(normalized)*2)
	args = append(args, instanceID)
	for _, hash := range normalized {
		args = append(args, hash)
	}
	for _, hash := range normalized {
		args = append(args, hash)
	}
	if _, err := s.db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("delete pooled member: %w", err)
	}
	return nil
}

func (s *CrossSeedPartialPoolMemberStore) DeleteExpired(ctx context.Context, now time.Time) (int64, error) {
	result, err := s.db.ExecContext(ctx, `DELETE FROM cross_seed_partial_pool_members WHERE expires_at <= ?`, now.UTC())
	if err != nil {
		return 0, fmt.Errorf("delete expired pooled members: %w", err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("delete expired pooled members rows affected: %w", err)
	}
	return rows, nil
}

func scanCrossSeedPartialPoolMember(scanner interface{ Scan(dest ...any) error }) (*CrossSeedPartialPoolMember, error) {
	var (
		member       CrossSeedPartialPoolMember
		targetHashV2 sql.NullString
		sourceFiles  string
	)
	if err := scanner.Scan(
		&member.ID,
		&member.SourceInstanceID,
		&member.SourceHash,
		&member.TargetInstanceID,
		&member.TargetHash,
		&targetHashV2,
		&member.TargetAddedOn,
		&member.TargetName,
		&member.Mode,
		&member.ManagedRoot,
		&member.SourcePieceLength,
		&member.MaxMissingBytesAfterRecheck,
		&sourceFiles,
		&member.CreatedAt,
		&member.UpdatedAt,
		&member.ExpiresAt,
	); err != nil {
		return nil, err
	}
	if targetHashV2.Valid {
		member.TargetHashV2 = targetHashV2.String
	}
	if err := json.Unmarshal([]byte(sourceFiles), &member.SourceFiles); err != nil {
		return nil, fmt.Errorf("decode source files json: %w", err)
	}
	return &member, nil
}

func nullIfBlank(value string) any {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	return trimmed
}
