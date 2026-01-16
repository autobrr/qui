// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// TransferState represents the current state of a transfer operation
type TransferState string

const (
	TransferStatePending        TransferState = "pending"
	TransferStatePreparing      TransferState = "preparing"
	TransferStateLinksCreating  TransferState = "links_creating"
	TransferStateLinksCreated   TransferState = "links_created"
	TransferStateAddingTorrent  TransferState = "adding_torrent"
	TransferStateTorrentAdded   TransferState = "torrent_added"
	TransferStateDeletingSource TransferState = "deleting_source"
	TransferStateCompleted      TransferState = "completed"
	TransferStateFailed         TransferState = "failed"
	TransferStateRolledBack     TransferState = "rolled_back"
	TransferStateCancelled      TransferState = "cancelled"
)

// terminalStates is the single source of truth for terminal transfer states.
var terminalStates = map[TransferState]struct{}{
	TransferStateCompleted:  {},
	TransferStateFailed:     {},
	TransferStateRolledBack: {},
	TransferStateCancelled:  {},
}

// validStates contains all valid transfer states for validation.
var validStates = map[TransferState]struct{}{
	TransferStatePending:        {},
	TransferStatePreparing:      {},
	TransferStateLinksCreating:  {},
	TransferStateLinksCreated:   {},
	TransferStateAddingTorrent:  {},
	TransferStateTorrentAdded:   {},
	TransferStateDeletingSource: {},
	TransferStateCompleted:      {},
	TransferStateFailed:         {},
	TransferStateRolledBack:     {},
	TransferStateCancelled:      {},
}

// IsTerminal returns true if the state is a terminal state (no further transitions)
func (s TransferState) IsTerminal() bool {
	_, ok := terminalStates[s]
	return ok
}

// IsValid returns true if the state is a recognized transfer state.
func (s TransferState) IsValid() bool {
	_, ok := validStates[s]
	return ok
}

// Transfer represents a torrent transfer between instances
type Transfer struct {
	ID               int64         `json:"id"`
	SourceInstanceID int           `json:"sourceInstanceId"`
	TargetInstanceID int           `json:"targetInstanceId"`
	TorrentHash      string        `json:"torrentHash"`
	TorrentName      string        `json:"torrentName"`
	State            TransferState `json:"state"`

	// Persisted for recovery
	SourceSavePath string `json:"sourceSavePath,omitempty"`
	TargetSavePath string `json:"targetSavePath,omitempty"`
	LinkMode       string `json:"linkMode,omitempty"` // "hardlink", "reflink", "direct"

	// Options
	DeleteFromSource bool              `json:"deleteFromSource"`
	PreserveCategory bool              `json:"preserveCategory"`
	PreserveTags     bool              `json:"preserveTags"`
	TargetCategory   string            `json:"targetCategory,omitempty"`
	TargetTags       []string          `json:"targetTags,omitempty"`
	PathMappings     map[string]string `json:"pathMappings,omitempty"`

	// Progress
	FilesTotal  int `json:"filesTotal"`
	FilesLinked int `json:"filesLinked"`

	// Error info
	Error string `json:"error,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"createdAt"`
	UpdatedAt   time.Time  `json:"updatedAt"`
	CompletedAt *time.Time `json:"completedAt,omitempty"`
}

// TransferStore handles database operations for transfers
type TransferStore struct {
	db dbinterface.Querier
}

// NewTransferStore creates a new TransferStore
func NewTransferStore(db dbinterface.Querier) *TransferStore {
	return &TransferStore{db: db}
}

// Create inserts a new transfer record
func (s *TransferStore) Create(ctx context.Context, t *Transfer) (*Transfer, error) {
	if t == nil {
		return nil, errors.New("transfer is nil")
	}
	if t.State == "" {
		t.State = TransferStatePending
	}
	if t.SourceInstanceID == 0 {
		return nil, errors.New("source instance ID is required")
	}
	if t.TargetInstanceID == 0 {
		return nil, errors.New("target instance ID is required")
	}
	if t.TorrentHash == "" {
		return nil, errors.New("torrent hash is required")
	}

	var targetTagsJSON sql.NullString
	if len(t.TargetTags) > 0 {
		data, err := json.Marshal(t.TargetTags)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal target_tags: %w", err)
		}
		targetTagsJSON = sql.NullString{String: string(data), Valid: true}
	}

	var pathMappingsJSON sql.NullString
	if len(t.PathMappings) > 0 {
		data, err := json.Marshal(t.PathMappings)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal path_mappings: %w", err)
		}
		pathMappingsJSON = sql.NullString{String: string(data), Valid: true}
	}

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO transfers (
			source_instance_id, target_instance_id, torrent_hash, torrent_name,
			state, source_save_path, target_save_path, link_mode,
			delete_from_source, preserve_category, preserve_tags,
			target_category, target_tags, path_mappings,
			files_total, files_linked, error
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`,
		t.SourceInstanceID, t.TargetInstanceID, t.TorrentHash, t.TorrentName,
		t.State, nullString(t.SourceSavePath), nullString(t.TargetSavePath), nullString(t.LinkMode),
		boolToInt(t.DeleteFromSource), boolToInt(t.PreserveCategory), boolToInt(t.PreserveTags),
		nullString(t.TargetCategory), targetTagsJSON, pathMappingsJSON,
		t.FilesTotal, t.FilesLinked, nullString(t.Error),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to insert transfer: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return nil, fmt.Errorf("failed to get last insert id: %w", err)
	}

	return s.Get(ctx, id)
}

// Get retrieves a transfer by ID
func (s *TransferStore) Get(ctx context.Context, id int64) (*Transfer, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, source_instance_id, target_instance_id, torrent_hash, torrent_name,
			state, source_save_path, target_save_path, link_mode,
			delete_from_source, preserve_category, preserve_tags,
			target_category, target_tags, path_mappings,
			files_total, files_linked, error,
			created_at, updated_at, completed_at
		FROM transfers
		WHERE id = ?
	`, id)

	return s.scanTransfer(row)
}

