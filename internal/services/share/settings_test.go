// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package share_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/services/share"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestTheShareSettingsKeepEveryFieldPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, db dbinterface.Querier) {
		store := share.NewSettingsStore(db)
		for _, want := range []share.Options{
			{MaxActive: 3, PeersPerShare: 5, TrackerURL: "https://tracker.example.com/room/announce", Defaults: share.Defaults{Days: 2}},
			{MaxActive: 1, PeersPerShare: 1, Defaults: share.Defaults{Days: 30, EndAtFirstDownload: true, FirstDownloaderOnly: true}},
		} {
			require.NoError(t, store.SaveSettings(t.Context(), want))
			got, err := store.Settings(t.Context())
			require.NoError(t, err)
			require.Equal(t, want, got)
		}
	})
}

func TestTheShareSettingsRefuseValuesThatShareCannotUse(t *testing.T) {
	t.Parallel()
	store := share.NewSettingsStore(testdb.NewMigratedSQLite(t, "share-settings"))
	valid := share.Options{MaxActive: 10, PeersPerShare: 20, Defaults: share.Defaults{Days: 7}}
	for name, edit := range map[string]func(o *share.Options){
		"no active Share":        func(o *share.Options) { o.MaxActive = 0 },
		"no day":                 func(o *share.Options) { o.Defaults.Days = 0 },
		"a tracker URL with ftp": func(o *share.Options) { o.TrackerURL = "ftp://tracker.example.com/announce" },
	} {
		t.Run(name, func(t *testing.T) {
			invalid := valid
			edit(&invalid)
			require.ErrorIs(t, store.SaveSettings(t.Context(), invalid), share.ErrInvalidSettings)
		})
	}
}
