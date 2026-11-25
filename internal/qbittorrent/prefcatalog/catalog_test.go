// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package prefcatalog

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const shownKeysPath = "../../../web/src/lib/preference-catalog-keys.json"

func appPreferencesKeys() []string {
	var keys []string
	typ := reflect.TypeFor[qbt.AppPreferences]()
	for field := range typ.Fields() {
		if key, _, _ := strings.Cut(field.Tag.Get("json"), ","); key != "" && key != "-" {
			keys = append(keys, key)
		}
	}
	return keys
}

func TestCatalogCoversAppPreferences(t *testing.T) {
	structKeys := appPreferencesKeys()

	var missing []string
	for _, key := range structKeys {
		if _, ok := byKey[key]; !ok {
			missing = append(missing, key)
		}
	}
	assert.Empty(t, missing, "AppPreferences keys without a catalog entry")

	seen := map[string]bool{}
	for _, entry := range catalog {
		assert.False(t, seen[entry.Key], "duplicate catalog entry %q", entry.Key)
		seen[entry.Key] = true
		assert.Contains(t, structKeys, entry.Key, "catalog entry without an AppPreferences field")
		assert.NotEmpty(t, entry.Tab, entry.Key)
		if entry.Mode != Editable {
			assert.NotEmpty(t, entry.Reason, "%q needs a reason", entry.Key)
		}
		if entry.Mode != NotShown {
			assert.NotEmpty(t, entry.Section, entry.Key)
			assert.NotEmpty(t, entry.Control, entry.Key)
		}
		if entry.DependsOn != "" {
			assert.Equal(t, ControlSwitch, byKey[entry.DependsOn].Control, "%q depends on %q", entry.Key, entry.DependsOn)
		}
	}
}

func TestShownKeysFileIsCurrent(t *testing.T) {
	var shown []string
	for _, entry := range catalog {
		if entry.Mode != NotShown {
			shown = append(shown, entry.Key)
		}
	}
	slices.Sort(shown)
	want, err := json.MarshalIndent(shown, "", "  ")
	require.NoError(t, err)
	want = append(want, '\n')

	got, _ := os.ReadFile(shownKeysPath)
	if bytes.Equal(got, want) {
		return
	}
	require.NoError(t, os.WriteFile(shownKeysPath, want, 0o600))
	t.Fatalf("%s was out of date and is now rewritten; commit it", shownKeysPath)
}
