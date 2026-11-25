// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"context"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

func (r *diskRoot) Link(ctx context.Context, oldName, newName string) error {
	if r.d.lostAt != nil && r.d.lostAt("link", newName) {
		return &os.LinkError{Op: "link", Old: oldName, New: newName, Err: fsops.ErrConnectionLost}
	}
	if r.d.noLinks {
		if err := r.Root.Link(ctx, oldName, newName); err == nil {
			_ = r.Remove(ctx, newName)
		}
		return &os.LinkError{Op: "link", Old: oldName, New: newName, Err: fs.ErrPermission}
	}
	return r.Root.Link(ctx, oldName, newName)
}

const (
	episode1 = "Show.S01E01.mkv"
	episode2 = "Show.S01E02.mkv"
	episode3 = "Show.S01E03.mkv"
)

func newPack(t *testing.T) show {
	t.Helper()
	return packOf(t,
		qbtstub.ContentFile{Path: episode1, Data: numbered("E01", 40)},
		qbtstub.ContentFile{Path: episode2, Data: numbered("E02", 36)},
		qbtstub.ContentFile{Path: episode3, Data: numbered("E03", 30)})
}

func packOf(t *testing.T, files ...qbtstub.ContentFile) show {
	t.Helper()
	data, hash := qbtstub.NewContentTorrent(t, showName, 16, files...)
	s := show{data: data, hash: hash, files: map[string][]byte{}}
	for _, f := range files {
		s.files[f.Path] = f.Data
	}
	return s
}

func numbered(prefix string, n int) []byte {
	var data []byte
	for i := 0; len(data) < n; i++ {
		data = fmt.Appendf(data, "%s.%d|", prefix, i)
	}
	return data[:n]
}

func (r *rig) placeOnTarget(files map[string][]byte) map[string]os.FileInfo {
	r.t.Helper()
	write(r.t, filepath.Join(r.dstDir, showName), files)
	before := map[string]os.FileInfo{}
	for name := range files {
		info, err := os.Stat(filepath.Join(r.dstDir, showName, name))
		require.NoError(r.t, err)
		before[name] = info
	}
	return before
}

func (r *rig) requireUntouched(before map[string]os.FileInfo, files map[string][]byte) {
	r.t.Helper()
	for name, was := range before {
		p := filepath.Join(r.dstDir, showName, name)
		now, err := os.Stat(p)
		require.NoError(r.t, err, name)
		require.True(r.t, os.SameFile(was, now), "%s is the same file under the same name", name)
		require.Equal(r.t, was.Size(), now.Size(), name)
		require.Equal(r.t, was.ModTime(), now.ModTime(), name)
		data, err := os.ReadFile(p)
		require.NoError(r.t, err)
		require.Equal(r.t, string(files[name]), string(data), name)
	}
}

func TestTransferUsesFilesAlreadyOnTheTarget(t *testing.T) {
	for _, tc := range []struct {
		name          string
		onTarget      []string
		incompleteExt bool
		checks        int
	}{
		{name: "every file", onTarget: []string{episode1, episode2, episode3}, checks: 1},
		{name: "with .!qB on, a correct file between two missing ones", onTarget: []string{episode2}, incompleteExt: true, checks: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			s := newPack(t)
			r.seed(s)
			r.dst.IncompleteFilesExt = tc.incompleteExt
			theirs := map[string][]byte{}
			for _, name := range tc.onTarget {
				theirs[name] = s.files[name]
			}
			before := r.placeOnTarget(theirs)
			var missing int64
			for name, data := range s.files {
				if !slices.Contains(tc.onTarget, name) {
					missing += int64(len(data))
				}
			}
			r.disk.free = max(missing, 1)
			written := map[string]struct{}{}
			r.disk.onWrite = func(name string) {
				final, _, _ := strings.Cut(path.Base(name), ".qui-transfer-")
				written[final] = struct{}{}
			}

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has every file, and no temporary file or link of qui")
			r.requireUntouched(before, theirs)
			var copied []string
			for name := range s.files {
				if !slices.Contains(tc.onTarget, name) {
					copied = append(copied, name)
				}
			}
			require.ElementsMatch(t, copied, slices.Collect(maps.Keys(written)), "qui writes only the missing files")
			seeding, err := r.sync.GetTorrentRefsFresh(t.Context(), r.target.ID, qbittorrent.TorrentRefFilter{Hashes: []string{s.hash}})
			require.NoError(t, err)
			require.Len(t, seeding, 1)
			require.Equal(t, "stalledUP", string(seeding[0].State))
			helpers, err := r.store.HelpersOf(t.Context(), transfer.OwnerTransfer, record.ID)
			require.NoError(t, err)
			require.Len(t, helpers, tc.checks, "a check-only helper for each check on the target")
			for _, h := range helpers {
				require.False(t, r.dst.Has(h.Hash), "qui removed its helper")
			}

			require.False(t, r.src.Has(s.hash))
			kept := map[string]string{}
			for _, name := range tc.onTarget {
				kept[showName+"/"+name] = string(s.files[name])
			}
			require.Equal(t, kept, tree(t, r.srcDir), "the source keeps the files that qui did not copy")
		})
	}
}

