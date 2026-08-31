// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"fmt"
	"slices"
	"strings"
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

// A zero-overlap pick must still reach the add, also on a link-mode instance
// (no linkable files would fail materialization): the recheck is the arbiter
// of a wrong pick, and a failed recheck leaves the torrent paused.
func TestCrossSeedManualTargetZeroOverlapStillAdds(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		targetHash   = "dddddddddddddddddddddddddddddddddddddddd"
		incomingName = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV"
	)

	tests := []struct {
		name         string
		useHardlinks bool
	}{
		{name: "regular instance"},
		{name: "hardlink instance skips link mode", useHardlinks: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			torrentBytes := createNamedFileTestTorrent(t, incomingName, incomingName+".mkv", 4<<20)

			instance := &models.Instance{ID: instanceID, Name: "main", UseHardlinks: tt.useHardlinks}
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
			require.Len(t, resp.Results, 1)
			require.Equal(t, "added", resp.Results[0].Status)
		})
	}
}

// recordingApplySyncManager records add options and bulk actions so tests can
// observe the recheck policy of an apply.
type recordingApplySyncManager struct {
	*applyFakeSyncManager
	addOptions  []map[string]string
	bulkActions []string
}

func (r *recordingApplySyncManager) AddTorrent(ctx context.Context, instanceID int, data []byte, opts map[string]string) (*qbt.TorrentAddResponse, error) {
	r.addOptions = append(r.addOptions, opts)
	return r.applyFakeSyncManager.AddTorrent(ctx, instanceID, data, opts)
}

func (r *recordingApplySyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	r.bulkActions = append(r.bulkActions, action)
	return r.applyFakeSyncManager.BulkAction(ctx, instanceID, hashes, action)
}

