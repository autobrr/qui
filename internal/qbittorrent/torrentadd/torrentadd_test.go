// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentadd_test

import (
	"bytes"
	"net/url"
	"os"
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

// sample is a torrent of three small files in the folder Sample.Show.S01-GRP.
type sample struct {
	file []byte
	hash string
}

var sampleFiles = []qbtstub.TorrentFile{
	{Path: "Sample.Show.S01E01-GRP.mkv", Size: 3},
	{Path: "Sample.Show.S01E02-GRP.mkv", Size: 5},
	{Path: "Sample.Show.S01-GRP.nfo", Size: 2},
}

func newSample(t *testing.T) sample {
	t.Helper()
	file, hash := qbtstub.NewTorrentFile(t, "Sample.Show.S01-GRP", sampleFiles...)
	return sample{file: file, hash: hash}
}

// writeSample puts the files of the sample torrent under dir.
func writeSample(t *testing.T, dir string) {
	t.Helper()
	root := filepath.Join(dir, "Sample.Show.S01-GRP")
	require.NoError(t, os.MkdirAll(root, 0o755))
	for _, f := range sampleFiles {
		require.NoError(t, os.WriteFile(filepath.Join(root, f.Path), bytes.Repeat([]byte("x"), int(f.Size)), 0o600))
	}
}

type rig struct {
	stub       *qbtstub.Server
	dir        string
	instanceID int
	sync       *qbittorrent.SyncManager
	adder      *torrentadd.Adder
}

// newRig starts a stub qBittorrent on WebAPI webAPI with a default save path
// in a temporary folder.
func newRig(t *testing.T, webAPI string) *rig {
	t.Helper()
	dir := t.TempDir()
	stub := qbtstub.New(t, dir)
	stub.WebAPIVersion = webAPI

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "target", stub.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	sync := qbittorrent.NewSyncManager(clientPool, nil)
	return &rig{stub: stub, dir: dir, instanceID: instance.ID, sync: sync, adder: torrentadd.New(sync)}
}

func (r *rig) add(t *testing.T, torrent sample, settings torrentadd.Settings) torrentadd.Result {
	t.Helper()
	return r.adder.Add(t.Context(), torrentadd.Request{
		InstanceID:   r.instanceID,
		Torrent:      torrent.file,
		Hash:         torrent.hash,
		Settings:     settings,
		Polls:        5,
		PollInterval: 10 * time.Millisecond,
	})
}

// The add reports what qBittorrent shows after the last poll: verified once a
// check found every file, checking while the check runs, accepted for a
// torrent that qBittorrent lists without its files, and failed with the
// reason.
func TestAddReportsTheResult(t *testing.T) {
	for _, tc := range []struct {
		name        string
		files       bool
		checkSyncs  int
		listAfter   int
		settings    torrentadd.Settings
		want        torrentadd.Outcome
		wantFailure torrentadd.Failure
		wantState   string
	}{
		{name: "a check that finds every file is verified", files: true, want: torrentadd.Verified, wantState: "stalledUP"},
		{name: "a check that still runs is checking", files: true, checkSyncs: 1000, want: torrentadd.Checking, wantState: "checkingDL"},
		{name: "a torrent without its files is accepted", want: torrentadd.Accepted, wantState: "stalledDL"},
		{name: "seed mode without the files fails", settings: torrentadd.Settings{SeedMode: true}, want: torrentadd.Failed,
			wantFailure: torrentadd.MissingFiles, wantState: "missingFiles"},
		{name: "a torrent that qBittorrent never lists fails", files: true, listAfter: 1000, want: torrentadd.Failed, wantFailure: torrentadd.NotListed},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "2.16.2")
			r.stub.CheckSyncs, r.stub.ListAfterSyncs = tc.checkSyncs, tc.listAfter
			if tc.files {
				writeSample(t, r.dir)
			}
			settings := tc.settings
			settings.Stopped = new(false)

			res := r.add(t, newSample(t), settings)

			require.Equal(t, tc.want, res.Outcome, "failure %d: %v", res.Failure, res.Err)
			require.Equal(t, tc.wantFailure, res.Failure)
			if tc.wantState == "" {
				require.Nil(t, res.Torrent)
				return
			}
			require.NotNil(t, res.Torrent)
			require.Equal(t, tc.wantState, string(res.Torrent.State))
		})
	}
}

