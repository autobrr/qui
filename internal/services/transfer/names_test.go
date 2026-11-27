// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

const episode = "Show.S01E01.mkv"

func (r *rig) seedOneFile(data []byte, layout, name string) string {
	r.t.Helper()
	at := filepath.Join(r.srcDir, filepath.FromSlash(name))
	require.NoError(r.t, os.MkdirAll(filepath.Dir(at), 0o755))
	require.NoError(r.t, os.WriteFile(at, data, 0o600))
	file, _ := singleFileTorrent(r.t, episode, data)
	return r.src.SeedWithLayout(file, r.srcPath(), layout)
}

var ways = []struct {
	name   string
	target side
}{
	{name: "copy through qui", target: onDisk},
	{name: "Transfer torrent", target: noAccess},
}

func TestTransferKeepsTheFolderOfOneFile(t *testing.T) {
	for _, tc := range ways {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, tc.target)
			r.connect()
			data := numbered("E01", 40)
			hash := r.seedOneFile(data, "Subfolder", "Show.S01E01/"+episode)

			record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID})

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, map[string]string{"Show.S01E01/" + episode: string(data)}, tree(t, r.dstDir))
			require.True(t, r.seeding(r.target.ID, hash), "the target seeds the torrent")
			require.Zero(t, r.dst.Writes("torrents/renameFile"))
			require.Zero(t, r.dst.Writes("torrents/recheck"))
		})
	}
}

func TestTransferChecksATorrentThatItRenamed(t *testing.T) {
	for _, tc := range ways {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, overSFTP, tc.target)
			r.connect()
			data := numbered("E01", 40)
			hash := r.seedOneFile(data, "", episode)
			require.NoError(t, r.sync.RenameTorrentFile(t.Context(), r.source.ID, hash, episode, "Season 1/Pilot.mkv"))

			record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID})

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, map[string]string{"Season 1/Pilot.mkv": string(data)}, tree(t, r.dstDir))
			require.True(t, r.seeding(r.target.ID, hash), "the target seeds the torrent")
			require.Equal(t, 1, r.dst.Writes("torrents/renameFile"))
			require.Equal(t, 1, r.dst.Writes("torrents/recheck"))
			require.Contains(t, logOf(record), "homelab checks the files of the torrent after their rename.")
		})
	}
}

func TestTransferSendsEachRenameOnce(t *testing.T) {
	r := newRig(t)
	r.dst.StaleFileLists = 2
	s := newShow(t, showName)
	r.seed(s)
	r.renameOnSource(s, "Show.S01E01.mkv", "Show.S01E00.mkv", "Show.S01E02.mkv", "Show.S01E03.mkv")

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, 2, r.dst.Writes("torrents/renameFile"))
	require.Zero(t, r.dst.Conflicts("torrents/renameFile"))
	require.True(t, r.seeding(r.target.ID, s.hash), "the target seeds the torrent")
}

func TestTransferFailsWhenTheCheckAfterTheRenameFails(t *testing.T) {
	r := newRig(t)
	data := numbered("E01", 40)
	hash := r.seedOneFile(data, "", episode)
	require.NoError(t, r.sync.RenameTorrentFile(t.Context(), r.source.ID, hash, episode, "Pilot.mkv"))
	r.dst.OnWrite = func(endpoint string) {
		if endpoint == "torrents/recheck" {
			require.NoError(t, os.WriteFile(filepath.Join(r.dstDir, "Pilot.mkv"), numbered("XXX", 40), 0o600))
		}
	}

	record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID})

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailureTemporary, record.FailureClass)
	require.Equal(t, "The check of the torrent on homelab did not pass: qBittorrent shows stoppedDL", record.FailureReason)
	require.True(t, r.src.Has(hash), "the source keeps the torrent")
	require.Equal(t, map[string]string{"Pilot.mkv": string(data)}, tree(t, r.srcDir))
	require.Less(t, r.dst.Syncs(), 20, "the wait ends when the check ends")
}

func (r *rig) seedMovedFolder(data []byte) string {
	r.t.Helper()
	hash := r.seedOneFile(data, "Subfolder", "Show.S01E01/"+episode)
	require.NoError(r.t, r.sync.RenameTorrentFile(r.t.Context(), r.source.ID, hash, "Show.S01E01/"+episode, "Show/"+episode))
	other := numbered("XXX", len(data))
	write(r.t, filepath.Join(r.dstDir, "Show.S01E01"), map[string][]byte{episode: other})
	return hash
}

func TestTransferFailsWhenARenameMovesAnotherFileOverTheCopy(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.connect()
	data := numbered("E01", 40)
	hash := r.seedMovedFolder(data)

	record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID})

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailureTemporary, record.FailureClass)
	require.Equal(t, "The check of the torrent on homelab did not pass: qBittorrent shows stoppedDL", record.FailureReason)
	require.True(t, r.src.Has(hash), "the source keeps the torrent")
	require.Equal(t, map[string]string{"Show/" + episode: string(data)}, tree(t, r.srcDir))
}

func TestTransferWaitsForTheCheckOfATargetWithASlowRefresh(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.dst.RefreshInterval, r.dst.StaleSyncs = 4, 6
	r.connect()
	data := numbered("E01", 40)
	hash := r.seedMovedFolder(data)

	record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID,
		Options: transfer.Options{After: transfer.AfterRemoveWithFiles, LeaveStopped: true}})

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "The check of the torrent on homelab did not pass: qBittorrent shows stoppedDL", record.FailureReason)
	require.True(t, r.src.Has(hash), "the source keeps the torrent")
	require.Equal(t, map[string]string{"Show/" + episode: string(data)}, tree(t, r.srcDir))
}

func TestTransferKeepsTheSourceWhenTheWaitForTheCheckRunsOut(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.dst.RefreshInterval, r.dst.StaleSyncs = 1000, 100
	r.connect()
	data := numbered("E01", 40)
	hash := r.seedMovedFolder(data)

	record := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID,
		Options: transfer.Options{After: transfer.AfterRemoveWithFiles, LeaveStopped: true}})

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "The check of the torrent on homelab did not pass: qBittorrent showed no result of the check within 20ms", record.FailureReason)
	require.True(t, r.src.Has(hash), "the source keeps the torrent")
	require.Equal(t, map[string]string{"Show/" + episode: string(data)}, tree(t, r.srcDir))
}

func TestRetryChecksATorrentThatAnEarlierRunRenamed(t *testing.T) {
	r := newRigOn(t, overSFTP, noAccess)
	r.connect()
	data := numbered("E01", 40)
	hash := r.seedMovedFolder(data)
	r.dst.OnWrite = func(endpoint string) {
		if endpoint == "torrents/recheck" {
			r.dst.Reject = http.StatusInternalServerError
		}
	}
	failed := r.run(transfer.Request{SourceInstanceID: r.source.ID, Hash: hash, TargetInstanceID: r.target.ID})
	require.Equal(t, transfer.FailureTemporary, failed.FailureClass, failed.FailureReason)
	r.dst.OnWrite, r.dst.Reject = nil, 0

	_, err := r.svc.Retry(t.Context(), failed.ID)
	require.NoError(t, err)
	r.svc.Wait()

	record := r.get(failed.ID)
	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "The check of the torrent on homelab did not pass: qBittorrent shows stoppedDL", record.FailureReason)
	require.True(t, r.src.Has(hash), "the source keeps the torrent")
}