// A perfect-twin Manual match (identical name and files) would sail through
// the automatic pipeline with skip_checking and no recheck. Manual matches
// bypass the release and content gates, so they must verify regardless.
func TestCrossSeedManualTargetValidatedFilesStillRecheck(t *testing.T) {
	t.Parallel()

	const (
		instanceID   = 1
		targetHash   = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		incomingName = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV"
		fileName     = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv"
		size         = int64(4 << 20)
	)
	torrentBytes := createNamedFileTestTorrent(t, incomingName, fileName, size)

	instance := &models.Instance{ID: instanceID, Name: "main"}
	target := qbt.Torrent{Hash: targetHash, Name: incomingName, SavePath: "/downloads", Progress: 1, Size: size}
	recorder := &recordingApplySyncManager{
		applyFakeSyncManager: &applyFakeSyncManager{newFakeSyncManager(instance, []qbt.Torrent{target}, map[string]qbt.TorrentFiles{
			targetHash: {{Name: incomingName + "/" + fileName, Size: size}},
		})},
	}
	svc := manualMatchTestService(instance, nil, nil)
	svc.syncManager = recorder

	startPaused := false
	resp, err := svc.CrossSeed(context.Background(), &CrossSeedRequest{
		TorrentData:       base64.StdEncoding.EncodeToString(torrentBytes),
		TargetInstanceIDs: []int{instanceID},
		ManualTargetHash:  targetHash,
		StartPaused:       &startPaused,
	})
	require.NoError(t, err)
	require.True(t, resp.Success, "manual match rejected: %+v", resp.Results)
	require.Len(t, recorder.addOptions, 1)
	require.Equal(t, "true", recorder.addOptions[0]["paused"], "manual match must add paused pending verification")
	require.Contains(t, recorder.bulkActions, "recheck", "manual match must always recheck")
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
	require.Equal(t, []string{"cross-seed"}, resp.DefaultTags, "the cross-seed tag is the default, matching the search dialog")
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

// On a link-mode instance the effective save path must track validation: a
// validated pick lands at the link destination, but a zero-overlap pick takes
// the regular add at the target's save path (the apply skips link modes for it).
func TestManualMatchProposalsEffectiveSavePathTracksValidation(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	instance := &models.Instance{ID: instanceID, Name: "main", UseHardlinks: true, HardlinkBaseDir: "/links"}

	const (
		incomingName = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV"
		fileName     = "Azure.Compass.2024.1080p.WEB-DL.AAC2.0.H.264-FoV.mkv"
		size         = int64(4 << 20)
	)
	torrentBytes := createNamedFileTestTorrent(t, incomingName, fileName, size)

	validated := qbt.Torrent{Hash: "6666666666666666666666666666666666666666", Name: incomingName, SavePath: "/downloads", Progress: 1, Size: size}
	zeroOverlap := qbt.Torrent{Hash: "7777777777777777777777777777777777777777", Name: "Sakura.Grove.S02.2160p.WEB-DL.x265-KIRI", SavePath: "/downloads", Progress: 1, Size: 9 << 20}
	svc := manualMatchTestService(instance, []qbt.Torrent{validated, zeroOverlap}, map[string]qbt.TorrentFiles{
		validated.Hash:   {{Name: incomingName + "/" + fileName, Size: size}},
		zeroOverlap.Hash: {{Name: "Sakura.Grove.S02.2160p.WEB-DL.x265-KIRI/episode.mkv", Size: 9 << 20}},
	})

	resp, err := svc.ManualMatchProposals(context.Background(), instanceID, torrentBytes, zeroOverlap.Hash)
	require.NoError(t, err)
	byHash := make(map[string]ManualMatchProposal, len(resp.Proposals))
	for _, p := range resp.Proposals {
		byHash[p.Hash] = p
	}
	require.Contains(t, byHash, validated.Hash)
	require.Contains(t, byHash, zeroOverlap.Hash)
	require.True(t, strings.HasPrefix(byHash[validated.Hash].EffectiveSavePath, "/links"),
		"validated pick previews the link destination, got %q", byHash[validated.Hash].EffectiveSavePath)
	require.Equal(t, "/downloads", byHash[zeroOverlap.Hash].EffectiveSavePath,
		"zero-overlap pick previews the target save path (regular add)")
}

// The requested hash must survive a coarse pass crowded with same-title keeps
// that outrank it on size ratio (e.g. every episode of a long-running show).
func TestManualMatchProposalsRequestedSurvivesCrowdedCoarsePass(t *testing.T) {
	t.Parallel()

	const instanceID = 1
	instance := &models.Instance{ID: instanceID, Name: "main"}

	incomingName := "Azure.Compass.S01.1080p.WEB-DL.AAC2.0.H.264-FoV"
	torrentBytes := createNamedFileTestTorrent(t, incomingName, incomingName+".mkv", 4<<20)
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	require.NoError(t, err)
	sourceTotal := meta.Info.TotalLength()

	requested := qbt.Torrent{
		Hash:     "5555555555555555555555555555555555555555",
		Name:     "Sakura.Grove.S02.2160p.WEB-DL.x265-KIRI",
		SavePath: "/downloads",
		Progress: 1,
		Size:     1, // worst possible ratio: crowded out without its reserved slot
	}
	torrents := make([]qbt.Torrent, 0, manualMatchCoarseLimit+11)
	torrents = append(torrents, requested)
	for i := range manualMatchCoarseLimit + 10 {
		torrents = append(torrents, qbt.Torrent{
			Hash:     fmt.Sprintf("%040x", i+1),
			Name:     fmt.Sprintf("Azure.Compass.S01E%02d.1080p.WEB-DL.AAC2.0.H.264-KIRI", i+1),
			SavePath: "/downloads",
			Progress: 1,
			Size:     sourceTotal,
		})
	}

	svc := manualMatchTestService(instance, torrents, nil)
	resp, err := svc.ManualMatchProposals(context.Background(), instanceID, torrentBytes, requested.Hash)
	require.NoError(t, err)
	require.True(t, slices.ContainsFunc(resp.Proposals, func(p ManualMatchProposal) bool {
		return p.Hash == requested.Hash
	}), "requested target must be included even when title keeps fill the coarse pass")
}