func TestTransferStopsAtAFileOnTheTargetThatFailsItsCheck(t *testing.T) {
	r := newRig(t)
	s := newPack(t)
	r.seed(s)
	r.dst.IncompleteFilesExt = true
	theirs := map[string][]byte{episode2: numbered("e02", 36)}
	before := r.placeOnTarget(theirs)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Equal(t, "Conflict: the file at "+filepath.Join(r.dstDir, showName, episode2)+" on homelab differs from the torrent.", record.FailureReason)
	r.requireUntouched(before, theirs)
	require.Equal(t, map[string]string{showName + "/" + episode2: string(theirs[episode2])}, tree(t, r.dstDir),
		"no link, no .!qB name, and no copy of qui remains")
	require.False(t, r.dst.Has(s.hash))
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
	require.False(t, r.src.Stopped(s.hash))
	require.Equal(t, []string{"torrents/add", "torrents/delete"}, r.dst.WriteLog(), "the add of the helper and its removal")
	require.Equal(t, "FilesChecked", r.dst.LastParams("torrents/add").Get("stopCondition"))
	require.Equal(t, "false", r.dst.LastParams("torrents/delete").Get("deleteFiles"))
}

func TestTransferTreatsAFileItCannotLinkAsAConflict(t *testing.T) {
	r := newRig(t)
	s := newPack(t)
	r.seed(s)
	r.disk.noLinks = true
	theirs := map[string][]byte{episode2: s.files[episode2]}
	before := r.placeOnTarget(theirs)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, transfer.FailurePermanent, record.FailureClass)
	require.Contains(t, record.FailureReason, "Conflict: qui cannot check the file at "+filepath.Join(r.dstDir, showName, episode2)+
		" on homelab, because it cannot make a hardlink there")
	require.Zero(t, r.dst.Writes("torrents/add"), "no helper checks the file")
	r.requireUntouched(before, theirs)
	require.Equal(t, map[string]string{showName + "/" + episode2: string(theirs[episode2])}, tree(t, r.dstDir))
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
}

func TestTransferProvesAFileOnTheTargetWithNoPieceOfItsOwn(t *testing.T) {
	r := newRig(t)
	const nfo = "Show.S01.nfo"
	files := []qbtstub.ContentFile{
		{Path: episode1, Data: numbered("E01", 40)},
		{Path: nfo, Data: numbered("nfo", 10)},
		{Path: episode2, Data: numbered("E02", 30)},
	}
	data, hash := qbtstub.NewContentTorrent(t, showName, 16, files...)
	s := show{data: data, hash: hash, files: map[string][]byte{}}
	for _, f := range files {
		s.files[f.Path] = f.Data
	}
	r.seed(s)
	theirs := map[string][]byte{nfo: numbered("NFO", 10)}
	before := r.placeOnTarget(theirs)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateFailed, record.State)
	require.Equal(t, "The check on homelab failed 2 of 5 pieces: 2, 3.", record.FailureReason, "the file lies in pieces 2 and 3")
	r.requireUntouched(before, theirs)
	require.Equal(t, map[string]string{showName + "/" + nfo: string(theirs[nfo])}, tree(t, r.dstDir))
	require.False(t, r.dst.Has(hash), "qui never adds a torrent that is not proven")
	require.Equal(t, contents(s, showName), tree(t, r.srcDir))
}
