// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"encoding/base64"
	"strings"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func v2OnlyTestTorrent(t *testing.T, name string) []byte {
	t.Helper()
	infoBytes, err := bencode.Marshal(&metainfo.Info{
		Name:        name,
		MetaVersion: 2,
		PieceLength: 16 * 1024,
		FileTree: metainfo.FileTree{Dir: map[string]metainfo.FileTree{
			name: {File: metainfo.FileTreeFile{Length: 1024, PiecesRoot: strings.Repeat("a", 32)}},
		}},
	})
	require.NoError(t, err)
	var torrent bytes.Buffer
	require.NoError(t, (&metainfo.MetaInfo{InfoBytes: infoBytes}).Write(&torrent))
	return torrent.Bytes()
}

func TestCrossSeedReportsTheHashOfTheTorrentVersion(t *testing.T) {
	const name = "Cedar.Harbor.2024.1080p.WEB-DL.x264-PINE.mkv"
	for _, tt := range []struct {
		name   string
		data   []byte
		wantV2 bool
	}{
		{name: "v1 torrent", data: createNamedFileTestTorrent(t, name, name, 1024)},
		{name: "v2-only torrent", data: v2OnlyTestTorrent(t, name), wantV2: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			meta, err := ParseTorrentMetadataWithInfo(tt.data)
			require.NoError(t, err)
			want := meta.HashV1
			if tt.wantV2 {
				require.False(t, meta.Info.HasV1())
				want = meta.HashV2
			}
			svc := manualMatchTestService(&models.Instance{ID: 1, Name: "main"}, nil, nil)
			resp, err := svc.CrossSeed(t.Context(), &CrossSeedRequest{
				TorrentData:       base64.StdEncoding.EncodeToString(tt.data),
				TargetInstanceIDs: []int{1},
			})
			require.NoError(t, err)
			require.Equal(t, want.String(), resp.TorrentInfo.Hash)
		})
	}
}
