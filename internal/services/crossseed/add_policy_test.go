// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"maps"
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
		name                    string
		files                   qbt.TorrentFiles
		wantDiscLayout          bool
		wantForcePaused         bool
		wantForceSkipAutoResume bool
		wantDiscMarker          string
	}{
		{
			name: "disc layout forces paused",
			files: qbt.TorrentFiles{
				{Name: "Movie/BDMV/index.bdmv"},
			},
			wantDiscLayout:  true,
			wantForcePaused: true,
			wantDiscMarker:  "BDMV",
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
			assert.Equal(t, tt.wantForcePaused, policy.ForcePaused)
			assert.Equal(t, tt.wantForceSkipAutoResume, policy.ForceSkipAutoResume)
			assert.Equal(t, tt.wantDiscMarker, policy.DiscMarker)
		})
	}
}

func TestAddPolicy_ApplyToAddOptions(t *testing.T) {
	tests := []struct {
		name        string
		policy      AddPolicy
		wantPaused  string
		wantStopped string
	}{
		{
			name:        "ForcePaused overrides options",
			policy:      AddPolicy{ForcePaused: true},
			wantPaused:  "true",
			wantStopped: "true",
		},
		{
			name:        "non-forced policy preserves options",
			policy:      AddPolicy{},
			wantPaused:  "false",
			wantStopped: "false",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			options := map[string]string{
				"paused":  "false",
				"stopped": "false",
			}

			tt.policy.ApplyToAddOptions(options)

			assert.Equal(t, tt.wantPaused, options["paused"])
			assert.Equal(t, tt.wantStopped, options["stopped"])
		})
	}
}

func TestAddPolicy_ShouldSkipAutoResume(t *testing.T) {
	tests := []struct {
		name   string
		policy AddPolicy
		want   bool
	}{
		{
			name:   "ForceSkipAutoResume true",
			policy: AddPolicy{ForceSkipAutoResume: true},
			want:   true,
		},
		{
			name:   "ForceSkipAutoResume false",
			policy: AddPolicy{ForceSkipAutoResume: false},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, tt.policy.ShouldSkipAutoResume())
		})
	}
}

func TestAddPolicy_StatusSuffix(t *testing.T) {
	tests := []struct {
		name         string
		policy       AddPolicy
		wantContains []string
		wantEmpty    bool
	}{
		{
			name: "disc layout returns suffix",
			policy: AddPolicy{
				DiscLayout: true,
				DiscMarker: "BDMV",
			},
			wantContains: []string{"disc layout", "BDMV", "recheck"},
		},
		{
			name:      "non-disc layout returns empty",
			policy:    AddPolicy{},
			wantEmpty: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suffix := tt.policy.StatusSuffix()
			if tt.wantEmpty {
				assert.Empty(t, suffix)
			}
			for _, want := range tt.wantContains {
				assert.Contains(t, suffix, want)
			}
		})
	}
}

// TestPolicyFlow_DiscLayoutForcesPaused tests the shared policy helper that all modes
// (regular, hardlink, reflink) call: PolicyForSourceFiles -> ApplyToAddOptions.
// This proves that disc layout torrents cannot be added "running" regardless of mode.
func TestPolicyFlow_DiscLayoutForcesPaused(t *testing.T) {
	tests := []struct {
		name         string
		files        qbt.TorrentFiles
		initialOpts  map[string]string
		wantPaused   string
		wantStopped  string
		wantSkipAuto bool
	}{
		{
			name: "BDMV disc overrides paused=false",
			files: qbt.TorrentFiles{
				{Name: "Movie/BDMV/index.bdmv"},
				{Name: "Movie/BDMV/STREAM/00000.m2ts"},
			},
			initialOpts:  map[string]string{"paused": "false", "stopped": "false"},
			wantPaused:   "true",
			wantStopped:  "true",
			wantSkipAuto: false,
		},
		{
			name: "VIDEO_TS disc overrides paused=false",
			files: qbt.TorrentFiles{
				{Name: "DVD/VIDEO_TS/VIDEO_TS.VOB"},
			},
			initialOpts:  map[string]string{"paused": "false", "stopped": "false"},
			wantPaused:   "true",
			wantStopped:  "true",
			wantSkipAuto: false,
		},
		{
			name: "non-disc preserves paused=false",
			files: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv"},
			},
			initialOpts:  map[string]string{"paused": "false", "stopped": "false"},
			wantPaused:   "false",
			wantStopped:  "false",
			wantSkipAuto: false,
		},
		{
			name: "non-disc preserves paused=true",
			files: qbt.TorrentFiles{
				{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv"},
			},
			initialOpts:  map[string]string{"paused": "true", "stopped": "true"},
			wantPaused:   "true",
			wantStopped:  "true",
			wantSkipAuto: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This is the exact flow used by all modes (regular, hardlink, reflink)
			policy := PolicyForSourceFiles(tt.files)
			opts := make(map[string]string)
			maps.Copy(opts, tt.initialOpts)
			policy.ApplyToAddOptions(opts)

			assert.Equal(t, tt.wantPaused, opts["paused"], "paused option mismatch")
			assert.Equal(t, tt.wantStopped, opts["stopped"], "stopped option mismatch")
			assert.Equal(t, tt.wantSkipAuto, policy.ShouldSkipAutoResume(), "ShouldSkipAutoResume mismatch")
		})
	}
}
