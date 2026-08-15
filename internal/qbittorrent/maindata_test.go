// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type testServerStateProvider struct {
	state qbt.ServerState
}

func (p testServerStateProvider) GetServerStateUnchecked() qbt.ServerState {
	return p.state
}

type testMainDataProvider struct {
	checkedTrackers   map[string][]string
	uncheckedTrackers map[string][]string
	categories        map[string]qbt.Category
	state             qbt.ServerState
}

func (p testMainDataProvider) GetTrackers() map[string][]string {
	return p.checkedTrackers
}

func (p testMainDataProvider) GetTrackersUnchecked() map[string][]string {
	return p.uncheckedTrackers
}

func (p testMainDataProvider) GetCategoriesUnchecked() map[string]qbt.Category {
	return p.categories
}

func (p testMainDataProvider) GetServerStateUnchecked() qbt.ServerState {
	return p.state
}

func TestMainDataServerStateCopiesState(t *testing.T) {
	t.Parallel()

	mainData := &qbt.MainData{
		ServerState: qbt.ServerState{
			ConnectionStatus: "connected",
			DlInfoSpeed:      1024,
		},
	}

	state := mainDataServerState(mainData)
	require.NotNil(t, state)
	require.Equal(t, int64(1024), state.DlInfoSpeed)

	mainData.ServerState.DlInfoSpeed = 2048
	require.Equal(t, int64(1024), state.DlInfoSpeed)
}

func TestMainDataModeForRequest(t *testing.T) {
	t.Parallel()

	synced := time.Unix(1700000000, 0)

	tests := []struct {
		name          string
		skipFreshData bool
		lastSync      time.Time
		want          mainDataReadMode
	}{
		{name: "cached read once synced", skipFreshData: true, lastSync: synced, want: mainDataReadCached},
		{name: "cold cache still reads checked", skipFreshData: true, want: mainDataRead},
		{name: "fresh data requested", lastSync: synced, want: mainDataRead},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, mainDataModeForRequest(tt.skipFreshData, tt.lastSync))
		})
	}
}

func TestResolveMainDataPicksTrackerGetterByMode(t *testing.T) {
	t.Parallel()

	provider := testMainDataProvider{
		checkedTrackers:   map[string][]string{"checked": {"abc123"}},
		uncheckedTrackers: map[string][]string{"unchecked": {"def456"}},
		categories:        map[string]qbt.Category{"movies": {Name: "movies", SavePath: "/movies"}},
		state:             qbt.ServerState{ConnectionStatus: "connected"},
	}

	tests := []struct {
		name string
		mode mainDataReadMode
		want string
	}{
		{name: "cached reads skip the freshness check", mode: mainDataReadCached, want: "unchecked"},
		{name: "fresh reads already synced", mode: mainDataReadFresh, want: "unchecked"},
		{name: "default reads ensure freshness", mode: mainDataRead, want: "checked"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := resolveMainData(provider, tt.mode)
			require.NotNil(t, got)
			require.Contains(t, got.Trackers, tt.want)
			require.Contains(t, got.Categories, "movies")
			require.Equal(t, "connected", got.ServerState.ConnectionStatus)

			// Never populated: cloning it is the cost this assembly avoids.
			require.Nil(t, got.Torrents)
		})
	}
}

func TestResolveServerStatePrefersSnapshotFallback(t *testing.T) {
	t.Parallel()

	got := resolveServerState(testServerStateProvider{
		state: qbt.ServerState{
			ConnectionStatus: "provider",
			DlInfoSpeed:      4096,
			UpInfoSpeed:      8192,
		},
	}, &qbt.ServerState{
		ConnectionStatus: "snapshot",
		DlInfoSpeed:      1024,
		UpInfoSpeed:      2048,
	})

	require.NotNil(t, got)
	require.Equal(t, "snapshot", got.ConnectionStatus)
	require.Equal(t, int64(1024), got.DlInfoSpeed)
	require.Equal(t, int64(2048), got.UpInfoSpeed)
}

func TestResolveServerStateFallsBackToUncheckedProvider(t *testing.T) {
	t.Parallel()

	got := resolveServerState(testServerStateProvider{
		state: qbt.ServerState{
			ConnectionStatus: "connected",
			DlInfoSpeed:      4096,
			UpInfoSpeed:      8192,
		},
	}, nil)

	require.NotNil(t, got)
	require.Equal(t, int64(4096), got.DlInfoSpeed)
	require.Equal(t, int64(8192), got.UpInfoSpeed)
}

func TestResolveUseSubcategoriesAlwaysEnabled(t *testing.T) {
	t.Parallel()

	got := resolveUseSubcategories(true, true, &qbt.MainData{
		ServerState: qbt.ServerState{
			ConnectionStatus: "connected",
			UseSubcategories: false,
		},
	}, nil)

	require.True(t, got)
}

func TestResolveUseSubcategoriesKeepsLegacyDisabledState(t *testing.T) {
	t.Parallel()

	got := resolveUseSubcategories(true, false, &qbt.MainData{
		ServerState: qbt.ServerState{
			ConnectionStatus: "connected",
			UseSubcategories: false,
		},
	}, map[string]qbt.Category{
		"Movies/HD": {Name: "Movies/HD"},
	})

	require.False(t, got)
}
