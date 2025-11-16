package crossseed

import (
	"context"
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/releases"
)

func TestGetMatchType_EnforcesLayoutCompatibility(t *testing.T) {
	t.Parallel()

	svc := &Service{releaseCache: releases.NewDefaultParser()}
	sourceRelease := rls.Release{Title: "Example", Year: 2024}
	candidateRelease := rls.Release{Title: "Example", Year: 2024}

	sourceFiles := qbt.TorrentFiles{{Name: "Example.2024.1080p.mkv", Size: 4 << 30}}
	archiveFiles := qbt.TorrentFiles{{Name: "Example.part01.rar", Size: 2 << 30}, {Name: "Example.part02.r00", Size: 2 << 30}}

	match := svc.getMatchType(sourceRelease, candidateRelease, sourceFiles, archiveFiles, nil)
	require.Empty(t, match, "mkv torrent should not match rar-only candidate")

	archiveMatch := svc.getMatchType(sourceRelease, candidateRelease, archiveFiles, archiveFiles, nil)
	require.NotEmpty(t, archiveMatch, "identical archive layouts should match")

	fileMatch := svc.getMatchType(sourceRelease, candidateRelease, sourceFiles, sourceFiles, nil)
	require.Equal(t, "exact", fileMatch, "identical file layouts should be exact matches")
}

func TestFindBestCandidateMatch_PrefersLayoutCompatibleTorrent(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache: releases.NewDefaultParser(),
		syncManager: &candidateSelectionSyncManager{
			files: map[string]qbt.TorrentFiles{
				"rar": {{Name: "Example.part01.rar", Size: 2 << 30}, {Name: "Example.part02.r00", Size: 2 << 30}},
				"mkv": {{Name: "Example.2024.1080p.mkv", Size: 4 << 30}},
			},
		},
	}

	sourceRelease := rls.Release{Title: "Example", Year: 2024}
	sourceFiles := qbt.TorrentFiles{{Name: "Example.2024.1080p.mkv", Size: 4 << 30}}

	candidate := CrossSeedCandidate{
		InstanceID: 1,
		Torrents: []qbt.Torrent{
			{Hash: "rar", Name: "Example.RAR.Release", Progress: 1.0},
			{Hash: "mkv", Name: "Example.2024.1080p.GRP", Progress: 1.0},
		},
	}

	bestTorrent, files, matchType := svc.findBestCandidateMatch(context.Background(), candidate, sourceRelease, sourceFiles, nil)
	require.NotNil(t, bestTorrent)
	require.Equal(t, "mkv", bestTorrent.Hash)
	require.Equal(t, "exact", matchType)
	require.Len(t, files, 1)
}

type candidateSelectionSyncManager struct {
	files map[string]qbt.TorrentFiles
}

func (c *candidateSelectionSyncManager) GetAllTorrents(context.Context, int) ([]qbt.Torrent, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) GetTorrentFiles(_ context.Context, _ int, hash string) (*qbt.TorrentFiles, error) {
	files, ok := c.files[hash]
	if !ok {
		return nil, fmt.Errorf("files not found")
	}
	copyFiles := make(qbt.TorrentFiles, len(files))
	copy(copyFiles, files)
	return &copyFiles, nil
}

func (c *candidateSelectionSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) BulkAction(context.Context, int, []string, string) error {
	return fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (c *candidateSelectionSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return fmt.Errorf("not implemented")
}

func (c *candidateSelectionSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return fmt.Errorf("not implemented")
}
