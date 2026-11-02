// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"iter"
	"path"
	"regexp"
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

var rarVolumeSuffix = regexp.MustCompile(`(?i)\.[r-z][0-9]{2}$`)

// classifyTorrentLayout inspects the largest non-ignored file inside a torrent
// to determine whether the content is stored as archives (.rar/.r00/etc.) or as
// regular media files (.mkv/.mp4/.flac/etc.). This heuristic mirrors how scene
// releases are structured in practice—the main payload is always the largest
// file, and any side files (.nfo, .sfv, etc.) are tiny.
func classifyTorrentLayout(files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) TorrentLayout {
	return classifyLayout(func(yield func(string, int64) bool) {
		for _, f := range files {
			if !yield(f.Name, f.Size) {
				return
			}
		}
	}, normalizer.Normalize)
}

// IsArchivedRelease reports whether a release stores its payload in archive
// volumes: its largest file is an archive volume once samples, proofs, and
// extras are left out. Names are slash paths relative to the release. Library
// Scan and Dir Scan share this test.
func IsArchivedRelease(files iter.Seq2[string, int64]) bool {
	return classifyLayout(files, func(name string) string {
		return strings.ToLower(strings.TrimSpace(name))
	}) == LayoutArchives
}

// IsArchiveVolume reports whether a file name is an archive volume, such as a
// .rar, .r00, .zip, or .7z file.
func IsArchiveVolume(name string) bool {
	return isArchiveFilename(strings.ToLower(name))
}

func classifyLayout(files iter.Seq2[string, int64], normalize func(string) string) TorrentLayout {
	var largestName string
	var largestSize int64

	for name, size := range files {
		if isIgnoredFileName(normalize(name)) {
			continue
		}
		if size > largestSize {
			largestSize = size
			largestName = name
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
	if rarVolumeSuffix.MatchString(nameLower) {
		return true
	}

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

	switch path.Ext(nameLower) {
	case ".rar", ".zip", ".7z":
		return true
	}

	return false
}
