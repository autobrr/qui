package crossseed

import (
	"context"
	"slices"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

// lateTorrentSyncManager lists one torrent under its qBittorrent hash, and only
// after the first lookups miss, as for a torrent that qui has not synced yet.
type lateTorrentSyncManager struct {
	*rootlessSavePathSyncManager
	torrent qbt.Torrent
	misses  int
}

func (m *lateTorrentSyncManager) listed(hashes []string) bool {
	if m.misses > 0 {
		m.misses--
		return false
	}
	return slices.Contains(hashes, m.torrent.Hash)
}

func (m *lateTorrentSyncManager) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if m.listed(filter.Hashes) {
		return []qbt.Torrent{m.torrent}, nil
	}
	return nil, nil
}

func (m *lateTorrentSyncManager) HasTorrentByAnyHash(_ context.Context, _ int, hashes []string) (*qbt.Torrent, bool, error) {
	if m.listed(hashes) {
		torrent := m.torrent
		return &torrent, true, nil
	}
	return nil, false, nil
}

func TestFindAddedTorrent_WaitsForSyncAndUsesQbitHash(t *testing.T) {
	// A hybrid torrent as qBittorrent 5.1 with libtorrent 2.0.15 lists it: under
	// the first 40 characters of the v2 hash.
	v1 := "6fa734f86d0dda275bfd134e84271c0c261f75fe"
	v2 := "aed4769c0a376d677ff697b66cbf88fea4c1c2b581bbacb4e017db9c324d47a3"
	listedAs := "aed4769c0a376d677ff697b66cbf88fea4c1c2b5"
	sm := &lateTorrentSyncManager{
		rootlessSavePathSyncManager: &rootlessSavePathSyncManager{},
		torrent:                     qbt.Torrent{Hash: listedAs, InfohashV1: v1, InfohashV2: v2},
		misses:                      2,
	}
	s := &Service{syncManager: sm}

	got, found, err := s.findAddedTorrent(t.Context(), 1, []string{v1, v2})
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, listedAs, got.Hash)
}
