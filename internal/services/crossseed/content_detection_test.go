package crossseed

import (
	"context"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestAnalyzeTorrentForSearchAsync_RejectsUnrelatedLargestFile(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance := &models.Instance{ID: 1, Name: "Test"}

	movieTorrent := qbt.Torrent{
		Hash:     "deadbeef",
		Name:     "Example.Movie.2001.1080p.BluRay.x264-GROUP",
		Progress: 1.0,
		Size:     10 << 30,
	}

	files := map[string]qbt.TorrentFiles{
		movieTorrent.Hash: {
			{
				Name: "Different.Series.S03.1080p.WEB-DL.DDP5.1.H.264-GROUP/Different.Series.S03E02.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv",
				Size: 8 << 30,
			},
		},
	}

	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{movieTorrent}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result, err := service.AnalyzeTorrentForSearchAsync(ctx, instance.ID, movieTorrent.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "movie", result.TorrentInfo.ContentType, "should fall back to torrent name when largest file is unrelated")
	require.Equal(t, "movie", result.TorrentInfo.SearchType)
	require.Equal(t, []int{2000}, result.TorrentInfo.SearchCategories)
}

func TestAnalyzeTorrentForSearchAsync_UsesLargestFileWhenTitlesAlign(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance := &models.Instance{ID: 1, Name: "Test"}

	tvTorrent := qbt.Torrent{
		Hash:     "abcd1234",
		Name:     "MadeUp.Show",
		Progress: 1.0,
		Size:     5 << 30,
	}

	files := map[string]qbt.TorrentFiles{
		tvTorrent.Hash: {
			{
				Name: "MadeUp.Show.S01E02.1080p.WEB-DL.DDP5.1.H.264-GROUP.mkv",
				Size: 3 << 30,
			},
		},
	}

	service := &Service{
		instanceStore:    &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager:      newFakeSyncManager(instance, []qbt.Torrent{tvTorrent}, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	result, err := service.AnalyzeTorrentForSearchAsync(ctx, instance.ID, tvTorrent.Hash, false)
	require.NoError(t, err)
	require.NotNil(t, result)

	require.Equal(t, "tv", result.TorrentInfo.ContentType, "aligned largest file should refine content detection")
	require.Equal(t, "tvsearch", result.TorrentInfo.SearchType)
	require.Equal(t, []int{5000}, result.TorrentInfo.SearchCategories)
}
