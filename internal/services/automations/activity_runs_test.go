// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestActivityRunStorePaging(t *testing.T) {
	store := newActivityRunStore(24*time.Hour, 10)
	items := []ActivityRunTorrent{
		{Hash: "a", Name: "Alpha"},
		{Hash: "b", Name: "Beta"},
		{Hash: "c", Name: "Charlie"},
	}

	store.Put(1, 99, items)

	page, ok := store.Get(99, 1, 0, 2)
	require.True(t, ok)
	require.Equal(t, 3, page.Total)
	require.Len(t, page.Items, 2)

	page, ok = store.Get(99, 1, 2, 2)
	require.True(t, ok)
	require.Len(t, page.Items, 1)

	_, ok = store.Get(100, 1, 0, 2)
	require.False(t, ok)
}

func TestActivityRunStoreMaxRuns(t *testing.T) {
	store := newActivityRunStore(24*time.Hour, 2)

	store.Put(1, 1, []ActivityRunTorrent{{Hash: "a"}})
	store.Put(2, 1, []ActivityRunTorrent{{Hash: "b"}})
	store.Put(3, 1, []ActivityRunTorrent{{Hash: "c"}})

	_, ok := store.Get(1, 1, 0, 10)
	require.False(t, ok)

	_, ok = store.Get(1, 2, 0, 10)
	require.True(t, ok)

	_, ok = store.Get(1, 3, 0, 10)
	require.True(t, ok)
}
