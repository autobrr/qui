package qbittorrent

import (
	"context"
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestNormalizeHashes(t *testing.T) {
	t.Parallel()

	normalized := normalizeHashes([]string{" ABC123 ", "abc123", "Def456", "def456", ""})

	require.Equal(t, []string{"abc123", "def456"}, normalized.canonical)
	require.Equal(t, map[string]struct{}{
		"abc123": {},
		"def456": {},
	}, normalized.canonicalSet)
	require.Equal(t, "ABC123", normalized.canonicalToPreferred["abc123"])
	require.Equal(t, []string{"ABC123", "abc123", "Def456", "def456", "DEF456"}, normalized.lookup)
}

func TestGetTorrentFilesBatch_NormalizesAndCaches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	client := &stubTorrentFilesClient{
		torrents: []qbt.Torrent{
			{Hash: "ABC123", Progress: 1.0},
			{Hash: "def456", Progress: 0.5},
		},
		filesByHash: map[string]qbt.TorrentFiles{
			"ABC123": {
				{
					Name: "cached-a.mkv",
					Size: 1,
				},
			},
			"Def456": {
				{
					Name: "def-file.mkv",
					Size: 2,
				},
			},
		},
	}

	fm := &stubFilesManager{
		cached: map[string]qbt.TorrentFiles{
			"abc123": {
				{
					Name: "cached-a.mkv",
					Size: 1,
				},
			},
		},
	}

	sm := &SyncManager{
		torrentFilesClientProvider: func(context.Context, int) (torrentFilesClient, error) {
			return client, nil
		},
	}
	sm.SetFilesManager(fm)

	filesByHash, err := sm.GetTorrentFilesBatch(ctx, 1, []string{"  ABC123 ", "abc123", "Def456"})
	require.NoError(t, err)

	require.Len(t, filesByHash, 2)
	require.Contains(t, filesByHash, "abc123")
	require.Contains(t, filesByHash, "def456")
	require.Equal(t, "cached-a.mkv", filesByHash["abc123"][0].Name)
	require.Equal(t, "def-file.mkv", filesByHash["def456"][0].Name)

	require.ElementsMatch(t, []string{"abc123", "def456"}, fm.lastHashes)
	require.Len(t, fm.cacheCalls, 1)
	require.Equal(t, cacheCall{hash: "def456", progress: 0.0}, fm.cacheCalls[0])

	require.Len(t, client.requestedHashes, 1)
	require.Equal(t, []string{"ABC123", "abc123", "Def456", "def456", "DEF456"}, client.requestedHashes[0])
	require.Equal(t, []string{"Def456"}, client.fileRequests)
}

func TestHasTorrentByAnyHash(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	lookup := &stubTorrentLookup{
		torrents: map[string]qbt.Torrent{
			"ABC123": {Hash: "ABC123", Name: "first"},
			"DEF456": {Hash: "zzz", InfohashV2: "def456", Name: "second"},
		},
	}

	sm := &SyncManager{
		torrentLookupProvider: func(context.Context, int) (torrentLookup, error) {
			return lookup, nil
		},
	}

	torrent, found, err := sm.HasTorrentByAnyHash(ctx, 1, []string{"  abc123 "})
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, torrent)
	require.Equal(t, "ABC123", torrent.Hash)

	torrent, found, err = sm.HasTorrentByAnyHash(ctx, 1, []string{"def456"})
	require.NoError(t, err)
	require.True(t, found)
	require.NotNil(t, torrent)
	require.Equal(t, "zzz", torrent.Hash)
	require.Equal(t, "second", torrent.Name)
}

type stubTorrentFilesClient struct {
	torrents        []qbt.Torrent
	filesByHash     map[string]qbt.TorrentFiles
	requestedHashes [][]string
	fileRequests    []string
}

func (c *stubTorrentFilesClient) getTorrentsByHashes(hashes []string) []qbt.Torrent {
	copied := append([]string(nil), hashes...)
	c.requestedHashes = append(c.requestedHashes, copied)
	return c.torrents
}

func (c *stubTorrentFilesClient) GetFilesInformationCtx(ctx context.Context, hash string) (*qbt.TorrentFiles, error) {
	c.fileRequests = append(c.fileRequests, hash)
	files, ok := c.filesByHash[hash]
	if !ok {
		return nil, fmt.Errorf("no files for hash %s", hash)
	}
	copied := make(qbt.TorrentFiles, len(files))
	copy(copied, files)
	return &copied, nil
}

type cacheCall struct {
	hash     string
	progress float64
}

type stubFilesManager struct {
	cached     map[string]qbt.TorrentFiles
	lastHashes []string
	cacheCalls []cacheCall
}

func (fm *stubFilesManager) GetCachedFiles(context.Context, int, string) (qbt.TorrentFiles, error) {
	return nil, nil
}

func (fm *stubFilesManager) GetCachedFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, []string, error) {
	fm.lastHashes = append([]string(nil), hashes...)

	cached := make(map[string]qbt.TorrentFiles, len(hashes))
	missing := make([]string, 0, len(hashes))

	for _, hash := range hashes {
		if files, ok := fm.cached[hash]; ok {
			copied := make(qbt.TorrentFiles, len(files))
			copy(copied, files)
			cached[hash] = copied
		} else {
			missing = append(missing, hash)
		}
	}

	return cached, missing, nil
}

func (fm *stubFilesManager) CacheFiles(_ context.Context, _ int, hash string, files qbt.TorrentFiles) error {
	fm.cacheCalls = append(fm.cacheCalls, cacheCall{hash: hash, progress: 0.0})
	fm.cached[hash] = files
	return nil
}

func (fm *stubFilesManager) CacheFilesBatch(_ context.Context, _ int, files map[string]qbt.TorrentFiles) error {
	for hash, torrentFiles := range files {
		fm.cacheCalls = append(fm.cacheCalls, cacheCall{hash: hash, progress: 0.0})
		fm.cached[hash] = torrentFiles
	}
	return nil
}

func (*stubFilesManager) InvalidateCache(context.Context, int, string) error {
	return nil
}

type stubTorrentLookup struct {
	torrents map[string]qbt.Torrent
}

func (s *stubTorrentLookup) GetTorrent(hash string) (qbt.Torrent, bool) {
	torrent, ok := s.torrents[hash]
	return torrent, ok
}
