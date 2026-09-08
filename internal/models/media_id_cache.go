// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"errors"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

// MediaIDCacheEntry is one cached MediaInfo external-ID extraction for a
// torrent. An entry with an empty IDType records a successful scan that found
// no usable ID, so later searches skip the file read.
type MediaIDCacheEntry struct {
	TorrentKey       string    `json:"torrent_key"`
	ContentType      string    `json:"content_type"`
	ExtractorVersion int       `json:"extractor_version"`
	IDType           string    `json:"id_type,omitempty"`
	IDValue          string    `json:"id_value,omitempty"`
	CachedAt         time.Time `json:"cached_at"`
}

// MediaIDCacheStore manages the media_id_cache table.
type MediaIDCacheStore struct {
	db dbinterface.Querier
}

// NewMediaIDCacheStore creates a new MediaIDCacheStore.
func NewMediaIDCacheStore(db dbinterface.Querier) *MediaIDCacheStore {
	return &MediaIDCacheStore{db: db}
}

// Get returns the cached entry for a torrent key and content type, or nil when
// no entry exists.
func (s *MediaIDCacheStore) Get(ctx context.Context, torrentKey, contentType string) (*MediaIDCacheEntry, error) {
	query := `
		SELECT torrent_key, content_type, extractor_version, id_type, id_value, cached_at
		FROM media_id_cache
		WHERE torrent_key = ? AND content_type = ?
	`

	var entry MediaIDCacheEntry
	var idType, idValue *string
	err := s.db.QueryRowContext(ctx, query, torrentKey, contentType).Scan(
		&entry.TorrentKey,
		&entry.ContentType,
		&entry.ExtractorVersion,
		&idType,
		&idValue,
		&entry.CachedAt,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	if idType != nil {
		entry.IDType = *idType
	}
	if idValue != nil {
		entry.IDValue = *idValue
	}
	return &entry, nil
}

// Set upserts a cache entry. An empty IDType stores a no-ID result (both ID
// columns NULL, enforced by a table constraint).
func (s *MediaIDCacheStore) Set(ctx context.Context, entry *MediaIDCacheEntry) error {
	var idType, idValue *string
	if entry.IDType != "" {
		idType = &entry.IDType
		idValue = &entry.IDValue
	}

	query := `
		INSERT INTO media_id_cache (torrent_key, content_type, extractor_version, id_type, id_value, cached_at)
		VALUES (?, ?, ?, ?, ?, ?)
		ON CONFLICT(torrent_key, content_type) DO UPDATE SET
			extractor_version = excluded.extractor_version,
			id_type = excluded.id_type,
			id_value = excluded.id_value,
			cached_at = excluded.cached_at
	`

	_, err := s.db.ExecContext(ctx, query, entry.TorrentKey, entry.ContentType, entry.ExtractorVersion, idType, idValue, time.Now().UTC())
	return err
}
