// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/pkg/infohash"
)

type CrossSeedBlocklistEntry struct {
	InstanceID int    `json:"instanceId"`
	InfoHash   string `json:"infoHash"`
	Note       string `json:"note,omitempty"`
	BlockedTorrent
	CreatedAt time.Time `json:"createdAt"`
}

type BlockedTorrent struct {
	Name    string `json:"torrentName,omitempty"`
	Tracker string `json:"tracker,omitempty"`
}

type CrossSeedBlocklistStore struct {
	db dbinterface.Querier
}

func NewCrossSeedBlocklistStore(db dbinterface.Querier) *CrossSeedBlocklistStore {
	return &CrossSeedBlocklistStore{db: db}
}

func (s *CrossSeedBlocklistStore) List(ctx context.Context, instanceID int) ([]*CrossSeedBlocklistEntry, error) {
	query := `
		SELECT instance_id, infohash, note, torrent_name, tracker, created_at
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
		if err := rows.Scan(&entry.InstanceID, &entry.InfoHash, &entry.Note, &entry.Name, &entry.Tracker, &entry.CreatedAt); err != nil {
			return nil, err
		}
		entries = append(entries, &entry)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	return entries, nil
}

func (s *CrossSeedBlocklistStore) Get(ctx context.Context, instanceID int, hash infohash.Hash) (*CrossSeedBlocklistEntry, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT instance_id, infohash, note, torrent_name, tracker, created_at
		FROM cross_seed_blocklist
		WHERE instance_id = ? AND infohash = ?
	`, instanceID, hash.String())

	var entry CrossSeedBlocklistEntry
	if err := row.Scan(&entry.InstanceID, &entry.InfoHash, &entry.Note, &entry.Name, &entry.Tracker, &entry.CreatedAt); err != nil {
		return nil, err
	}

	return &entry, nil
}

func (s *CrossSeedBlocklistStore) Upsert(ctx context.Context, instanceID int, hash infohash.Hash, note string, torrent BlockedTorrent) (*CrossSeedBlocklistEntry, error) {
	if instanceID <= 0 {
		return nil, errors.New("instanceID must be positive")
	}
	if hash.IsZero() {
		return nil, errors.New("infohash is required")
	}
	note = strings.TrimSpace(note)

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cross_seed_blocklist (instance_id, infohash, note, torrent_name, tracker)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(instance_id, infohash)
		DO UPDATE SET
			note = excluded.note,
			torrent_name = CASE WHEN excluded.torrent_name <> '' THEN excluded.torrent_name ELSE cross_seed_blocklist.torrent_name END,
			tracker = CASE WHEN excluded.tracker <> '' THEN excluded.tracker ELSE cross_seed_blocklist.tracker END
	`, instanceID, hash.String(), note, strings.TrimSpace(torrent.Name), strings.TrimSpace(torrent.Tracker))
	if err != nil {
		return nil, err
	}

	return s.Get(ctx, instanceID, hash)
}

func (s *CrossSeedBlocklistStore) Delete(ctx context.Context, instanceID int, hash infohash.Hash) error {
	if hash.IsZero() {
		return errors.New("infohash is required")
	}

	res, err := s.db.ExecContext(ctx, `
		DELETE FROM cross_seed_blocklist
		WHERE instance_id = ? AND infohash = ?
	`, instanceID, hash.String())
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

func (s *CrossSeedBlocklistStore) FindBlocked(ctx context.Context, instanceID int, hashes []infohash.Hash) (infohash.Hash, bool, error) {
	hashes = slices.DeleteFunc(slices.Clone(hashes), infohash.Hash.IsZero)
	if instanceID <= 0 || len(hashes) == 0 {
		return infohash.Hash{}, false, nil
	}

	placeholders := buildPlaceholders(len(hashes))
	query := fmt.Sprintf(`
		SELECT infohash
		FROM cross_seed_blocklist
		WHERE instance_id = ? AND infohash IN (%s)
		LIMIT 1
	`, placeholders)

	args := make([]any, 0, len(hashes)+1)
	args = append(args, instanceID)
	for _, h := range hashes {
		args = append(args, h.String())
	}

	var stored string
	if err := s.db.QueryRowContext(ctx, query, args...).Scan(&stored); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return infohash.Hash{}, false, nil
		}
		return infohash.Hash{}, false, err
	}

	blocked, err := infohash.Parse(stored)
	if err != nil {
		return infohash.Hash{}, false, fmt.Errorf("blocklist row %q: %w", stored, err)
	}
	return blocked, true, nil
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
