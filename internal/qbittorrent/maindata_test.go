// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

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
	checkedData   *qbt.MainData
	uncheckedData *qbt.MainData
}

func (p testMainDataProvider) GetData() *qbt.MainData {
	return p.checkedData
}

func (p testMainDataProvider) GetDataUnchecked() *qbt.MainData {
	return p.uncheckedData
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

func TestResolveMainDataUsesUncheckedSnapshotForCachedReads(t *testing.T) {
	t.Parallel()

	cached := &qbt.MainData{
		ServerState: qbt.ServerState{ConnectionStatus: "cached"},
	}
	checked := &qbt.MainData{
		ServerState: qbt.ServerState{ConnectionStatus: "checked"},
	}

	got := resolveMainData(testMainDataProvider{
		checkedData:   checked,
		uncheckedData: cached,
	}, mainDataReadCached)

	require.Same(t, cached, got)
}

func TestResolveMainDataUsesCheckedSnapshotForDefaultReads(t *testing.T) {
	t.Parallel()

	cached := &qbt.MainData{
		ServerState: qbt.ServerState{ConnectionStatus: "cached"},
	}
	checked := &qbt.MainData{
		ServerState: qbt.ServerState{ConnectionStatus: "checked"},
	}

	got := resolveMainData(testMainDataProvider{
		checkedData:   checked,
		uncheckedData: cached,
	}, mainDataRead)

	require.Same(t, checked, got)
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
