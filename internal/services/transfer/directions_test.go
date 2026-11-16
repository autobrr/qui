// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

// A push copies from qui's own disk to a target over SFTP. A target with
// fsync@openssh.com confirms each flush, and qui's own check proves the
// copy. Without it, a check on the target proves the copy.
func TestTransferPushesToATargetOverSFTP(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fsync bool
		proof string
		adds  int
	}{
		{name: "with fsync", fsync: true, proof: "Proof: qui checked every piece, and homelab confirmed each flush.", adds: 1},
		{name: "without fsync", proof: "Proof: the check on homelab passed all 5 pieces.", adds: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, overSFTP)
			r.dstSSH.SetFsync(tc.fsync)
			s := newShow(t, showName)
			r.seed(s)

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, transfer.WaySFTP, record.Way)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has the files, and no temporary file")
			require.Contains(t, logOf(record), "Copying 70 B over SFTP from seedbox to homelab.")
			require.Contains(t, logOf(record), tc.proof)
			require.Equal(t, tc.adds, r.dst.Writes("torrents/add"))
			require.True(t, r.dst.Has(s.hash))
			require.False(t, r.src.Has(s.hash))
			require.Empty(t, tree(t, r.srcDir))
		})
	}
}

// A relay copies between two instances over SFTP, through qui.
func TestTransferRelaysBetweenTwoSFTPSides(t *testing.T) {
	r := newRigOn(t, overSFTP, overSFTP)
	r.dstSSH.SetFsync(true)
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WaySFTP, record.Way)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "Copying 70 B over SFTP from seedbox to homelab.")
	require.Contains(t, logOf(record), "Proof: qui checked every piece, and homelab confirmed each flush.")
	require.True(t, r.dst.Has(s.hash))
	require.False(t, r.src.Has(s.hash))
	require.Empty(t, tree(t, r.srcDir))
}

// Two instances on qui's own disk get a local copy when they are on two
// filesystems, and hardlinks when they are on one. The removal from the
// source then removes the source paths, and the links keep the files.
func TestTransferOnQuisOwnDisk(t *testing.T) {
	for _, tc := range []struct {
		name            string
		otherFilesystem bool
		// restart stops qui at the check on the target, and starts it again.
		restart bool
		log     string
	}{
		{name: "a local copy", otherFilesystem: true, log: "Copying 70 B on qui's own disk from seedbox to homelab."},
		{name: "hardlinks", log: "Linking 2 files (70 B) on qui's own disk from seedbox to homelab."},
		{name: "hardlinks after a restart", restart: true, log: "Linking 2 files (70 B) on qui's own disk from seedbox to homelab."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, onDisk)
			r.disk.otherFilesystem = tc.otherFilesystem
			s := newShow(t, showName)
			r.seed(s)
			before := map[string]os.FileInfo{}
			for name := range s.files {
				info, err := os.Stat(filepath.Join(r.srcDir, showName, name))
				require.NoError(t, err)
				before[name] = info
			}

			var once sync.Once
			if tc.restart {
				r.dst.OnAdd = func(string) { once.Do(r.cancel) }
			}

			id, err := r.svc.Start(t.Context(), r.request(s))
			require.NoError(t, err)
			r.svc.Wait()
			if tc.restart {
				r.restart()
				r.svc.Wait()
			}

			record := r.get(id)
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, transfer.WayLocal, record.Way)
			require.Contains(t, logOf(record), tc.log)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has the files, and no temporary file")
			writes, _ := r.disk.log()
			require.Equal(t, tc.otherFilesystem, len(writes) > 0, "a hardlink writes no byte")
			for name, info := range before {
				after, err := os.Stat(filepath.Join(r.dstDir, showName, name))
				require.NoError(t, err)
				require.Equal(t, !tc.otherFilesystem, os.SameFile(info, after), name)
			}
			require.True(t, r.dst.Has(s.hash))
			require.False(t, r.src.Has(s.hash))
			require.Empty(t, tree(t, r.srcDir), "the removal from the source removes the source paths")
		})
	}
}

// When both instances see the same files, no byte moves: the target adds the
// torrent on the files of the source, and the source keeps them.
func TestTransferWithNoCopy(t *testing.T) {
	r := newRigOn(t, onDisk, onDisk)
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Placement.SavePath = r.srcDir

	record := r.run(req)

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WayNone, record.Way)
	require.Contains(t, logOf(record), "No copy: homelab sees the same files as seedbox.")
	require.Contains(t, logOf(record), "Removed the torrent from seedbox and kept its files, because homelab reads the same files: 70 B stay in use.")
	writes, creates := r.disk.log()
	require.Empty(t, writes)
	require.Empty(t, creates)
	require.Equal(t, contents(s, showName), tree(t, r.srcDir), "the files stay, and no file of qui is left")
	require.Empty(t, tree(t, r.dstDir))
	require.True(t, r.dst.Has(s.hash))
	require.Equal(t, r.srcDir, r.dst.LastParams("torrents/add").Get("savepath"))
	require.False(t, r.src.Has(s.hash))
}
