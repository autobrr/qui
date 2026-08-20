// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestSearchSourceSize(t *testing.T) {
	tests := []struct {
		name    string
		torrent qbt.Torrent
		want    int64
	}{
		{
			// A torrent with deselected files reports the smaller wanted size
			// in Size while Progress still reads 1.0. Torznab candidates
			// advertise the full release size, so the band must compare
			// against TotalSize.
			name:    "deselected files use the full size",
			torrent: qbt.Torrent{Size: 7 << 30, TotalSize: 10 << 30},
			want:    10 << 30,
		},
		{
			name:    "fully selected torrent sizes are equal",
			torrent: qbt.Torrent{Size: 10 << 30, TotalSize: 10 << 30},
			want:    10 << 30,
		},
		{
			name:    "missing total size falls back to wanted size",
			torrent: qbt.Torrent{Size: 10 << 30},
			want:    10 << 30,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, searchSourceSize(&tt.torrent))
		})
	}
}
