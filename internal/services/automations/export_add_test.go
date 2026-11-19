// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"net/url"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent/torrentadd"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// The add on the target gets the parameters that Export sent before it added
// through the add module.
func TestExportAddParameters(t *testing.T) {
	for _, tc := range []struct {
		name     string
		action   models.ExportToInstanceAction
		savePath string
		want     url.Values
	}{
		{
			name: "paused to a save path",
			action: models.ExportToInstanceAction{Category: "tv", Tags: []string{"one", "two"}, Paused: true, SkipChecking: new(false),
				ContentLayout: "NoSubfolder"},
			savePath: "target",
			want: url.Values{"autoTMM": {"false"}, "savepath": {"target"}, "category": {"tv"}, "tags": {"one,two"},
				"paused": {"true"}, "stopped": {"true"}, "contentLayout": {"NoSubfolder"}},
		},
		{
			name:   "started to a category",
			action: models.ExportToInstanceAction{Category: "tv"},
			want: url.Values{"autoTMM": {"true"}, "category": {"tv"}, "seedMode": {"true"},
				"skip_checking": {"true"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			stub := qbtstub.New(t, dir)
			db := testdb.NewMigratedSQLite(t, t.Name())
			instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
			require.NoError(t, err)
			instance, err := instanceStore.Create(t.Context(), "target", stub.URL, "", "", nil, nil, false, new(false))
			require.NoError(t, err)
			clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
			require.NoError(t, err)
			t.Cleanup(func() { _ = clientPool.Close() })
			file, hash := qbtstub.NewTorrentFile(t, "Sample.Show.S01-GRP", qbtstub.TorrentFile{Path: "Sample.Show.S01E01-GRP.mkv", Size: 3})
			want := tc.want
			if tc.savePath != "" {
				tc.savePath = filepath.Join(dir, tc.savePath)
				want["savepath"] = []string{tc.savePath}
			}

			torrentadd.New(qbittorrent.NewSyncManager(clientPool, nil)).Add(t.Context(), torrentadd.Request{
				InstanceID: instance.ID, Torrent: file, Hash: hash, Settings: exportAddSettings(&tc.action, tc.savePath), Polls: 1,
			})

			require.Equal(t, want, stub.LastParams("torrents/add"))
		})
	}
}

// A check that still runs when the polls end counts as success only when the
// export did not skip the check. A failed verification removes the torrent
// from the target.
func TestExportVerifyFailure(t *testing.T) {
	for _, tc := range []struct {
		name         string
		added        torrentadd.Result
		skipChecking bool
		want         string
	}{
		{name: "still checking after skip checking", added: torrentadd.Result{Outcome: torrentadd.Checking}, skipChecking: true,
			want: "Verification timed out after 10 attempts (30s)"},
		{name: "missing files", added: torrentadd.Result{Failure: torrentadd.MissingFiles, Torrent: &qbt.Torrent{SavePath: "/data/tv"}},
			want: "Files missing on target instance (savePath: /data/tv)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, exportVerifyFailure(tc.added, 2, "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", tc.skipChecking))
		})
	}
}
