// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"net/http"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// newStubClient returns a client of a qbtstub that reports version, with the
// version read once.
func newStubClient(t *testing.T, version string) (*Client, *qbtstub.Server) {
	t.Helper()
	stub := qbtstub.New(t, "/data")
	stub.WebAPIVersion = version
	client := &Client{Client: qbt.NewClient(qbt.Config{Host: stub.URL, APIKey: "test-key"})}
	require.NoError(t, client.RefreshCapabilities(t.Context()))
	return client, stub
}

// Each row has the highest version below the cut-off and the lowest at it.
func TestClientSupports(t *testing.T) {
	t.Parallel()

	tests := []struct {
		capability  Capability
		unsupported []string
		supported   []string
	}{
		{CapRenameTorrent, []string{"1.9.0"}, []string{"2.0.0"}},
		{CapTrackerEditing, []string{"2.1.1"}, []string{"2.2.0"}},
		{CapFilePriority, []string{"2.1.1"}, []string{"2.2.0"}},
		{CapRenameFile, []string{"2.3.0"}, []string{"2.4.0"}},
		{CapRenameFolder, []string{"2.6.2"}, []string{"2.7.0"}},
		{CapTorrentTmpPath, []string{"2.8.3"}, []string{"2.8.4"}},
		{CapTorrentExport, []string{"2.8.10"}, []string{"2.8.11"}},
		{CapSubcategories, []string{"2.8.19"}, []string{"2.9.0"}},
		{CapSetRSSFeedURL, []string{"2.9.0"}, []string{"2.9.1"}},
		// qBittorrent 4.6.x exports .torrent files without their trackers.
		{CapExportOmitsTrackers, []string{"2.9.0", "2.9.4", "2.11.4"}, []string{"2.9.1", "2.9.3"}},
		{CapCategorySavePathNesting, []string{"2.9.3"}, []string{"2.10.0"}},
		{CapLiveReannounce, []string{"2.9.3"}, []string{"2.10.0"}},
		{CapListFieldsV5, []string{"2.10.4"}, []string{"2.11.2"}},
		{CapTorrentCreation, []string{"2.11.1"}, []string{"2.11.2"}},
		{CapPathAutocomplete, []string{"2.11.1"}, []string{"2.11.2"}},
		{CapTestEmail, []string{"2.11.1"}, []string{"2.11.2"}},
		{CapSetTags, []string{"2.11.3"}, []string{"2.11.4"}},
		{CapTrackerHealth, []string{"2.11.3"}, []string{"2.11.4"}},
		{CapAnnounceFlags, []string{"2.11.6"}, []string{"2.11.7"}},
		{CapSetComment, []string{"2.12.0"}, []string{"2.12.1"}},
		{CapSubcategoriesAlwaysEnabled, []string{"2.14.1"}, []string{"2.15.0"}},
		{CapShareLimitsAction, []string{"2.15.0"}, []string{"2.15.1"}},
		{CapProcessInfo, []string{"2.15.0"}, []string{"2.15.1"}},
		{CapListCreatedBy, []string{"2.15.0"}, []string{"2.15.1"}},
		{CapFreeSpaceAtPath, []string{"2.15.1"}, []string{"2.15.2"}},
		{CapCategoryFileNames, []string{"2.15.3"}, []string{"2.15.4"}},
		{CapShareLimitsMode, []string{"2.15.1"}, []string{"2.16.0"}},
		{CapPartialCategoryEdit, []string{"2.16.0"}, []string{"2.16.1"}},
	}

	tested := map[Capability]bool{}
	for _, tc := range tests {
		tested[tc.capability] = true
		for want, versions := range map[bool][]string{false: tc.unsupported, true: tc.supported} {
			for _, version := range versions {
				client, _ := newStubClient(t, version)
				require.Equal(t, want, client.Supports(tc.capability), "capability %d at Web API %s", tc.capability, version)
			}
		}
	}
	for capability := range capabilityVersions {
		require.True(t, tested[capability], "capability %d has no boundary row", capability)
	}
}

// A saturated WebUI times out and is still the same qBittorrent, and a
// request that the caller stops says nothing about the instance.
// regression: #3765
func TestClientKeepsTheVersionWhenAReadTimesOutOrIsCancelled(t *testing.T) {
	t.Parallel()

	for name, stop := range map[string]func(context.Context) (context.Context, context.CancelFunc){
		"timed out": func(ctx context.Context) (context.Context, context.CancelFunc) {
			return context.WithDeadline(ctx, time.Now())
		},
		"cancelled": func(ctx context.Context) (context.Context, context.CancelFunc) {
			ctx, cancel := context.WithCancel(ctx)
			cancel()
			return ctx, cancel
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, _ := newStubClient(t, "2.16.2")

			ctx, cancel := stop(t.Context())
			defer cancel()
			require.Error(t, client.RefreshCapabilities(ctx))
			require.Equal(t, "2.16.2", client.GetWebAPIVersion())
			require.True(t, client.Supports(CapShareLimitsAction))
		})
	}
}

