// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"encoding/base64"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestCrossSeedGameArchiveVolumes(t *testing.T) {
	const name = "Silver.Valley.2-CODEX"
	for _, suffix := range []string{".r01", ".s01", ".S01", ".s97", ".s98", ".s99", ".t00", ".t01", ".t10", ".z99"} {
		t.Run(suffix, func(t *testing.T) {
			info := metainfo.Info{Name: name, PieceLength: 16 * 1024}
			var files qbt.TorrentFiles
			for _, volume := range []string{".rar", ".r99", ".s00", suffix} {
				fileName := "codex-silver.valley.2" + volume
				info.Files = append(info.Files, metainfo.FileInfo{Path: []string{fileName}, Length: 1024})
				files = append(files, qbt.TorrentFile{Name: name + "/" + fileName, Size: 1024, Progress: 1})
			}
			sortTorrentFiles(info.Files)
			infoBytes, err := bencode.Marshal(info)
			require.NoError(t, err)
			meta := metainfo.MetaInfo{InfoBytes: infoBytes}
			var torrentBytes bytes.Buffer
			require.NoError(t, meta.Write(&torrentBytes))

			instance := &models.Instance{ID: 1, Name: "test"}
			torrent := qbt.Torrent{Hash: "source", Name: name, Progress: 1, Size: 4096}
			service := &Service{
				instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{1: instance}},
				syncManager:      &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{torrent}, map[string]qbt.TorrentFiles{"source": files})},
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}
			response, err := service.CrossSeed(t.Context(), &CrossSeedRequest{
				TorrentData:                  base64.StdEncoding.EncodeToString(torrentBytes.Bytes()),
				TargetInstanceIDs:            []int{1},
				FindIndividualEpisodes:       true,
				StartPaused:                  new(true),
				SkipAutoResume:               true,
				SkipPieceBoundarySafetyCheck: true,
				SearchDecision:               searchDecisionProvenance{Class: searchCandidateClassStrict, SourceInstanceID: 1, SourceHash: "source"},
			})
			require.NoError(t, err)
			require.True(t, response.Success, "identical complete game files must apply: %+v", response.Results)
		})
	}
}

func TestParseFileReleasePreservesTVStructure(t *testing.T) {
	service := &Service{releaseCache: NewReleaseCache()}
	for _, name := range []string{"Example.Show.S03E02-GRP.s01", "Example.Show.S03E02-GRP.mkv"} {
		parsed := service.parseFileRelease(name)
		require.Equal(t, 3, parsed.Series, name)
		require.Equal(t, 2, parsed.Episode, name)
	}
	// A torrent title ending in .S01 still names a season.
	require.Equal(t, 1, service.parseReleaseName("Example.Show.S01").Series)
}
