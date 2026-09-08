// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// These constants define persisted partial pool modes, states, and coordination markers.
const (
	CrossSeedPartialPoolStatusActive  = "active"
	CrossSeedPartialPoolStatusDormant = "dormant"

	CrossSeedPartialPoolModeHardlink = "hardlink"
	CrossSeedPartialPoolModeReflink  = "reflink"

	CrossSeedPartialPoolMemberStatusVerifying  = "verifying"
	CrossSeedPartialPoolMemberStatusWaiting    = "waiting"
	CrossSeedPartialPoolMemberStatusBlocked    = "blocked"
	CrossSeedPartialPoolMemberStatusAcquiring  = "acquiring"
	CrossSeedPartialPoolMemberStatusRechecking = "rechecking"
	CrossSeedPartialPoolMemberStatusComplete   = "complete"
	CrossSeedPartialPoolMemberStatusManual     = "manual"
	CrossSeedPartialPoolMemberStatusRemoved    = "removed"

	CrossSeedPartialPoolFileStatusPresent     = "present"
	CrossSeedPartialPoolFileStatusMissing     = "missing"
	CrossSeedPartialPoolFileStatusAcquiring   = "acquiring"
	CrossSeedPartialPoolFileStatusAvailable   = "available"
	CrossSeedPartialPoolFileStatusPropagating = "propagating"
	CrossSeedPartialPoolFileStatusVerifying   = "verifying"
	CrossSeedPartialPoolFileStatusVerified    = "verified"
	CrossSeedPartialPoolFileStatusManual      = "manual"

	CrossSeedPartialPoolRecheckPending   = "partial pool recheck pending"
	CrossSeedPartialPoolRecheckRequested = "partial pool recheck requested"
)

// CrossSeedPartialPool groups partial link-mode torrents by their original source.
type CrossSeedPartialPool struct {
	ID               int64
	SourceInstanceID int
	SourceTorrentKey string
	Status           string
	CreatedAt        time.Time
	UpdatedAt        time.Time
	Members          []*CrossSeedPartialPoolMember
}

// CrossSeedPartialPoolMember is one qBittorrent torrent managed by a partial
// pool. Review and retry fields persist coordinator sub-state separately from
// LastError diagnostics.
type CrossSeedPartialPoolMember struct {
	ID                  int64
	PoolID              int64
	InstanceID          int
	TorrentKey          string
	InfoHashV1          string
	InfoHashV2          string
	Mode                string
	RootPath            string
	Status              string
	MissingBytes        int64
	StartedByPool       bool
	LastDownloadedBytes *int64
	LastProgressAt      *time.Time
	RetryAfter          *time.Time
	ReviewPausePending  bool
	ResumeAttempts      *int64
	RecoveryAttempts    *int64
	LastError           string
	CreatedAt           time.Time
	UpdatedAt           time.Time
	Files               []*CrossSeedPartialPoolMemberFile
}

