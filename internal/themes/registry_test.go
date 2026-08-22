// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package themes

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestRegistryIDsPinned pins the derived id of every committed free theme.
// These ids are stored in browsers and in the theme_settings table, so a
// change here breaks every user's saved selection: the id rule is frozen.
func TestRegistryIDsPinned(t *testing.T) {
	want := map[string]struct{}{
		"minimal":         {},
		"autobrr":         {},
		"the-kyle":        {},
		"nightwalker":     {},
		"napster":         {},
		"swizzin":         {},
		"kanagawa-dragon": {},
		"kanagawa-wave":   {},
	}

	for _, theme := range All() {
		if theme.Premium {
			continue
		}
		_, ok := want[theme.ID]
		require.True(t, ok, "unexpected free theme id %q (new theme? add it here)", theme.ID)
		delete(want, theme.ID)
	}
	require.Empty(t, want, "missing free themes")
}

// TestRegistryOrder pins the catalog order: the default first, then display
// names case-insensitively, so lowercase names like "autobrr" do not sink to
// the bottom.
func TestRegistryOrder(t *testing.T) {
	all := All()
	require.Equal(t, "minimal", all[0].ID)
	for i := 2; i < len(all); i++ {
		require.LessOrEqual(t,
			strings.ToLower(all[i-1].Name), strings.ToLower(all[i].Name),
			"themes %q and %q out of order", all[i-1].Name, all[i].Name)
	}
}

// TestParsePremiumFromDir pins the premium classification: location is
// authoritative, so a file under assets/premium/ without a metadata header
// must never be served as a free theme.
func TestParsePremiumFromDir(t *testing.T) {
	noHeader := ":root {\n  --primary: red;\n}\n"
	require.True(t, parse(noHeader, "mystery", true).Premium, "premium dir must classify premium without a header")
	require.False(t, parse(noHeader, "mystery", false).Premium)
	require.True(t, parse("/* @premium: true */\n"+noHeader, "mystery", false).Premium, "header alone still classifies premium")
}

func TestGenerateID(t *testing.T) {
	require.Equal(t, "the-kyle", GenerateID("The Kyle"))
	require.Equal(t, "amber-minimal", GenerateID("Amber Minimal"))
	require.Equal(t, "tokyo-night", GenerateID("Tokyo Night"))
	require.Equal(t, "a-b-c", GenerateID("  A/B & C!  "))
}

func TestRegistryParse(t *testing.T) {
	minimalFound := false
	for _, theme := range All() {
		require.NotEmpty(t, theme.ID)
		require.NotEmpty(t, theme.Name)
		require.NotEmpty(t, theme.CSS)
		require.NotEmpty(t, theme.Preview.Light, "theme %s has no light preview vars", theme.ID)
		require.NotEmpty(t, theme.Preview.Dark, "theme %s has no dark preview vars", theme.ID)
		if theme.ID == "minimal" {
			minimalFound = true
			require.False(t, theme.Premium)
			require.Equal(t, "Minimal", theme.Name)
			require.Contains(t, theme.Preview.Light, "--primary")
		}
	}
	require.True(t, minimalFound)
	require.Equal(t, "minimal", All()[0].ID, "default theme must sort first")
	require.True(t, Exists("minimal"))
	require.False(t, Exists("nope"))
}

// Locked previews render outside their stylesheet, so every swatch must be a
// concrete color, never an unresolved var() reference (e.g. Catppuccin's
// "--primary: var(--variation-color)").
func TestPreviewVarsAreConcrete(t *testing.T) {
	for _, theme := range All() {
		for mode, vars := range map[string]map[string]string{"light": theme.Preview.Light, "dark": theme.Preview.Dark} {
			for name, value := range vars {
				require.NotContains(t, value, "var(", "theme %s %s preview %s is unresolved", theme.ID, mode, name)
			}
		}
	}
}

func TestExtractPreviewVars(t *testing.T) {
	css := `:root {
  --variation-mauve: oklch(0.55 0.25 297);
  --variation-color: var(--variation-mauve);
  --primary: var(--variation-color);
  --secondary: blue;
  --cycle-a: var(--cycle-b);
  --cycle-b: var(--cycle-a);
  --accent: var(--cycle-a);
}

.dark {
  --primary: var(--variation-color);
  --secondary: navy;
  --accent: var(--missing);
}
`
	light := extractPreviewVars(css, ":root")
	require.Equal(t, "oklch(0.55 0.25 297)", light["--primary"])
	require.Equal(t, "blue", light["--secondary"])
	require.NotContains(t, light, "--accent", "cyclic reference must drop the swatch")

	// The dark block resolves through :root for variables it does not redefine.
	dark := extractPreviewVars(css, ".dark")
	require.Equal(t, "oklch(0.55 0.25 297)", dark["--primary"])
	require.Equal(t, "navy", dark["--secondary"])
	require.NotContains(t, dark, "--accent", "unresolvable reference must drop the swatch")

	require.Nil(t, extractPreviewVars(css, ".missing"))
}
