// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"regexp"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

const (
	categorySavePathVariable = "CategorySavePath"
	defaultSavePathVariable  = "DefaultSavePath"
)

func usesSavePathVariable(path string) bool {
	return strings.Contains(path, categorySavePathVariable) || strings.Contains(path, defaultSavePathVariable)
}

// rulesUseSavePathVariables reports whether any enabled rule's move path or
// export save path references .CategorySavePath or .DefaultSavePath.
func rulesUseSavePathVariables(rules []*models.Automation) bool {
	for _, rule := range rules {
		if rule == nil || !rule.Enabled || rule.Conditions == nil {
			continue
		}
		if move := rule.Conditions.Move; move != nil && move.Enabled && usesSavePathVariable(move.Path) {
			return true
		}
		if export := rule.Conditions.ExportToInstance; export != nil && export.Enabled && usesSavePathVariable(export.SavePath) {
			return true
		}
	}
	return false
}

// qbtInvalidPathChars matches Utils::Fs::toValidPath, which qBittorrent applies
// to a category name it turns into a folder (same as orphanscan's copy).
var qbtInvalidPathChars = regexp.MustCompile(`[:?"*<>|]+`)

// buildCategorySavePaths resolves every category's save path the way
// qBittorrent's categorySavePath does for Auto TMM. The "" key holds the
// default save path, for uncategorized torrents. nest is
// SyncManager.CategorySavePathsNest: whether an empty-path subcategory goes under
// its parent's path or under the default path with its full name.
func buildCategorySavePaths(categories map[string]qbt.Category, defaultSavePath string, nest bool) map[string]string {
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
			p = name
			if i := strings.LastIndex(name, "/"); nest && i >= 0 {
				base, p = resolve(name[:i]), name[i+1:]
			}
			p = qbtInvalidPathChars.ReplaceAllString(p, " ")
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