// A proxy can answer the version request with its login page.
func TestClientSupportsNothingWhileTheVersionIsUnknown(t *testing.T) {
	t.Parallel()

	for name, fail := range map[string]func(*qbtstub.Server){
		"failed read":    func(s *qbtstub.Server) { s.WebAPIVersionStatus = http.StatusBadGateway },
		"not a version":  func(s *qbtstub.Server) { s.WebAPIVersion = "<html><title>Login</title></html>" },
		"empty response": func(s *qbtstub.Server) { s.WebAPIVersion = " " },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			client, stub := newStubClient(t, "2.16.2")
			require.True(t, client.Supports(CapSetTags))

			fail(stub)
			require.Error(t, client.RefreshCapabilities(t.Context()))
			require.Empty(t, client.GetWebAPIVersion())
			for capability := range capabilityVersions {
				require.False(t, client.Supports(capability), "capability %d", capability)
			}

			stub.WebAPIVersionStatus = 0
			stub.WebAPIVersion = "2.16.2"
			require.NoError(t, client.RefreshCapabilities(t.Context()))
			require.True(t, client.Supports(CapSetTags), "the next good read restores the capabilities")
		})
	}
}

// forgetVersion makes the version unknown on a client that stays healthy, as a
// failed read outside the health check does.
func forgetVersion(t *testing.T, client *Client, stub *qbtstub.Server) {
	t.Helper()
	good := stub.WebAPIVersion
	stub.WebAPIVersion = "<html><title>Login</title></html>"
	require.Error(t, client.RefreshCapabilities(t.Context()))
	require.Empty(t, client.GetWebAPIVersion())
	stub.WebAPIVersion = good
}

// A share-limit edit with an unknown version reads it again, and does not
// drop the action and still report success.
// regression: #3765
func TestShareLimitEditReadsAnUnknownVersion(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	stub := qbtstub.New(t, root)
	stub.WebAPIVersion = "2.15.1"
	stub.Add(qbtstub.Torrent{Hash: hashA, Name: "Show.S01E01-GRP", SavePath: root, Files: []string{"Show.S01E01.mkv"}})
	client, err := NewClientWithTimeout(1, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	sm := NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil)

	forgetVersion(t, client, stub)
	require.NoError(t, sm.SetTorrentShareLimit(t.Context(), 1, []string{hashA}, 2, -2, -2, "Stop", ""))
	require.Contains(t, stub.LastBody("torrents/setShareLimits"), "shareLimitAction=Stop")

	forgetVersion(t, client, stub)
	stub.WebAPIVersionStatus = http.StatusBadGateway
	require.Error(t, sm.SetTorrentShareLimit(t.Context(), 1, []string{hashA}, 2, -2, -2, "Stop", ""),
		"an edit that cannot read the version fails, not sends without the action")
}

// A backup of qBittorrent 4.6 with an unknown version reads it again, so it
// still patches the trackers into the exported files.
// regression: #3765
func TestOmitsTrackersOnExportReadsAnUnknownVersion(t *testing.T) {
	t.Parallel()
	client, stub := newStubClient(t, "2.9.2")
	client.isHealthy = true
	sm := &SyncManager{clientPool: &ClientPool{clients: map[int]*Client{1: client}}}

	forgetVersion(t, client, stub)
	omits, err := sm.OmitsTrackersOnExport(t.Context(), 1)
	require.NoError(t, err)
	require.True(t, omits)

	forgetVersion(t, client, stub)
	stub.WebAPIVersionStatus = http.StatusBadGateway
	_, err = sm.OmitsTrackersOnExport(t.Context(), 1)
	require.Error(t, err)
}

// versionSink records, for each sync update, whether the client had the
// flags of the new version when the stream and automations got the update,
// and each sync error.
type versionSink struct {
	client         *Client
	liveReannounce []bool
	errs           []error
}

func (s *versionSink) HandleSyncUpdate(int, qbt.SyncUpdate) {
	s.liveReannounce = append(s.liveReannounce, s.client.Supports(CapLiveReannounce))
}
func (*versionSink) HandleTrackerHealthUpdated(int)     {}
func (s *versionSink) HandleSyncError(_ int, err error) { s.errs = append(s.errs, err) }

