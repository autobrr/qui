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

func ruleTemplatesUseSavePaths(ac *models.ActionConditions) bool {
	if move := ac.Move; move != nil && move.Enabled && usesSavePathVariable(move.Path) {
		return true
	}
	if export := ac.ExportToInstance; export != nil && export.Enabled && usesSavePathVariable(export.SavePath) {
		return true
	}
	transfer := ac.TransferToInstance
	return transfer != nil && transfer.Enabled && usesSavePathVariable(transfer.SavePath)
}

func buildCategorySavePaths(categories map[string]qbt.Category, defaultSavePath string, rule qbittorrent.CategoryPathRule) map[string]string {
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
			p = g.join(base, qbittorrent.CategoryFolderName(folder, rule))
		} else if !g.isAbs(p) {
			p = g.join(defaultSavePath, p)
		}
		p = g.trimTrailingSeparators(p)
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

type savePathGrammar struct {
	windows bool
}

func (g savePathGrammar) separators() string {
	if g.windows {
		return `/\`
	}
	return "/"
}

func (g savePathGrammar) isAbs(p string) bool {
	if strings.HasPrefix(p, "/") {
		return true
	}
	if !g.windows {
		return false
	}
	return strings.HasPrefix(p, `\`) || len(p) >= 2 && p[1] == ':' && ('a' <= p[0] && p[0] <= 'z' || 'A' <= p[0] && p[0] <= 'Z')
}

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

func (g savePathGrammar) join(base, rel string) string {
	if g.windows {
		return strings.TrimRight(base, g.separators()) + `\` + strings.ReplaceAll(rel, "/", `\`)
	}
	return strings.TrimRight(base, "/") + "/" + rel
}
