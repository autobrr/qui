// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json/jsontext"
	"encoding/json/v2"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/metadata"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	endpointPack   = "Harbor.Signal.S01.1080p.WEB-DL.H.264-LUMA"
	endpointUnheld = "Copper.Lantern.2025.1080p.WEB-DL.H.264-LUMA"
)

func endpointEpisode(n int) string {
	return fmt.Sprintf("Harbor.Signal.S01E%02d.1080p.WEB-DL.H.264-LUMA", n)
}

func endpointEpisodes(count int) []string {
	names := make([]string, count)
	for i := range names {
		names[i] = endpointEpisode(i + 1)
	}
	return names
}

type seasonPackEndpointRequest struct {
	TorrentName string `json:"torrentName"`
	TorrentData string `json:"torrentData,omitempty"`
	InstanceIDs []int  `json:"instanceIds,omitempty"`
	Indexer     string `json:"indexer,omitempty"`
}

type seasonPackEndpointRecord struct {
	Status   int                   `json:"status"`
	Response jsontext.Value        `json:"response"`
	Runs     []seasonPackRunRecord `json:"runs"`
}

type seasonPackRunRecord struct {
	TorrentName     string  `json:"torrentName"`
	Phase           string  `json:"phase"`
	Status          string  `json:"status"`
	Reason          string  `json:"reason"`
	Message         string  `json:"message"`
	InstanceID      *int    `json:"instanceId,omitempty"`
	AddedInstanceID *int    `json:"addedInstanceId,omitempty"`
	MatchedEpisodes int     `json:"matchedEpisodes"`
	TotalEpisodes   int     `json:"totalEpisodes"`
	Coverage        float64 `json:"coverage"`
	LinkMode        string  `json:"linkMode"`
}

type heldTitlesState int

const (
	heldTitlesOff heldTitlesState = iota
	heldTitlesBuilding
	heldTitlesReady
)

type heldTitlesNeverBuilt struct {
	*internalqb.SyncManager
	started atomic.Bool
}

func (h *heldTitlesNeverBuilt) InstanceSnapshot(ctx context.Context, instanceID int) *qbt.SyncSnapshot {
	if !h.started.Load() {
		return nil
	}
	return h.SyncManager.InstanceSnapshot(ctx, instanceID)
}

func (*heldTitlesNeverBuilt) AddSyncEventSink(internalqb.SyncEventSink) {}

type seasonPackAlternateTitles map[string][]string

func (a seasonPackAlternateTitles) AlternateTitles(_ context.Context, held crossseed.HeldRelease) (crossseed.AlternateTitles, error) {
	return crossseed.AlternateTitles{Titles: a[held.Title]}, nil
}

type seasonPackEndpointArr struct {
	total   int
	titles  []string
	lookups atomic.Int64
	totals  atomic.Int64
	onTotal func(call int64)
}

func (a *seasonPackEndpointArr) LookupExternalIDs(context.Context, string, arr.ContentType) (*arr.ExternalIDsResult, error) {
	a.lookups.Add(1)
	return nil, nil
}

func (a *seasonPackEndpointArr) LookupSeasonEpisodeTotal(context.Context, string, int) (*arr.SeasonEpisodeTotalResult, error) {
	call := a.totals.Add(1)
	if a.onTotal != nil {
		a.onTotal(call)
	}
	if a.total == 0 {
		return nil, nil
	}
	return &arr.SeasonEpisodeTotalResult{TotalEpisodes: a.total, Titles: a.titles}, nil
}

type seasonPackEndpointSync struct {
	*internalqb.SyncManager
	listErr    error
	hasErr     error
	addErr     error
	recheckErr error
	hidden     bool
	lists      atomic.Int64
	adds       atomic.Int64
	added      map[string]string
	onAdd      func()
}

func (s *seasonPackEndpointSync) GetSyncOwner(ctx context.Context, instanceID int) (*syncowner.Owner, error) {
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.SyncManager.GetSyncOwner(ctx, instanceID)
}

