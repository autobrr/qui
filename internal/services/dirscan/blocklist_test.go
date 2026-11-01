// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTryMatchAndInject_PreAddCheck(t *testing.T) {
	t.Parallel()

	// A hybrid torrent carries both hashes, so one fixture covers a v1 and a v2 entry.
	tests := []struct {
		name         string
		blockHash    func(*ParsedTorrent) string
		blocklistErr bool
		existing     bool
		// link stores a route between the target, instance 1, and instance 2,
		// and puts the existing torrent on instance 2 only.
		link *dirScanLink
		// skipped is set when the Pre-add check refuses the add without a match.
		skipped bool
		// retry is set when the searchee must stay pending for the next scan.
		retry bool
	}{
		{name: "not blocked"},
		{name: "v1 blocked", blockHash: func(p *ParsedTorrent) string { return p.InfoHash }, skipped: true},
		{name: "v2 blocked", blockHash: func(p *ParsedTorrent) string { return p.InfoHashV2 }, skipped: true},
		{name: "blocklist read fails", blocklistErr: true, skipped: true},
		{name: "already in qBittorrent", existing: true},
		{name: "hash on the target's Cross-seed instance", existing: true, link: &dirScanLink{}},
		{name: "unavailable Cross-seed instance skips the match", link: &dirScanLink{state: "offline"}, skipped: true, retry: true},
		{name: "hash on a Source instance of the target", existing: true, link: &dirScanLink{fromSource: true}},
		{name: "disabled Source instance is skipped", existing: true, link: &dirScanLink{fromSource: true, state: "disabled"}},
		{name: "offline Source instance skips the match", link: &dirScanLink{fromSource: true, state: "offline"}, skipped: true, retry: true},
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

			// No content type, so the indexer filter needs no capability lookup.
			const name = "example-data-archive.bin"
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
			result := &jackett.SearchResult{Title: name, IndexerID: indexer.ID, DownloadURL: "http://indexer.example.invalid/dl", GUID: "guid", Size: 4}
			cache := models.NewTorznabTorrentCacheStore(db)
			require.NoError(t, cache.Store(ctx, &models.TorznabTorrentCacheEntry{
				IndexerID: indexer.ID, CacheKey: result.GUID, TorrentData: torrentBytes,
			}))

			blocklist := models.NewCrossSeedBlocklistStore(db)
			if tt.blockHash != nil {
				_, err = blocklist.Upsert(ctx, &models.CrossSeedBlocklistEntry{InstanceID: instance.ID, InfoHash: tt.blockHash(parsed)})
				require.NoError(t, err)
			}
			if tt.blocklistErr {
				closed := testdb.NewMigratedSQLite(t, "dirscan-blocklist-closed")
				require.NoError(t, closed.Close())
				blocklist = models.NewCrossSeedBlocklistStore(closed)
			}
			var existing *qbt.Torrent
			if tt.existing {
				existing = &qbt.Torrent{Hash: parsed.InfoHash, Name: name}
			}

			sourceDir := t.TempDir()
			sourceFile := filepath.Join(sourceDir, name)
			require.NoError(t, os.WriteFile(sourceFile, []byte("data"), 0o600))
			searchee := &Searchee{Name: name, Path: sourceFile, Files: []*ScannedFile{{Path: sourceFile, RelPath: name, Size: 4}}}

			manager := &failingTorrentAdder{err: errors.New("add stopped by test")}
			qbit := adderQbit{torrentAdder: manager, existing: existing}
			if tt.link != nil {
				qbit.existingOn = 2
			}
			adder := crossseed.NewAdder(qbit, blocklist, nil,
				func(context.Context) int64 { return 0 }, func(context.Context, int, []string) {})
			if tt.link != nil {
				adder.SetCrossSeedRoutes(dirScanRoutes(t, db, *tt.link))
			}
			cacheDB := &cacheTouchDB{DB: db, done: make(chan struct{})}
			svc := &Service{
				store:          models.NewDirScanStore(db),
				adder:          adder,
				jackettService: jackett.NewService(nil, jackett.WithTorrentCache(models.NewTorznabTorrentCacheStore(cacheDB))),
				injector:       NewInjector(nil, crossseed.NewAligner(manager), adder, qbit, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance)),
			}
			if tt.link != nil && tt.existing {
				// The search precheck before the download checks the target only.
				exists, checked := svc.searchResultAlreadyExists(ctx, instance.ID, &jackett.SearchResult{InfoHashV1: parsed.InfoHash}, nil)
				require.True(t, checked)
				require.False(t, exists)
			}

			svc.parser = NewParser(nil)
			svc.searcher = NewSearcher(&stubJackettSearcher{response: &jackett.SearchResponse{
				Results: []jackett.SearchResult{*result}, RequestedIndexerIDs: []int{indexer.ID}, CoveredIndexerIDs: []int{indexer.ID},
			}}, svc.parser)

			l := zerolog.New(io.Discard)
			dir := &models.DirScanDirectory{ID: 1, TargetInstanceID: instance.ID}
			settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict}
			matches, outcome := svc.processSearchee(ctx, dir, searchee, settings, NewMatcher(MatchModeStrict, 0), 0, map[int]struct{}{indexer.ID: {}}, nil, &l)
			// Fetch starts a detached cache touch; join it before SQLite and TempDir cleanup.
			select {
			case <-cacheDB.done:
			case <-time.After(10 * time.Second):
				t.Fatal("torrent cache touch did not finish")
			}

			// A searchee that does not count as searched stays pending, and the next scan tries it again.
			require.Equal(t, !tt.retry, outcome.searched, "only an unavailable linked instance keeps the searchee pending")
			var match *searcheeMatch
			if len(matches) > 0 {
				match = matches[0]
			}
			found := tt.existing && (tt.link == nil || tt.link.state == "")
			cleared := !tt.skipped && !found
			require.Equal(t, cleared, manager.called, "AddTorrent must run only for a torrent that the Pre-add check clears")
			switch {
			case found:
				require.NotNil(t, match)
				require.True(t, match.inQBittorrent)
			case tt.skipped:
				// A skipped match is neither injected nor failed.
				require.Nil(t, match)
			}
		})
	}
}