// newSyncedStubClient returns a pooled client of a qbtstub with one torrent,
// a sync manager over it, and a sink that records each update.
func newSyncedStubClient(t *testing.T, version string) (*Client, *qbtstub.Server, *SyncManager, *versionSink) {
	t.Helper()
	root := t.TempDir()
	stub := qbtstub.New(t, root)
	stub.WebAPIVersion = version
	stub.Add(qbtstub.Torrent{Hash: hashA, Name: "Show.S01E01-GRP", SavePath: root, Files: []string{"Show.S01E01.mkv"}})
	client, err := NewClientWithTimeout(1, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	sink := &versionSink{client: client}
	client.SetSyncEventSink(sink)
	return client, stub, NewSyncManager(&ClientPool{clients: map[int]*Client{1: client}}, nil), sink
}

// A restarted qBittorrent sends a full update. qui reads the version before
// the stream, automations or the torrent list read the new list, so an
// upgrade or a downgrade takes effect without a qui restart.
// regression: #3760
func TestFullSyncUpdateReadsTheVersionFirst(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct{ from, to string }{{"2.15.1", "2.9.3"}, {"2.9.3", "2.15.1"}} {
		t.Run(tc.from+" to "+tc.to, func(t *testing.T) {
			t.Parallel()
			client, stub, sm, sink := newSyncedStubClient(t, tc.from)
			require.NoError(t, client.SyncOwner().Sync(t.Context()))

			stub.WebAPIVersion = tc.to
			require.NoError(t, client.SyncOwner().Sync(t.Context()))

			live := tc.to == "2.15.1"
			require.Equal(t, []bool{!live, live}, sink.liveReannounce)
			resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: 1})
			require.NoError(t, err)
			require.Equal(t, map[int]string{1: tc.to}, resp.WebAPIVersions)
			require.Len(t, resp.Torrents, 1)
			require.Equal(t, live, resp.Torrents[0].Reannounce != nil)
		})
	}
}

// A failed version read on a full update leaves the version unknown, and the
// sync still succeeds.
// regression: #3760
func TestFullSyncUpdateWithAFailedVersionReadLeavesTheVersionUnknown(t *testing.T) {
	t.Parallel()
	client, stub, sm, sink := newSyncedStubClient(t, "2.15.1")

	stub.WebAPIVersionStatus = http.StatusBadGateway
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	require.Equal(t, []bool{false}, sink.liveReannounce)
	require.Empty(t, client.GetWebAPIVersion())
	require.Empty(t, sink.errs, "the sync succeeded, so the stream gets no error")

	resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: 1})
	require.NoError(t, err)
	require.Equal(t, map[int]string{1: ""}, resp.WebAPIVersions)
}

// A version read that times out on a full update also leaves the version
// unknown. A health check keeps the old version on a timeout, but a full update
// can come from a restarted qBittorrent with another version.
// regression: #3760
func TestFullSyncUpdateWithATimedOutVersionReadLeavesTheVersionUnknown(t *testing.T) {
	t.Parallel()
	client, stub := newStubClient(t, "2.15.1")

	// go-qbittorrent calls this hook before a full update enters the cache.
	stub.HangWebAPIVersion = true
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	require.NoError(t, client.refreshCapabilitiesBeforeFullUpdate(ctx, nil))
	require.Empty(t, client.GetWebAPIVersion())
	require.False(t, client.Supports(CapLiveReannounce))
}

// A partial update sends no version request: only a restart can change the
// version, and qBittorrent sends a full update after each one.
func TestPartialSyncUpdateSendsNoVersionRequest(t *testing.T) {
	t.Parallel()
	client, stub, _, _ := newSyncedStubClient(t, "2.15.1")
	stub.PartialUpdates = true

	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	reads := stub.VersionReads()
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
	require.Equal(t, 2, stub.Syncs())
	require.Equal(t, reads, stub.VersionReads())
}

// The Unified view carries the version of each instance, so the page can ask
// the one that changed for its capabilities again.
func TestUnifiedViewCarriesTheVersionOfEachInstance(t *testing.T) {
	t.Parallel()
	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "qbittorrent-unified-versions"), make([]byte, 32))
	require.NoError(t, err)

	clients := map[int]*Client{}
	want := map[int]string{}
	for _, version := range []string{"2.9.3", "2.15.1"} {
		instance, err := store.Create(t.Context(), version, "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		stub := qbtstub.New(t, "/data")
		stub.WebAPIVersion = version
		client, err := NewClientWithTimeout(instance.ID, stub.URL, "", "", "", nil, nil, false, time.Second, time.Second)
		require.NoError(t, err)
		t.Cleanup(client.optimisticUpdates.Close)
		require.NoError(t, client.SyncOwner().Sync(t.Context()))
		clients[instance.ID] = client
		want[instance.ID] = version
	}
	sm := NewSyncManager(&ClientPool{instanceStore: store, clients: clients}, nil)

	resp, err := sm.ListTorrents(t.Context(), TorrentListView{Unified: true})
	require.NoError(t, err)
	require.Equal(t, want, resp.WebAPIVersions)
}