func (s *seasonPackEndpointSync) GetCachedInstanceTorrents(ctx context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	s.lists.Add(1)
	if s.listErr != nil {
		return nil, s.listErr
	}
	return s.SyncManager.GetCachedInstanceTorrents(ctx, instanceID)
}

func (s *seasonPackEndpointSync) HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	if s.hasErr != nil {
		return nil, false, s.hasErr
	}
	return s.SyncManager.HasTorrentByAnyHash(ctx, instanceID, hashes)
}

func (s *seasonPackEndpointSync) AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	s.adds.Add(1)
	s.added = options
	if s.onAdd != nil {
		s.onAdd()
	}
	if s.addErr != nil {
		return nil, s.addErr
	}
	return s.SyncManager.AddTorrent(ctx, instanceID, fileContent, options)
}

func (s *seasonPackEndpointSync) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	if action == "recheck" && s.recheckErr != nil {
		return s.recheckErr
	}
	return s.SyncManager.BulkAction(ctx, instanceID, hashes, action)
}

func (s *seasonPackEndpointSync) GetDirectoryContentCtx(_ context.Context, _ int, dir string, _ qbt.DirectoryContentMode, _ bool) (any, error) {
	entries, err := os.ReadDir(dir)
	if err != nil || s.hidden {
		return nil, fmt.Errorf("%w: 404", qbt.ErrUnexpectedStatus)
	}
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = filepath.Join(dir, entry.Name())
	}
	return names, nil
}

type seasonPackEndpointDB struct {
	*database.DB
	offline bool
}

func (d *seasonPackEndpointDB) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	if d.offline {
		canceled, cancel := context.WithCancel(ctx)
		cancel()
		ctx = canceled
	}
	return d.DB.QueryRowContext(ctx, query, args...)
}

type seasonPackEndpointRig struct {
	t           *testing.T
	root, links string
	router      chi.Router
	svc         *crossseed.Service
	settings    *models.CrossSeedAutomationSettings
	settingsErr error
	arr         *seasonPackEndpointArr
	sync        *seasonPackEndpointSync
	instances   *models.InstanceStore
	blocklist   *models.CrossSeedBlocklistStore
	routes      *models.CrossSeedRouteStore
	db          *database.DB
	blocklistDB *seasonPackEndpointDB
	servers     map[int]*qbtstub.Server
	settler     *crossseed.Settler
}

