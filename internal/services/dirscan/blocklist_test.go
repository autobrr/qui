// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTryMatchAndInject_SkipsBlocklistedInfohash(t *testing.T) {
	t.Parallel()

	// A hybrid torrent carries both hashes, so one fixture covers a v1 and a v2 entry.
	tests := []struct {
		name      string
		blockHash func(*ParsedTorrent) string
	}{
		{name: "not blocked"},
		{name: "v1 blocked", blockHash: func(p *ParsedTorrent) string { return p.InfoHash }},
		{name: "v2 blocked", blockHash: func(p *ParsedTorrent) string { return p.InfoHashV2 }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			db := testdb.NewMigratedSQLite(t, "dirscan-blocklist")

			instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			localFS := true
			instance, err := instanceStore.Create(ctx, "Test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, &localFS)
			require.NoError(t, err)

			indexerStore, err := models.NewTorznabIndexerStore(db, []byte("01234567890123456789012345678901"))
			require.NoError(t, err)
			indexer, err := indexerStore.Create(ctx, "Indexer", "http://indexer.example.invalid", "api-key", nil, nil, true, 0, 30)
			require.NoError(t, err)

			const name = "Example.Movie.2024.1080p.WEB-DL.x264-GRP.mkv"
			torrentBytes := buildTorrentBytes(t, &metainfo.Info{
				Name:        name,
				PieceLength: 16384,
				Length:      4,
				Pieces:      []byte(strings.Repeat("p", 20)),
				MetaVersion: 2,
				FileTree: metainfo.FileTree{Dir: map[string]metainfo.FileTree{
					name: {File: metainfo.FileTreeFile{Length: 4, PiecesRoot: strings.Repeat("r", 32)}},
				}},
			})
			parsed, err := ParseTorrentBytes(torrentBytes)
			require.NoError(t, err)
			require.NotEmpty(t, parsed.InfoHashV2)

			// A cache hit keeps the download off the network; the nil indexer store
			// makes a miss panic instead of calling a real host.
			result := &jackett.SearchResult{Title: name, IndexerID: indexer.ID, DownloadURL: "http://indexer.example.invalid/dl", GUID: "guid"}
			cache := models.NewTorznabTorrentCacheStore(db)
			require.NoError(t, cache.Store(ctx, &models.TorznabTorrentCacheEntry{
				IndexerID: indexer.ID, CacheKey: result.GUID, TorrentData: torrentBytes,
			}))

			blocklist := models.NewCrossSeedBlocklistStore(db)
			blocked := tt.blockHash != nil
			if blocked {
				_, err = blocklist.Upsert(ctx, &models.CrossSeedBlocklistEntry{InstanceID: instance.ID, InfoHash: tt.blockHash(parsed)})
				require.NoError(t, err)
			}

			sourceDir := t.TempDir()
			sourceFile := filepath.Join(sourceDir, name)
			require.NoError(t, os.WriteFile(sourceFile, []byte("data"), 0o600))
			searchee := &Searchee{Name: name, Path: sourceFile, Files: []*ScannedFile{{Path: sourceFile, RelPath: name, Size: 4}}}

			adder := &failingTorrentAdder{err: errors.New("add stopped by test")}
			svc := &Service{
				store:          models.NewDirScanStore(db),
				blocklistStore: blocklist,
				jackettService: jackett.NewService(nil, jackett.WithTorrentCache(cache)),
				injector:       NewInjector(nil, adder, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance)),
			}

			l := zerolog.New(io.Discard)
			dir := &models.DirScanDirectory{ID: 1, TargetInstanceID: instance.ID}
			settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict}
			match := svc.tryMatchAndInject(ctx, dir, searchee, nil, result, "movie", settings, NewMatcher(MatchModeStrict, 0), 0, &l)

			require.Equal(t, !blocked, adder.called, "AddTorrent must run only for an infohash that is not blocklisted")
			if blocked {
				require.Nil(t, match)
			}
		})
	}
}
