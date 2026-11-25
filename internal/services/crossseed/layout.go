// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path"
	"regexp"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/stringutils"
)

type TorrentLayout string

const (
	LayoutUnknown  TorrentLayout = "unknown"
	LayoutFiles    TorrentLayout = "files"
	LayoutArchives TorrentLayout = "archives"
)

var rarVolumeSuffix = regexp.MustCompile(`(?i)\.[r-z][0-9]{2}$`)

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
	if rarVolumeSuffix.MatchString(nameLower) {
		return true
	}

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
