package crossseed

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed/gazellemusic"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

// testQuerier wraps sql.DB to implement dbinterface.Querier for store tests.
type testQuerier struct {
	*sql.DB
}

type testTx struct {
	*sql.Tx
}

func (t *testTx) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return t.Tx.ExecContext(ctx, query, args...)
}

func (t *testTx) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(ctx, query, args...)
}

func (t *testTx) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return t.Tx.QueryRowContext(ctx, query, args...)
}

func (t *testTx) Commit() error {
	return t.Tx.Commit()
}

func (t *testTx) Rollback() error {
	return t.Tx.Rollback()
}

func (q *testQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	tx, err := q.DB.BeginTx(ctx, opts)
	if err != nil {
		return nil, err
	}
	return &testTx{Tx: tx}, nil
}

type staticInstanceStore struct {
	inst *models.Instance
}

func (s *staticInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if s.inst != nil && s.inst.ID == id {
		return s.inst, nil
	}
	return nil, models.ErrInstanceNotFound
}

func (s *staticInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	if s.inst == nil {
		return []*models.Instance{}, nil
	}
	return []*models.Instance{s.inst}, nil
}

type completionGazelleSyncMock struct {
	torrent         qbt.Torrent
	getTorrentsHits int
	exportHits      int
}

func (m *completionGazelleSyncMock) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	m.getTorrentsHits++
	if len(filter.Hashes) == 0 {
		return []qbt.Torrent{m.torrent}, nil
	}
	for _, h := range filter.Hashes {
		if normalizeHash(h) == normalizeHash(m.torrent.Hash) {
			return []qbt.Torrent{m.torrent}, nil
		}
	}
	return []qbt.Torrent{}, nil
}

func (m *completionGazelleSyncMock) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	out := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		out[normalizeHash(h)] = qbt.TorrentFiles{{Name: "01 - track.flac", Size: 123}}
	}
	return out, nil
}

func (m *completionGazelleSyncMock) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	m.exportHits++
	return nil, "", "", nil
}

func (m *completionGazelleSyncMock) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *completionGazelleSyncMock) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return &qbt.TorrentProperties{}, nil
}

func (m *completionGazelleSyncMock) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{}, nil
}

func (m *completionGazelleSyncMock) AddTorrent(context.Context, int, []byte, map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
}

func (m *completionGazelleSyncMock) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (m *completionGazelleSyncMock) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (m *completionGazelleSyncMock) ExtractDomainFromURL(urlStr string) string {
	u, err := url.Parse(strings.TrimSpace(urlStr))
	if err == nil && u != nil && u.Host != "" {
		return u.Hostname()
	}
	// best-effort: raw host fallback
	urlStr = strings.TrimSpace(urlStr)
	urlStr = strings.TrimPrefix(urlStr, "http://")
	urlStr = strings.TrimPrefix(urlStr, "https://")
	if i := strings.IndexByte(urlStr, '/'); i >= 0 {
		urlStr = urlStr[:i]
	}
	return strings.TrimSpace(urlStr)
}

func (m *completionGazelleSyncMock) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (m *completionGazelleSyncMock) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (m *completionGazelleSyncMock) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (m *completionGazelleSyncMock) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (m *completionGazelleSyncMock) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (m *completionGazelleSyncMock) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (m *completionGazelleSyncMock) CreateCategory(context.Context, int, string, string) error {
	return nil
}

func TestHandleTorrentCompletion_AllowsGazelleWhenJackettMissing(t *testing.T) {
	// Not parallel: the test stubs the package-level findGazelleMatch.
	stubGazelleMatchLookup(t)

	db, err := sql.Open("sqlite", "file:completion_gazelle_guard?mode=memory&cache=shared")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	q := &testQuerier{DB: db}

	_, err = q.ExecContext(context.Background(), `
		CREATE TABLE instance_crossseed_completion_settings (
			instance_id INTEGER PRIMARY KEY,
			enabled INTEGER NOT NULL,
			categories_json TEXT NOT NULL,
			tags_json TEXT NOT NULL,
			exclude_categories_json TEXT NOT NULL,
			exclude_tags_json TEXT NOT NULL,
			indexer_ids_json TEXT NOT NULL,
			bypass_torznab_cache INTEGER NOT NULL DEFAULT 0,
			completion_delay_seconds INTEGER NOT NULL DEFAULT 0,
			updated_at DATETIME NOT NULL
		);
	`)
	if err != nil {
		t.Fatalf("create completion settings table: %v", err)
	}

	_, err = q.ExecContext(context.Background(), `
		INSERT INTO instance_crossseed_completion_settings (
			instance_id, enabled, categories_json, tags_json,
			exclude_categories_json, exclude_tags_json, indexer_ids_json, bypass_torznab_cache, updated_at
		) VALUES (1, 1, '[]', '[]', '[]', '[]', '[]', 0, ?);
	`, time.Now().UTC())
	if err != nil {
		t.Fatalf("insert completion settings: %v", err)
	}

	completionStore := models.NewInstanceCrossSeedCompletionStore(q)

	src := qbt.Torrent{
		Hash:         "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		Name:         "test (2026) [FLAC]",
		Tracker:      "https://flacsfor.me/announce",
		Progress:     1.0,
		CompletionOn: 123,
	}

	syncMock := &completionGazelleSyncMock{torrent: src}

	svc := &Service{
		instanceStore: &staticInstanceStore{inst: &models.Instance{ID: 1, Name: "music"}},
		syncManager:   syncMock,
		// jackettService intentionally nil
		completionStore: completionStore,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				GazelleEnabled: true,
				OrpheusAPIKey:  "ops-key",
			}, nil
		},

		releaseCache: NewReleaseCache(),
	}

	svc.HandleTorrentCompletion(context.Background(), 1, src)

	if syncMock.getTorrentsHits == 0 {
		t.Fatalf("expected completion flow to call syncManager.GetTorrents for Gazelle even when jackett is nil")
	}
}

