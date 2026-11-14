// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
)

const (
	alignerHash   = "201984dc31d8f7f719a14087b8f97cf52267ce8a"
	alignerHashV2 = "aa1984dc31d8f7f719a14087b8f97cf52267ce8aaa1984dc31d8f7f719a14087"
)

// alignerFake is a qBittorrent that keeps one torrent's file list and applies
// renames to it.
type alignerFake struct {
	mu    sync.Mutex
	hash  string
	files qbt.TorrentFiles
	// hiddenReads hides the torrent for that many file list reads.
	hiddenReads int
	// lagReads delays each rename by that many file list reads.
	lagReads int
	// dropRenames makes that many renames return nil and change nothing.
	dropRenames int
	renameErr   error
	filesErr    error
	pending     []pendingRename
	renames     []string
	filesCalls  int
}

type pendingRename struct {
	reads  int
	folder bool
	from   string
	to     string
}

func (f *alignerFake) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.filesCalls++
	if f.filesErr != nil {
		return nil, f.filesErr
	}
	if f.hiddenReads > 0 {
		f.hiddenReads--
		return map[string]qbt.TorrentFiles{}, nil
	}
	kept := f.pending[:0]
	for _, p := range f.pending {
		if p.reads > 0 {
			p.reads--
			kept = append(kept, p)
			continue
		}
		f.apply(p)
	}
	f.pending = kept

	out := map[string]qbt.TorrentFiles{}
	for _, hash := range hashes {
		if normalizeHash(hash) == f.hash {
			out[f.hash] = append(qbt.TorrentFiles(nil), f.files...)
		}
	}
	return out, nil
}

func (f *alignerFake) RenameTorrentFile(_ context.Context, _ int, _, oldPath, newPath string) error {
	return f.rename(false, oldPath, newPath)
}

func (f *alignerFake) RenameTorrentFolder(_ context.Context, _ int, _, oldPath, newPath string) error {
	return f.rename(true, oldPath, newPath)
}

func (f *alignerFake) rename(folder bool, from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	kind := "file"
	if folder {
		kind = "folder"
	}
	f.renames = append(f.renames, kind+":"+from+">"+to)
	if f.renameErr != nil {
		return f.renameErr
	}
	if f.dropRenames > 0 {
		f.dropRenames--
		return nil
	}
	f.pending = append(f.pending, pendingRename{reads: f.lagReads, folder: folder, from: from, to: to})
	return nil
}

func (f *alignerFake) apply(p pendingRename) {
	for i := range f.files {
		name := f.files[i].Name
		switch {
		case name == p.from:
			f.files[i].Name = p.to
		case p.folder && strings.HasPrefix(name, p.from+"/"):
			f.files[i].Name = p.to + "/" + strings.TrimPrefix(name, p.from+"/")
		}
	}
}

func (f *alignerFake) names() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	names := make([]string, 0, len(f.files))
	for _, file := range f.files {
		names = append(names, file.Name)
	}
	return names
}

func testAligner(sync alignerSync) *Aligner {
	a := NewAligner(sync)
	a.filesWait = 50 * time.Millisecond
	a.verifyWait = 20 * time.Millisecond
	a.pollEvery = time.Millisecond
	a.retryDelay = time.Millisecond
	a.timeout = 5 * time.Second
	return a
}

func TestAligner_Align_RenamesFilesThenFolder(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{
			hash: alignerHash,
			files: qbt.TorrentFiles{
				{Name: "Old.Root/movie.mkv"},
				{Name: "Old.Root/movie.nfo"},
			},
		}

		ok := NewAligner(fake).Align(t.Context(), 1, alignerHash, AlignmentPlan{
			Files:      []FileRename{{OldPath: "Old.Root/movie.mkv", NewPath: "Old.Root/Movie (2020).mkv"}},
			FolderFrom: "Old.Root",
			FolderTo:   "Movie (2020)",
		})

		require.True(t, ok)
		require.Equal(t, []string{
			"file:Old.Root/movie.mkv>Old.Root/Movie (2020).mkv",
			"folder:Old.Root>Movie (2020)",
		}, fake.renames)
		require.Equal(t, []string{"Movie (2020)/Movie (2020).mkv", "Movie (2020)/movie.nfo"}, fake.names())
	})
}

func TestAligner_Align_WaitsForAsyncRename(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "a.mkv"}}, lagReads: 3}

		ok := NewAligner(fake).Align(t.Context(), 1, alignerHash, AlignmentPlan{
			Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}},
		})

		require.True(t, ok)
		require.Equal(t, []string{"file:a.mkv>b.mkv"}, fake.renames)
	})
}

func TestAligner_Align_RetriesDroppedRenames(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "Old/a.mkv"}}, dropRenames: 2}

		ok := NewAligner(fake).Align(t.Context(), 1, alignerHash, AlignmentPlan{FolderFrom: "Old", FolderTo: "New"})

		require.True(t, ok)
		require.Equal(t, []string{"folder:Old>New", "folder:Old>New", "folder:Old>New"}, fake.renames)
		require.Equal(t, []string{"New/a.mkv"}, fake.names())
	})
}

