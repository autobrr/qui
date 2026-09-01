// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	mediainfo "github.com/autobrr/go-mediainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
)

func generalReport(fields ...mediainfo.Field) mediainfo.Report {
	return mediainfo.Report{General: mediainfo.Stream{Kind: mediainfo.StreamGeneral, Fields: fields}}
}

func TestExtractMediaExternalIDs(t *testing.T) {
	tests := []struct {
		name        string
		contentType string
		fields      []mediainfo.Field
		wantType    string
		wantValue   string
	}{
		{
			name:        "movie prefers imdb",
			contentType: "movie",
			fields: []mediainfo.Field{
				{Name: "IMDB", Value: "tt0111161"},
				{Name: "TMDB", Value: "movie/278"},
			},
			wantType:  "imdb",
			wantValue: "tt0111161",
		},
		{
			name:        "movie falls back to namespaced tmdb",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "TMDB", Value: "movie/278"}},
			wantType:    "tmdb",
			wantValue:   "278",
		},
		{
			name:        "movie rejects tv-namespaced tmdb",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "TMDB", Value: "tv/1399"}},
		},
		{
			name:        "movie rejects collection tmdb",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "TMDB", Value: "collection/10"}},
		},
		{
			name:        "bare tmdb number is ambiguous and rejected",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "TMDB", Value: "278"}},
		},
		{
			name:        "movie accepts tvdb movies namespace",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "TVDB2", Value: "movies/19799"}},
			wantType:    "tvdb",
			wantValue:   "19799",
		},
		{
			name:        "movie rejects bare tvdb series id",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "TVDB", Value: "431162"}},
		},
		{
			name:        "tv prefers tvdb over imdb",
			contentType: "tv",
			fields: []mediainfo.Field{
				{Name: "IMDB", Value: "tt0903747"},
				{Name: "TVDB", Value: "81189"},
			},
			wantType:  "tvdb",
			wantValue: "81189",
		},
		{
			name:        "tv accepts series-namespaced tvdb2",
			contentType: "tv",
			fields:      []mediainfo.Field{{Name: "TVDB2", Value: "series/431162"}},
			wantType:    "tvdb",
			wantValue:   "431162",
		},
		{
			name:        "conflicting tvdb tags reject the namespace and fall through",
			contentType: "tv",
			fields: []mediainfo.Field{
				{Name: "TVDB", Value: "81189"},
				{Name: "TVDB2", Value: "series/431162"},
				{Name: "IMDB", Value: "tt0903747"},
			},
			wantType:  "imdb",
			wantValue: "tt0903747",
		},
		{
			name:        "agreeing tvdb and tvdb2 tags keep the namespace",
			contentType: "tv",
			fields: []mediainfo.Field{
				{Name: "TVDB", Value: "81189"},
				{Name: "TVDB2", Value: "series/81189"},
			},
			wantType:  "tvdb",
			wantValue: "81189",
		},
		{
			name:        "anime uses the tv ordering",
			contentType: "anime",
			fields: []mediainfo.Field{
				{Name: "IMDB", Value: "tt2560140"},
				{Name: "TVDB", Value: "267440"},
			},
			wantType:  "tvdb",
			wantValue: "267440",
		},
		{
			name:        "malformed imdb rejected",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "IMDB", Value: "0111161"}},
		},
		{
			name:        "values are trimmed and lowercased",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "IMDB", Value: "  TT0111161  "}},
			wantType:    "imdb",
			wantValue:   "tt0111161",
		},
		{
			name:        "no tags yields nothing",
			contentType: "movie",
			fields:      []mediainfo.Field{{Name: "ENCODER", Value: "libebml"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			idType, idValue := extractMediaExternalIDs(generalReport(tt.fields...), tt.contentType)
			require.Equal(t, tt.wantType, idType)
			require.Equal(t, tt.wantValue, idValue)
		})
	}
}

