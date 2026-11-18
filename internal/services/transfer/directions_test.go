// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

// A push copies from qui's own disk to a target over SFTP. A target with
// fsync@openssh.com confirms each flush, and qui's own check proves the
// copy.
func TestTransferPushesToATargetOverSFTP(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fsync bool
		proof string
		adds  int
	}{
		{name: "with fsync", fsync: true, proof: "Proof: qui checked every piece, and homelab confirmed each flush.", adds: 1},
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

// A relay copies between two instances over SFTP, through qui, when the
// Transfer forces a copy through qui.
func TestTransferRelaysBetweenTwoSFTPSides(t *testing.T) {
	r := newRigOn(t, overSFTP, overSFTP)
	r.dstSSH.SetFsync(true)
	s := newShow(t, showName)
	r.seed(s)
	req := r.request(s)
	req.Options.ForcedWay = transfer.WaySFTP

	record := r.run(req)

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
		{name: "hardlinks after a restart", restart: true, log: "Linking 2 files (70 B) on qui's own disk from seedbox to homelab."},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, onDisk)
			r.disk.otherFilesystem = tc.otherFilesystem
			s := newShow(t, showName)
			r.seed(s)
			before := map[string]os.FileInfo{}
			for name := range s.files {
				// os.Stat on Windows reads the identity of a file by its path
				// only in os.SameFile, and the removal from the source removes
				// the path. The stat of an open file reads it at once.
				f, err := os.Open(filepath.Join(r.srcDir, showName, name))
				require.NoError(t, err)
				info, err := f.Stat()
				require.NoError(t, errors.Join(err, f.Close()))
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

// With no copy, the target reads the files of the source. A target that
// manages the torrent moves them when the folder of its category changes, and
// a source that keeps seeding would lose them. So the torrent stays on manual
// management on the target while the source seeds it.
// regression: #4242
func TestTransferWithNoCopyLeavesTheFilesToASourceThatKeepsSeeding(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after transfer.After
		moved bool
	}{
		{name: "the source keeps seeding", after: transfer.AfterKeepSeeding},
		{name: "the source gives the torrent up", after: transfer.AfterKeepFiles, moved: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, onDisk)
			r.dst.AutoManagement = true
			r.dst.AddCategory("tv", r.srcDir)
			s := newShow(t, showName)
			r.seed(s)
			req := r.request(s)
			req.Placement.Category = "tv"
			req.Options.After = tc.after

			record := r.run(req)
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, transfer.WayNone, record.Way)
			require.NoError(t, r.sync.EditCategory(t.Context(), r.target.ID, "tv", r.dstDir))

			stays, leaves := r.srcDir, r.dstDir
			if tc.moved {
				stays, leaves = r.dstDir, r.srcDir
			}
			require.Equal(t, contents(s, showName), tree(t, stays))
			require.Empty(t, tree(t, leaves))
		})
	}
}

// A target that cannot confirm a flush has no Proven pieces. After a stop, a
// check on the target finds the pieces that the partial files hold, and qui
// copies only the pieces that failed that check. With qBittorrent's setting
// "Append .!qB extension to incomplete files" on, the check renames the
// partial files, and qui writes the missing pieces into those names.
func TestTransferResumesOnATargetWithoutFsync(t *testing.T) {
	for _, tc := range []struct {
		name          string
		incompleteExt bool
	}{
		{name: "the .!qB setting on", incompleteExt: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, overSFTP, func(c *transfer.Config) { c.FlushBytes = 64 << 10 })
			r.dst.IncompleteFilesExt = tc.incompleteExt
			s := newBigShow(t)
			r.seed(s)
			count := 0
			r.disk.onWrite = func(string) {
				if count++; count == 30 {
					r.cancel()
				}
			}

			id, err := r.svc.Start(t.Context(), r.request(s))
			require.NoError(t, err)
			r.svc.Wait()
			// The target lost bytes of piece 3, as a host can when it stops
			// before a flush.
			first := filepath.Join(r.dstDir, showName, "Show.S01E01.mkv.qui-transfer-"+strconv.FormatInt(id, 10))
			lose(t, first, 3*bigPiece+50, 100)
			failed := failedPieces(t, s, r.dstDir, id)
			require.Contains(t, failed, int64(3))
			require.Greater(t, len(failed), 30, "the copy stopped in the first file")
			stopped, _ := r.disk.log()

			r.restart()
			r.svc.Wait()

			record := r.get(id)
			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has the files, and no temporary file")
			require.Contains(t, logOf(record), fmt.Sprintf("The check on homelab passed %d of 63 pieces of the partial files.", 63-len(failed)))
			writes, creates := r.disk.log()
			for name, n := range creates {
				require.Equal(t, 1, n, "qui made %s once, and wrote into it after the stop", name)
			}
			resumed := writes[len(stopped):]
			written := spans(t, s, id, resumed)
			requireWholePieces(t, int64(len(s.files["Show.S01E01.mkv"])+len(s.files["Show.S01E02.mkv"])), written)
			var pieces []int64
			for _, w := range written {
				if piece := w.start / bigPiece; len(pieces) == 0 || pieces[len(pieces)-1] != piece {
					pieces = append(pieces, piece)
				}
			}
			require.Equal(t, failed, pieces, "qui copies the pieces that failed the check, each one time")
			for _, w := range resumed {
				require.Equal(t, tc.incompleteExt, strings.HasSuffix(w.name, ".!qB"), "the check renamed each partial file: %s", w.name)
			}
		})
	}
}

