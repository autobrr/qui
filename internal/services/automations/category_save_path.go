// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	categorySavePathVariable = "CategorySavePath"
	defaultSavePathVariable  = "DefaultSavePath"
)

func usesSavePathVariable(path string) bool {
	return strings.Contains(path, categorySavePathVariable) || strings.Contains(path, defaultSavePathVariable)
}

// ruleTemplatesUseSavePaths reports whether an enabled move path or export save
// path references .CategorySavePath or .DefaultSavePath.
func ruleTemplatesUseSavePaths(ac *models.ActionConditions) bool {
	if move := ac.Move; move != nil && move.Enabled && usesSavePathVariable(move.Path) {
		return true
	}
	export := ac.ExportToInstance
	return export != nil && export.Enabled && usesSavePathVariable(export.SavePath)
}

// buildCategorySavePaths resolves every category's save path the way
// qBittorrent's categorySavePath does for Auto TMM. The "" key holds the
// default save path, for uncategorized torrents. It keeps its own separator
// handling because qbittorrent.CategorySavePath needs a backend's path dialect,
// and a move needs no file access.
func buildCategorySavePaths(categories map[string]qbt.Category, defaultSavePath string, rule qbittorrent.CategoryPathRule) map[string]string {
	// qBittorrent reports its default save path absolute and with native
	// separators, so only a POSIX host's starts with "/".
	g := savePathGrammar{windows: !strings.HasPrefix(defaultSavePath, "/")}
	resolved := make(map[string]string, len(categories)+1)
	resolved[""] = g.trimTrailingSeparators(defaultSavePath)

	var resolve func(name string) string
	resolve = func(name string) string {
		if p, ok := resolved[name]; ok {
			return p
		}
		p := categories[name].SavePath
		if p == "" {
			base, folder := defaultSavePath, name
			if parent, leaf, found := strings.CutLast(name, "/"); rule.Nest && found {
				base, folder = resolve(parent), leaf
			}
			// A folder name is always relative, even "a:b" from a qBittorrent 5.3+ Linux build.
			p = g.join(base, qbittorrent.CategoryFolderName(folder, rule))
		} else if !g.isAbs(p) {
			p = g.join(defaultSavePath, p)
		}
		p = g.trimTrailingSeparators(p)
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

// savePathGrammar reads paths the way qBittorrent's Path does on its host,
// independent of the OS qui runs on. On a POSIX host a backslash is a file
// name character.
type savePathGrammar struct {
	windows bool
}

func (g savePathGrammar) separators() string {
	if g.windows {
		return `/\`
	}
	return "/"
}

// isAbs follows QDir::isAbsolutePath: on Windows a leading separator or a
// drive letter, even "C:x", counts as absolute.
func (g savePathGrammar) isAbs(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	if !g.windows {
		return false
	}
	return strings.HasPrefix(p, `\`) || len(p) >= 2 && p[1] == ':' && ('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

// trimTrailingSeparators keeps "{{.CategorySavePath}}/x" from rendering "//x", which never matches the stored path.
func (g savePathGrammar) trimTrailingSeparators(p string) string {
	trimmed := strings.TrimRight(p, g.separators())
	switch {
	case trimmed == "" && p != "":
		return p[:1]
	case g.windows && len(trimmed) == 2 && trimmed[1] == ':' && len(p) > 2:
		return p[:3]
	}
	return trimmed
}

// join puts rel, whose slashes may separate category names, under base with
// the host's separator.
func (g savePathGrammar) join(base, rel string) string {
	if g.windows {
		return strings.TrimRight(base, g.separators()) + `\` + strings.ReplaceAll(rel, "/", `\`)
	}
	return strings.TrimRight(base, "/") + "/" + rel
}