// GetByHash retrieves a non-terminal transfer by torrent hash
func (s *TransferStore) GetByHash(ctx context.Context, hash string) (*Transfer, error) {
	query := `
		SELECT id, source_instance_id, target_instance_id, torrent_hash, torrent_name,
			state, source_save_path, target_save_path, link_mode,
			delete_from_source, preserve_category, preserve_tags,
			target_category, target_tags, path_mappings,
			files_total, files_linked, error,
			created_at, updated_at, completed_at
		FROM transfers
		WHERE torrent_hash = ? AND state NOT IN (`

	args := []any{hash}
	first := true
	for state := range terminalStates {
		if !first {
			query += ", "
		}
		query += "?"
		args = append(args, state)
		first = false
	}
	query += `) ORDER BY created_at DESC LIMIT 1`

	row := s.db.QueryRowContext(ctx, query, args...)
	return s.scanTransfer(row)
}

// Update updates a transfer record
func (s *TransferStore) Update(ctx context.Context, t *Transfer) error {
	if t == nil {
		return errors.New("transfer is nil")
	}

	var targetTagsJSON sql.NullString
	if len(t.TargetTags) > 0 {
		data, err := json.Marshal(t.TargetTags)
		if err != nil {
			return fmt.Errorf("failed to marshal target_tags: %w", err)
		}
		targetTagsJSON = sql.NullString{String: string(data), Valid: true}
	}

	var pathMappingsJSON sql.NullString
	if len(t.PathMappings) > 0 {
		data, err := json.Marshal(t.PathMappings)
		if err != nil {
			return fmt.Errorf("failed to marshal path_mappings: %w", err)
		}
		pathMappingsJSON = sql.NullString{String: string(data), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE transfers SET
			torrent_name = ?, state = ?,
			source_save_path = ?, target_save_path = ?, link_mode = ?,
			delete_from_source = ?, preserve_category = ?, preserve_tags = ?,
			target_category = ?, target_tags = ?, path_mappings = ?,
			files_total = ?, files_linked = ?, error = ?, completed_at = ?
		WHERE id = ?
	`,
		t.TorrentName, t.State,
		nullString(t.SourceSavePath), nullString(t.TargetSavePath), nullString(t.LinkMode),
		boolToInt(t.DeleteFromSource), boolToInt(t.PreserveCategory), boolToInt(t.PreserveTags),
		nullString(t.TargetCategory), targetTagsJSON, pathMappingsJSON,
		t.FilesTotal, t.FilesLinked, nullString(t.Error), nullTime(t.CompletedAt),
		t.ID,
	)
	return err
}

// UpdateState updates just the state and optional error message
func (s *TransferStore) UpdateState(ctx context.Context, id int64, state TransferState, errorMsg string) error {
	var completedAt sql.NullTime
	if state.IsTerminal() {
		completedAt = sql.NullTime{Time: time.Now().UTC(), Valid: true}
	}

	_, err := s.db.ExecContext(ctx, `
		UPDATE transfers SET state = ?, error = ?, completed_at = ?
		WHERE id = ?
	`, state, nullString(errorMsg), completedAt, id)
	return err
}

// UpdateProgress updates files_linked count
func (s *TransferStore) UpdateProgress(ctx context.Context, id int64, filesLinked int) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE transfers SET files_linked = ?
		WHERE id = ?
	`, filesLinked, id)
	return err
}

// ListByStates returns transfers in any of the given states
func (s *TransferStore) ListByStates(ctx context.Context, states []TransferState, limit, offset int) ([]*Transfer, error) {
	if len(states) == 0 {
		return []*Transfer{}, nil
	}

	query := `
		SELECT id, source_instance_id, target_instance_id, torrent_hash, torrent_name,
			state, source_save_path, target_save_path, link_mode,
			delete_from_source, preserve_category, preserve_tags,
			target_category, target_tags, path_mappings,
			files_total, files_linked, error,
			created_at, updated_at, completed_at
		FROM transfers
		WHERE state IN (`

	args := make([]any, len(states))
	for i, state := range states {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args[i] = state
	}
	query += `) ORDER BY created_at ASC LIMIT ? OFFSET ?`

	args = append(args, limit, offset)
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanTransfers(rows)
}

// ListByInstance returns transfers for a given source or target instance
func (s *TransferStore) ListByInstance(ctx context.Context, instanceID int, limit, offset int) ([]*Transfer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_instance_id, target_instance_id, torrent_hash, torrent_name,
			state, source_save_path, target_save_path, link_mode,
			delete_from_source, preserve_category, preserve_tags,
			target_category, target_tags, path_mappings,
			files_total, files_linked, error,
			created_at, updated_at, completed_at
		FROM transfers
		WHERE source_instance_id = ? OR target_instance_id = ?
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, instanceID, instanceID, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanTransfers(rows)
}

// ListRecent returns recent transfers across all instances
func (s *TransferStore) ListRecent(ctx context.Context, limit, offset int) ([]*Transfer, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, source_instance_id, target_instance_id, torrent_hash, torrent_name,
			state, source_save_path, target_save_path, link_mode,
			delete_from_source, preserve_category, preserve_tags,
			target_category, target_tags, path_mappings,
			files_total, files_linked, error,
			created_at, updated_at, completed_at
		FROM transfers
		ORDER BY created_at DESC
		LIMIT ? OFFSET ?
	`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	return s.scanTransfers(rows)
}

// Delete removes a transfer record
func (s *TransferStore) Delete(ctx context.Context, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM transfers WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if rows == 0 {
		return sql.ErrNoRows
	}
	return nil
}

// MarkInterrupted marks in-progress transfers as failed (for recovery)
func (s *TransferStore) MarkInterrupted(ctx context.Context, states []TransferState, errorMsg string) (int64, error) {
	if len(states) == 0 {
		return 0, nil
	}

	query := `UPDATE transfers SET state = ?, error = ?, completed_at = ? WHERE state IN (`
	now := time.Now().UTC()
	args := []any{TransferStateFailed, errorMsg, now}

	for i, state := range states {
		if i > 0 {
			query += ", "
		}
		query += "?"
		args = append(args, state)
	}
	query += ")"

	res, err := s.db.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, err
	}

	return res.RowsAffected()
}

type scannable interface {
	Scan(rows ...any) error
}

// scanTransfer scans a single transfer row
func (s *TransferStore) scanTransfer(row scannable) (*Transfer, error) {
	var t Transfer
	var sourceSavePath, targetSavePath, linkMode sql.NullString
	var targetCategory sql.NullString
	var targetTagsJSON, pathMappingsJSON sql.NullString
	var errorStr sql.NullString
	var completedAt sql.NullTime

	err := row.Scan(
		&t.ID, &t.SourceInstanceID, &t.TargetInstanceID, &t.TorrentHash, &t.TorrentName,
		&t.State, &sourceSavePath, &targetSavePath, &linkMode,
		&t.DeleteFromSource, &t.PreserveCategory, &t.PreserveTags,
		&targetCategory, &targetTagsJSON, &pathMappingsJSON,
		&t.FilesTotal, &t.FilesLinked, &errorStr,
		&t.CreatedAt, &t.UpdatedAt, &completedAt,
	)
	if err != nil {
		return nil, err
	}

	t.SourceSavePath = sourceSavePath.String
	t.TargetSavePath = targetSavePath.String
	t.LinkMode = linkMode.String
	t.TargetCategory = targetCategory.String
	t.Error = errorStr.String

	if completedAt.Valid {
		t.CompletedAt = &completedAt.Time
	}

	if targetTagsJSON.Valid && targetTagsJSON.String != "" {
		if err := json.Unmarshal([]byte(targetTagsJSON.String), &t.TargetTags); err != nil {
			return nil, fmt.Errorf("failed to unmarshal target_tags: %w", err)
		}
	}

	if pathMappingsJSON.Valid && pathMappingsJSON.String != "" {
		if err := json.Unmarshal([]byte(pathMappingsJSON.String), &t.PathMappings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal path_mappings: %w", err)
		}
	}

	return &t, nil
}

// scanTransfers scans multiple transfer rows
func (s *TransferStore) scanTransfers(rows *sql.Rows) ([]*Transfer, error) {
	var transfers []*Transfer

	for rows.Next() {
		t, err := s.scanTransfer(rows)
		if err != nil {
			return nil, err
		}

		transfers = append(transfers, t)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	return transfers, nil
}

// nullString converts a string to sql.NullString
func nullString(s string) sql.NullString {
	if s == "" {
		return sql.NullString{}
	}
	return sql.NullString{String: s, Valid: true}
}

// nullTime converts a *time.Time to sql.NullTime
func nullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{}
	}
	return sql.NullTime{Time: *t, Valid: true}
}
