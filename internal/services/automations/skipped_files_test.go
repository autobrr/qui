// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestBuildSkippedFilesResult(t *testing.T) {
	t.Parallel()

	got := buildSkippedFilesResult(map[string]qbt.TorrentFiles{
		"partial": {{Name: "a.mkv", Priority: 1}, {Name: "b.mkv", Priority: 0}},
		"full":    {{Name: "a.mkv", Priority: 1}, {Name: "b.mkv", Priority: 7}},
		"nometa":  {},
	})

	require.Equal(t, map[string]bool{"partial": true, "full": false}, got)
}
