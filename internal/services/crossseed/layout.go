// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path/filepath"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/stringutils"
)

// TorrentLayout classifies how a torrent stores its payload so we can avoid
// cross-seeding across fundamentally different file structures.
type TorrentLayout string

const (
	LayoutUnknown  TorrentLayout = "unknown"
	LayoutFiles    TorrentLayout = "files"
	LayoutArchives TorrentLayout = "archives"
)

var archiveExtensions = buildArchiveExtensionSet()

func buildArchiveExtensionSet() map[string]struct{} {
	exts := map[string]struct{}{
		".rar": {},
		".zip": {},
		".7z":  {},
	}
	for i := 0; i <= 99; i++ {
		suffix := ""
		if i < 10 {
			suffix = ".r0" + strconv.Itoa(i)
		} else {
			suffix = ".r" + strconv.Itoa(i)
		}
		exts[suffix] = struct{}{}
	}
	return exts
}

// classifyTorrentLayout inspects the largest non-ignored file inside a torrent
// to determine whether the content is stored as archives (.rar/.r00/etc.) or as
// regular media files (.mkv/.mp4/.flac/etc.). This heuristic mirrors how scene
// releases are structured in practice—the main payload is always the largest
// file, and any side files (.nfo, .sfv, etc.) are tiny.
func classifyTorrentLayout(files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) TorrentLayout {
	var largestName string
	var largestSize int64

	for _, f := range files {
		if shouldIgnoreFile(f.Name, normalizer) {
			continue
		}
		if f.Size > largestSize {
			largestSize = f.Size
			largestName = f.Name
		}
	}

	if largestSize == 0 || strings.TrimSpace(largestName) == "" {
		return LayoutUnknown
	}

	nameLower := strings.ToLower(largestName)
	if isArchiveFilename(nameLower) {
		return LayoutArchives
	}

	return LayoutFiles
}

func isArchiveFilename(nameLower string) bool {
	// Handle multi-part suffixes (e.g. .tar.gz) by checking against known
	// suffix list before falling back to simple extension matching.
	archiveSuffixes := []string{
		".tar.gz",
		".tar.xz",
		".tar.bz2",
		".tar",
	}
	for _, suffix := range archiveSuffixes {
		if strings.HasSuffix(nameLower, suffix) {
			return true
		}
	}

	ext := filepath.Ext(nameLower)
	if _, ok := archiveExtensions[ext]; ok {
		return true
	}

	return false
}