func TestLargestMKVPath(t *testing.T) {
	dir := t.TempDir()
	writeFile := func(name string, size int) {
		full := filepath.Join(dir, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(full), 0o750))
		require.NoError(t, os.WriteFile(full, make([]byte, size), 0o600))
	}
	writeFile("Show/episode1.mkv", 10)
	writeFile("Show/episode2.mkv", 30)
	writeFile("Show/sample.mp4", 100)
	// A deselected file can exist on disk as a stub at the declared size;
	// its Progress < 1 must exclude it even though it is the largest mkv.
	writeFile("Show/stub.mkv", 500)

	files := qbt.TorrentFiles{
		{Name: "Show/episode1.mkv", Size: 10, Progress: 1},
		{Name: "Show/episode2.mkv", Size: 30, Progress: 1},
		{Name: "Show/sample.mp4", Size: 100, Progress: 1},
		{Name: "Show/missing.mkv", Size: 999, Progress: 1},
		{Name: "Show/stub.mkv", Size: 500, Progress: 0.4},
	}

	got := largestMKVPath(context.Background(), local.NewBackend(), dir, files)
	require.Equal(t, filepath.Join(dir, "Show", "episode2.mkv"), got)
}

type fakeMediaIDCache struct {
	entries map[string]*models.MediaIDCacheEntry
	getErr  error
	setErr  error
	sets    int
}

func newFakeMediaIDCache() *fakeMediaIDCache {
	return &fakeMediaIDCache{entries: make(map[string]*models.MediaIDCacheEntry)}
}

func (f *fakeMediaIDCache) Get(_ context.Context, torrentKey, contentType string) (*models.MediaIDCacheEntry, error) {
	if f.getErr != nil {
		return nil, f.getErr
	}
	return f.entries[torrentKey+"|"+contentType], nil
}

func (f *fakeMediaIDCache) Set(_ context.Context, entry *models.MediaIDCacheEntry) error {
	f.sets++
	if f.setErr != nil {
		return f.setErr
	}
	f.entries[entry.TorrentKey+"|"+entry.ContentType] = entry
	return nil
}

// mediaIDTestFixture builds a service whose analyzer returns the given report
// (or error) and a complete single-MKV torrent rooted in a temp dir.
func mediaIDTestFixture(t *testing.T, report mediainfo.Report, analyzeErr error) (*Service, *fakeMediaIDCache, *models.Instance, *qbt.Torrent, qbt.TorrentFiles, *int) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "movie.mkv"), make([]byte, 10), 0o600))

	cache := newFakeMediaIDCache()
	analyzeCalls := 0
	svc := &Service{
		mediaIDCacheStore: cache,
		analyzeMediaFile: func(context.Context, string) (mediainfo.Report, error) {
			analyzeCalls++
			return report, analyzeErr
		},
	}
	instance := &models.Instance{ID: 1, HasLocalFilesystemAccess: true}
	// The MKV path is resolved through the instance's filesystem backend.
	svc.SetBackendPool(fsops.NewPool(
		&discPolicyInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		local.NewBackend(),
	))
	torrent := &qbt.Torrent{Name: "Movie.2024.1080p.WEB-DL-GROUP", Hash: "ABC123", SavePath: dir}
	files := qbt.TorrentFiles{{Name: "movie.mkv", Size: 10, Progress: 1}}
	return svc, cache, instance, torrent, files, &analyzeCalls
}

