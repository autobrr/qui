// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	localbackend "github.com/autobrr/qui/internal/fsops/local"
)

func TestBuildMissingFilesResultRejectedPathLeavesUnknown(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "present.mkv"), []byte("present"), 0o600))

	torrent := qbt.Torrent{Hash: "hash", SavePath: dir, Progress: 1}
	result := buildMissingFilesResult(
		context.Background(),
		localbackend.NewBackend(),
		map[string]qbt.Torrent{torrent.Hash: torrent},
		map[string]qbt.TorrentFiles{
			torrent.Hash: {
				{Name: "present.mkv"},
				{Name: `AC\DC.mkv`},
			},
		},
	)

	require.NotContains(t, result, torrent.Hash)
}
