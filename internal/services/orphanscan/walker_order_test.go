// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"math/rand/v2"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/autobrr/qui/internal/fsops"
)

// reorderWalkBackend replays the local walk in another order that still emits
// each directory before its children, which is all a remote walk guarantees.
type reorderWalkBackend struct {
	fsops.Backend
	reorder func([]fsops.WalkEntry) []fsops.WalkEntry
}

func (b *reorderWalkBackend) WalkDir(ctx context.Context, root string, opts fsops.WalkOptions) (<-chan fsops.WalkEntry, error) {
	in, err := b.Backend.WalkDir(ctx, root, opts)
	if err != nil {
		return nil, err
	}
	var entries []fsops.WalkEntry
	for e := range in {
		entries = append(entries, e)
	}
	entries = b.reorder(entries)
	out := make(chan fsops.WalkEntry, len(entries))
	for _, e := range entries {
		out <- e
	}
	close(out)
	return out, nil
}

// reverseSiblings is a depth-first walk with every directory listed backwards.
func reverseSiblings(entries []fsops.WalkEntry) []fsops.WalkEntry {
	out := slices.Clone(entries)
	slices.SortFunc(out, func(a, b fsops.WalkEntry) int {
		switch {
		case a.RelPath == b.RelPath:
			return 0
		case a.RelPath == ".":
			return -1
		case b.RelPath == ".":
			return 1
		}
		as := strings.Split(a.RelPath, string(filepath.Separator))
		bs := strings.Split(b.RelPath, string(filepath.Separator))
		for i := range min(len(as), len(bs)) {
			if c := strings.Compare(as[i], bs[i]); c != 0 {
				return -c
			}
		}
		return len(as) - len(bs)
	})
	return out
}

// randomParentFirst picks the next entry at random among those whose parent
// has already been emitted, so siblings across directories interleave.
func randomParentFirst(seed uint64) func([]fsops.WalkEntry) []fsops.WalkEntry {
	return func(entries []fsops.WalkEntry) []fsops.WalkEntry {
		rng := rand.New(rand.NewPCG(seed, seed^0x9e3779b97f4a7c15))
		present := make(map[string]bool, len(entries))
		for _, e := range entries {
			present[e.Path] = true
		}
		children := make(map[string][]fsops.WalkEntry)
		var ready []fsops.WalkEntry
		for _, e := range entries {
			parent := filepath.Dir(e.Path)
			if e.RelPath == "." || !present[parent] {
				ready = append(ready, e)
				continue
			}
			children[parent] = append(children[parent], e)
		}
		out := make([]fsops.WalkEntry, 0, len(entries))
		for len(ready) > 0 {
			i := rng.IntN(len(ready))
			e := ready[i]
			ready[i] = ready[len(ready)-1]
			ready = ready[:len(ready)-1]
			out = append(out, e)
			ready = append(ready, children[e.Path]...)
		}
		return out
	}
}

type orderTestFile struct {
	rel   string
	inUse bool
}

type orderTestRoot struct {
	files []orderTestFile
	// orphans and dirs are what the lexical local walk reports, relative to the root.
	orphans []string
	dirs    []string
}

