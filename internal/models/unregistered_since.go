// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

const (
	unregisteredSincePutChunk    = 300
	unregisteredSinceDeleteChunk = 500
)

type UnregisteredSinceStore struct {
	db dbinterface.Querier
}

func NewUnregisteredSinceStore(db dbinterface.Querier) *UnregisteredSinceStore {
	return &UnregisteredSinceStore{db: db}
}

func (s *UnregisteredSinceStore) ListUnregisteredSince(ctx context.Context, instanceID int) (map[string]time.Time, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT torrent_hash, since FROM unregistered_since WHERE instance_id = ?`, instanceID)
	if err != nil {
		return nil, fmt.Errorf("list unregistered since: %w", err)
	}
	defer rows.Close()

	since := make(map[string]time.Time)
	for rows.Next() {
		var hash string
		var at time.Time
		if err := rows.Scan(&hash, &at); err != nil {
			return nil, fmt.Errorf("scan unregistered since: %w", err)
		}
		since[hash] = at.UTC()
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list unregistered since: %w", err)
	}
	return since, nil
}

func (s *UnregisteredSinceStore) PutUnregisteredSince(ctx context.Context, instanceID int, since map[string]time.Time) error {
	hashes := slices.Sorted(maps.Keys(since))
	return s.inTx(ctx, func(tx dbinterface.TxQuerier) error {
		for chunk := range slices.Chunk(hashes, unregisteredSincePutChunk) {
			query := dbinterface.BuildQueryWithPlaceholders(`
				INSERT INTO unregistered_since (instance_id, torrent_hash, since)
				VALUES %s
				ON CONFLICT(instance_id, torrent_hash) DO UPDATE SET since = excluded.since
			`, 3, len(chunk))
			args := make([]any, 0, 3*len(chunk))
			for _, hash := range chunk {
				args = append(args, instanceID, hash, since[hash].UTC())
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("store unregistered since: %w", err)
			}
		}
		return nil
	})
}

func (s *UnregisteredSinceStore) DeleteUnregisteredSince(ctx context.Context, instanceID int, hashes []string) error {
	return s.inTx(ctx, func(tx dbinterface.TxQuerier) error {
		for chunk := range slices.Chunk(hashes, unregisteredSinceDeleteChunk) {
			query := `DELETE FROM unregistered_since WHERE instance_id = ? AND torrent_hash IN (` +
				strings.TrimSuffix(strings.Repeat("?,", len(chunk)), ",") + `)`
			args := make([]any, 0, 1+len(chunk))
			args = append(args, instanceID)
			for _, hash := range chunk {
				args = append(args, hash)
			}
			if _, err := tx.ExecContext(ctx, query, args...); err != nil {
				return fmt.Errorf("remove unregistered since: %w", err)
			}
		}
		return nil
	})
}

func (s *UnregisteredSinceStore) inTx(ctx context.Context, write func(tx dbinterface.TxQuerier) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()
	if err := write(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
