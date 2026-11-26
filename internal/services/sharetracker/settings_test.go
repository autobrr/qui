// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharetracker_test

import (
	"context"
	"net/netip"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/sharetracker"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func engines(t *testing.T, run func(t *testing.T, store *sharetracker.SettingsStore)) {
	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		run(t, sharetracker.NewSettingsStore(testdb.NewMigratedSQLite(t, "share-tracker")))
	})
	t.Run("postgres", func(t *testing.T) {
		run(t, sharetracker.NewSettingsStore(testdb.NewMigratedPostgres(t, "share-tracker")))
	})
}

func TestTheTrackerDoesNotListenWithTheDefaultSettingsPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, store *sharetracker.SettingsStore) {
		settings, err := store.Settings(t.Context())
		require.NoError(t, err)
		require.Zero(t, settings.Port)
		require.Empty(t, settings.PublicAddress)
		require.Empty(t, settings.TrustedProxies)

		s := &shares{address: map[int64]netip.AddrPort{}}
		s.set(share())
		tracker := sharetracker.New(sharetracker.Config{Shares: s, Reports: &reports{}, Host: "127.0.0.1"})
		t.Cleanup(func() { require.NoError(t, tracker.Stop(context.Background())) })
		port := freePort(t)
		require.NoError(t, tracker.Start(t.Context(), sharetracker.Settings{Port: port}))
		requireListening(t, port, true)

		require.NoError(t, tracker.Start(t.Context(), settings))
		requireListening(t, port, false)
	})
}

func TestTheSettingsKeepEveryFieldPostgresIntegration(t *testing.T) {
	t.Parallel()
	engines(t, func(t *testing.T, store *sharetracker.SettingsStore) {
		for _, want := range []sharetracker.Settings{
			{Port: 6969, PublicAddress: "https://share.example.com", TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8", "::1", "fd00::/8"}},
			{Port: 0, PublicAddress: "http://203.0.113.10:6969", TrustedProxies: []string{}},
			{Port: 65535},
		} {
			require.NoError(t, store.SaveSettings(t.Context(), want))
			got, err := store.Settings(t.Context())
			require.NoError(t, err)
			require.Equal(t, want.Port, got.Port)
			require.Equal(t, want.PublicAddress, got.PublicAddress)
			require.ElementsMatch(t, want.TrustedProxies, got.TrustedProxies)
		}
	})
}

func TestTheSettingsRefuseValuesThatTheTrackerCannotUse(t *testing.T) {
	t.Parallel()
	store := sharetracker.NewSettingsStore(testdb.NewMigratedSQLite(t, "share-tracker"))
	saved := sharetracker.Settings{Port: 6969, PublicAddress: "https://share.example.com", TrustedProxies: []string{"127.0.0.1"}}
	require.NoError(t, store.SaveSettings(t.Context(), saved))

	for name, invalid := range map[string]sharetracker.Settings{
		"negative port":                 {Port: -1},
		"port above 65535":              {Port: 65536},
		"public address with ftp":       {Port: 6969, PublicAddress: "ftp://share.example.com"},
		"public address with a login":   {Port: 6969, PublicAddress: "https://user:password@share.example.com"},
		"public address with a query":   {Port: 6969, PublicAddress: "https://share.example.com/?passkey=x"},
		"trusted proxy that is a name":  {Port: 6969, TrustedProxies: []string{"proxy.example.com"}},
		"trusted proxy with a bad mask": {Port: 6969, TrustedProxies: []string{"10.0.0.0/33"}},
		"public address that ends in ?": {Port: 6969, PublicAddress: "https://share.example.com/?"},
		"public address that ends in #": {Port: 6969, PublicAddress: "https://share.example.com/#"},
	} {
		t.Run(name, func(t *testing.T) {
			require.ErrorIs(t, store.SaveSettings(t.Context(), invalid), sharetracker.ErrInvalidSettings)
			got, err := store.Settings(t.Context())
			require.NoError(t, err)
			require.Equal(t, saved, got)
		})
	}
}
