// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/json/v2"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/experimental"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

type heldLibrary struct {
	mu        sync.Mutex
	torrents  []qbt.Torrent
	files     map[string]qbt.TorrentFiles
	fileReads []string
	rid       int64
	url       string
	onFiles   func()
	down      bool
	adding    qbt.Torrent
}

type heldMainData struct {
	Rid        int64                  `json:"rid"`
	FullUpdate bool                   `json:"full_update"`
	Torrents   map[string]qbt.Torrent `json:"torrents"`
}

func serveHeldLibrary(t *testing.T, torrents []qbt.Torrent, files map[string]qbt.TorrentFiles) *heldLibrary {
	t.Helper()
	library := &heldLibrary{torrents: torrents, files: files}
	srv := httptest.NewServer(http.HandlerFunc(library.serve))
	t.Cleanup(srv.Close)
	library.url = srv.URL
	return library
}

func (l *heldLibrary) serve(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v2/torrents/files" && l.onFiles != nil {
		l.onFiles()
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	switch r.URL.Path {
	case "/api/v2/auth/login":
		_, _ = io.WriteString(w, "Ok.")
	case "/api/v2/app/webapiVersion":
		_, _ = io.WriteString(w, "2.11.4")
	case "/api/v2/sync/maindata":
		l.rid++
		byHash := make(map[string]qbt.Torrent, len(l.torrents))
		for _, torrent := range l.torrents {
			byHash[torrent.Hash] = torrent
		}
		_ = json.MarshalWrite(w, heldMainData{Rid: l.rid, FullUpdate: true, Torrents: byHash})
	case "/api/v2/torrents/info":
		out := []qbt.Torrent{}
		for hash := range strings.SplitSeq(r.FormValue("hashes"), "|") {
			if i := slices.IndexFunc(l.torrents, func(torrent qbt.Torrent) bool { return torrent.Hash == hash }); i >= 0 {
				torrent := l.torrents[i]
				torrent.Trackers = []qbt.TorrentTracker{{Url: torrent.Tracker, Status: qbt.TrackerStatusOK}}
				out = append(out, torrent)
			}
		}
		_ = json.MarshalWrite(w, out)
	case "/api/v2/torrents/files":
		hash := r.FormValue("hash")
		l.fileReads = append(l.fileReads, hash)
		files := l.files[hash]
		if files == nil {
			files = qbt.TorrentFiles{}
		}
		_ = json.MarshalWrite(w, files)
	case "/api/v2/torrents/add":
		l.torrents = append(slices.Clone(l.torrents), l.adding)
		_, _ = io.WriteString(w, "Ok.")
	default:
		http.NotFound(w, r)
	}
}

func (l *heldLibrary) syncs() int64 {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rid
}

func (l *heldLibrary) set(torrents []qbt.Torrent) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.torrents = torrents
}

func (l *heldLibrary) takeFileReads() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	reads := l.fileReads
	l.fileReads = nil
	slices.Sort(reads)
	return reads
}

type heldRig struct {
	t         *testing.T
	db        *database.DB
	store     *models.InstanceStore
	pool      *qbittorrent.ClientPool
	syncs     *qbittorrent.SyncManager
	instances map[int]*models.Instance
	libraries map[int]*heldLibrary
	clients   map[int]*qbittorrent.Client
	sinks     *heldSinks
	settings  models.CrossSeedAutomationSettings
}

type heldSinks struct {
	pool  *qbittorrent.ClientPool
	added []qbittorrent.SyncEventSink
}

func (s *heldSinks) AddSyncEventSink(sink qbittorrent.SyncEventSink) {
	s.added = append(s.added, sink)
	s.pool.AddSyncEventSink(sink)
}

func newHeldRig(t *testing.T, libraries ...*heldLibrary) *heldRig {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "held-titles")
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	pool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	rig := &heldRig{
		t: t, db: db, store: instanceStore, pool: pool, syncs: qbittorrent.NewSyncManager(pool, nil), sinks: &heldSinks{pool: pool},
		instances: map[int]*models.Instance{}, libraries: map[int]*heldLibrary{}, clients: map[int]*qbittorrent.Client{},
	}
	for i, library := range libraries {
		instance, err := instanceStore.Create(t.Context(), "instance"+string(rune('a'+i)), library.url, "user", "pass", nil, nil, false, nil)
		require.NoError(t, err)
		rig.instances[instance.ID] = instance
		rig.libraries[instance.ID] = library
		rig.clients[instance.ID] = rig.connect(instance.ID)
	}
	testutil.SetUnexportedField(t, pool, "clients", maps.Clone(rig.clients))
	return rig
}

func waitForSyncsAtCleanup(tb testing.TB, client *qbittorrent.Client) {
	tb.Helper()
	tb.Cleanup(func() {
		owner := client.SyncOwner()
		for done := owner.PendingSync(time.Minute); done != nil; done = owner.PendingSync(time.Minute) {
			<-done
		}
	})
}

func (r *heldRig) newClient(instanceID int) *qbittorrent.Client {
	r.t.Helper()
	client, err := qbittorrent.NewClientWithTimeout(instanceID, r.libraries[instanceID].url, "", "", "", nil, nil, false, time.Minute, time.Minute)
	require.NoError(r.t, err)
	waitForSyncsAtCleanup(r.t, client)
	return client
}