func TestAligner_Align_FailsAfterThreeDroppedRenames(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "a.mkv"}}, dropRenames: 3}

		ok := NewAligner(fake).Align(t.Context(), 1, alignerHash, AlignmentPlan{
			Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}},
		})

		require.False(t, ok)
		require.Len(t, fake.renames, 3)
	})
}

func TestAligner_Align_AcceptsAPIErrorWhenListShowsNewName(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{
			hash:      alignerHash,
			files:     qbt.TorrentFiles{{Name: "b.mkv"}},
			renameErr: errors.New("invalid newPath or oldPath, or newPath already in use"),
		}

		ok := NewAligner(fake).Align(t.Context(), 1, alignerHash, AlignmentPlan{
			Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}},
		})

		require.True(t, ok)
		require.Len(t, fake.renames, 1)
	})
}

func TestAligner_Align_UnreadableFileListIsNotConfirmed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		renameErr error
		plan      AlignmentPlan
	}{
		{name: "file", plan: AlignmentPlan{Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}}}},
		{name: "folder", plan: AlignmentPlan{FolderFrom: "Old", FolderTo: "New"}},
		{name: "file after API error", renameErr: errors.New("rename failed"), plan: AlignmentPlan{Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				fake := &alignerFake{
					hash:      alignerHash,
					files:     qbt.TorrentFiles{{Name: "Old/a.mkv"}},
					filesErr:  errors.New("file list unavailable"),
					renameErr: tt.renameErr,
				}

				require.False(t, NewAligner(fake).Align(t.Context(), 1, alignerHash, tt.plan))
				require.Len(t, fake.renames, 3)
			})
		})
	}
}

func TestAligner_Align_ListWithNeitherPathIsNotConfirmed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan AlignmentPlan
	}{
		{name: "file", plan: AlignmentPlan{Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}}}},
		{name: "folder", plan: AlignmentPlan{FolderFrom: "Old", FolderTo: "New"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "Other/c.mkv"}}}

				require.False(t, NewAligner(fake).Align(t.Context(), 1, alignerHash, tt.plan))
			})
		})
	}
}

func TestAligner_Align_FailureWarningNamesTheRenameError(t *testing.T) {
	previousLogger := log.Logger
	var buf bytes.Buffer
	log.Logger = zerolog.New(&buf).Level(zerolog.WarnLevel)
	t.Cleanup(func() { log.Logger = previousLogger })

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{
			hash:      alignerHash,
			files:     qbt.TorrentFiles{{Name: "a.mkv"}},
			renameErr: errors.New("invalid newPath or oldPath, or newPath already in use"),
		}

		require.False(t, NewAligner(fake).Align(t.Context(), 1, alignerHash, AlignmentPlan{
			Files: []FileRename{{OldPath: "a.mkv", NewPath: "b.mkv"}},
		}))
		require.Contains(t, buf.String(), `"error":"invalid newPath or oldPath, or newPath already in use"`)
	})
}

func TestAligner_Align_IgnoresRunContextCancel(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "Old/a.mkv"}}}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		ok := NewAligner(fake).Align(ctx, 1, alignerHash, AlignmentPlan{
			Files:      []FileRename{{OldPath: "Old/a.mkv", NewPath: "Old/b.mkv"}},
			FolderFrom: "Old",
			FolderTo:   "New",
		})

		require.True(t, ok)
		require.Equal(t, []string{"New/b.mkv"}, fake.names())
	})
}

func TestAligner_WaitForFiles_WaitsUntilTorrentIsListed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "a.mkv"}}, hiddenReads: 3}

		hash, files, ok := NewAligner(fake).WaitForFiles(t.Context(), 1, []string{strings.ToUpper(alignerHash)})

		require.True(t, ok)
		require.Equal(t, alignerHash, hash)
		require.Equal(t, qbt.TorrentFiles{{Name: "a.mkv"}}, files)
		require.Equal(t, 4, fake.filesCalls)
	})
}

func TestAligner_WaitForFiles_FindsV2OnlyTorrentByTruncatedHash(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHashV2[:40], files: qbt.TorrentFiles{{Name: "a.mkv"}}}

		hash, _, ok := NewAligner(fake).WaitForFiles(t.Context(), 1, []string{alignerHashV2})

		require.True(t, ok)
		require.Equal(t, alignerHashV2[:40], hash)
	})
}

func TestAligner_WaitForFiles_GivesUpWhenTorrentNeverListed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		fake := &alignerFake{hash: alignerHash, files: qbt.TorrentFiles{{Name: "a.mkv"}}, hiddenReads: 1 << 30}
		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		hash, files, ok := NewAligner(fake).WaitForFiles(ctx, 1, []string{alignerHash})

		require.False(t, ok)
		require.Empty(t, hash)
		require.Empty(t, files)
		require.Greater(t, fake.filesCalls, 1, "a cancelled run context must not end the wait")
	})
}
