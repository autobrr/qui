// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

type ExternalIDs struct {
	IMDbID   string `json:"imdb_id,omitempty"`
	TMDbID   int    `json:"tmdb_id,omitempty"`
	TVDbID   int    `json:"tvdb_id,omitempty"`
	TVMazeID int    `json:"tvmaze_id,omitempty"`
}

func (e *ExternalIDs) IsEmpty() bool {
	return e.IMDbID == "" && e.TMDbID == 0 && e.TVDbID == 0 && e.TVMazeID == 0
}

type EpisodeMap struct {
	Season   int `json:"season"`
	Episode  int `json:"episode"`
	Absolute int `json:"absolute"`
}

type ArrIDCacheEntry struct {
	ID            int64       `json:"id"`
	TitleHash     string      `json:"title_hash"`
	ContentType   string      `json:"content_type"`
	ArrInstanceID *int        `json:"arr_instance_id,omitempty"`
	ExternalIDs   ExternalIDs `json:"external_ids"`
	Titles        []string    `json:"titles,omitempty"`
	HasTitles     bool        `json:"has_titles"`
	EpisodeMap    *EpisodeMap `json:"episode_map,omitempty"`
	HasEpisodeMap bool        `json:"has_episode_map"`
	IsNegative    bool        `json:"is_negative"`
	CachedAt      time.Time   `json:"cached_at"`
	ExpiresAt     time.Time   `json:"expires_at"`
}

type ArrIDCacheStore struct {
	db dbinterface.Querier
}

func NewArrIDCacheStore(db dbinterface.Querier) *ArrIDCacheStore {
	return &ArrIDCacheStore{db: db}
}

func ComputeTitleHash(title string) string {
	normalized := normalizeLowerTrim(title)
	hash := sha256.Sum256([]byte(normalized))
	return hex.EncodeToString(hash[:])
}

func (s *ArrIDCacheStore) Get(ctx context.Context, titleHash, contentType string) (*ArrIDCacheEntry, error) {
	query := `
		SELECT id, title_hash, content_type, arr_instance_id, imdb_id, tmdb_id, tvdb_id, tvmaze_id, titles_json,
			episode_map_season, episode_map_episode, episode_map_absolute, episode_map_known, is_negative, cached_at, expires_at
		FROM arr_id_cache
		WHERE title_hash = ? AND content_type = ? AND expires_at > ?
	`

	var entry ArrIDCacheEntry
	var imdbID, titlesJSON *string
	var tmdbID, tvdbID, tvmazeID, mapSeason, mapEpisode, mapAbsolute *int
	var mapKnown, isNegative int

	err := s.db.QueryRowContext(ctx, query, titleHash, contentType, time.Now().UTC()).Scan(
		&entry.ID,
		&entry.TitleHash,
		&entry.ContentType,
		&entry.ArrInstanceID,
		&imdbID,
		&tmdbID,
		&tvdbID,
		&tvmazeID,
		&titlesJSON,
		&mapSeason,
		&mapEpisode,
		&mapAbsolute,
		&mapKnown,
		&isNegative,
		&entry.CachedAt,
		&entry.ExpiresAt,
	)
	if err != nil {
		return nil, err
	}

	if imdbID != nil {
		entry.ExternalIDs.IMDbID = *imdbID
	}
	if tmdbID != nil {
		entry.ExternalIDs.TMDbID = *tmdbID
	}
	if tvdbID != nil {
		entry.ExternalIDs.TVDbID = *tvdbID
	}
	if tvmazeID != nil {
		entry.ExternalIDs.TVMazeID = *tvmazeID
	}
	if titlesJSON != nil {
		entry.HasTitles = true
		if err := json.Unmarshal([]byte(*titlesJSON), &entry.Titles); err != nil {
			return nil, fmt.Errorf("failed to decode arr id cache titles: %w", err)
		}
	}
	entry.HasEpisodeMap = SQLiteIntToBool(mapKnown)
	if mapSeason != nil && mapEpisode != nil && mapAbsolute != nil {
		entry.EpisodeMap = &EpisodeMap{Season: *mapSeason, Episode: *mapEpisode, Absolute: *mapAbsolute}
	}
	entry.IsNegative = SQLiteIntToBool(isNegative)

	return &entry, nil
}

