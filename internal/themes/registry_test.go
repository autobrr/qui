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
	want := map[string]bool{
		"minimal":         false,
		"autobrr":         false,
		"the-kyle":        false,
		"nightwalker":     false,
		"napster":         false,
		"swizzin":         false,
		"kanagawa-dragon": false,
		"kanagawa-wave":   false,
	}

	for _, theme := range All() {
		if theme.Premium {
			continue
		}
		premium, ok := want[theme.ID]
		require.True(t, ok, "unexpected free theme id %q (new theme? add it here)", theme.ID)
		require.Equal(t, premium, theme.Premium)
		delete(want, theme.ID)
	}
	require.Empty(t, want, "missing free themes")
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