// lose overwrites n bytes at offset of a file with zeros.
func lose(t *testing.T, name string, offset, n int64) {
	t.Helper()
	f, err := os.OpenFile(name, os.O_WRONLY, 0)
	require.NoError(t, err)
	_, err = f.WriteAt(make([]byte, n), offset)
	require.NoError(t, err)
	require.NoError(t, f.Close())
}

// failedPieces returns the pieces of s that the temporary files of the
// Transfer on the target do not hold whole and right.
func failedPieces(t *testing.T, s show, dir string, id int64) []int64 {
	t.Helper()
	names := []string{"Show.S01E01.mkv", "Show.S01E02.mkv"}
	var want, got []byte
	missing := make([][2]int, 0, len(names))
	for _, name := range names {
		file := s.files[name]
		data, err := os.ReadFile(filepath.Join(dir, showName, name+".qui-transfer-"+strconv.FormatInt(id, 10)))
		if !errors.Is(err, fs.ErrNotExist) {
			require.NoError(t, err)
		}
		missing = append(missing, [2]int{len(want) + len(data), len(want) + len(file)})
		want = append(want, file...)
		got = append(got, data...)
		got = append(got, make([]byte, len(file)-len(data))...)
	}
	var failed []int64
	for start := 0; start < len(want); start += bigPiece {
		end := min(start+bigPiece, len(want))
		gap := slices.ContainsFunc(missing, func(m [2]int) bool { return m[0] < end && start < m[1] })
		if gap || !bytes.Equal(want[start:end], got[start:end]) {
			failed = append(failed, int64(start/bigPiece))
		}
	}
	return failed
}

// On a relay to a target that cannot confirm a flush, a dropped connection
// to the source also leads to a check of the partial files, and qui copies
// each piece one time. A drop after qui flushed pieces does not count
// against Config.Reconnects.
func TestTransferRelayGoesOnAfterDropsWithoutFsync(t *testing.T) {
	r := newRigOn(t, overSFTP, overSFTP, func(c *transfer.Config) {
		c.FlushBytes = 64 << 10
		c.Reconnects = 1
	})
	s := newBigShow(t)
	r.seed(s)
	count := 0
	r.disk.onWrite = func(string) {
		if count++; count == 10 || count == 30 || count == 50 {
			r.ssh.DropConnections()
		}
	}

	req := r.request(s)
	req.Options.ForcedWay = transfer.WaySFTP
	record := r.run(req)

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Equal(t, 3, strings.Count(logOf(record), "The connection to seedbox dropped."))
	require.Equal(t, 3, strings.Count(logOf(record), "pieces of the partial files."))
	writes, _ := r.disk.log()
	written := spans(t, s, record.ID, writes)
	requireWholePieces(t, int64(len(s.files["Show.S01E01.mkv"])+len(s.files["Show.S01E02.mkv"])), written)
	var pieces []int64
	for _, w := range written {
		if piece := w.start / bigPiece; len(pieces) == 0 || pieces[len(pieces)-1] != piece {
			pieces = append(pieces, piece)
		}
	}
	require.Len(t, pieces, 63)
	for i, piece := range pieces {
		require.Equal(t, int64(i), piece, "qui copies each piece one time")
	}
}

// A dropped connection to an SFTP target resumes the copy at once, as a drop
// on the source does: qui writes on into the temporary files that it made.
// regression: #4231
func TestTransferPushGoesOnAfterTheTargetDrops(t *testing.T) {
	r := newRigOn(t, onDisk, overSFTP, func(c *transfer.Config) { c.FlushBytes = 64 << 10 })
	r.dstSSH.SetFsync(true)
	s := newBigShow(t)
	r.seed(s)
	count := 0
	r.disk.onWrite = func(string) {
		if count++; count == 10 || count == 30 || count == 50 {
			r.dstSSH.DropConnections()
		}
	}

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "The connection to homelab dropped.")
	require.NotContains(t, logOf(record), "The connection to seedbox dropped.")
	_, creates := r.disk.log()
	for name, n := range creates {
		require.Equal(t, 1, n, "qui made %s once, and wrote into it after each drop", name)
	}
}
