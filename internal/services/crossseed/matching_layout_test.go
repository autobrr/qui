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

func TestFindBestCandidateMatch_PrefersTopLevelFolderOnTie(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache: releases.NewDefaultParser(),
		syncManager: &candidateSelectionSyncManager{
			files: map[string]qbt.TorrentFiles{
				"single": {{Name: "payload.bin", Size: 4 << 30}},
				"folder": {
					{Name: "folder/payload.bin", Size: 4 << 30},
					{Name: "folder/extra.txt", Size: 1 << 20},
				},
			},
		},
	}

	sourceRelease := rls.Release{}
	sourceFiles := qbt.TorrentFiles{{Name: "PAYLOAD.bin", Size: 4 << 30}}

	candidate := CrossSeedCandidate{
		InstanceID: 1,
		Torrents: []qbt.Torrent{
			{Hash: "single", Name: "Minimal.Payload", Progress: 1.0},
			{Hash: "folder", Name: "Minimal.Payload", Progress: 1.0},
		},
	}

	singleRelease := svc.releaseCache.Parse("Minimal.Payload")
	singleMatch := svc.getMatchType(sourceRelease, singleRelease, sourceFiles, svc.syncManager.(*candidateSelectionSyncManager).files["single"], nil)
	folderMatch := svc.getMatchType(sourceRelease, singleRelease, sourceFiles, svc.syncManager.(*candidateSelectionSyncManager).files["folder"], nil)
	require.Equal(t, singleMatch, folderMatch, "test setup should create identical match priorities")
	require.Equal(t, "size", singleMatch)

	bestTorrent, files, matchType := svc.findBestCandidateMatch(context.Background(), candidate, sourceRelease, sourceFiles, nil)
	require.NotNil(t, bestTorrent)
	require.Equal(t, "folder", bestTorrent.Hash, "top-level folder layout should win tie-breakers")
	require.Equal(t, "size", matchType)
	require.Len(t, files, 2, "should return folder-based file list")
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

func (c *candidateSelectionSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func TestGetMatchTypeFromTitle_FallbackWhenReleaseKeysMissing(t *testing.T) {
	t.Parallel()

	svc := &Service{releaseCache: releases.NewDefaultParser()}
	targetName := "[TestGroup] Example Show - 1150 (1080p) [ABCDEF01]"
	candidateName := "[TestGroup] Example Show - 1150 (1080p) [ABCDEF01]"
	targetRelease := rls.Release{Title: "Example Show"}
	candidateRelease := rls.Release{Title: "Example Show"}

	// Use a filename that won't produce any usable release keys when parsed.
	candidateFiles := qbt.TorrentFiles{
		{Name: "random_data_file.bin", Size: 1024},
	}

	match := svc.getMatchTypeFromTitle(targetName, candidateName, targetRelease, candidateRelease, candidateFiles, nil)
	require.Equal(t, "partial-in-pack", match, "fallback should treat matching titles as candidates when parsing fails")
}

func TestGetMatchTypeFromTitle_NonEpisodicRequiresMatchingReleaseKey(t *testing.T) {
	t.Parallel()

	svc := &Service{releaseCache: releases.NewDefaultParser()}

	// Non-episodic content with different years should not match purely because
	// candidate files have some parsed metadata.
	targetName := "Movie.2020.1080p.BluRay.x264-GROUP"
	targetRelease := rls.Release{
		Title: "Movie 2020",
		Year:  2020,
	}

	candidateName := "Completely.Different.Movie.2012.1080p.BluRay.x264-OTHER"
	candidateRelease := rls.Release{
		Title: "Different Movie 2012",
		Year:  2012,
	}

	candidateFiles := qbt.TorrentFiles{
		{Name: "Different.Movie.2012.1080p.BluRay.x264-OTHER.mkv", Size: 4 << 30},
	}

	match := svc.getMatchTypeFromTitle(targetName, candidateName, targetRelease, candidateRelease, candidateFiles, nil)
	require.Empty(t, match, "non-episodic candidates with mismatched release keys should not match")
}

func TestGetMatchTypeFromTitle_NonEpisodicWithMatchingReleaseKey(t *testing.T) {
	t.Parallel()

	svc := &Service{releaseCache: releases.NewDefaultParser()}

	targetName := "Movie.2020.1080p.BluRay.x264-GROUP"
	targetRelease := rls.Release{
		Title: "Movie 2020",
		Year:  2020,
	}

	candidateName := "Another.Movie.2020.1080p.BluRay.x264-OTHER"
	candidateRelease := rls.Release{
		Title: "Another Movie 2020",
		Year:  2020,
	}

	candidateFiles := qbt.TorrentFiles{
		{Name: "Another.Movie.2020.1080p.BluRay.x264-OTHER.mkv", Size: 4 << 30},
	}

	match := svc.getMatchTypeFromTitle(targetName, candidateName, targetRelease, candidateRelease, candidateFiles, nil)
	require.Equal(t, "partial-in-pack", match, "non-episodic candidates with matching release keys should match")
}

func TestGetMatchType_FileNameFallback(t *testing.T) {
	t.Parallel()

	svc := &Service{releaseCache: releases.NewDefaultParser()}
	sourceRelease := rls.Release{Title: "Example Show"}
	candidateRelease := rls.Release{Title: "Example Show"}

	sourceFiles := qbt.TorrentFiles{
		{Name: "[TestGroup] Example Show - 1150 (1080p) [ABCDEF01].mkv", Size: 1 << 30},
	}
	candidateFiles := qbt.TorrentFiles{
		{Name: "[TestGroup] Example Show - 1150 (1080p) [ABCDEF01]/[TestGroup] Example Show - 1150 (1080p) [ABCDEF01].mkv", Size: 1 << 30},
	}

	match := svc.getMatchType(sourceRelease, candidateRelease, sourceFiles, candidateFiles, nil)
	require.Equal(t, "size", match, "single-file torrents with matching base names should fallback to size match")
}