// CrossSeedPartialPoolMemberFile is durable per-file completion state.
type CrossSeedPartialPoolMemberFile struct {
	ID                int64
	MemberID          int64
	FileIndex         int
	RelativePath      string
	SizeBytes         int64
	PiecesRoot        string
	WantedAtAdmission bool
	MaterializedAtAdd bool
	// ReplaceableAtAdd records that no target path existed immediately before the qBittorrent add.
	ReplaceableAtAdd bool
	Status           string
	SourceFileID     *int64
	LastError        string
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

// CrossSeedPartialPoolRegistration contains the source-resolution evidence and
// the member/files inserted in one transaction.
type CrossSeedPartialPoolRegistration struct {
	SourceInstanceID int
	SourceTorrentKey string
	SourceAliases    []string

	MatchedInstanceID int
	MatchedTorrentKey string
	MatchedAliases    []string

	Member CrossSeedPartialPoolMember
	Files  []CrossSeedPartialPoolMemberFile
}

// NullableInt64Update distinguishes no update from setting a value or NULL.
type NullableInt64Update struct {
	Set   bool
	Value *int64
}

// NullableTimeUpdate distinguishes no update from setting a value or NULL.
type NullableTimeUpdate struct {
	Set   bool
	Value *time.Time
}

// PartialPoolMemberMutation contains fields changed with a member status claim.
type PartialPoolMemberMutation struct {
	MissingBytes        *int64
	StartedByPool       *bool
	LastDownloadedBytes NullableInt64Update
	LastProgressAt      NullableTimeUpdate
	RetryAfter          NullableTimeUpdate
	ReviewPausePending  *bool
	ResumeAttempts      NullableInt64Update
	RecoveryAttempts    NullableInt64Update
	LastError           *string
}

// PartialPoolFileMutation contains fields changed with a file status claim.
type PartialPoolFileMutation struct {
	SourceFileID NullableInt64Update
	LastError    *string
}

func normalizePartialPoolKey(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizedPartialPoolAliases(values ...[]string) []string {
	seen := make(map[string]struct{})
	var aliases []string
	for _, group := range values {
		for _, value := range group {
			value = normalizePartialPoolKey(value)
			if value == "" {
				continue
			}
			if _, ok := seen[value]; ok {
				continue
			}
			seen[value] = struct{}{}
			aliases = append(aliases, value)
		}
	}
	return aliases
}

var errPartialPoolRegistrationChanged = errors.New("partial pool changed during registration")

// RegisterPartialPoolMember atomically resolves or creates the source pool and
// persists one member plus every file. An alias-equivalent re-admission keeps
// the member identity but replaces its stale admission and file state.
func (s *CrossSeedStore) RegisterPartialPoolMember(ctx context.Context, registration CrossSeedPartialPoolRegistration) (*CrossSeedPartialPool, *CrossSeedPartialPoolMember, error) {
	member := registration.Member
	member.TorrentKey = normalizePartialPoolKey(member.TorrentKey)
	member.InfoHashV1 = normalizePartialPoolKey(member.InfoHashV1)
	member.InfoHashV2 = normalizePartialPoolKey(member.InfoHashV2)
	if member.InstanceID <= 0 || member.TorrentKey == "" {
		return nil, nil, errors.New("partial pool member instance and torrent key are required")
	}
	if member.Mode != CrossSeedPartialPoolModeHardlink && member.Mode != CrossSeedPartialPoolModeReflink {
		return nil, nil, fmt.Errorf("invalid partial pool member mode %q", member.Mode)
	}
	if strings.TrimSpace(member.RootPath) == "" {
		return nil, nil, errors.New("partial pool member root path is required")
	}
	if len(registration.Files) == 0 {
		return nil, nil, errors.New("partial pool member files are required")
	}
	if member.Status == "" {
		member.Status = CrossSeedPartialPoolMemberStatusVerifying
	}
	if !validPartialPoolMemberStatus(member.Status) {
		return nil, nil, fmt.Errorf("invalid partial pool member status %q", member.Status)
	}

	files := make([]CrossSeedPartialPoolMemberFile, len(registration.Files))
	copy(files, registration.Files)
	seenIndexes := make(map[int]struct{}, len(files))
	for i := range files {
		file := &files[i]
		if file.FileIndex < 0 {
			return nil, nil, fmt.Errorf("partial pool file index must be non-negative: %d", file.FileIndex)
		}
		if file.RelativePath == "" {
			return nil, nil, errors.New("partial pool file relative path is required")
		}
		if file.SizeBytes < 0 {
			return nil, nil, fmt.Errorf("partial pool file size must be non-negative: %d", file.SizeBytes)
		}
		if _, exists := seenIndexes[file.FileIndex]; exists {
			return nil, nil, fmt.Errorf("duplicate partial pool file index %d", file.FileIndex)
		}
		seenIndexes[file.FileIndex] = struct{}{}
		if file.Status == "" {
			if file.MaterializedAtAdd {
				file.Status = CrossSeedPartialPoolFileStatusPresent
			} else {
				file.Status = CrossSeedPartialPoolFileStatusMissing
			}
		}
		if !validPartialPoolFileStatus(file.Status) {
			return nil, nil, fmt.Errorf("invalid partial pool file status %q", file.Status)
		}
	}
	for {
		pool, registered, err := s.registerPartialPoolMember(ctx, registration, member, files)
		if !errors.Is(err, errPartialPoolRegistrationChanged) {
			return pool, registered, err
		}
		if err := ctx.Err(); err != nil {
			return nil, nil, err
		}
	}
}

func (s *CrossSeedStore) registerPartialPoolMember(ctx context.Context, registration CrossSeedPartialPoolRegistration, member CrossSeedPartialPoolMember, files []CrossSeedPartialPoolMemberFile) (*CrossSeedPartialPool, *CrossSeedPartialPoolMember, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("begin partial pool registration: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	memberAliases := normalizedPartialPoolAliases([]string{member.TorrentKey, member.InfoHashV1, member.InfoHashV2})
	var poolID, memberID int64
	reAdmission := false
	if existingID, existingPoolID, found, findErr := findPartialPoolMemberByAliases(ctx, tx, member.InstanceID, memberAliases); findErr != nil {
		return nil, nil, findErr
	} else if found {
		memberID = existingID
		poolID = existingPoolID
		reAdmission = true
	}

	if poolID == 0 && registration.SourceInstanceID > 0 {
		sourceAliases := normalizedPartialPoolAliases([]string{registration.SourceTorrentKey}, registration.SourceAliases)
		_, inheritedPoolID, found, findErr := findPartialPoolMemberByAliases(ctx, tx, registration.SourceInstanceID, sourceAliases)
		if findErr != nil {
			return nil, nil, findErr
		}
		if found {
			poolID = inheritedPoolID
		} else {
			sourceKey := normalizePartialPoolKey(registration.SourceTorrentKey)
			if sourceKey == "" && len(sourceAliases) > 0 {
				sourceKey = sourceAliases[0]
			}
			if sourceKey == "" {
				return nil, nil, errors.New("partial pool source torrent key is required")
			}
			poolID, err = getOrCreatePartialPool(ctx, tx, registration.SourceInstanceID, sourceKey)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	if poolID == 0 {
		matchedAliases := normalizedPartialPoolAliases([]string{registration.MatchedTorrentKey}, registration.MatchedAliases)
		if registration.MatchedInstanceID > 0 {
			_, inheritedPoolID, found, findErr := findPartialPoolMemberByAliases(ctx, tx, registration.MatchedInstanceID, matchedAliases)
			if findErr != nil {
				return nil, nil, findErr
			}
			if found {
				poolID = inheritedPoolID
			}
		}
		if poolID == 0 {
			matchedKey := normalizePartialPoolKey(registration.MatchedTorrentKey)
			if matchedKey == "" && len(matchedAliases) > 0 {
				matchedKey = matchedAliases[0]
			}
			if registration.MatchedInstanceID <= 0 || matchedKey == "" {
				return nil, nil, errors.New("partial pool matched source identity is required")
			}
			poolID, err = getOrCreatePartialPool(ctx, tx, registration.MatchedInstanceID, matchedKey)
			if err != nil {
				return nil, nil, err
			}
		}
	}

	// Every admission and downloader claim takes the pool row first. Keeping
	// that lock order prevents a claim from overlooking an uncommitted member
	// on PostgreSQL and serializes competing claims before they inspect members.
	result, err := tx.ExecContext(ctx, `UPDATE cross_seed_partial_pools SET status = status WHERE id = ?`, poolID)
	if err != nil {
		return nil, nil, fmt.Errorf("lock partial pool for registration: %w", err)
	}
	locked, err := result.RowsAffected()
	if err != nil {
		return nil, nil, fmt.Errorf("inspect partial pool registration lock: %w", err)
	}
	if locked != 1 {
		return nil, nil, errPartialPoolRegistrationChanged
	}
	admittedAt := time.Now().UTC()
	if _, err := tx.ExecContext(ctx, `UPDATE cross_seed_partial_pools SET status = ?, updated_at = ? WHERE id = ?`, CrossSeedPartialPoolStatusActive, admittedAt, poolID); err != nil {
		return nil, nil, fmt.Errorf("activate partial pool registration: %w", err)
	}

	member.PoolID = poolID
	type existingFileState struct {
		id           int64
		relativePath string
		sizeBytes    int64
		piecesRoot   string
		sourceFileID *int64
	}
	existingFiles := make(map[int]existingFileState)
	if reAdmission {
		rows, loadErr := tx.QueryContext(ctx, `
			SELECT id, file_index, relative_path, size_bytes, pieces_root, source_file_id
			FROM cross_seed_partial_pool_member_files
			WHERE member_id = ?
		`, memberID)
		if loadErr != nil {
			return nil, nil, fmt.Errorf("load existing partial pool files: %w", loadErr)
		}
		for rows.Next() {
			var state existingFileState
			var fileIndex int
			var piecesRoot sql.NullString
			var sourceFileID sql.NullInt64
			if scanErr := rows.Scan(&state.id, &fileIndex, &state.relativePath, &state.sizeBytes, &piecesRoot, &sourceFileID); scanErr != nil {
				rows.Close()
				return nil, nil, fmt.Errorf("scan existing partial pool file: %w", scanErr)
			}
			if piecesRoot.Valid {
				state.piecesRoot = piecesRoot.String
			}
			if sourceFileID.Valid {
				value := sourceFileID.Int64
				state.sourceFileID = &value
			}
			existingFiles[fileIndex] = state
		}
		if closeErr := rows.Close(); closeErr != nil {
			return nil, nil, closeErr
		}
		if rowsErr := rows.Err(); rowsErr != nil {
			return nil, nil, rowsErr
		}
	}
	if reAdmission {
		result, updateErr := tx.ExecContext(ctx, `
			UPDATE cross_seed_partial_pool_members
			SET torrent_key = ?, infohash_v1 = ?, infohash_v2 = ?, mode = ?,
			    root_path = ?, status = ?, missing_bytes = ?,
			    started_by_pool = ?, last_downloaded_bytes = NULL,
			    last_progress_at = NULL, retry_after = NULL,
			    review_pause_pending = ?, resume_attempts = NULL,
			    recovery_attempts = NULL, last_error = ?,
			    created_at = ?, updated_at = ?
			WHERE id = ? AND pool_id = ?
		`,
			member.TorrentKey, member.InfoHashV1, member.InfoHashV2, member.Mode,
			member.RootPath, member.Status, member.MissingBytes,
			BoolToSQLite(false), BoolToSQLite(false), member.LastError, admittedAt, admittedAt,
			memberID, poolID,
		)
		if updateErr != nil {
			return nil, nil, fmt.Errorf("reset partial pool member admission: %w", updateErr)
		}
		updated, updateErr := result.RowsAffected()
		if updateErr != nil {
			return nil, nil, fmt.Errorf("inspect partial pool member admission reset: %w", updateErr)
		}
		if updated != 1 {
			return nil, nil, errors.New("partial pool member disappeared during re-admission")
		}
	} else {
		err = tx.QueryRowContext(ctx, `
			INSERT INTO cross_seed_partial_pool_members (
				pool_id, instance_id, torrent_key, infohash_v1, infohash_v2,
				mode, root_path, status, missing_bytes,
				started_by_pool, last_downloaded_bytes, last_progress_at, retry_after,
				review_pause_pending, resume_attempts, recovery_attempts, last_error,
				created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
			RETURNING id
		`,
			poolID, member.InstanceID, member.TorrentKey, member.InfoHashV1, member.InfoHashV2,
			member.Mode, member.RootPath, member.Status, member.MissingBytes,
			BoolToSQLite(member.StartedByPool), member.LastDownloadedBytes, member.LastProgressAt, member.RetryAfter,
			BoolToSQLite(member.ReviewPausePending), member.ResumeAttempts, member.RecoveryAttempts, member.LastError,
			admittedAt, admittedAt,
		).Scan(&memberID)
		if err != nil {
			if isUniqueConstraintError(err) {
				_ = tx.Rollback()
				pool, existing, resolveErr := s.ResolvePartialPoolMember(ctx, member.InstanceID, memberAliases...)
				if resolveErr == nil && existing != nil {
					return pool, existing, nil
				}
			}
			return nil, nil, fmt.Errorf("insert partial pool member: %w", err)
		}
	}

	incomingIndexes := make(map[int]struct{}, len(files))
	var replacedSourceIDs []int64
	for i := range files {
		file := &files[i]
		incomingIndexes[file.FileIndex] = struct{}{}
		if existing, ok := existingFiles[file.FileIndex]; ok {
			sameFile := existing.relativePath == file.RelativePath &&
				existing.sizeBytes == file.SizeBytes &&
				strings.EqualFold(existing.piecesRoot, file.PiecesRoot)
			if !sameFile {
				replacedSourceIDs = append(replacedSourceIDs, existing.id)
			} else if member.Mode == CrossSeedPartialPoolModeHardlink && existing.sourceFileID != nil {
				value := *existing.sourceFileID
				file.SourceFileID = &value
			}
		}
		var piecesRoot any
		if file.PiecesRoot != "" {
			piecesRoot = strings.ToLower(file.PiecesRoot)
		}
		query := `
			INSERT INTO cross_seed_partial_pool_member_files (
				member_id, file_index, relative_path, size_bytes, pieces_root,
				wanted_at_admission, materialized_at_add, replaceable_at_add,
				status, source_file_id, last_error, created_at, updated_at
			) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`
		if reAdmission {
			query += `
				ON CONFLICT(member_id, file_index) DO UPDATE SET
					relative_path = excluded.relative_path,
					size_bytes = excluded.size_bytes,
					pieces_root = excluded.pieces_root,
					wanted_at_admission = excluded.wanted_at_admission,
					materialized_at_add = excluded.materialized_at_add,
					replaceable_at_add = excluded.replaceable_at_add,
					status = excluded.status,
					source_file_id = excluded.source_file_id,
					last_error = excluded.last_error,
					created_at = excluded.created_at,
					updated_at = excluded.updated_at
			`
		}
		_, err = tx.ExecContext(ctx, query,
			memberID, file.FileIndex, file.RelativePath, file.SizeBytes, piecesRoot,
			BoolToSQLite(file.WantedAtAdmission), BoolToSQLite(file.MaterializedAtAdd),
			BoolToSQLite(file.ReplaceableAtAdd), file.Status, file.SourceFileID, file.LastError,
			admittedAt, admittedAt,
		)
		if err != nil {
			return nil, nil, fmt.Errorf("insert partial pool file %d: %w", file.FileIndex, err)
		}
	}
	if reAdmission {
		for fileIndex, existing := range existingFiles {
			if _, retained := incomingIndexes[fileIndex]; !retained {
				replacedSourceIDs = append(replacedSourceIDs, existing.id)
			}
		}
		if len(replacedSourceIDs) > 0 {
			placeholders := strings.TrimSuffix(strings.Repeat("?,", len(replacedSourceIDs)), ",")
			reason := "propagation source was replaced during re-admission"
			hardlinkDependencies := fmt.Sprintf(`
				WITH RECURSIVE hardlink_dependencies(id, member_id) AS (
					SELECT child.id, child.member_id
					FROM cross_seed_partial_pool_member_files child
					JOIN cross_seed_partial_pool_members member ON member.id = child.member_id
					WHERE member.mode = ? AND child.source_file_id IN (%s)
					UNION
					SELECT child.id, child.member_id
					FROM cross_seed_partial_pool_member_files child
					JOIN cross_seed_partial_pool_members member ON member.id = child.member_id
					JOIN hardlink_dependencies parent ON child.source_file_id = parent.id
					WHERE member.mode = ?
				)
			`, placeholders)
			dependencyArgs := []any{CrossSeedPartialPoolModeHardlink}
			for _, id := range replacedSourceIDs {
				dependencyArgs = append(dependencyArgs, id)
			}
			dependencyArgs = append(dependencyArgs, CrossSeedPartialPoolModeHardlink)

			memberArgs := append([]any{}, dependencyArgs...)
			memberArgs = append(memberArgs,
				CrossSeedPartialPoolMemberStatusManual,
				BoolToSQLite(true),
				reason,
				CrossSeedPartialPoolMemberStatusRemoved,
			)
			if _, err := tx.ExecContext(ctx, hardlinkDependencies+`
				UPDATE cross_seed_partial_pool_members
				SET status = ?, review_pause_pending = ?, resume_attempts = NULL,
				    recovery_attempts = NULL, last_error = ?, updated_at = CURRENT_TIMESTAMP
				WHERE status <> ? AND id IN (
					SELECT member_id FROM hardlink_dependencies
				)
			`, memberArgs...); err != nil {
				return nil, nil, fmt.Errorf("quarantine replaced hardlink members: %w", err)
			}

			reflinkArgs := append([]any{}, dependencyArgs...)
			reflinkArgs = append(reflinkArgs,
				CrossSeedPartialPoolFileStatusMissing,
				CrossSeedPartialPoolFileStatusPropagating,
				CrossSeedPartialPoolFileStatusVerifying,
				CrossSeedPartialPoolModeReflink,
			)
			for _, id := range replacedSourceIDs {
				reflinkArgs = append(reflinkArgs, id)
			}
			releaseReflinkDependencies := fmt.Sprintf(`
				UPDATE cross_seed_partial_pool_member_files
				SET status = ?, source_file_id = NULL, last_error = '', updated_at = CURRENT_TIMESTAMP
				WHERE status IN (?, ?)
				  AND member_id IN (
					SELECT id FROM cross_seed_partial_pool_members WHERE mode = ?
				  )
				  AND (
					source_file_id IN (%s)
					OR source_file_id IN (SELECT id FROM hardlink_dependencies)
				  )
			`, placeholders)
			if _, err := tx.ExecContext(ctx, hardlinkDependencies+releaseReflinkDependencies, reflinkArgs...); err != nil {
				return nil, nil, fmt.Errorf("release replaced reflink dependencies: %w", err)
			}

			fileArgs := append([]any{}, dependencyArgs...)
			fileArgs = append(fileArgs,
				CrossSeedPartialPoolFileStatusManual,
				reason,
			)
			if _, err := tx.ExecContext(ctx, hardlinkDependencies+`
				UPDATE cross_seed_partial_pool_member_files
				SET status = ?, source_file_id = NULL, last_error = ?, updated_at = CURRENT_TIMESTAMP
				WHERE id IN (SELECT id FROM hardlink_dependencies)
			`, fileArgs...); err != nil {
				return nil, nil, fmt.Errorf("quarantine replaced hardlink dependencies: %w", err)
			}
		}

		indexPlaceholders := strings.TrimSuffix(strings.Repeat("?,", len(files)), ",")
		args := []any{memberID}
		for _, file := range files {
			args = append(args, file.FileIndex)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf(`
			DELETE FROM cross_seed_partial_pool_member_files
			WHERE member_id = ? AND file_index NOT IN (%s)
		`, indexPlaceholders), args...); err != nil {
			return nil, nil, fmt.Errorf("remove stale partial pool member files: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return nil, nil, fmt.Errorf("commit partial pool registration: %w", err)
	}

	pool, err := s.GetPartialPool(ctx, poolID)
	if err != nil {
		return nil, nil, err
	}
	return pool, partialPoolMemberByID(pool, memberID), nil
}

func getOrCreatePartialPool(ctx context.Context, tx dbinterface.TxQuerier, sourceInstanceID int, sourceTorrentKey string) (int64, error) {
	var poolID int64
	err := tx.QueryRowContext(ctx, `
		INSERT INTO cross_seed_partial_pools (source_instance_id, source_torrent_key, status)
		VALUES (?, ?, ?)
		ON CONFLICT(source_instance_id, source_torrent_key) DO UPDATE SET
			status = excluded.status,
			updated_at = CURRENT_TIMESTAMP
		RETURNING id
	`, sourceInstanceID, sourceTorrentKey, CrossSeedPartialPoolStatusActive).Scan(&poolID)
	if err != nil {
		return 0, fmt.Errorf("get or create partial pool: %w", err)
	}
	return poolID, nil
}

func findPartialPoolMemberByAliases(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}, instanceID int, aliases []string) (memberID, poolID int64, found bool, err error) {
	if instanceID <= 0 || len(aliases) == 0 {
		return 0, 0, false, nil
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(aliases)), ",")
	query := fmt.Sprintf(`
		SELECT id, pool_id
		FROM cross_seed_partial_pool_members
		WHERE instance_id = ? AND status <> ? AND (
			LOWER(torrent_key) IN (%[1]s) OR
			LOWER(infohash_v1) IN (%[1]s) OR
			LOWER(infohash_v2) IN (%[1]s)
		)
		ORDER BY id
		LIMIT 1
	`, placeholders)
	args := make([]any, 0, 2+len(aliases)*3)
	args = append(args, instanceID, CrossSeedPartialPoolMemberStatusRemoved)
	for range 3 {
		for _, alias := range aliases {
			args = append(args, alias)
		}
	}
	err = q.QueryRowContext(ctx, query, args...).Scan(&memberID, &poolID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if err != nil {
		return 0, 0, false, fmt.Errorf("resolve partial pool member alias: %w", err)
	}
	return memberID, poolID, true, nil
}

func partialPoolMemberByID(pool *CrossSeedPartialPool, id int64) *CrossSeedPartialPoolMember {
	if pool == nil {
		return nil
	}
	for _, member := range pool.Members {
		if member.ID == id {
			return member
		}
	}
	return nil
}

// GetPartialPool loads a pool and all of its members/files.
func (s *CrossSeedStore) GetPartialPool(ctx context.Context, poolID int64) (*CrossSeedPartialPool, error) {
	pool := &CrossSeedPartialPool{}
	err := s.db.QueryRowContext(ctx, `
		SELECT id, source_instance_id, source_torrent_key, status, created_at, updated_at
		FROM cross_seed_partial_pools WHERE id = ?
	`, poolID).Scan(&pool.ID, &pool.SourceInstanceID, &pool.SourceTorrentKey, &pool.Status, &pool.CreatedAt, &pool.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("load partial pool: %w", err)
	}

	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pool_id, instance_id, torrent_key, infohash_v1, infohash_v2,
		       mode, root_path, status, missing_bytes,
		       started_by_pool, last_downloaded_bytes, last_progress_at, retry_after,
		       review_pause_pending, resume_attempts, recovery_attempts,
		       last_error, created_at, updated_at
		FROM cross_seed_partial_pool_members
		WHERE pool_id = ? AND status <> ?
		ORDER BY instance_id, torrent_key
	`, poolID, CrossSeedPartialPoolMemberStatusRemoved)
	if err != nil {
		return nil, fmt.Errorf("load partial pool members: %w", err)
	}
	memberByID := make(map[int64]*CrossSeedPartialPoolMember)
	for rows.Next() {
		member, scanErr := scanPartialPoolMember(rows)
		if scanErr != nil {
			rows.Close()
			return nil, scanErr
		}
		pool.Members = append(pool.Members, member)
		memberByID[member.ID] = member
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	fileRows, err := s.db.QueryContext(ctx, `
		SELECT id, member_id, file_index, relative_path, size_bytes, pieces_root,
		       wanted_at_admission, materialized_at_add, replaceable_at_add, status, source_file_id,
		       last_error, created_at, updated_at
		FROM cross_seed_partial_pool_member_files
		WHERE member_id IN (
			SELECT id FROM cross_seed_partial_pool_members
			WHERE pool_id = ? AND status <> ?
		)
		ORDER BY member_id, file_index
	`, poolID, CrossSeedPartialPoolMemberStatusRemoved)
	if err != nil {
		return nil, fmt.Errorf("load partial pool files: %w", err)
	}
	defer fileRows.Close()
	for fileRows.Next() {
		file, scanErr := scanPartialPoolFile(fileRows)
		if scanErr != nil {
			return nil, scanErr
		}
		if member := memberByID[file.MemberID]; member != nil {
			member.Files = append(member.Files, file)
		}
	}
	if err := fileRows.Err(); err != nil {
		return nil, err
	}
	return pool, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanPartialPoolMember(scanner rowScanner) (*CrossSeedPartialPoolMember, error) {
	member := &CrossSeedPartialPoolMember{}
	var started, reviewPausePending int
	var lastDownloaded, resumeAttempts, recoveryAttempts sql.NullInt64
	var lastProgress, retryAfter sql.NullTime
	err := scanner.Scan(
		&member.ID, &member.PoolID, &member.InstanceID, &member.TorrentKey,
		&member.InfoHashV1, &member.InfoHashV2, &member.Mode, &member.RootPath,
		&member.Status, &member.MissingBytes, &started,
		&lastDownloaded, &lastProgress, &retryAfter, &reviewPausePending,
		&resumeAttempts, &recoveryAttempts, &member.LastError,
		&member.CreatedAt, &member.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan partial pool member: %w", err)
	}
	member.StartedByPool = SQLiteIntToBool(started)
	member.ReviewPausePending = SQLiteIntToBool(reviewPausePending)
	if lastDownloaded.Valid {
		value := lastDownloaded.Int64
		member.LastDownloadedBytes = &value
	}
	if lastProgress.Valid {
		value := lastProgress.Time
		member.LastProgressAt = &value
	}
	if retryAfter.Valid {
		value := retryAfter.Time
		member.RetryAfter = &value
	}
	if resumeAttempts.Valid {
		value := resumeAttempts.Int64
		member.ResumeAttempts = &value
	}
	if recoveryAttempts.Valid {
		value := recoveryAttempts.Int64
		member.RecoveryAttempts = &value
	}
	return member, nil
}

func scanPartialPoolFile(scanner rowScanner) (*CrossSeedPartialPoolMemberFile, error) {
	file := &CrossSeedPartialPoolMemberFile{}
	var piecesRoot sql.NullString
	var wanted, materialized, replaceable int
	var sourceFileID sql.NullInt64
	err := scanner.Scan(
		&file.ID, &file.MemberID, &file.FileIndex, &file.RelativePath,
		&file.SizeBytes, &piecesRoot, &wanted, &materialized, &replaceable, &file.Status,
		&sourceFileID, &file.LastError, &file.CreatedAt, &file.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("scan partial pool file: %w", err)
	}
	if piecesRoot.Valid {
		file.PiecesRoot = piecesRoot.String
	}
	file.WantedAtAdmission = SQLiteIntToBool(wanted)
	file.MaterializedAtAdd = SQLiteIntToBool(materialized)
	file.ReplaceableAtAdd = SQLiteIntToBool(replaceable)
	if sourceFileID.Valid {
		value := sourceFileID.Int64
		file.SourceFileID = &value
	}
	return file, nil
}

// ListPartialPoolsForReconciliation loads every pool with a non-removed
// member. Manual and complete members remain observable for completion/removal.
func (s *CrossSeedStore) ListPartialPoolsForReconciliation(ctx context.Context) ([]*CrossSeedPartialPool, error) {
	return s.listPartialPoolsForReconciliation(ctx, "")
}

// ListActivePartialPoolsForReconciliation loads scheduled pools with a
// non-removed member.
func (s *CrossSeedStore) ListActivePartialPoolsForReconciliation(ctx context.Context) ([]*CrossSeedPartialPool, error) {
	return s.listPartialPoolsForReconciliation(ctx, CrossSeedPartialPoolStatusActive)
}

// ListPartialPoolMembersAwaitingRecheckObservation loads only members whose
// requested piece check has not yet been observed.
func (s *CrossSeedStore) ListPartialPoolMembersAwaitingRecheckObservation(ctx context.Context) ([]*CrossSeedPartialPoolMember, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, pool_id, instance_id, torrent_key, infohash_v1, infohash_v2,
		       mode, root_path, status, missing_bytes,
		       started_by_pool, last_downloaded_bytes, last_progress_at, retry_after,
		       review_pause_pending, resume_attempts, recovery_attempts,
		       last_error, created_at, updated_at
		FROM cross_seed_partial_pool_members
		WHERE status IN (?, ?) AND last_error = ?
		ORDER BY id
	`, CrossSeedPartialPoolMemberStatusVerifying, CrossSeedPartialPoolMemberStatusRechecking, CrossSeedPartialPoolRecheckRequested)
	if err != nil {
		return nil, fmt.Errorf("list partial pool recheck observations: %w", err)
	}
	defer rows.Close()

	var members []*CrossSeedPartialPoolMember
	for rows.Next() {
		member, scanErr := scanPartialPoolMember(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		members = append(members, member)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return members, nil
}

func (s *CrossSeedStore) listPartialPoolsForReconciliation(ctx context.Context, status string) ([]*CrossSeedPartialPool, error) {
	statusFilter := ""
	args := []any{CrossSeedPartialPoolMemberStatusRemoved}
	if status != "" {
		statusFilter = "p.status = ? AND"
		args = append([]any{status}, args...)
	}
	rows, err := s.db.QueryContext(ctx, `
		SELECT p.id
		FROM cross_seed_partial_pools p
		WHERE `+statusFilter+` EXISTS (
			SELECT 1 FROM cross_seed_partial_pool_members m
			WHERE m.pool_id = p.id AND m.status <> ?
			)
		ORDER BY p.id
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("list partial pools: %w", err)
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	pools := make([]*CrossSeedPartialPool, 0, len(ids))
	for _, id := range ids {
		pool, err := s.GetPartialPool(ctx, id)
		if err != nil {
			return nil, err
		}
		pools = append(pools, pool)
	}
	return pools, nil
}

// ResolvePartialPoolMember finds a member by any hash alias and reactivates its pool.
func (s *CrossSeedStore) ResolvePartialPoolMember(ctx context.Context, instanceID int, aliases ...string) (*CrossSeedPartialPool, *CrossSeedPartialPoolMember, error) {
	normalized := normalizedPartialPoolAliases(aliases)
	memberID, poolID, found, err := findPartialPoolMemberByAliases(ctx, s.db, instanceID, normalized)
	if err != nil || !found {
		return nil, nil, err
	}
	if err := s.SetPartialPoolStatus(ctx, poolID, CrossSeedPartialPoolStatusActive); err != nil {
		return nil, nil, err
	}
	pool, err := s.GetPartialPool(ctx, poolID)
	if err != nil {
		return nil, nil, err
	}
	return pool, partialPoolMemberByID(pool, memberID), nil
}

// SetPartialPoolStatus changes pool scheduling state.
func (s *CrossSeedStore) SetPartialPoolStatus(ctx context.Context, poolID int64, status string) error {
	if status != CrossSeedPartialPoolStatusActive && status != CrossSeedPartialPoolStatusDormant {
		return fmt.Errorf("invalid partial pool status %q", status)
	}
	_, err := s.db.ExecContext(ctx, `UPDATE cross_seed_partial_pools SET status = ?, updated_at = ? WHERE id = ?`, status, time.Now().UTC(), poolID)
	if err != nil {
		return fmt.Errorf("update partial pool status: %w", err)
	}
	return nil
}

// SetPartialPoolStatusIfUnchanged changes scheduling state only when the pool
// has not been updated since it was loaded.
func (s *CrossSeedStore) SetPartialPoolStatusIfUnchanged(ctx context.Context, poolID int64, updatedAt time.Time, status string) (bool, error) {
	if status != CrossSeedPartialPoolStatusActive && status != CrossSeedPartialPoolStatusDormant {
		return false, fmt.Errorf("invalid partial pool status %q", status)
	}
	result, err := s.db.ExecContext(ctx, `
		UPDATE cross_seed_partial_pools
		SET status = ?, updated_at = ?
		WHERE id = ? AND updated_at = ?
	`, status, time.Now().UTC(), poolID, updatedAt)
	if err != nil {
		return false, fmt.Errorf("compare and set partial pool status: %w", err)
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// TransitionPartialPoolMember applies an expected-state and admission compare-and-set.
func (s *CrossSeedStore) TransitionPartialPoolMember(ctx context.Context, memberID int64, admittedAt time.Time, expected []string, status string, mutation PartialPoolMemberMutation) (bool, error) {
	if len(expected) == 0 || !validPartialPoolMemberStatus(status) {
		return false, errors.New("partial pool member transition requires valid states")
	}
	for _, from := range expected {
		if !validPartialPoolMemberTransition(from, status) {
			return false, fmt.Errorf("invalid partial pool member transition %s -> %s", from, status)
		}
	}

	sets := []string{"status = ?"}
	args := []any{status}
	if mutation.MissingBytes != nil {
		sets = append(sets, "missing_bytes = ?")
		args = append(args, max(*mutation.MissingBytes, 0))
	}
	terminal := status == CrossSeedPartialPoolMemberStatusManual || status == CrossSeedPartialPoolMemberStatusComplete || status == CrossSeedPartialPoolMemberStatusRemoved
	if mutation.StartedByPool != nil && !terminal {
		sets = append(sets, "started_by_pool = ?")
		args = append(args, BoolToSQLite(*mutation.StartedByPool))
	}
	if mutation.LastDownloadedBytes.Set {
		sets = append(sets, "last_downloaded_bytes = ?")
		args = append(args, mutation.LastDownloadedBytes.Value)
	}
	if mutation.LastProgressAt.Set {
		sets = append(sets, "last_progress_at = ?")
		args = append(args, mutation.LastProgressAt.Value)
	}
	if mutation.RetryAfter.Set {
		sets = append(sets, "retry_after = ?")
		args = append(args, mutation.RetryAfter.Value)
	}
	if mutation.ReviewPausePending != nil {
		sets = append(sets, "review_pause_pending = ?")
		args = append(args, BoolToSQLite(*mutation.ReviewPausePending))
	}
	if mutation.ResumeAttempts.Set {
		sets = append(sets, "resume_attempts = ?")
		args = append(args, mutation.ResumeAttempts.Value)
	}
	if mutation.RecoveryAttempts.Set {
		sets = append(sets, "recovery_attempts = ?")
		args = append(args, mutation.RecoveryAttempts.Value)
	}
	if mutation.LastError != nil {
		sets = append(sets, "last_error = ?")
		args = append(args, *mutation.LastError)
	}
	if terminal {
		sets = append(sets, "started_by_pool = ?")
		args = append(args, BoolToSQLite(false))
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(expected)), ",")
	query := fmt.Sprintf(`UPDATE cross_seed_partial_pool_members SET %s WHERE id = ? AND created_at = ? AND status IN (%s)`, strings.Join(sets, ", "), placeholders)
	args = append(args, memberID, admittedAt)
	for _, from := range expected {
		args = append(args, from)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("transition partial pool member: %w", err)
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// ClaimPartialPoolDownloader locks the pool row before inspecting current pool
// membership, then claims at most one eligible downloader whose admission
// generation matches admittedAt.
func (s *CrossSeedStore) ClaimPartialPoolDownloader(ctx context.Context, memberID, downloadedBytes int64, admittedAt, now, admissionCutoff time.Time) (bool, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin partial pool downloader claim: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var poolID int64
	if err := tx.QueryRowContext(ctx, `SELECT pool_id FROM cross_seed_partial_pool_members WHERE id = ?`, memberID).Scan(&poolID); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("resolve partial pool downloader claim: %w", err)
	}

	lock, err := tx.ExecContext(ctx, `
		UPDATE cross_seed_partial_pools
		SET status = status
		WHERE id = ?
	`, poolID)
	if err != nil {
		return false, fmt.Errorf("lock partial pool downloader claim: %w", err)
	}
	locked, err := lock.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("inspect partial pool downloader lock: %w", err)
	}
	if locked != 1 {
		return false, nil
	}

	result, err := tx.ExecContext(ctx, `
		UPDATE cross_seed_partial_pool_members AS selected
		SET status = ?, started_by_pool = ?, last_downloaded_bytes = ?,
		    last_progress_at = ?, retry_after = NULL, review_pause_pending = ?,
		    resume_attempts = NULL, recovery_attempts = NULL, last_error = '',
		    updated_at = CURRENT_TIMESTAMP
		WHERE selected.id = ? AND selected.created_at = ? AND selected.status = ?
		  AND (selected.retry_after IS NULL OR selected.retry_after <= ?)
		  AND NOT EXISTS (
			SELECT 1 FROM cross_seed_partial_pool_members other
			WHERE other.pool_id = selected.pool_id
			  AND (
				other.status IN (?, ?)
				OR (
					other.status = ?
					AND (
						other.last_error <> ?
						OR EXISTS (
							SELECT 1 FROM cross_seed_partial_pool_member_files file
							WHERE file.member_id = other.id
							  AND file.status IN (?, ?)
						)
					)
				)
				OR (other.status <> ? AND other.created_at > ?)
			  )
		  )
	`,
		CrossSeedPartialPoolMemberStatusAcquiring,
		BoolToSQLite(true),
		downloadedBytes,
		now,
		BoolToSQLite(false),
		memberID,
		admittedAt,
		CrossSeedPartialPoolMemberStatusWaiting,
		now,
		CrossSeedPartialPoolMemberStatusAcquiring,
		CrossSeedPartialPoolMemberStatusRechecking,
		CrossSeedPartialPoolMemberStatusVerifying,
		CrossSeedPartialPoolRecheckPending,
		CrossSeedPartialPoolFileStatusPropagating,
		CrossSeedPartialPoolFileStatusVerifying,
		CrossSeedPartialPoolMemberStatusRemoved,
		admissionCutoff,
	)
	if err != nil {
		return false, fmt.Errorf("claim partial pool downloader: %w", err)
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return false, err
	}
	if changed != 1 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit partial pool downloader claim: %w", err)
	}
	return true, nil
}

// TransitionPartialPoolFile applies an expected-state and admission compare-and-set.
func (s *CrossSeedStore) TransitionPartialPoolFile(ctx context.Context, fileID int64, admittedAt time.Time, expected []string, status string, mutation PartialPoolFileMutation) (bool, error) {
	if len(expected) == 0 || !validPartialPoolFileStatus(status) {
		return false, errors.New("partial pool file transition requires valid states")
	}
	for _, from := range expected {
		if !validPartialPoolFileTransition(from, status) {
			return false, fmt.Errorf("invalid partial pool file transition %s -> %s", from, status)
		}
	}
	sets := []string{"status = ?"}
	args := []any{status}
	if mutation.SourceFileID.Set {
		sets = append(sets, "source_file_id = ?")
		args = append(args, mutation.SourceFileID.Value)
	}
	if mutation.LastError != nil {
		sets = append(sets, "last_error = ?")
		args = append(args, *mutation.LastError)
	}
	sets = append(sets, "updated_at = CURRENT_TIMESTAMP")
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(expected)), ",")
	query := fmt.Sprintf(`UPDATE cross_seed_partial_pool_member_files SET %s WHERE id = ? AND created_at = ? AND status IN (%s)`, strings.Join(sets, ", "), placeholders)
	args = append(args, fileID, admittedAt)
	for _, from := range expected {
		args = append(args, from)
	}
	result, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return false, fmt.Errorf("transition partial pool file: %w", err)
	}
	changed, err := result.RowsAffected()
	return changed == 1, err
}

// TransitionPartialPoolHardlinkRollback atomically marks a verifying file
// missing, clears its source, and records the member's pending follow-up check.
// It returns false without committing when either compare-and-set no longer
// matches, and returns an error for invalid input or database failures.
func (s *CrossSeedStore) TransitionPartialPoolHardlinkRollback(ctx context.Context, memberID, fileID int64, memberAdmittedAt, fileAdmittedAt time.Time, memberStatus string) (bool, error) {
	if memberID == 0 || fileID == 0 || (memberStatus != CrossSeedPartialPoolMemberStatusVerifying && memberStatus != CrossSeedPartialPoolMemberStatusRechecking) {
		return false, errors.New("partial pool hardlink rollback requires valid member and file state")
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin partial pool hardlink rollback: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	fileResult, err := tx.ExecContext(ctx, `
		UPDATE cross_seed_partial_pool_member_files
		SET status = ?, source_file_id = NULL, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND member_id = ? AND created_at = ? AND status = ?
	`, CrossSeedPartialPoolFileStatusMissing, fileID, memberID, fileAdmittedAt, CrossSeedPartialPoolFileStatusVerifying)
	if err != nil {
		return false, fmt.Errorf("record partial pool hardlink rollback file: %w", err)
	}
	fileChanged, err := fileResult.RowsAffected()
	if err != nil {
		return false, err
	}
	if fileChanged != 1 {
		return false, nil
	}

	memberResult, err := tx.ExecContext(ctx, `
		UPDATE cross_seed_partial_pool_members
		SET last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND created_at = ? AND status = ?
	`, CrossSeedPartialPoolRecheckPending, memberID, memberAdmittedAt, memberStatus)
	if err != nil {
		return false, fmt.Errorf("record partial pool hardlink rollback member: %w", err)
	}
	memberChanged, err := memberResult.RowsAffected()
	if err != nil {
		return false, err
	}
	if memberChanged != 1 {
		return false, nil
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit partial pool hardlink rollback: %w", err)
	}
	return true, nil
}

// MarkPartialPoolMemberRemoved records qBittorrent removal only while the
// member's pool and admission timestamp still match, then deletes an empty pool.
func (s *CrossSeedStore) MarkPartialPoolMemberRemoved(ctx context.Context, poolID, memberID int64, admittedAt time.Time, reason string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	result, err := tx.ExecContext(ctx, `UPDATE cross_seed_partial_pools SET status = status WHERE id = ?`, poolID)
	if err != nil {
		return fmt.Errorf("lock partial pool for removal: %w", err)
	}
	locked, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if locked != 1 {
		return tx.Commit()
	}
	result, err = tx.ExecContext(ctx, `
		UPDATE cross_seed_partial_pool_members
		SET status = ?, started_by_pool = ?, review_pause_pending = ?,
		    resume_attempts = NULL, recovery_attempts = NULL,
		    last_error = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ? AND pool_id = ? AND created_at = ? AND status <> ?
	`, CrossSeedPartialPoolMemberStatusRemoved, BoolToSQLite(false), BoolToSQLite(false), reason, memberID, poolID, admittedAt, CrossSeedPartialPoolMemberStatusRemoved)
	if err != nil {
		return err
	}
	removed, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if removed != 1 {
		return tx.Commit()
	}
	var remaining int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM cross_seed_partial_pool_members WHERE pool_id = ? AND status <> ?`, poolID, CrossSeedPartialPoolMemberStatusRemoved).Scan(&remaining); err != nil {
		return err
	}
	if remaining == 0 {
		if _, err := tx.ExecContext(ctx, `DELETE FROM cross_seed_partial_pools WHERE id = ?`, poolID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// PruneEmptyPartialPools removes source anchors left after instance cascades.
func (s *CrossSeedStore) PruneEmptyPartialPools(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `
		DELETE FROM cross_seed_partial_pools
		WHERE NOT EXISTS (
			SELECT 1 FROM cross_seed_partial_pool_members
			WHERE cross_seed_partial_pool_members.pool_id = cross_seed_partial_pools.id
		)
	`)
	return err
}

func validPartialPoolMemberStatus(status string) bool {
	switch status {
	case CrossSeedPartialPoolMemberStatusVerifying,
		CrossSeedPartialPoolMemberStatusWaiting,
		CrossSeedPartialPoolMemberStatusBlocked,
		CrossSeedPartialPoolMemberStatusAcquiring,
		CrossSeedPartialPoolMemberStatusRechecking,
		CrossSeedPartialPoolMemberStatusComplete,
		CrossSeedPartialPoolMemberStatusManual,
		CrossSeedPartialPoolMemberStatusRemoved:
		return true
	default:
		return false
	}
}

func validPartialPoolMemberTransition(from, to string) bool {
	if from == to {
		return validPartialPoolMemberStatus(from)
	}
	switch from {
	case CrossSeedPartialPoolMemberStatusVerifying:
		return to == CrossSeedPartialPoolMemberStatusWaiting || to == CrossSeedPartialPoolMemberStatusBlocked || to == CrossSeedPartialPoolMemberStatusComplete || to == CrossSeedPartialPoolMemberStatusManual || to == CrossSeedPartialPoolMemberStatusRemoved
	case CrossSeedPartialPoolMemberStatusWaiting:
		return to == CrossSeedPartialPoolMemberStatusAcquiring || to == CrossSeedPartialPoolMemberStatusRechecking || to == CrossSeedPartialPoolMemberStatusBlocked || to == CrossSeedPartialPoolMemberStatusComplete || to == CrossSeedPartialPoolMemberStatusManual || to == CrossSeedPartialPoolMemberStatusRemoved
	case CrossSeedPartialPoolMemberStatusBlocked:
		return to == CrossSeedPartialPoolMemberStatusWaiting || to == CrossSeedPartialPoolMemberStatusRechecking || to == CrossSeedPartialPoolMemberStatusComplete || to == CrossSeedPartialPoolMemberStatusManual || to == CrossSeedPartialPoolMemberStatusRemoved
	case CrossSeedPartialPoolMemberStatusAcquiring:
		return to == CrossSeedPartialPoolMemberStatusRechecking || to == CrossSeedPartialPoolMemberStatusWaiting || to == CrossSeedPartialPoolMemberStatusComplete || to == CrossSeedPartialPoolMemberStatusManual || to == CrossSeedPartialPoolMemberStatusRemoved
	case CrossSeedPartialPoolMemberStatusRechecking:
		return to == CrossSeedPartialPoolMemberStatusWaiting || to == CrossSeedPartialPoolMemberStatusBlocked || to == CrossSeedPartialPoolMemberStatusComplete || to == CrossSeedPartialPoolMemberStatusManual || to == CrossSeedPartialPoolMemberStatusRemoved
	case CrossSeedPartialPoolMemberStatusManual:
		return to == CrossSeedPartialPoolMemberStatusComplete || to == CrossSeedPartialPoolMemberStatusRemoved
	case CrossSeedPartialPoolMemberStatusComplete:
		return to == CrossSeedPartialPoolMemberStatusManual || to == CrossSeedPartialPoolMemberStatusRemoved
	default:
		return false
	}
}

func validPartialPoolFileStatus(status string) bool {
	switch status {
	case CrossSeedPartialPoolFileStatusPresent,
		CrossSeedPartialPoolFileStatusMissing,
		CrossSeedPartialPoolFileStatusAcquiring,
		CrossSeedPartialPoolFileStatusAvailable,
		CrossSeedPartialPoolFileStatusPropagating,
		CrossSeedPartialPoolFileStatusVerifying,
		CrossSeedPartialPoolFileStatusVerified,
		CrossSeedPartialPoolFileStatusManual:
		return true
	default:
		return false
	}
}

func validPartialPoolFileTransition(from, to string) bool {
	if from == to {
		return validPartialPoolFileStatus(from)
	}
	switch from {
	case CrossSeedPartialPoolFileStatusPresent:
		return to == CrossSeedPartialPoolFileStatusVerified || to == CrossSeedPartialPoolFileStatusManual
	case CrossSeedPartialPoolFileStatusMissing:
		return to == CrossSeedPartialPoolFileStatusAcquiring || to == CrossSeedPartialPoolFileStatusAvailable || to == CrossSeedPartialPoolFileStatusPropagating || to == CrossSeedPartialPoolFileStatusManual
	case CrossSeedPartialPoolFileStatusAcquiring:
		return to == CrossSeedPartialPoolFileStatusAvailable || to == CrossSeedPartialPoolFileStatusMissing || to == CrossSeedPartialPoolFileStatusManual
	case CrossSeedPartialPoolFileStatusAvailable:
		return to == CrossSeedPartialPoolFileStatusVerified || to == CrossSeedPartialPoolFileStatusManual
	case CrossSeedPartialPoolFileStatusPropagating:
		return to == CrossSeedPartialPoolFileStatusVerifying || to == CrossSeedPartialPoolFileStatusMissing || to == CrossSeedPartialPoolFileStatusManual
	case CrossSeedPartialPoolFileStatusVerifying:
		return to == CrossSeedPartialPoolFileStatusVerified || to == CrossSeedPartialPoolFileStatusMissing || to == CrossSeedPartialPoolFileStatusManual
	case CrossSeedPartialPoolFileStatusVerified:
		return to == CrossSeedPartialPoolFileStatusManual
	default:
		return false
	}
}
