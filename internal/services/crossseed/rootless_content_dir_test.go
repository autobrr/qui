package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestResolveRootlessContentDir(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		torrent       *qbt.Torrent
		candidateFiles qbt.TorrentFiles
		expected      string
	}{
		{
			name:          "nil torrent",
			torrent:       nil,
			candidateFiles: qbt.TorrentFiles{{Name: "f.mkv"}},
			expected:      "",
		},
		{
			name:          "empty content path",
			torrent:       &qbt.Torrent{ContentPath: ""},
			candidateFiles: qbt.TorrentFiles{{Name: "f.mkv"}},
			expected:      "",
		},
		{
			name:          "no candidate files",
			torrent:       &qbt.Torrent{ContentPath: "/downloads/show/f.mkv"},
			candidateFiles: nil,
			expected:      "",
		},
		{
			name:          "dot content path",
			torrent:       &qbt.Torrent{ContentPath: "."},
			candidateFiles: qbt.TorrentFiles{{Name: "f.mkv"}},
			expected:      "",
		},
		{
			name:          "single file extracts dir",
			torrent:       &qbt.Torrent{ContentPath: "/downloads/show/f.mkv"},
			candidateFiles: qbt.TorrentFiles{{Name: "f.mkv"}},
			expected:      "/downloads/show",
		},
		{
			name:          "single file relative path returns empty",
			torrent:       &qbt.Torrent{ContentPath: "file.mkv"},
			candidateFiles: qbt.TorrentFiles{{Name: "file.mkv"}},
			expected:      "",
		},
		{
			name:          "single file normalizes backslashes",
			torrent:       &qbt.Torrent{ContentPath: "/downloads\\tv\\Show\\file.mkv"},
			candidateFiles: qbt.TorrentFiles{{Name: "file.mkv"}},
			expected:      "/downloads/tv/Show",
		},
		{
			name:          "multi-file uses content path",
			torrent:       &qbt.Torrent{ContentPath: "/downloads/show"},
			candidateFiles: qbt.TorrentFiles{{Name: "f1.mkv"}, {Name: "f2.mkv"}},
			expected:      "/downloads/show",
		},
		{
			name:          "multi-file cleans trailing slash",
			torrent:       &qbt.Torrent{ContentPath: "/downloads/show/"},
			candidateFiles: qbt.TorrentFiles{{Name: "f1.mkv"}, {Name: "f2.mkv"}},
			expected:      "/downloads/show",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.expected, resolveRootlessContentDir(tt.torrent, tt.candidateFiles))
		})
	}
}

