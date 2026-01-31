// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestClassifyTorrentLayout(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		files  qbt.TorrentFiles
		expect TorrentLayout
	}{
		{
			name: "single mkv with sidecar nfo",
			files: qbt.TorrentFiles{
				{Name: "Show.S01E01.1080p.WEB-DL.mkv", Size: 4 << 30},
				{Name: "Show.S01E01.nfo", Size: 1024},
			},
			expect: LayoutFiles,
		},
		{
			name: "rar multi-part release",
			files: qbt.TorrentFiles{
				{Name: "Release.part01.rar", Size: 2 << 30},
				{Name: "Release.part02.r00", Size: 2 << 30},
				{Name: "Release.sfv", Size: 2048},
			},
			expect: LayoutArchives,
		},
		{
			name: "gz archive",
			files: qbt.TorrentFiles{
				{Name: "Archive.tar.gz", Size: 1 << 30},
			},
			expect: LayoutArchives,
		},
		{
			name: "all ignored files (hardcoded patterns)",
			files: qbt.TorrentFiles{
				{Name: "readme.txt", Size: 512},
				{Name: "info.nfo", Size: 1024},
			},
			expect: LayoutUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			layout := classifyTorrentLayout(tt.files, stringutils.NewDefaultNormalizer())
			require.Equal(t, tt.expect, layout)
		})
	}
}
