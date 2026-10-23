// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
)

func TestIsDiscLayoutTorrent(t *testing.T) {
	tests := []struct {
		name       string
		files      qbt.TorrentFiles
		wantDisc   bool
		wantMarker string
	}{
		{
			name: "BDMV at root level",
			files: qbt.TorrentFiles{
				{Name: "BDMV/index.bdmv"},
				{Name: "BDMV/STREAM/00001.m2ts"},
			},
			wantDisc:   true,
			wantMarker: "BDMV",
		},
		{
			name: "BDMV nested in folder",
			files: qbt.TorrentFiles{
				{Name: "Movie.2024.BluRay/BDMV/index.bdmv"},
				{Name: "Movie.2024.BluRay/BDMV/STREAM/00001.m2ts"},
			},
			wantDisc:   true,
			wantMarker: "BDMV",
		},
		{
			name: "VIDEO_TS at root",
			files: qbt.TorrentFiles{
				{Name: "VIDEO_TS/VIDEO_TS.VOB"},
				{Name: "VIDEO_TS/VTS_01_0.VOB"},
			},
			wantDisc:   true,
			wantMarker: "VIDEO_TS",
		},
		{
			name: "VIDEO_TS deeply nested",
			files: qbt.TorrentFiles{
				{Name: "Show/Season1/Disc1/VIDEO_TS/VIDEO_TS.VOB"},
			},
			wantDisc:   true,
			wantMarker: "VIDEO_TS",
		},
		{
			name: "case insensitive bdmv",
			files: qbt.TorrentFiles{
				{Name: "Movie/bdmv/index.bdmv"},
			},
			wantDisc:   true,
			wantMarker: "BDMV",
		},
		{
			name: "case insensitive video_ts mixed case",
			files: qbt.TorrentFiles{
				{Name: "Movie/Video_TS/video.vob"},
			},
			wantDisc:   true,
			wantMarker: "VIDEO_TS",
		},
		{
			name: "Windows path separators",
			files: qbt.TorrentFiles{
				{Name: "Movie\\BDMV\\index.bdmv"},
				{Name: "Movie\\BDMV\\STREAM\\00001.m2ts"},
			},
			wantDisc:   true,
			wantMarker: "BDMV",
		},
		{
			name: "not disc - regular movie",
			files: qbt.TorrentFiles{
				{Name: "Movie.2024.BluRay.1080p.mkv"},
			},
			wantDisc:   false,
			wantMarker: "",
		},
		{
			name: "not disc - BDMV as file extension only",
			files: qbt.TorrentFiles{
				{Name: "Movie/index.bdmv"},
			},
			wantDisc:   false,
			wantMarker: "",
		},
		{
			name: "not disc - BDMV as substring in folder name",
			files: qbt.TorrentFiles{
				{Name: "BDMV_backup/file.txt"},
				{Name: "myBDMV/data.bin"},
			},
			wantDisc:   false,
			wantMarker: "",
		},
		{
			name: "not disc - VIDEO_TS as substring",
			files: qbt.TorrentFiles{
				{Name: "VIDEO_TS_files/video.txt"},
				{Name: "old_VIDEO_TS/backup.dat"},
			},
			wantDisc:   false,
			wantMarker: "",
		},
		{
			name:       "not disc - empty files",
			files:      qbt.TorrentFiles{},
			wantDisc:   false,
			wantMarker: "",
		},
		{
			name: "mixed content with disc structure",
			files: qbt.TorrentFiles{
				{Name: "Movie/README.txt"},
				{Name: "Movie/BDMV/index.bdmv"},
				{Name: "Movie/sample.mkv"},
			},
			wantDisc:   true,
			wantMarker: "BDMV",
		},
		{
			name: "single segment BDMV is filename not directory",
			files: qbt.TorrentFiles{
				{Name: "BDMV"},
			},
			wantDisc:   false,
			wantMarker: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotDisc, gotMarker := isDiscLayoutTorrent(tt.files)
			assert.Equal(t, tt.wantDisc, gotDisc, "isDisc mismatch")
			assert.Equal(t, tt.wantMarker, gotMarker, "marker mismatch")
		})
	}
}

func TestPolicyForSourceFiles(t *testing.T) {
	tests := []struct {
		name           string
		files          qbt.TorrentFiles
		wantDiscLayout bool
		wantDiscMarker string
	}{
		{
			name: "disc layout",
			files: qbt.TorrentFiles{
				{Name: "Movie/BDMV/index.bdmv"},
			},
			wantDiscLayout: true,
			wantDiscMarker: "BDMV",
		},
		{
			name: "non-disc layout returns empty policy",
			files: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.mkv"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			policy := PolicyForSourceFiles(tt.files)

			assert.Equal(t, tt.wantDiscLayout, policy.DiscLayout)
			assert.Equal(t, tt.wantDiscMarker, policy.DiscMarker)
		})
	}
}
