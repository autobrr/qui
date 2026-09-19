// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

const categorySavePathVariable = "CategorySavePath"

// rulesUseCategorySavePath reports whether any enabled rule's move path or
// export save path references .CategorySavePath.
func rulesUseCategorySavePath(rules []*models.Automation) bool {
	for _, rule := range rules {
		if rule == nil || !rule.Enabled || rule.Conditions == nil {
			continue
		}
		if move := rule.Conditions.Move; move != nil && move.Enabled && strings.Contains(move.Path, categorySavePathVariable) {
			return true
		}
		if export := rule.Conditions.ExportToInstance; export != nil && export.Enabled && strings.Contains(export.SavePath, categorySavePathVariable) {
			return true
		}
	}
	return false
}

// buildCategorySavePaths resolves every category's save path the way
// qBittorrent's categorySavePath does for Auto TMM. The "" key holds the
// default save path, for uncategorized torrents.
func buildCategorySavePaths(categories map[string]qbt.Category, defaultSavePath string) map[string]string {
	resolved := make(map[string]string, len(categories)+1)
	resolved[""] = defaultSavePath

	var resolve func(name string) string
	resolve = func(name string) string {
		if p, ok := resolved[name]; ok {
			return p
		}
		p := categories[name].SavePath
		base := defaultSavePath
		if p == "" {
			// qBittorrent uses the parent's resolved path plus the leaf name,
			// even when subcategories are turned off.
			parent, leaf := "", name
			if i := strings.LastIndex(name, "/"); i >= 0 {
				parent, leaf = name[:i], name[i+1:]
			}
			base, p = resolve(parent), leaf
		}
		if !isAbsoluteRemotePath(p) {
			p = joinRemotePath(base, p)
		}
		// A missing parent still resolves implicitly, but must not become a
		// known category: a torrent in it has no category save path.
		if _, known := categories[name]; known {
			resolved[name] = p
		}
		return p
	}

	for name := range categories {
		resolve(name)
	}
	return resolved
}

// isAbsoluteRemotePath reports whether p is absolute on either a POSIX or a
// Windows qBittorrent host, independent of the OS qui runs on.
func isAbsoluteRemotePath(p string) bool {
	if strings.HasPrefix(p, "/") || strings.HasPrefix(p, `\`) {
		return true
	}
	return len(p) >= 2 && p[1] == ':' && ((p[0] >= 'a' && p[0] <= 'z') || (p[0] >= 'A' && p[0] <= 'Z'))
}

// joinRemotePath joins with the separator the base already uses, so a Windows
// default save path keeps its backslashes.
func joinRemotePath(base, rel string) string {
	sep := "/"
	if strings.Contains(base, `\`) && !strings.Contains(base, "/") {
		sep = `\`
	}
	return strings.TrimRight(base, `/\`) + sep + rel
}
