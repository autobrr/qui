// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
)

// resolveBackupSavePath decides which per-torrent save path (if any) to persist
// for a backup item. It returns the trimmed save path when it should be stored,
// or "" to leave placement to the torrent's category on restore.
//
// A path is stored only when the category cannot reproduce it: the torrent is
// uncategorized, the category's save path was not captured, or the two paths
// differ. This keeps the backup table free of redundant paths for ordinary
// category-managed torrents while still capturing divergent ones (cross-seed
// hardlinks, manual relocations, Auto TMM off).
func resolveBackupSavePath(rawSavePath, category string, categories map[string]models.CategorySnapshot) string {
	savePath := strings.TrimSpace(rawSavePath)
	if savePath == "" {
		return ""
	}

	if category != "" {
		if snap, ok := categories[category]; ok {
			if normalizeSavePathForCompare(savePath) == normalizeSavePathForCompare(snap.SavePath) {
				return ""
			}
		}
	}

	return savePath
}

// normalizeSavePathForCompare canonicalises an opaque qBittorrent-side path for
// equality checks only. It trims surrounding whitespace and trailing separators
// but otherwise leaves the path verbatim: these paths may be POSIX or Windows
// style and must never be run through filepath.Clean/FromSlash, which would
// corrupt a foreign-host path.
func normalizeSavePathForCompare(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), `/\`)
}