func (s *ArrIDCacheStore) Set(ctx context.Context, titleHash, contentType string, arrInstanceID *int, ids *ExternalIDs, isNegative bool, ttl time.Duration) error {
	return s.SetWithTitles(ctx, titleHash, contentType, arrInstanceID, ids, nil, nil, false, isNegative, ttl)
}

func (s *ArrIDCacheStore) SetWithTitles(ctx context.Context, titleHash, contentType string, arrInstanceID *int, ids *ExternalIDs, titles []string, episodeMap *EpisodeMap, episodeMapKnown, isNegative bool, ttl time.Duration) error {
	now := time.Now().UTC()
	expiresAt := now.Add(ttl)

	var imdbID, titlesJSON *string
	var tmdbID, tvdbID, tvmazeID, mapSeason, mapEpisode, mapAbsolute *int

	if episodeMap != nil {
		mapSeason, mapEpisode, mapAbsolute = &episodeMap.Season, &episodeMap.Episode, &episodeMap.Absolute
	}
	if ids != nil {
		if ids.IMDbID != "" {
			imdbID = &ids.IMDbID
		}
		if ids.TMDbID > 0 {
			tmdbID = &ids.TMDbID
		}
		if ids.TVDbID > 0 {
			tvdbID = &ids.TVDbID
		}
		if ids.TVMazeID > 0 {
			tvmazeID = &ids.TVMazeID
		}
	}
	if titles != nil {
		encodedTitles, err := json.Marshal(titles)
		if err != nil {
			return fmt.Errorf("failed to encode arr id cache titles: %w", err)
		}
		titlesValue := string(encodedTitles)
		titlesJSON = &titlesValue
	}

	query := `
		INSERT INTO arr_id_cache (title_hash, content_type, arr_instance_id, imdb_id, tmdb_id, tvdb_id, tvmaze_id, titles_json,
			episode_map_season, episode_map_episode, episode_map_absolute, episode_map_known, is_negative, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(title_hash, content_type) DO UPDATE SET
			arr_instance_id = excluded.arr_instance_id,
			imdb_id = excluded.imdb_id,
			tmdb_id = excluded.tmdb_id,
			tvdb_id = excluded.tvdb_id,
			tvmaze_id = excluded.tvmaze_id,
			titles_json = excluded.titles_json,
			episode_map_season = excluded.episode_map_season,
			episode_map_episode = excluded.episode_map_episode,
			episode_map_absolute = excluded.episode_map_absolute,
			episode_map_known = excluded.episode_map_known,
			is_negative = excluded.is_negative,
			cached_at = CURRENT_TIMESTAMP,
			expires_at = excluded.expires_at
		WHERE excluded.is_negative = 0
			OR arr_id_cache.is_negative = 1
			OR arr_id_cache.expires_at <= ?
	`

	_, err := s.db.ExecContext(ctx, query, titleHash, contentType, arrInstanceID, imdbID, tmdbID, tvdbID, tvmazeID, titlesJSON,
		mapSeason, mapEpisode, mapAbsolute, BoolToSQLite(episodeMapKnown), BoolToSQLite(isNegative), expiresAt, now)
	if err != nil {
		return fmt.Errorf("failed to set arr id cache entry: %w", err)
	}

	return nil
}

func (s *ArrIDCacheStore) CleanupExpired(ctx context.Context) (int64, error) {
	query := `DELETE FROM arr_id_cache WHERE expires_at <= ?`

	result, err := s.db.ExecContext(ctx, query, time.Now().UTC())
	if err != nil {
		return 0, fmt.Errorf("failed to cleanup expired arr id cache entries: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("failed to get rows affected: %w", err)
	}

	return rowsAffected, nil
}
