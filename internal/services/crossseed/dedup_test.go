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