func TestLookupMediaFileIDs(t *testing.T) {
	ctx := context.Background()
	imdbReport := generalReport(mediainfo.Field{Name: "IMDB", Value: "tt0111161"})

	t.Run("nil store disables the fallback", func(t *testing.T) {
		svc := &Service{}
		require.Nil(t, svc.lookupMediaFileIDs(ctx, &models.Instance{HasLocalFilesystemAccess: true}, &qbt.Torrent{Hash: "abc"}, nil, "movie"))
	})

	t.Run("unsupported content type is skipped", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		require.Nil(t, svc.lookupMediaFileIDs(ctx, instance, torrent, files, "music"))
		require.Zero(t, *calls)
		require.Zero(t, cache.sets)
	})

	t.Run("successful scan returns and caches the ID", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		ids := svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie")
		require.NotNil(t, ids)
		require.Equal(t, "tt0111161", ids.IMDbID)
		require.Equal(t, 1, *calls)
		entry := cache.entries["abc123|movie"]
		require.NotNil(t, entry)
		require.Equal(t, "imdb", entry.IDType)
		require.Equal(t, "tt0111161", entry.IDValue)
		require.Equal(t, mediaIDExtractorVersion, entry.ExtractorVersion)
	})

	t.Run("positive cache hit skips the analyzer", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		cache.entries["abc123|movie"] = &models.MediaIDCacheEntry{
			TorrentKey: "abc123", ContentType: "movie",
			ExtractorVersion: mediaIDExtractorVersion,
			IDType:           "tvdb", IDValue: "19799",
		}
		ids := svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie")
		require.NotNil(t, ids)
		require.Equal(t, 19799, ids.TVDbID)
		require.Zero(t, *calls)
	})

	t.Run("cache read error falls through to analysis", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		cache.getErr = errors.New("db closed")
		ids := svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie")
		require.NotNil(t, ids)
		require.Equal(t, "tt0111161", ids.IMDbID)
		require.Equal(t, 1, *calls)
	})

	t.Run("negative cache hit returns nil without file access", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		cache.entries["abc123|movie"] = &models.MediaIDCacheEntry{
			TorrentKey: "abc123", ContentType: "movie",
			ExtractorVersion: mediaIDExtractorVersion,
		}
		require.Nil(t, svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie"))
		require.Zero(t, *calls)
		require.Zero(t, cache.sets)
	})

	t.Run("extractor version mismatch re-analyzes and replaces the row", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		cache.entries["abc123|movie"] = &models.MediaIDCacheEntry{
			TorrentKey: "abc123", ContentType: "movie",
			ExtractorVersion: mediaIDExtractorVersion - 1,
		}
		ids := svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie")
		require.NotNil(t, ids)
		require.Equal(t, 1, *calls)
		require.Equal(t, mediaIDExtractorVersion, cache.entries["abc123|movie"].ExtractorVersion)
	})

	t.Run("no local filesystem access skips analysis and caching", func(t *testing.T) {
		svc, cache, _, torrent, files, calls := mediaIDTestFixture(t, imdbReport, nil)
		require.Nil(t, svc.lookupMediaFileIDs(ctx, &models.Instance{}, torrent, files, "movie"))
		require.Zero(t, *calls)
		require.Zero(t, cache.sets)
	})

	t.Run("analyzer error is not cached", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, mediainfo.Report{}, errors.New("short read"))
		require.Nil(t, svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie"))
		require.Equal(t, 1, *calls)
		require.Zero(t, cache.sets)
	})

	t.Run("scan without usable tags caches a negative row", func(t *testing.T) {
		svc, cache, instance, torrent, files, calls := mediaIDTestFixture(t, generalReport(), nil)
		require.Nil(t, svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie"))
		require.Equal(t, 1, *calls)
		entry := cache.entries["abc123|movie"]
		require.NotNil(t, entry)
		require.Empty(t, entry.IDType)
		require.Empty(t, entry.IDValue)
	})

	t.Run("torrent without an mkv is skipped without caching", func(t *testing.T) {
		svc, cache, instance, torrent, _, calls := mediaIDTestFixture(t, imdbReport, nil)
		files := qbt.TorrentFiles{{Name: "movie.mp4", Size: 10}}
		require.Nil(t, svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie"))
		require.Zero(t, *calls)
		require.Zero(t, cache.sets)
	})

	t.Run("cache write failure still returns the extracted ID", func(t *testing.T) {
		svc, cache, instance, torrent, files, _ := mediaIDTestFixture(t, imdbReport, nil)
		cache.setErr = errors.New("db closed")
		ids := svc.lookupMediaFileIDs(ctx, instance, torrent, files, "movie")
		require.NotNil(t, ids)
		require.Equal(t, "tt0111161", ids.IMDbID)
	})
}

func TestMediaIDsToExternalIDs(t *testing.T) {
	require.Equal(t, &models.ExternalIDs{IMDbID: "tt1"}, mediaIDsToExternalIDs("imdb", "tt1"))
	require.Equal(t, &models.ExternalIDs{TMDbID: 278}, mediaIDsToExternalIDs("tmdb", "278"))
	require.Equal(t, &models.ExternalIDs{TVDbID: 81189}, mediaIDsToExternalIDs("tvdb", "81189"))
	require.Nil(t, mediaIDsToExternalIDs("tvdb", "not-a-number"))
	require.Nil(t, mediaIDsToExternalIDs("", ""))
}
