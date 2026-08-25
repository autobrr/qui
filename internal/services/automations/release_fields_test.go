// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/releases"
)

func TestTorrentEffectiveName_TVKeys(t *testing.T) {
	t.Parallel()

	ctx := &EvalContext{ReleaseParser: releases.NewDefaultParser()}
	name := func(n string) string {
		return torrentEffectiveName(qbt.Torrent{Name: n}, ctx)
	}

	pack := name("Show.Name.S01.1080p.WEB.H264-GRP")
	episode := name("Show.Name.S01E05.1080p.WEB.H264-GRP")
	twoParter := name("Show.Name.S01E05E06.1080p.WEB.H264-GRP")

	require.Equal(t, "show name|s01", pack)
	require.Equal(t, "show name|s01e05", episode)
	// A range parses with Episode 0 like a pack, but it is its own item: a
	// keep-newest rule grouped by effective name must never see the two-parter
	// and the season pack as one group.
	require.Equal(t, "show name|s01e05e06", twoParter)
	require.NotEqual(t, pack, twoParter)
	require.NotEqual(t, episode, twoParter)
}
