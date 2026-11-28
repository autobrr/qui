package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestTrackerSortOrdersSameDomainByTrackerURLBeforeHash(t *testing.T) {
	sm := &SyncManager{}
	torrents := []*qbt.Torrent{
		{Hash: "aaaa", Tracker: "https://tracker.example.invalid/b/announce"},
		{Hash: "bbbb", Tracker: "https://tracker.example.invalid/a/announce"},
	}

	sm.sortTorrents(torrents, "tracker", false, false, nil)

	require.Equal(t, []string{"bbbb", "aaaa"}, []string{torrents[0].Hash, torrents[1].Hash})
}
