// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func TestLocalDiscUnitsOnPath(t *testing.T) {
	// Local-only diagnostic test.
	// Run with:
	//   $env:QUI_LOCAL_SCAN_PATH='D:\\UA_Linked\\BHD'; go test ./internal/services/orphanscan -run TestLocalDiscUnitsOnPath -count=1 -v
	path := os.Getenv("QUI_LOCAL_SCAN_PATH")
	if path == "" {
		t.Skip("set QUI_LOCAL_SCAN_PATH to run this local test")
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("path is not a directory: %q", path)
	}

	tfm := NewTorrentFileMap()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	orphans, truncated, err := walkScanRootDiscUnits(ctx, path, tfm, nil, 0, 50_000)
	if err != nil {
		t.Fatalf("walkScanRootDiscUnits(%q): %v", path, err)
	}

	sort.Slice(orphans, func(i, j int) bool {
		if orphans[i].Size == orphans[j].Size {
			return orphans[i].Path < orphans[j].Path
		}
		return orphans[i].Size > orphans[j].Size
	})

	t.Logf("disc units found: %d (truncated=%v)", len(orphans), truncated)
	maxLog := min(30, len(orphans))
	for i := range maxLog {
		t.Logf("%3d) %s (size=%d)", i+1, orphans[i].Path, orphans[i].Size)
	}
}

//nolint:revive // local-only diagnostic test; readability > lint thresholds
func TestLocalAllUnitsOnPath(t *testing.T) {
	// Local-only diagnostic test.
	// Counts "orphan units" as the orphan scan sees them:
	//   - Disc layouts (BDMV/VIDEO_TS) collapse to a parent folder unit
	//   - Everything else is per-file
	// Run with:
	//   $env:QUI_LOCAL_SCAN_PATH='D:\\UA_Linked\\BHD'; go test ./internal/services/orphanscan -run TestLocalAllUnitsOnPath -count=1 -v
	path := os.Getenv("QUI_LOCAL_SCAN_PATH")
	if path == "" {
		t.Skip("set QUI_LOCAL_SCAN_PATH to run this local test")
	}
	path = filepath.Clean(path)

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %q: %v", path, err)
	}
	if !info.IsDir() {
		t.Fatalf("path is not a directory: %q", path)
	}

	// No torrent map in this local diagnostic: treat everything on disk as "orphan".
	// We still run the same grouping logic so disc layouts collapse to folder units.
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()

	discUnits := make(map[string]int64)
	var nonDiscUnits int64
	var totalFiles int64
	var totalBytes int64
	var maxFilePath string
	var maxFileSize int64
	discUnitCache := make(map[string]discUnitDecision)

	err = filepath.WalkDir(path, func(p string, d os.DirEntry, walkErr error) error {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		if walkErr != nil {
			if os.IsPermission(walkErr) {
				return nil
			}
			return walkErr
		}

		// Skip symlinks (match orphan scan behavior)
		if d.Type()&os.ModeSymlink != 0 {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}

		if d.IsDir() {
			return nil
		}

		fi, infoErr := d.Info()
		if infoErr != nil {
			return nil //nolint:nilerr // best-effort local diagnostic
		}

		totalFiles++
		sz := fi.Size()
		totalBytes += sz
		if sz > maxFileSize {
			maxFileSize = sz
			maxFilePath = p
		}

		unitPath, isDiscUnit := discOrphanUnit(path, p, discUnitCache)
		if isDiscUnit {
			discUnits[unitPath] += sz
			return nil
		}

		// For non-disc, each file is its own unit.
		nonDiscUnits++
		return nil
	})
	if err != nil {
		t.Fatalf("walk %q: %v", path, err)
	}

	totalUnits := int64(len(discUnits)) + nonDiscUnits
	t.Logf("scan path: %s", path)
	t.Logf("files seen: %d (bytes=%d)", totalFiles, totalBytes)
	t.Logf("units: total=%d (disc=%d, non-disc=%d)", totalUnits, len(discUnits), nonDiscUnits)
	if maxFilePath != "" {
		t.Logf("largest file: %s (size=%d)", maxFilePath, maxFileSize)
	}

	// Log top disc units by aggregated size.
	type discUnit struct {
		path string
		size int64
	}
	list := make([]discUnit, 0, len(discUnits))
	for p, s := range discUnits {
		list = append(list, discUnit{path: p, size: s})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].size == list[j].size {
			return strings.ToLower(list[i].path) < strings.ToLower(list[j].path)
		}
		return list[i].size > list[j].size
	})

	maxLog := min(30, len(list))
	for i := range maxLog {
		t.Logf("disc %3d) %s (size=%d)", i+1, list[i].path, list[i].size)
	}
}
