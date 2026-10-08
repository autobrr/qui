// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"regexp"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
)

// qbtInvalidPathChars is the regex from Utils::Fs::toValidPath; slashes are
// absent there because they separate path segments.
var qbtInvalidPathChars = regexp.MustCompile(`[:?"*<>|]+`)

// toValidPath converts a category name into the relative path qBittorrent
// creates for it, so a category called "movies:hd" is protected at "movies hd".
func toValidPath(name string) string {
	return qbtInvalidPathChars.ReplaceAllString(name, " ")
}

// CategorySavePath mirrors SessionImpl::categorySavePath. Returns "" when the
// destination cannot be determined; a depth cap here would silently drop
// protection for a deeply nested category.
func CategorySavePath(d fsops.PathDialect, name string, categories map[string]qbt.Category, defaultSavePath string, useSubcategories bool) string {
	savePath := categories[name].SavePath
	if savePath != "" {
		savePath = d.Clean(savePath)
		if d.IsAbs(savePath) {
			return savePath
		}
		if defaultSavePath == "" {
			return ""
		}
		return d.Join(defaultSavePath, savePath)
	}

	// Category names are slash-delimited whatever the host separator is.
	if useSubcategories {
		if i := strings.LastIndex(name, "/"); i > 0 {
			parent := CategorySavePath(d, name[:i], categories, defaultSavePath, useSubcategories)
			if parent == "" {
				return ""
			}
			// qBittorrent converts only the last segment and resolves the rest
			// through the parent category.
			return d.Join(parent, d.FromSlash(toValidPath(name[i+1:])))
		}
	}

	if defaultSavePath == "" {
		return ""
	}
	return d.Join(defaultSavePath, d.FromSlash(toValidPath(name)))
}
