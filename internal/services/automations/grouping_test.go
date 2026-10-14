// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/releases"
)

// The JSON reference lists GroupKeys as the allowed keys, so each one must build a group key.
func TestBuildGroupKey_AcceptsEachGroupKey(t *testing.T) {
	t.Parallel()

	torrent := qbt.Torrent{
		Hash:        "abc",
		Name:        "Movie.Title.2024.2160p.BluRay.DTS-HD.MA.5.1.HDR.x265-GRP",
		ContentPath: "/data/Movie.Title.2024.2160p.BluRay.DTS-HD.MA.5.1.HDR.x265-GRP",
		SavePath:    "/data",
		Tracker:     "https://tracker.example.invalid/announce",
	}
	evalCtx := &EvalContext{
		ReleaseParser:           releases.NewDefaultParser(),
		HardlinkSignatureByHash: map[string]string{"abc": "sig"},
	}

	for _, key := range GroupKeys {
		_, ok := buildGroupKey([]string{key}, torrent, &qbittorrent.SyncManager{}, evalCtx)
		require.True(t, ok, key)
	}

	_, ok := buildGroupKey([]string{"unknown"}, torrent, &qbittorrent.SyncManager{}, evalCtx)
	require.False(t, ok)
}
