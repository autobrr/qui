// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/filesmanager"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	cacheModeHashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	cacheModeHashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// filesCacheRig is a real SyncManager with the real files cache over a stub
// qBittorrent.
type filesCacheRig struct {
	stub       *qbtstub.Server
	clients    *qbittorrent.ClientPool
	sm         *qbittorrent.SyncManager
	db         *database.DB
	instanceID int
}

func newFilesCacheRig(t *testing.T, torrents ...qbtstub.Torrent) *filesCacheRig {
	t.Helper()
	ctx := t.Context()
	savePath := t.TempDir()
	stub := qbtstub.New(t, savePath)
	for _, torrent := range torrents {
		torrent.SavePath = savePath
		stub.Add(torrent)
	}

	db := testdb.NewMigratedSQLite(t, "crossseed-files-cache-modes")
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "lib", stub.URL, "user", "pass", nil, nil, false, new(true))
	require.NoError(t, err)

	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	_, err = clients.GetClient(ctx, instance.ID)
	require.NoError(t, err)

	sm := qbittorrent.NewSyncManager(clients, nil)
	sm.SetFilesManager(filesmanager.NewService(db))
	return &filesCacheRig{stub: stub, clients: clients, sm: sm, db: db, instanceID: instance.ID}
}

// rename renames the torrent's root in qBittorrent's own WebUI, which moves
// its print, and syncs.
func (r *filesCacheRig) rename(t *testing.T, torrent qbtstub.Torrent, name string) {
	t.Helper()
	r.stub.DeleteElsewhere(torrent.Hash)
	torrent.SavePath = r.stub.DefaultSavePath()
	torrent.Name = name
	r.stub.Add(torrent)
	client, err := r.clients.GetClient(t.Context(), r.instanceID)
	require.NoError(t, err)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
}

// age moves the write of the hash's cached list back by d. withoutPrint
// leaves the row as an image that knows no print writes it.
func (r *filesCacheRig) age(t *testing.T, hash string, d time.Duration, withoutPrint bool) {
	t.Helper()
	at := time.Now().UTC().Add(-d)
	query := `UPDATE torrent_files_sync SET last_synced_at = ?, print_synced_at = ? WHERE torrent_hash_id = (SELECT id FROM string_pool WHERE value = ?)`
	if withoutPrint {
		query = `UPDATE torrent_files_sync SET last_synced_at = ?, print_synced_at = ?, maindata_print = NULL WHERE torrent_hash_id = (SELECT id FROM string_pool WHERE value = ?)`
	}
	_, err := r.db.ExecContext(t.Context(), query, at, at, hash)
	require.NoError(t, err)
}

// fileRequests reads the lists of hashes under ctx and returns the number of
// requests qBittorrent received for them.
func (r *filesCacheRig) fileRequests(ctx context.Context, t *testing.T, hashes ...string) int {
	t.Helper()
	before := r.stub.Calls("torrents/files")
	files, err := r.sm.GetTorrentFilesBatch(ctx, r.instanceID, hashes)
	require.NoError(t, err)
	require.Len(t, files, len(hashes))
	return r.stub.Calls("torrents/files") - before
}

func TestCopyGroupsFilesContextFetchesOnlyAMovedPrint(t *testing.T) {
	t.Parallel()

	moved := qbtstub.Torrent{Hash: cacheModeHashA, Name: "Show.S01", Layout: qbtstub.RootFolder, Files: []string{"e01.mkv"}}
	kept := qbtstub.Torrent{Hash: cacheModeHashB, Name: "Film.2020", Layout: qbtstub.SingleFile}
	rig := newFilesCacheRig(t, moved, kept)
	ctx := filesContext(t.Context())

	require.Equal(t, 2, rig.fileRequests(ctx, t, cacheModeHashA, cacheModeHashB))

	rig.rename(t, moved, "Show.S01.REPACK")
	rig.age(t, cacheModeHashB, 100*time.Hour, false)

	require.Equal(t, 1, rig.fileRequests(ctx, t, cacheModeHashA, cacheModeHashB), "only the moved print is fetched")
}

func TestCopyGroupsFilesContextFetchesAListWithNoPrintOnce(t *testing.T) {
	t.Parallel()

	kept := qbtstub.Torrent{Hash: cacheModeHashA, Name: "Film.2020", Layout: qbtstub.SingleFile}
	rig := newFilesCacheRig(t, kept)
	ctx := filesContext(t.Context())
	require.Equal(t, 1, rig.fileRequests(ctx, t, cacheModeHashA))

	rig.age(t, cacheModeHashA, 100*time.Hour, true)

	require.Equal(t, 1, rig.fileRequests(ctx, t, cacheModeHashA), "a list with no print cannot show a change")
	require.Zero(t, rig.fileRequests(ctx, t, cacheModeHashA), "the fetch stored a print")
}

func TestLocalReadsKeepTheirFilesAge(t *testing.T) {
	t.Parallel()

	folder := qbtstub.Torrent{Hash: cacheModeHashA, Name: "Season 2", Layout: qbtstub.RootFolder, Files: []string{"e01.mkv"}}
	rig := newFilesCacheRig(t, folder)
	require.Equal(t, 1, rig.fileRequests(t.Context(), t, cacheModeHashA))

	// A print that moved does not count for a local read: only the age does.
	rig.rename(t, folder, "Season 02")
	rig.age(t, cacheModeHashA, 20*time.Hour, false)

	svc := rig.localReadService()
	before := rig.stub.Calls("torrents/files")
	svc.fillLocalReads(t.Context(), rig.instanceID, []localTorrent{{hash: cacheModeHashA, name: "Season 2"}}, make(chan struct{}), true)
	require.Equal(t, before, rig.stub.Calls("torrents/files"), "the first fill reads the 20 h old list")
}

func TestLocalReadRefreshKeepsAListWithAMatchingPrintUnderADay(t *testing.T) {
	t.Parallel()

	folder := qbtstub.Torrent{Hash: cacheModeHashA, Name: "Season 2", Layout: qbtstub.RootFolder, Files: []string{"e01.mkv"}}
	rig := newFilesCacheRig(t, folder)
	require.Equal(t, 1, rig.fileRequests(t.Context(), t, cacheModeHashA))
	svc := rig.localReadService()
	refresh := func() int {
		before := rig.stub.Calls("torrents/files")
		svc.fillLocalReads(t.Context(), rig.instanceID, []localTorrent{{hash: cacheModeHashA, name: "Season 2"}}, make(chan struct{}), false)
		return rig.stub.Calls("torrents/files") - before
	}

	rig.age(t, cacheModeHashA, 2*time.Hour, false)
	require.Zero(t, refresh(), "a 2 h old list with a matching print is kept")

	rig.age(t, cacheModeHashA, 24*time.Hour, false)
	require.Equal(t, 1, refresh(), "a list past its backstop is fetched")
}

func (r *filesCacheRig) localReadService() *Service {
	return &Service{
		syncManager:      r.sm,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{}, nil
		},
	}
}