func TestWalkScanRoot_DiscUnitsIgnoreWalkOrder(t *testing.T) {
	roots := []orderTestRoot{
		{
			files: []orderTestFile{
				// A partly seeded disc whose first disc file in walk order is an orphan.
				{rel: "Partial/BDMV/CLIPINF/00001.clpi"},
				{rel: "Partial/BDMV/PLAYLIST/00001.mpls"},
				{rel: "Partial/BDMV/STREAM/00001.m2ts", inUse: true},
				{rel: "Partial/Season 1/e01.mkv"},
				{rel: "Partial/extra/deep/x/y.nfo"},
				// The first disc file in walk order is the seeded one.
				{rel: "InUseFirst/BDMV/CLIPINF/00001.clpi", inUse: true},
				{rel: "InUseFirst/BDMV/STREAM/00001.m2ts"},
				{rel: "InUseFirst/extra/y.nfo"},
				{rel: "Seeded/BDMV/index.bdmv", inUse: true},
				{rel: "Seeded/BDMV/STREAM/00000.m2ts", inUse: true},
				{rel: "Seeded/extra/y.nfo"},
				{rel: "Orphaned/BDMV/index.bdmv"},
				{rel: "Orphaned/BDMV/STREAM/00000.m2ts"},
				{rel: "Orphaned/extra/y.nfo"},
				// A second disc nested in the extras of a partly seeded one.
				{rel: "Nested/BDMV/STREAM/00000.m2ts"},
				{rel: "Nested/BDMV/STREAM/00001.m2ts", inUse: true},
				{rel: "Nested/extra/Disc2/VIDEO_TS/VTS_01_1.VOB"},
				{rel: "Nested/extra/Disc2/info.nfo"},
				// "AUX" is walked before "AUX.d" although "AUX.d/" sorts first as a string.
				{rel: "Segments/BDMV/AUX/a.bin", inUse: true},
				{rel: "Segments/BDMV/AUX.d/b.bin"},
				{rel: "Segments/extra.nfo"},
				// The mirror case: the orphan in "AUX" still comes first, so the sibling stays hidden.
				{rel: "Mirror/BDMV/AUX/a.bin"},
				{rel: "Mirror/BDMV/AUX.d/b.bin", inUse: true},
				{rel: "Mirror/extra.nfo"},
				// Orphan and in-use disc files alternate in walk order.
				{rel: "Interleaved/BDMV/CLIPINF/a.clpi"},
				{rel: "Interleaved/BDMV/PLAYLIST/b.mpls", inUse: true},
				{rel: "Interleaved/BDMV/STREAM/c.m2ts"},
				{rel: "Interleaved/extra/y.nfo"},
				{rel: "Sandwich/BDMV/CLIPINF/a.clpi", inUse: true},
				{rel: "Sandwich/BDMV/PLAYLIST/b.mpls"},
				{rel: "Sandwich/BDMV/STREAM/c.m2ts", inUse: true},
				{rel: "Sandwich/extra/y.nfo"},
				// One name is a prefix of the other.
				{rel: "Prefix/BDMV/STREAM/00001", inUse: true},
				{rel: "Prefix/BDMV/STREAM/00001.m2ts"},
				{rel: "Prefix/extra.nfo"},
				{rel: "Fallback/BDMV/STREAM/00000.m2ts"},
				{rel: "Fallback/readme.txt", inUse: true},
				{rel: "Dvd/VIDEO_TS/VIDEO_TS.IFO"},
				{rel: "Dvd/VIDEO_TS/VTS_01_1.VOB", inUse: true},
				{rel: "Dvd/Extras/x.mkv"},
			},
			orphans: []string{
				"Fallback/BDMV", "InUseFirst/extra/y.nfo", "Orphaned", "Prefix/extra.nfo",
				"Sandwich/extra/y.nfo", "Seeded/extra/y.nfo", "Segments/extra.nfo",
			},
			dirs: []string{
				"Fallback/BDMV", "InUseFirst/extra", "Nested/extra", "Orphaned",
				"Partial/extra", "Partial/extra/deep", "Sandwich/extra", "Seeded/extra",
			},
		},
		{
			files: []orderTestFile{
				{rel: "BDMV/STREAM/00000.m2ts", inUse: true},
				{rel: "BDMV/STREAM/00001.m2ts"},
				{rel: "loose.mkv"},
				{rel: "Show/Season 1/e1.mkv", inUse: true},
				{rel: "Show/Season 1/e2.mkv"},
			},
			orphans: []string{"Show/Season 1/e2.mkv", "loose.mkv"},
			dirs:    nil,
		},
	}

	parent := t.TempDir()
	tfm := NewTorrentFileMap(fsops.HostPaths)
	rootPaths := make([]string, len(roots))
	for i, r := range roots {
		root := filepath.Join(parent, string(rune('a'+i)))
		rootPaths[i] = root
		for _, f := range r.files {
			p := filepath.Join(root, filepath.FromSlash(f.rel))
			writeOldFile(t, p)
			if f.inUse {
				tfm.Add(normalizePath(fsops.HostPaths, p))
			}
		}
	}

	scan := func(t *testing.T, root string, backend fsops.Backend) (orphans, dirs []string) {
		t.Helper()
		files, abandoned, err := walkScanRootCollectingDirs(t.Context(), root, tfm, nil, 0, backend)
		if err != nil {
			t.Fatal(err)
		}
		for _, f := range files {
			orphans = append(orphans, relSlash(t, root, f.Path))
		}
		for _, d := range abandoned {
			dirs = append(dirs, relSlash(t, root, d.Path))
		}
		slices.Sort(orphans)
		slices.Sort(dirs)
		return orphans, dirs
	}

	orders := map[string]func([]fsops.WalkEntry) []fsops.WalkEntry{
		"reverse": reverseSiblings,
	}
	for seed := range uint64(50) {
		orders[fmt.Sprintf("random-%d", seed)] = randomParentFirst(seed)
	}

	for i, r := range roots {
		root := rootPaths[i]
		orphans, dirs := scan(t, root, newTestBackend())
		if !slices.Equal(orphans, r.orphans) || !slices.Equal(dirs, r.dirs) {
			t.Fatalf("root %d lexical walk: orphans %q dirs %q, want %q and %q", i, orphans, dirs, r.orphans, r.dirs)
		}
		for name, reorder := range orders {
			got, gotDirs := scan(t, root, &reorderWalkBackend{Backend: newTestBackend(), reorder: reorder})
			if !slices.Equal(got, orphans) || !slices.Equal(gotDirs, dirs) {
				t.Errorf("root %d %s walk: orphans %q dirs %q, lexical walk gave %q and %q", i, name, got, gotDirs, orphans, dirs)
			}
		}
	}
}

func relSlash(t *testing.T, root, path string) string {
	t.Helper()
	rel, err := filepath.Rel(root, path)
	if err != nil {
		t.Fatal(err)
	}
	return filepath.ToSlash(rel)
}