// qBittorrent refuses bytes that are no torrent.
func TestAddRefused(t *testing.T) {
	r := newRig(t, "2.16.2")

	res := r.add(t, sample{file: []byte("not a torrent"), hash: newSample(t).hash}, torrentadd.Settings{})

	require.Equal(t, torrentadd.Failed, res.Outcome)
	require.Equal(t, torrentadd.AddRefused, res.Failure)
	require.ErrorContains(t, res.Err, "torrent file not valid")
}

// Each stated setting reaches the add call as qBittorrent names it, and an
// unset one is left out, so qBittorrent applies its own default.
func TestAddSendsEverySetting(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings func(dir string) torrentadd.Settings
		want     func(dir string) url.Values
	}{
		{
			name: "every setting",
			settings: func(dir string) torrentadd.Settings {
				return torrentadd.Settings{
					SavePath: filepath.Join(dir, "target"), Category: "tv", Tags: []string{"one", "two"},
					ContentLayout: qbt.ContentLayoutSubfolderNone, Stopped: new(false), Forced: true, AutoTMM: new(false),
					UseDownloadPath: new(true), SeedMode: true, StopWhenFilesChecked: true,
					ShareLimits: &torrentadd.ShareLimits{RatioLimit: 1.5, SeedingTimeLimit: 60, InactiveSeedingTimeLimit: -1},
				}
			},
			want: func(dir string) url.Values {
				return url.Values{
					"savepath": {filepath.Join(dir, "target")}, "category": {"tv"}, "tags": {"one,two"},
					"contentLayout": {"NoSubfolder"}, "stopped": {"false"}, "paused": {"false"}, "forced": {"true"},
					"autoTMM": {"false"}, "useDownloadPath": {"true"}, "seedMode": {"true"}, "skip_checking": {"true"},
					"stopCondition": {"FilesChecked"}, "ratioLimit": {"1.5"}, "seedingTimeLimit": {"60"},
					"inactiveSeedingTimeLimit": {"-1"},
				}
			},
		},
		{
			name:     "no setting",
			settings: func(string) torrentadd.Settings { return torrentadd.Settings{} },
			want:     func(string) url.Values { return url.Values{} },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "2.16.2")

			r.add(t, newSample(t), tc.settings(r.dir))

			require.Equal(t, tc.want(r.dir), r.stub.LastParams("torrents/add"))
		})
	}
}

// qBittorrent 5.3 refuses file priorities in the upload of a .torrent, so the
// add is stopped, the priorities follow with the file priority call, and the
// torrent then starts as the caller asked. Only the wanted files are on disk.
func TestAddSetsPrioritiesThenStarts(t *testing.T) {
	priorities := []int{7, 0, 1}
	for _, tc := range []struct {
		name       string
		settings   torrentadd.Settings
		checkSyncs int
		want       torrentadd.Outcome
		wantState  string
		wantStart  []string
	}{
		{name: "starts forced", settings: torrentadd.Settings{Forced: true}, want: torrentadd.Verified, wantState: "forcedUP",
			wantStart: []string{"torrents/setForceStart"}},
		{name: "stays stopped", settings: torrentadd.Settings{Stopped: new(true)}, want: torrentadd.Accepted, wantState: "stoppedDL"},
		// libtorrent drops seed mode when a file priority is "do not download"
		// at add time (the torrent constructor in torrent.cpp), but keeps it
		// when the priority comes later. The check would still run here.
		{name: "keeps seed mode", settings: torrentadd.Settings{SeedMode: true}, checkSyncs: 1000, want: torrentadd.Verified,
			wantState: "stalledUP", wantStart: []string{"torrents/start"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "2.16.2")
			r.stub.CheckSyncs = tc.checkSyncs
			writeSample(t, r.dir)
			torrent := newSample(t)
			require.NoError(t, os.Remove(filepath.Join(r.dir, "Sample.Show.S01-GRP", "Sample.Show.S01E02-GRP.mkv")))
			settings := tc.settings
			settings.FilePriorities = priorities

			res := r.add(t, torrent, settings)

			require.Equal(t, tc.want, res.Outcome, "failure %d: %v", res.Failure, res.Err)
			require.Equal(t, tc.wantState, string(res.Torrent.State))
			require.Equal(t, priorities, r.stub.FilePriorities(torrent.hash))
			add := r.stub.LastParams("torrents/add")
			require.Equal(t, "true", add.Get("stopped"))
			require.NotContains(t, add, "filePriorities")
			wantLog := append([]string{"torrents/add", "torrents/filePrio", "torrents/filePrio", "torrents/filePrio"}, tc.wantStart...)
			require.Equal(t, wantLog, r.stub.WriteLog())
		})
	}
}

