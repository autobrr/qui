// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package themes

import (
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
