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

	"github.com/autobrr/qui/internal/testutil/qbtstub"
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
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
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