func newSeasonPackEndpointRig(t *testing.T, tc *seasonPackEndpointCase, held heldTitlesState) *seasonPackEndpointRig {
	t.Helper()
	root := t.TempDir()
	r := &seasonPackEndpointRig{
		t: t, root: root, links: filepath.Join(root, "links"),
		settings: models.DefaultCrossSeedAutomationSettings(),
		arr:      &seasonPackEndpointArr{total: tc.total, titles: tc.sonarrTitles},
		servers:  map[int]*qbtstub.Server{},
		db:       testdb.NewMigratedSQLite(t, "season-pack-endpoints"),
	}
	r.settings.SeasonPackEnabled = true
	r.settings.SeasonPackCoverageThreshold = 0.75
	require.NoError(t, os.MkdirAll(r.links, 0o755))

	var err error
	r.instances, err = models.NewInstanceStore(r.db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	r.addInstance("lib", tc.held)
	if len(tc.heldElsewhere) > 0 {
		r.addInstance("other", tc.heldElsewhere)
	}

	clients, err := internalqb.NewClientPool(r.instances, models.NewInstanceErrorStore(r.db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	r.sync = &seasonPackEndpointSync{SyncManager: internalqb.NewSyncManager(clients, nil)}
	r.blocklistDB = &seasonPackEndpointDB{DB: r.db}
	r.blocklist = models.NewCrossSeedBlocklistStore(r.blocklistDB)
	r.routes = models.NewCrossSeedRouteStore(r.db)
	runs := models.NewSeasonPackRunStore(r.db)
	backends := fsops.NewPool(r.instances, local.NewBackend())

	r.svc = crossseed.NewService(r.instances, r.sync.SyncManager, nil, nil, r.blocklist, nil, nil, nil, nil, nil, nil, nil, nil, false, runs, crossseed.NewSettler(r.sync.SyncManager, runs), nil, nil)
	r.svc.SetBackendPool(backends)
	routes := crossseedroute.NewService(r.routes, r.instances, clients, backends)
	r.svc.SetCrossSeedRoutes(routes)
	testutil.SetUnexportedField(t, r.svc, "metadataService", (*metadata.Service)(nil))
	testutil.SetUnexportedField(t, r.svc, "arrService", r.arr)
	testutil.SetUnexportedField(t, r.svc, "syncManager", r.sync)
	r.settler = crossseed.NewSettler(r.sync, runs)
	testutil.SetUnexportedField(t, r.svc, "adder", crossseed.NewAdder(r.sync, r.blocklist, r.settler, r.svc.ResumeBudgetBytes, r.svc.RunExternalProgram))
	testutil.SetUnexportedField(t, r.svc, "visibleRetryDelay", time.Nanosecond)
	testutil.SetUnexportedField(t, r.svc, "automationSettingsLoader", func(context.Context) (*models.CrossSeedAutomationSettings, error) {
		if r.settingsErr != nil {
			return nil, r.settingsErr
		}
		return r.settings, nil
	})

	handler := NewCrossSeedHandler(r.svc, nil, r.instances, runs, routes, nil)
	open := func(next http.Handler) http.Handler { return next }
	r.router = chi.NewRouter()
	r.router.Route("/api", func(router chi.Router) { handler.Routes(router, open, open) })

	if tc.setup != nil {
		tc.setup(r)
	}
	for id := range r.servers {
		_, err := r.sync.SyncManager.GetCachedInstanceTorrents(t.Context(), id)
		require.NoError(t, err)
	}
	r.startHeldTitles(held, clients, tc.alternates)
	if tc.fail != nil {
		tc.fail(r)
	}
	return r
}

func (r *seasonPackEndpointRig) startHeldTitles(state heldTitlesState, clients *internalqb.ClientPool, alternates seasonPackAlternateTitles) {
	r.t.Helper()
	features := experimental.New([]string{string(experimental.HeldTitles)})
	switch state {
	case heldTitlesOff:
	case heldTitlesBuilding:
		never := &heldTitlesNeverBuilt{SyncManager: r.sync.SyncManager}
		r.svc.StartHeldTitles(r.t.Context(), features, never, never, nil)
		never.started.Store(true)
	case heldTitlesReady:
		r.svc.StartHeldTitles(r.t.Context(), features, r.sync.SyncManager, clients, []crossseed.AlternateTitleSource{alternates})
		require.Eventually(r.t, func() bool {
			before := r.arr.lookups.Load()
			r.post("/api/cross-seed/webhook/check", mustJSON(r.t, seasonPackEndpointRequest{TorrentName: endpointUnheld}))
			return r.arr.lookups.Load() == before
		}, 10*time.Second, 10*time.Millisecond)
		if len(alternates) > 0 {
			require.Eventually(r.t, func() bool {
				status, _ := r.webhookCheck(webhookPackRequest{TorrentName: endpointEpisode(1)})
				return status == http.StatusOK
			}, 10*time.Second, 10*time.Millisecond)
		}
	}
}

func (r *seasonPackEndpointRig) addInstance(name string, held []string) int {
	r.t.Helper()
	downloads := filepath.Join(r.root, name)
	server := qbtstub.New(r.t, downloads)
	server.SetRefreshInterval(60_000)
	for i, title := range held {
		server.Add(qbtstub.Torrent{Hash: fmt.Sprintf("%d%039d", len(r.servers)+1, i+1), Name: title, SavePath: downloads, Layout: qbtstub.RootFolder, Files: []string{title + ".mkv"}})
	}
	instance, err := r.instances.Create(r.t.Context(), name, server.URL, "user", "pass", nil, nil, false, new(true))
	require.NoError(r.t, err)
	r.servers[instance.ID] = server
	r.updateInstance(instance.ID, &models.InstanceUpdateParams{UseHardlinks: new(true), HardlinkBaseDir: new(r.links)})
	return instance.ID
}

func (r *seasonPackEndpointRig) updateInstance(id int, params *models.InstanceUpdateParams) {
	r.t.Helper()
	instance, err := r.instances.Get(r.t.Context(), id)
	require.NoError(r.t, err)
	_, err = r.instances.Update(r.t.Context(), id, instance.Name, instance.Host, instance.Username, "", nil, nil, params)
	require.NoError(r.t, err)
}

func (r *seasonPackEndpointRig) fillResumeQueue() {
	r.sync.recheckErr = errors.New("recheck rejected")
	defer func() { r.sync.recheckErr = nil }()
	for !r.settler.Settle(r.t.Context(), crossseed.AddedTorrent{InstanceID: 1, Hashes: []string{"other"}}).QueueFull {
		continue
	}
}

func (r *seasonPackEndpointRig) post(path string, body []byte) *httptest.ResponseRecorder {
	r.t.Helper()
	req := httptest.NewRequestWithContext(r.t.Context(), http.MethodPost, path, bytes.NewReader(body))
	resp := httptest.NewRecorder()
	r.router.ServeHTTP(resp, req)
	return resp
}

func (r *seasonPackEndpointRig) runs() []seasonPackRunRecord {
	r.t.Helper()
	list := httptest.NewRecorder()
	r.router.ServeHTTP(list, httptest.NewRequestWithContext(r.t.Context(), http.MethodGet, "/api/cross-seed/season-pack/runs", nil))
	require.Equal(r.t, http.StatusOK, list.Code)
	var runs []seasonPackRunRecord
	require.NoError(r.t, json.Unmarshal([]byte(r.portable(list.Body.Bytes())), &runs))
	return runs
}

func (r *seasonPackEndpointRig) record(endpoint string, body []byte) seasonPackEndpointRecord {
	r.t.Helper()
	resp := r.post("/api/cross-seed/season-pack/"+endpoint, body)
	return seasonPackEndpointRecord{Status: resp.Code, Response: jsontext.Value(r.portable(resp.Body.Bytes())), Runs: r.runs()}
}

func (r *seasonPackEndpointRig) portable(body []byte) string {
	text := string(body)
	for _, path := range []string{r.links, r.root} {
		escaped := strings.Trim(string(mustJSON(r.t, path)), `"`)
		text = strings.ReplaceAll(text, escaped, "<"+filepath.Base(path)+">")
	}
	return strings.ReplaceAll(text, `\\`, "/")
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	data, err := json.Marshal(value)
	require.NoError(t, err)
	return data
}

func endpointTorrent(t *testing.T, name string, length int64, files ...string) []byte {
	t.Helper()
	info := metainfo.Info{Name: name, PieceLength: 16 << 10}
	for _, file := range files {
		info.Files = append(info.Files, metainfo.FileInfo{Path: []string{file}, Length: length})
	}
	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi := metainfo.MetaInfo{AnnounceList: [][]string{{"https://tracker.example.invalid/announce"}}, InfoBytes: infoBytes}
	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

func endpointPackTorrent(t *testing.T, length int64, episodes int) []byte {
	t.Helper()
	files := endpointEpisodes(episodes)
	for i := range files {
		files[i] += ".mkv"
	}
	return endpointTorrent(t, endpointPack, length, files...)
}

func endpointHash(t *testing.T, torrent []byte) string {
	t.Helper()
	meta, err := crossseed.ParseTorrentMetadataWithInfo(torrent)
	require.NoError(t, err)
	return meta.HashV1
}