// dirScanLink is a route between the Dir Scan target, instance 1, and instance 2.
type dirScanLink struct {
	// fromSource makes instance 2 a Source instance that routes to the target.
	// Otherwise the target routes to instance 2.
	fromSource bool
	// state of instance 2: online when empty, "disabled", or "offline".
	state string
}

// dirScanRoutes adds instance 2 to db on a local fake qBittorrent, stores the
// route of link, and returns the route service.
func dirScanRoutes(t *testing.T, db *database.DB, link dirScanLink) *crossseedroute.Service {
	t.Helper()
	ctx := t.Context()

	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	host := qbtstub.New(t, t.TempDir()).URL
	if link.state == "offline" {
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(down.Close)
		host = down.URL
	}
	linked, err := instances.Create(ctx, "linked", host, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	require.Equal(t, 2, linked.ID)
	if link.state == "disabled" {
		_, err = instances.SetActiveState(ctx, linked.ID, false)
		require.NoError(t, err)
	}
	route := &models.CrossSeedRoute{SourceInstanceID: 1, CrossSeedInstanceID: 2}
	if link.fromSource {
		route.SourceInstanceID, route.CrossSeedInstanceID = 2, 1
	}
	store := models.NewCrossSeedRouteStore(db)
	require.NoError(t, store.Upsert(ctx, route))

	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	return crossseedroute.NewService(store, instances, clients, nil)
}

type cacheTouchDB struct {
	*database.DB
	done chan struct{}
}

func (db *cacheTouchDB) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	defer func() { db.done <- struct{}{} }()
	return db.DB.ExecContext(ctx, query, args...)
}
