// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package themes embeds the built-in theme CSS files and exposes their
// metadata so the API can serve and validate them. The CSS is the single
// source of truth; the frontend parses the full stylesheet, this package
// only reads the small metadata header and the preview color variables.
package themes

import (
	"cmp"
	"embed"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"
)

//go:embed assets
var assetsFS embed.FS

// Theme is one built-in theme: its metadata and raw CSS.
type Theme struct {
	ID          string
	Name        string
	Description string
	Premium     bool
	CSS         string
	// Preview holds the light/dark swatch colors shown for locked premium
	// themes, extracted from --primary/--secondary/--accent.
	Preview Preview
}

type Preview struct {
	Light map[string]string `json:"light"`
	Dark  map[string]string `json:"dark"`
}

var (
	nameRe        = regexp.MustCompile(`@name:\s*(.+?)\s*(?:\n|\*)`)
	descriptionRe = regexp.MustCompile(`@description:\s*(.+?)\s*(?:\n|\*)`)
	premiumRe     = regexp.MustCompile(`@premium:\s*true`)
	slugRe        = regexp.MustCompile(`[^a-z0-9]+`)
)

// GenerateID derives a theme id from its name. The rule is frozen: derived
// ids are stored in the database and in browsers, so it must never change.
// TestRegistryIDsPinned enforces this for every committed theme.
func GenerateID(name string) string {
	return strings.Trim(slugRe.ReplaceAllString(strings.ToLower(name), "-"), "-")
}

var registry = load()

// All returns every embedded theme, "minimal" (the default) first, then by
// name, matching the order the old bundled loader presented.
func All() []Theme {
	return registry
}

// Exists reports whether id names an embedded built-in theme.
func Exists(id string) bool {
	return slices.ContainsFunc(registry, func(t Theme) bool { return t.ID == id })
}

func load() []Theme {
	var themes []Theme
	for _, dir := range []string{"assets", "assets/premium"} {
		premium := dir == "assets/premium"
		paths, _ := fs.Glob(assetsFS, dir+"/*.css")
		for _, p := range paths {
			css, err := fs.ReadFile(assetsFS, p)
			if err != nil {
				continue
			}
			themes = append(themes, parse(string(css), strings.TrimSuffix(path.Base(p), ".css"), premium))
		}
	}
	slices.SortFunc(themes, func(a, b Theme) int {
		if a.ID == "minimal" {
			return -1
		}
		if b.ID == "minimal" {
			return 1
		}
		return cmp.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	})
	return themes
}

// parse reads a theme from its CSS. fromPremiumDir classifies the theme as
// premium by its location, so a missing or malformed metadata header can
// never serve a premium stylesheet ungated.
func parse(css, fallbackID string, fromPremiumDir bool) Theme {
	t := Theme{CSS: css, Premium: fromPremiumDir || premiumRe.MatchString(css)}

	if m := nameRe.FindStringSubmatch(css); m != nil {
		t.Name = m[1]
	}
	if m := descriptionRe.FindStringSubmatch(css); m != nil {
		t.Description = m[1]
	}
	t.ID = cmp.Or(GenerateID(t.Name), fallbackID)

	t.Preview = Preview{
		Light: extractPreviewVars(css, ":root"),
		Dark:  extractPreviewVars(css, ".dark"),
	}
	return t
}

// extractPreviewVars pulls the swatch variables out of one selector block.
// Indirect values ("--primary: var(--variation-color)") resolve against the
// block itself and then :root, so locked previews carry concrete colors that
// render outside the stylesheet.
func extractPreviewVars(css, selector string) map[string]string {
	own := blockVars(css, selector)
	if own == nil {
		return nil
	}
	root := own
	if selector != ":root" {
		root = blockVars(css, ":root")
	}

	vars := make(map[string]string, 3)
	for _, name := range []string{"--primary", "--secondary", "--accent"} {
		if v, ok := resolveVar(own[name], own, root); ok {
			vars[name] = v
		}
	}
	return vars
}

// blockVars parses every custom property in the first block for selector.
func blockVars(css, selector string) map[string]string {
	start := strings.Index(css, selector+" {")
	if start < 0 {
		start = strings.Index(css, selector+"{")
	}
	if start < 0 {
		return nil
	}
	block := css[start:]
	if end := strings.Index(block, "}"); end >= 0 {
		block = block[:end]
	}

	vars := make(map[string]string)
	for line := range strings.Lines(block) {
		name, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		name = strings.TrimSpace(name)
		if !strings.HasPrefix(name, "--") {
			continue
		}
		vars[name] = strings.TrimSuffix(strings.TrimSpace(value), ";")
	}
	return vars
}

// resolveVar follows var(--x) references through own, then root. An
// unresolvable or cyclic reference drops the swatch.
func resolveVar(value string, own, root map[string]string) (string, bool) {
	for range 10 {
		inner, isRef := strings.CutPrefix(value, "var(")
		if !isRef {
			return value, value != ""
		}
		name := strings.TrimSpace(strings.TrimSuffix(inner, ")"))
		value = cmp.Or(own[name], root[name])
	}
	return "", false
}
