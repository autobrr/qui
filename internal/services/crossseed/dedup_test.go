package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestService_deduplicateSourceTorrents_PreservesEpisodesAlongsideSeasonPacks(t *testing.T) {
	svc := &Service{
		releaseCache: NewReleaseCache(),
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

	deduped, duplicates := svc.deduplicateSourceTorrents([]qbt.Torrent{seasonPack, episode})
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

	dedupedEpisodes, duplicateMap := svc.deduplicateSourceTorrents(duplicateEpisodes)
	require.Len(t, dedupedEpisodes, 1, "exact episode duplicates should still collapse to the oldest torrent")
	require.Equal(t, "hash-older-episode", dedupedEpisodes[0].Hash)
	require.Contains(t, duplicateMap, "hash-older-episode")
	require.ElementsMatch(t, []string{"hash-newer-episode"}, duplicateMap["hash-older-episode"])
}
