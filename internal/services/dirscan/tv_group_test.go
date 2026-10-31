// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// episodeSearcher answers a season search with the season result and an
// episode search with the result for that episode. It records each search and
// counts the answers with a result, since each one leads to a torrent download.
type episodeSearcher struct {
	indexerID int
	season    []jackett.SearchResult
	episodes  map[int][]jackett.SearchResult
	searched  []string
	answered  int
}

func (s *episodeSearcher) SearchWithScope(_ context.Context, req *jackett.TorznabSearchRequest, _ string) error {
	results := s.season
	label := "season"
	if req.Episode != nil {
		results = s.episodes[*req.Episode]
		label = fmt.Sprintf("E%02d", *req.Episode)
	}
	s.searched = append(s.searched, label)
	if len(results) > 0 {
		s.answered++
	}
	req.OnAllComplete(&jackett.SearchResponse{
		Results:             results,
		RequestedIndexerIDs: []int{s.indexerID},
		CoveredIndexerIDs:   []int{s.indexerID},
	}, nil)
	return nil
}

// An injected single episode does not cover the other episodes of its season,
// so the run still searches them. An injected season pack does. An episode
// that waits for an offline linked instance stays pending after a season
// search with no result.
//
// regression: #3571
func TestProcessRootSearchee_TVGroupSkip(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		seasonPack   bool
		offlineLink  bool
		wantSearched []string
		wantAdded    int
		wantStatus   models.DirScanFileStatus
	}{
		{name: "episode injects", wantSearched: []string{"season", "E01", "E02", "E03"}, wantAdded: 3, wantStatus: models.DirScanFileStatusMatched},
		{name: "season pack injects", seasonPack: true, wantSearched: []string{"season"}, wantAdded: 1, wantStatus: models.DirScanFileStatusMatched},
		{name: "offline linked instance keeps episodes pending", offlineLink: true, wantSearched: []string{"season", "E01", "E02", "E03"}, wantStatus: models.DirScanFileStatusPending},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := t.Context()
			db := testdb.NewMigratedSQLite(t, "dirscan-tv-group")
			key := []byte("01234567890123456789012345678901")

			instanceStore, err := models.NewInstanceStore(db, key)
			require.NoError(t, err)
			instance, err := instanceStore.Create(ctx, "Test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, new(true))
			require.NoError(t, err)

			indexerStore, err := models.NewTorznabIndexerStore(db, key)
			require.NoError(t, err)
			indexer, err := indexerStore.Create(ctx, "Indexer", "http://indexer.example.invalid", "api-key", nil, nil, true, 0, 30)
			require.NoError(t, err)

			store := models.NewDirScanStore(db)
			dir, err := store.CreateDirectory(ctx, &models.DirScanDirectory{
				Path:                t.TempDir(),
				Enabled:             true,
				TargetInstanceID:    instance.ID,
				ScanIntervalMinutes: 60,
			})
			require.NoError(t, err)

			const folder = "Example.Show.S01.1080p.WEB-DL.x264-GRP"
			root := &Searchee{Name: folder, Path: filepath.Join(dir.Path, folder)}
			for ep := 1; ep <= 3; ep++ {
				name := fmt.Sprintf("Example.Show.S01E%02d.1080p.WEB-DL.x264-GRP.mkv", ep)
				root.Files = append(root.Files, &ScannedFile{
					Path:    filepath.Join(root.Path, name),
					RelPath: name,
					Size:    int64(1000 + ep),
					ModTime: time.Now(),
				})
			}

			// Each result is a cache hit, so no download leaves the test.
			cache := models.NewTorznabTorrentCacheStore(db)
			searcher := &episodeSearcher{indexerID: indexer.ID, episodes: map[int][]jackett.SearchResult{}}
			addResult := func(guid string, info *metainfo.Info) jackett.SearchResult {
				torrentBytes := buildTorrentBytes(t, info)
				require.NoError(t, cache.Store(ctx, &models.TorznabTorrentCacheEntry{
					IndexerID: indexer.ID, CacheKey: guid, TorrentData: torrentBytes,
				}))
				return jackett.SearchResult{
					Title:       info.Name,
					IndexerID:   indexer.ID,
					DownloadURL: "http://indexer.example.invalid/dl/" + guid,
					GUID:        guid,
					Size:        info.TotalLength(),
				}
			}
			for i, f := range root.Files {
				searcher.episodes[i+1] = []jackett.SearchResult{addResult(fmt.Sprintf("ep-%d", i+1), &metainfo.Info{
					Name:        f.RelPath,
					PieceLength: 16384,
					Length:      f.Size,
					Pieces:      []byte(strings.Repeat("p", 20)),
				})}
			}
			if tt.seasonPack {
				pack := &metainfo.Info{Name: folder, PieceLength: 16384, Pieces: []byte(strings.Repeat("p", 20))}
				for _, f := range root.Files {
					pack.Files = append(pack.Files, metainfo.FileInfo{Length: f.Size, Path: []string{f.RelPath}})
				}
				searcher.season = []jackett.SearchResult{addResult("pack", pack)}
			}

			manager := &recordingTorrentManager{}
			adder := testAdder(manager, (&settleFake{}).idle(), nil)
			if tt.offlineLink {
				adder.SetCrossSeedRoutes(dirScanRoutes(t, db, dirScanLink{state: "offline"}))
			}
			cacheDB := &cacheTouchDB{DB: db, done: make(chan struct{}, len(root.Files)+1)}
			parser := NewParser(nil)
			svc := &Service{
				store:  store,
				parser: parser,
				adder:  adder,
				jackettService: jackett.NewService(indexerStore,
					jackett.WithTorrentCache(models.NewTorznabTorrentCacheStore(cacheDB))),
				searcher:    NewSearcher(searcher, parser),
				searchGate:  searchGate{interval: time.Nanosecond},
				runProgress: map[int64]*runProgress{},
				injector: NewInjector(nil, crossseed.NewAligner(manager), adder, nil,
					&fakeInstanceStore{instance: instance}, nil, testBackendPool(instance)),
			}

			l := zerolog.New(io.Discard)
			scanResult := &ScanResult{Searchees: []*Searchee{root}}
			trackedFiles, err := svc.refreshTrackedFilesFromScan(ctx, dir.ID, scanResult, &seedingIndex{}, &l)
			require.NoError(t, err)
			enabled := map[int]struct{}{indexer.ID: {}}
			work := selectEligibleRootWork(scanResult, trackedFiles, parser, 0, time.Now(), enabled, false, &l)
			require.Len(t, work.roots, 1)

			settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict, StartPaused: true}
			_, added := svc.processRootSearchee(ctx, dir, root, work.roots[0].items, &seedingIndex{}, trackedFiles,
				map[tvGroupKey]struct{}{}, settings, NewMatcher(MatchModeStrict, 0), 1, 0, 0, enabled, &l)

			// Each download is a cache hit that starts a detached touch; join them
			// before SQLite and TempDir cleanup.
			for range searcher.answered {
				select {
				case <-cacheDB.done:
				case <-time.After(10 * time.Second):
					t.Fatal("torrent cache touch did not finish")
				}
			}

			require.Equal(t, tt.wantSearched, searcher.searched)
			require.Equal(t, tt.wantAdded, added)

			files, err := store.ListFiles(ctx, dir.ID, nil, 10, 0)
			require.NoError(t, err)
			require.Len(t, files, len(root.Files))
			for _, f := range files {
				require.Equal(t, tt.wantStatus, f.Status, f.FilePath)
			}
		})
	}
}
