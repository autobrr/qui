// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestService_deduplicateSourceTorrents_PreservesEpisodesAlongsideSeasonPacks(t *testing.T) {
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	seasonPack := qbt.Torrent{
		Hash:    "hash-pack",
		Name:    "Generic.Show.2025.S01.1080p.WEB-DL.DDP5.1.H.264-GEN",
		AddedOn: 2,
	}
	episode := qbt.Torrent{
		Hash:    "hash-episode",
		Name:    "Generic.Show.2025.S01E01.1080p.WEB-DL.DDP5.1.H.264-GEN",
		AddedOn: 1,
	}

	deduped, duplicates := svc.deduplicateSourceTorrents(context.Background(), 1, []qbt.Torrent{seasonPack, episode})
	require.Len(t, deduped, 2, "season pack should not eliminate individual episodes during deduplication")
	require.Empty(t, duplicates)

	kept := make(map[string]struct{})
	for _, torrent := range deduped {
		kept[torrent.Hash] = struct{}{}
	}

	require.Contains(t, kept, seasonPack.Hash)
	require.Contains(t, kept, episode.Hash)

	duplicateEpisodes := []qbt.Torrent{
		{
			Hash:    "hash-newer-episode",
			Name:    episode.Name,
			AddedOn: 10,
		},
		{
			Hash:    "hash-older-episode",
			Name:    episode.Name,
			AddedOn: 5,
		},
	}

	dedupedEpisodes, duplicateMap := svc.deduplicateSourceTorrents(context.Background(), 1, duplicateEpisodes)
	require.Len(t, dedupedEpisodes, 1, "exact episode duplicates should still collapse to the oldest torrent")
	require.Equal(t, "hash-older-episode", dedupedEpisodes[0].Hash)
	require.Contains(t, duplicateMap, "hash-older-episode")
	require.ElementsMatch(t, []string{"hash-newer-episode"}, duplicateMap["hash-older-episode"])
}

func TestService_deduplicateSourceTorrents_PrefersRootFolders(t *testing.T) {
	files := map[string]qbt.TorrentFiles{
		"hash-root": {
			{Name: "Show.S01/Show.S01E01.mkv", Size: 1 << 20},
		},
		"hash-flat": {
			{Name: "Show.S01E01.mkv", Size: 1 << 20},
		},
	}

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		syncManager:      &fakeSyncManager{files: files},
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	torrents := []qbt.Torrent{
		{Hash: "hash-flat", Name: "Generic.Show.2025.S01E01.1080p.WEB-DL", AddedOn: 1},
		{Hash: "hash-root", Name: "Generic.Show.2025.S01E01.1080p.WEB-DL", AddedOn: 2},
	}

	deduped, _ := svc.deduplicateSourceTorrents(context.Background(), 1, torrents)
	require.Len(t, deduped, 1)
	require.Equal(t, "hash-root", deduped[0].Hash, "prefer torrent with root folder")
}

// countingFilesSyncManager records which hashes the deduplication pass asks
// file lists for.
type countingFilesSyncManager struct {
	fakeSyncManager
	requested []string
}

func (c *countingFilesSyncManager) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	c.requested = append(c.requested, hashes...)
	return c.fakeSyncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

// Regression: deduplication used to fetch a file list for every torrent in the
// library before a search run logged anything. Only the members of a group that
// holds more than one torrent need one, so unique content costs no file request
// at all. Discord report: cross-seed scans take an absurd amount of time to
// start, with nothing in the log until the enumeration finishes.
func TestService_deduplicateSourceTorrents_FetchesFilesForGroupMembersOnly(t *testing.T) {
	files := map[string]qbt.TorrentFiles{
		"hash-flat":  {{Name: "Show.S01E01.mkv", Size: 1 << 20}},
		"hash-root":  {{Name: "Show.S01/Show.S01E01.mkv", Size: 1 << 20}},
		"hash-other": {{Name: "Other.S01E01.mkv", Size: 1 << 20}},
	}

	sync := &countingFilesSyncManager{files: files}
	svc := &Service{
		releaseCache:     NewReleaseCache(),
		syncManager:      sync,
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	torrents := []qbt.Torrent{
		{Hash: "hash-flat", Name: "Generic.Show.2025.S01E01.1080p.WEB-DL", AddedOn: 1},
		{Hash: "hash-other", Name: "Second.Show.2024.S03E07.1080p.WEB-DL", AddedOn: 2},
		{Hash: "hash-root", Name: "Generic.Show.2025.S01E01.1080p.WEB-DL", AddedOn: 3},
	}

	deduped, _ := svc.deduplicateSourceTorrents(context.Background(), 1, torrents)
	require.Len(t, deduped, 2)
	require.ElementsMatch(t, []string{"hash-flat", "hash-root"}, sync.requested)
}