func (r *heldRig) connect(instanceID int) *qbittorrent.Client {
	r.t.Helper()
	client := r.newClient(instanceID)
	require.NoError(r.t, client.SyncOwner().Sync(r.t.Context()))
	return client
}

func (r *heldRig) reconnect(instanceID int) {
	r.t.Helper()
	client := r.newClient(instanceID)
	client.SetSyncEventSink(r.sinks.added[0])
	r.clients[instanceID] = client
	testutil.SetUnexportedField(r.t, r.pool, "clients", maps.Clone(r.clients))
	require.NoError(r.t, client.SyncOwner().Sync(r.t.Context()))
}

func (r *heldRig) add(library *heldLibrary) int {
	r.t.Helper()
	instance, err := r.store.Create(r.t.Context(), "instance"+string(rune('a'+len(r.instances))), "http://qbt.example.invalid", "user", "pass", nil, nil, false, nil)
	require.NoError(r.t, err)
	r.instances[instance.ID] = instance
	r.libraries[instance.ID] = library
	if library.down {
		r.clients[instance.ID] = r.newClient(instance.ID)
		testutil.SetUnexportedField(r.t, r.pool, "clients", maps.Clone(r.clients))
		return instance.ID
	}
	r.reconnect(instance.ID)
	return instance.ID
}

func (r *heldRig) startNoBackgroundSyncs() {
	r.t.Helper()
	for _, client := range r.clients {
		testutil.SetUnexportedField(r.t, client.SyncOwner(), "readsSync", false)
	}
}

func (r *heldRig) sync(instanceID int) {
	r.t.Helper()
	require.NoError(r.t, r.clients[instanceID].SyncOwner().Sync(r.t.Context()))
}

func (r *heldRig) fullScan() (*Service, *spyARRLookupService) {
	spy := &spyARRLookupService{}
	return &Service{
		instanceStore:    r.store,
		syncManager:      r.syncs,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		arrService:       spy,
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			settings := r.settings
			return &settings, nil
		},
	}, spy
}

func (r *heldRig) heldTitles(ctx context.Context, sources ...AlternateTitleSource) (*Service, *spyARRLookupService) {
	svc, spy := r.fullScan()
	svc.StartHeldTitles(ctx, experimental.New([]string{string(experimental.HeldTitles)}), r.syncs, r.sinks, sources)
	return svc, spy
}

func (r *heldRig) waitReady(svc *Service) {
	r.t.Helper()
	ids := slices.Sorted(maps.Keys(r.instances))
	require.Eventually(r.t, func() bool { return svc.heldTitles.ready(r.t.Context(), ids) }, 30*time.Second, 5*time.Millisecond)
}

func checkAnnounce(t *testing.T, svc *Service, name string, size uint64, instanceIDs ...int) *WebhookCheckResponse {
	t.Helper()
	resp, err := svc.CheckWebhook(t.Context(), &WebhookCheckRequest{InstanceIDs: instanceIDs, TorrentName: name, Size: size})
	require.NoError(t, err)
	return resp
}

func requireNotHeld(t *testing.T, svc *Service, name string, size int64) {
	t.Helper()
	require.Empty(t, svc.heldTitles.candidates(heldTitlesQuery{announced: svc.matcher().readName(name), size: size}, []int{1}))
}

func readyHeldTitles(t *testing.T, torrents []qbt.Torrent, files map[string]qbt.TorrentFiles, settings models.CrossSeedAutomationSettings) *Service {
	t.Helper()
	rig := newHeldRig(t, serveHeldLibrary(t, torrents, files))
	rig.settings = settings
	rig.startNoBackgroundSyncs()
	held, _ := rig.heldTitles(t.Context())
	rig.waitReady(held)
	return held
}

func (r *heldRig) sameNameGroups() [][]qbittorrent.CopyGroupMember {
	type release struct {
		name string
		size int64
	}
	groups := map[release][]qbittorrent.CopyGroupMember{}
	for _, id := range slices.Sorted(maps.Keys(r.libraries)) {
		for _, torrent := range r.libraries[id].torrents {
			key := release{torrent.Name, torrent.TotalSize}
			groups[key] = append(groups[key], qbittorrent.CopyGroupMember{InstanceID: id, Hash: torrent.Hash})
		}
	}
	maps.DeleteFunc(groups, func(_ release, group []qbittorrent.CopyGroupMember) bool { return len(group) < 2 })
	return slices.Collect(maps.Values(groups))
}

func (r *heldRig) publishCopyGroups(groups ...[]qbittorrent.CopyGroupMember) {
	ids := map[int]map[string]uint64{}
	for id, group := range groups {
		for _, member := range group {
			if ids[member.InstanceID] == nil {
				ids[member.InstanceID] = map[string]uint64{}
			}
			ids[member.InstanceID][member.Hash] = uint64(id)
		}
	}
	r.syncs.SetCopyGroups(qbittorrent.NewCopyGroups(ids, nil, slices.Sorted(maps.Keys(r.instances))))
}
