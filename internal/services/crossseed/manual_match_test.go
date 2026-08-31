// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func manualMatchTestService(instance *models.Instance, torrents []qbt.Torrent, files map[string]qbt.TorrentFiles) *Service {
	return &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      &applyFakeSyncManager{newFakeSyncManager(instance, torrents, files)},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}
}

func TestFindCandidatesManualTarget(t *testing.T) {
	t.Parallel()

	const (
		instanceID = 1
		targetHash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	instance := &models.Instance{ID: instanceID, Name: "main"}
	target := qbt.Torrent{Hash: targetHash, Name: "Totally.Different.Show.S01.1080p.WEB-DL.x264-AAA", Progress: 1}

	t.Run("pinned target bypasses release matching", func(t *testing.T) {
		svc := manualMatchTestService(instance, []qbt.Torrent{target}, nil)
		resp, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       "Unrelated.Concert.2024.720p.WEB.x265-BBB",
			TargetInstanceIDs: []int{instanceID},
			ManualTargetHash:  targetHash,
		})
		require.NoError(t, err)
		require.Len(t, resp.Candidates, 1)
		require.Equal(t, "manual", resp.Candidates[0].MatchType)
		require.Len(t, resp.Candidates[0].Torrents, 1)
		require.Equal(t, targetHash, resp.Candidates[0].Torrents[0].Hash)
	})

	t.Run("unknown hash errors", func(t *testing.T) {
		svc := manualMatchTestService(instance, []qbt.Torrent{target}, nil)
		_, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       "Whatever.2024.1080p.WEB.x264-GRP",
			TargetInstanceIDs: []int{instanceID},
			ManualTargetHash:  "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		})
		require.ErrorContains(t, err, "not found")
	})

	t.Run("incomplete target errors", func(t *testing.T) {
		incomplete := target
		incomplete.Progress = 0.5
		svc := manualMatchTestService(instance, []qbt.Torrent{incomplete}, nil)
		_, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:       "Whatever.2024.1080p.WEB.x264-GRP",
			TargetInstanceIDs: []int{instanceID},
			ManualTargetHash:  targetHash,
		})
		require.ErrorContains(t, err, "complete")
	})

	t.Run("requires exactly one instance", func(t *testing.T) {
		svc := manualMatchTestService(instance, []qbt.Torrent{target}, nil)
		_, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
			TorrentName:      "Whatever.2024.1080p.WEB.x264-GRP",
			ManualTargetHash: targetHash,
		})
		require.ErrorContains(t, err, "instance")
	})
}

// A retitled listing: the uploaded torrent's files are byte-identical to the
// target's, but the target's qBittorrent name shares nothing with the incoming
// name, so automatic matching rejects the pair on the release prefilter.
func TestCrossSeedManualTargetAppliesMismatchedTitle(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		targetHash   = "cccccccccccccccccccccccccccccccccccccccc"
		incomingName = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV"
		fileName     = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv"
		size         = int64(4 << 20)
	)
	torrentBytes := createNamedFileTestTorrent(t, incomingName, fileName, size)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	target := qbt.Torrent{Hash: targetHash, Name: "Renamed by user long ago", SavePath: "/downloads", Progress: 1, Size: size}
	svc := manualMatchTestService(instance, []qbt.Torrent{target}, map[string]qbt.TorrentFiles{
		targetHash: {{Name: incomingName + "/" + fileName, Size: size}},
	})

	// Without the pin the release prefilter rejects the pair.
	auto, err := svc.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
		TargetInstanceIDs: []int{instanceID},
	})
	require.NoError(t, err)
	require.False(t, auto.Success)

	resp, err := svc.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
		TargetInstanceIDs: []int{instanceID},
		ManualTargetHash:  targetHash,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "manual match rejected: %+v", resp.Results)
	require.Len(t, resp.Results, 1)
	require.Equal(t, "added", resp.Results[0].Status)
	require.NotNil(t, resp.Results[0].MatchedTorrent)
	require.Equal(t, targetHash, resp.Results[0].MatchedTorrent.Hash)
}

