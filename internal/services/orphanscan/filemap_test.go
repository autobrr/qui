// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"runtime"
	"testing"
)

func TestNormalizePath_WindowsCaseInsensitive(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosWindows {
		t.Skip("windows-only path normalization")
	}

	p1 := normalizePath(`L:\movies\Code.8.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv`)
	p2 := normalizePath(`l:\MOVIES\Code.8.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv`)
	if p1 != p2 {
		t.Fatalf("expected normalized paths equal on windows:\n  %q\n  %q", p1, p2)
	}

	m := NewTorrentFileMap()
	m.Add(p1)
	if !m.Has(p2) {
		t.Fatalf("expected torrent file map to match regardless of casing: %q", p2)
	}
}

func TestFindScanRoot_WindowsCaseInsensitive(t *testing.T) {
	t.Parallel()

	if runtime.GOOS != goosWindows {
		t.Skip("windows-only path matching")
	}

	root := `l:\movies`
	path := `L:\movies\Code.8.2019.1080p.AMZN.WEB-DL.DDP5.1.H.264-NTG.mkv`

	got := findScanRoot(path, []string{root})
	if got != root {
		t.Fatalf("expected scan root %q, got %q", root, got)
	}
}