// With only a RED key, a completion search for an OPS torrent asks RED one
// time, through the API, and asks the other Torznab indexers.
func TestExecuteCompletionSearch_OPSSourceAsksREDThroughTheAPIAndTorznab(t *testing.T) {
	// Not parallel: the test replaces the package-level newGazelleClient.
	const (
		instanceID = 1
		sourceHash = "0123456789abcdef0123456789abcdef01234567"
		sourceName = "Lumen Harbor - Quiet Tides (2024) [FLAC]"
	)

	var redAPIRequests, otherAPIRequests atomic.Int32
	gazelleAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "red-key" {
			redAPIRequests.Add(1)
		} else {
			otherAPIRequests.Add(1)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"status":"success","response":{"results":[]}}`)
	}))
	t.Cleanup(gazelleAPI.Close)
	prevNewClient := newGazelleClient
	newGazelleClient = func(host, _, apiKey string) (*gazellemusic.Client, error) {
		return gazellemusic.NewClient(host, gazelleAPI.URL, apiKey)
	}
	t.Cleanup(func() { newGazelleClient = prevNewClient })

	torznabFake := func(searches *atomic.Int32) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("t") == "caps" {
				w.Header().Set("Content-Type", "application/xml")
				_, _ = fmt.Fprint(w, `<caps><limits default="100" max="100"/><searching>
					<search available="yes" supportedParams="q"/>
					<music-search available="yes" supportedParams="q"/>
				</searching><categories><category id="3000" name="Audio"/></categories></caps>`)
				return
			}
			searches.Add(1)
			w.Header().Set("Content-Type", "application/rss+xml")
			_, _ = fmt.Fprint(w, `<rss version="2.0"><channel><title>Torznab</title></channel></rss>`)
		}))
		t.Cleanup(server.Close)
		return server
	}
	var genericSearches, redTorznabSearches atomic.Int32
	genericIndexer := torznabFake(&genericSearches)
	redTorznab := torznabFake(&redTorznabSearches)

	db := testdb.NewMigratedSQLite(t, "completion-ops-source-red-key")
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	settings := &models.CrossSeedAutomationSettings{GazelleEnabled: true, RedactedAPIKey: "red-key"}
	_, err = store.UpsertSettings(t.Context(), settings)
	require.NoError(t, err)

	source := qbt.Torrent{
		Hash:     sourceHash,
		Name:     sourceName,
		Size:     123,
		Progress: 1,
		Tracker:  "https://home.opsfet.ch/announce",
	}
	filterCache := ttlcache.New[string, *AsyncIndexerFilteringState]()
	filterCache.Set(asyncFilteringCacheKey(instanceID, sourceHash), &AsyncIndexerFilteringState{
		CapabilitiesCompleted: true,
		ContentCompleted:      true,
		CapabilityIndexers:    []int{7, 8},
		FilteredIndexers:      []int{7, 8},
		contentType:           "music",
	}, ttlcache.DefaultTTL)

	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instanceID: {ID: instanceID, Name: "music"}}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{
			{ID: 7, Name: "Music Indexer", BaseURL: genericIndexer.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true},
			{ID: 8, Name: "Redacted", BaseURL: redTorznab.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5, Enabled: true},
		}),
		syncManager: &hashFilteringSyncManager{gazelleSkipHashSyncManager{
			torrents:    []qbt.Torrent{source},
			filesByHash: map[string]qbt.TorrentFiles{sourceHash: {{Name: sourceName + "/01 - Lumen Harbor - Quiet Tides.flac", Size: 123}}},
		}},
		automationStore:     store,
		asyncFilteringCache: filterCache,
		releaseCache:        NewReleaseCache(),
		searchResultCache:   ttlcache.New[string, cachedTorrentSearchResults](),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return settings, nil
		},
	}

	err = service.executeCompletionSearch(t.Context(), instanceID, &source, settings, &models.InstanceCrossSeedCompletionSettings{
		InstanceID: instanceID,
		Enabled:    true,
	})

	require.NoError(t, err)
	require.Positive(t, redAPIRequests.Load(), "the OPS torrent must ask RED through the API")
	require.Zero(t, otherAPIRequests.Load(), "only RED has a key")
	require.Positive(t, genericSearches.Load(), "the OPS torrent must ask the other Torznab indexers")
	require.Zero(t, redTorznabSearches.Load(), "RED has a key, so its Torznab indexer must not be asked")
}
