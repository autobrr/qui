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

// CrossSeedBlocklistEntry represents a per-instance cross-seed blocklist item.
type CrossSeedBlocklistEntry struct {
	InstanceID int       `json:"instanceId"`
	InfoHash   string    `json:"infoHash"`
	Note       string    `json:"note,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

type CrossSeedBlocklistStore struct {
	db dbinterface.Querier
}

func NewCrossSeedBlocklistStore(db dbinterface.Querier) *CrossSeedBlocklistStore {
	return &CrossSeedBlocklistStore{db: db}
}

func (s *CrossSeedBlocklistStore) List(ctx context.Context, instanceID int) ([]*CrossSeedBlocklistEntry, error) {
	query := `
		SELECT instance_id, infohash, note, created_at
		FROM cross_seed_blocklist
	`
	args := []any{}
	if instanceID > 0 {
		query += " WHERE instance_id = ?"
		args = append(args, instanceID)
	}
	query += " ORDER BY created_at DESC"

	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var entries []*CrossSeedBlocklistEntry
	for rows.Next() {
		var entry CrossSeedBlocklistEntry
		if err := rows.Scan(&entry.InstanceID, &entry.InfoHash, &entry.Note, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *CrossSeedBlocklistStore) Get(ctx context.Context, instanceID int, infoHash string) (*CrossSeedBlocklistEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT instance_id, infohash, note, created_at
		FROM cross_seed_blocklist
		WHERE instance_id = ? AND infohash = ?
	`, instanceID, normalizeInfoHash(infoHash))

	var entry CrossSeedBlocklistEntry
	if err := row.Scan(&entry.InstanceID, &entry.InfoHash, &entry.Note, &entry.CreatedAt); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (s *CrossSeedBlocklistStore) Upsert(ctx context.Context, entry *CrossSeedBlocklistEntry) (*CrossSeedBlocklistEntry, error) {
	if entry == nil {
		return nil, errors.New("entry is nil")
	}
	if entry.InstanceID <= 0 {
		return nil, errors.New("instanceID must be positive")
	}

	normalized := normalizeInfoHash(entry.InfoHash)
	if normalized == "" {
		return nil, errors.New("infohash is required")
	}
	note := strings.TrimSpace(entry.Note)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cross_seed_blocklist (instance_id, infohash, note)
		VALUES (?, ?, ?)
		ON CONFLICT(instance_id, infohash)
		DO UPDATE SET note = excluded.note
	`, entry.InstanceID, normalized, note)
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, entry.InstanceID, normalized)
}

func (s *CrossSeedBlocklistStore) Delete(ctx context.Context, instanceID int, infoHash string) error {
	normalized := normalizeInfoHash(infoHash)
	if normalized == "" {
		return errors.New("infohash is required")
	}

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM cross_seed_blocklist
		WHERE instance_id = ? AND infohash = ?
	`, instanceID, normalized)
	if err != nil {
		return err
	}

	affected, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if affected == 0 {
		return sql.ErrNoRows
	}

	return nil
}

// FindBlocked returns the first blocked infohash from the provided list.
func (s *CrossSeedBlocklistStore) FindBlocked(ctx context.Context, instanceID int, hashes []string) (string, bool, error) {
	if instanceID <= 0 || len(hashes) == 0 {
		return "", false, nil
	}

	normalized := normalizeInfoHashList(hashes)
	if len(normalized) == 0 {
		return "", false, nil
	}

	placeholders := buildPlaceholders(len(normalized))
	query := fmt.Sprintf(`
		SELECT infohash
		FROM cross_seed_blocklist
		WHERE instance_id = ? AND infohash IN (%s)
		LIMIT 1
	`, placeholders)

	args := make([]any, 0, len(normalized)+1)
	args = append(args, instanceID)
	for _, h := range normalized {
		args = append(args, h)
	}

	var infohash string
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&infohash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", false, nil
		}
		return "", false, err
	}

	return infohash, true, nil
}

func normalizeInfoHash(value string) string {
	return strings.ToLower(strings.TrimSpace(value))
}

func normalizeInfoHashList(values []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeInfoHash(value)
		if normalized == "" {
			continue
		}
		if _, exists := seen[normalized]; exists {
			continue
		}
		seen[normalized] = struct{}{}
		out = append(out, normalized)
	}
	return out
}

func buildPlaceholders(count int) string {
	if count <= 0 {
		return ""
	}
	var sb strings.Builder
	for i := range count {
		if i > 0 {
			sb.WriteString(", ")
		}
		sb.WriteByte('?')
	}
	return sb.String()
}