// A zero-overlap pick must still reach the add: the recheck is the arbiter of
// a wrong pick, and a failed recheck leaves the torrent paused.
func TestCrossSeedManualTargetZeroOverlapStillAdds(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		targetHash   = "dddddddddddddddddddddddddddddddddddddddd"
		incomingName = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV"
	)
	torrentBytes := createNamedFileTestTorrent(t, incomingName, incomingName+".mkv", 4<<20)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	target := qbt.Torrent{Hash: targetHash, Name: "Sakura.Grove.S02.2160p.WEB-DL.x265-KIRI", SavePath: "/downloads", Progress: 1, Size: 9 << 20}
	svc := manualMatchTestService(instance, []qbt.Torrent{target}, map[string]qbt.TorrentFiles{
		targetHash: {{Name: "Sakura.Grove.S02.2160p.WEB-DL.x265-KIRI/episode.mkv", Size: 9 << 20}},
	})

	resp, err := svc.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
		TargetInstanceIDs: []int{instanceID},
		ManualTargetHash:  targetHash,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "zero-overlap manual match rejected: %+v", resp.Results)
}

func TestManualMatchProposals(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	instance := &models.Instance{ID: instanceID, Name: "main"}

	incomingName := "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-FoV"
	torrentBytes := createTestTorrent(t, incomingName, []string{
		"Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv",
		"Azure.Compass.S01E02.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv",
	}, 256*1024)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)

	require.Len(t, meta.Files, 2)
	fileSizes := []int64{meta.Files[0].Size, meta.Files[1].Size}

	fullOverlap := qbt.Torrent{Hash: "1111111111111111111111111111111111111111", Name: "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-KIRI", SavePath: "/downloads", Category: "tv", Progress: 1, Size: meta.Info.TotalLength()}
	partialOverlap := qbt.Torrent{Hash: "2222222222222222222222222222222222222222", Name: "Azure.Compass.S01E01.1080p.WEB-DL.AAC2.0.H.264-KIRI", SavePath: "/downloads", Progress: 1, Size: fileSizes[0]}
	noOverlap := qbt.Torrent{Hash: "3333333333333333333333333333333333333333", Name: "Sakura.Grove.S02.2160p.WEB-DL.x265-KIRI", SavePath: "/downloads", Progress: 1, Size: 999_999}
	incomplete := qbt.Torrent{Hash: "4444444444444444444444444444444444444444", Name: "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-AAA", SavePath: "/downloads", Progress: 0.4, Size: meta.Info.TotalLength()}

	files := map[string]qbt.TorrentFiles{
		fullOverlap.Hash: {
			{Name: "pack/E01.mkv", Size: fileSizes[0]},
			{Name: "pack/E02.mkv", Size: fileSizes[1]},
		},
		partialOverlap.Hash: {{Name: "E01.mkv", Size: fileSizes[0]}},
		noOverlap.Hash:      {{Name: "other.mkv", Size: 999_999}},
	}

	svc := manualMatchTestService(instance, []qbt.Torrent{fullOverlap, partialOverlap, noOverlap, incomplete}, files)

	resp, err := svc.ManualMatchProposals(context.Background(), instanceID, torrentBytes, "")
	require.NoError(t, err)
	require.Equal(t, meta.Name, resp.SourceName)
	require.NotEmpty(t, resp.Proposals)
	require.Equal(t, fullOverlap.Hash, resp.Proposals[0].Hash)
	require.Equal(t, meta.Info.TotalLength(), resp.Proposals[0].OverlapBytes)
	require.InDelta(t, 1.0, resp.Proposals[0].OverlapFraction, 0.001)
	for _, p := range resp.Proposals {
		require.NotEqual(t, incomplete.Hash, p.Hash, "incomplete torrents are not proposable")
		require.NotEqual(t, noOverlap.Hash, p.Hash, "zero-overlap torrents are not proposed")
	}
	require.Equal(t, "/downloads", resp.Proposals[0].EffectiveSavePath)
	require.Equal(t, "tv", resp.Proposals[0].Category)

	// A requested target is always reported, with its overlap, even at zero.
	resp, err = svc.ManualMatchProposals(context.Background(), instanceID, torrentBytes, noOverlap.Hash)
	require.NoError(t, err)
	found := false
	for _, p := range resp.Proposals {
		if p.Hash == noOverlap.Hash {
			found = true
			require.Zero(t, p.OverlapBytes)
		}
	}
	require.True(t, found, "requested target must be included")
}
