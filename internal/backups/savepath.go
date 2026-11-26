// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package backups

import (
	"strings"

	"github.com/autobrr/qui/internal/models"
)

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

func normalizeSavePathForCompare(p string) string {
	return strings.TrimRight(strings.TrimSpace(p), `/\`)
}