// qBittorrent lists a new torrent only once libtorrent added it. The calls
// after the add wait for it with the retry for calls after an add.
func TestAddWaitsForTheNewTorrent(t *testing.T) {
	for _, tc := range []struct {
		name     string
		settings torrentadd.Settings
	}{
		{name: "file priorities", settings: torrentadd.Settings{FilePriorities: []int{1, 1, 1}}},
		{name: "recheck", settings: torrentadd.Settings{Stopped: new(true), Recheck: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t, "2.16.2")
			r.stub.ListAfterSyncs = 6
			writeSample(t, r.dir)

			res := r.add(t, newSample(t), tc.settings)

			require.Equal(t, torrentadd.Verified, res.Outcome, "failure %d: %v", res.Failure, res.Err)
		})
	}
}

// With the stop condition "files checked", a torrent that qBittorrent stops
// after its check cannot change any more, so the polls end there, also when
// the check found pieces missing.
func TestAddEndsWhenTheCheckStopsTheTorrent(t *testing.T) {
	r := newRig(t, "2.16.2")
	r.stub.CheckSyncs = 1

	res := r.adder.Add(t.Context(), torrentadd.Request{
		InstanceID:   r.instanceID,
		Torrent:      newSample(t).file,
		Hash:         newSample(t).hash,
		Settings:     torrentadd.Settings{Stopped: new(false), StopWhenFilesChecked: true},
		Polls:        1000,
		PollInterval: time.Millisecond,
	})

	require.Equal(t, torrentadd.Accepted, res.Outcome, "failure %d: %v", res.Failure, res.Err)
	require.Equal(t, "stoppedDL", string(res.Torrent.State))
	require.Less(t, r.stub.Syncs(), 10)
}

// The user can raise the status refresh interval in qBittorrent while qui
// holds the old one in its cache of the preferences. The wait after a recheck
// reads the interval fresh, so it does not take the complete state from
// before the recheck as the result of the check.
// regression: #4242
func TestRecheckReadsTheRefreshIntervalFromQBittorrent(t *testing.T) {
	r := newRig(t, "2.16.2")
	r.stub.CheckSyncs, r.stub.StaleSyncs = 1, 3
	writeSample(t, r.dir)
	torrent := newSample(t)
	res := r.add(t, torrent, torrentadd.Settings{Stopped: new(true), SeedMode: true})
	require.Equal(t, torrentadd.Verified, res.Outcome, "failure %d: %v", res.Failure, res.Err)
	_, err := r.sync.GetAppPreferences(t.Context(), r.instanceID)
	require.NoError(t, err)
	r.stub.SetRefreshInterval(40)
	require.NoError(t, os.RemoveAll(filepath.Join(r.dir, "Sample.Show.S01-GRP")))

	res = r.adder.Recheck(t.Context(), torrentadd.Request{InstanceID: r.instanceID, Hash: torrent.hash, Polls: 10, PollInterval: 10 * time.Millisecond})

	require.Equal(t, torrentadd.Accepted, res.Outcome, "failure %d: %v", res.Failure, res.Err)
	require.Equal(t, "stoppedDL", string(res.Torrent.State))
}
