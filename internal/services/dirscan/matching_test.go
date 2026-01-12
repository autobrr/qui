package dirscan

import (
	"bytes"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	"github.com/stretchr/testify/require"
)

func buildTorrentBytes(t *testing.T, info metainfo.Info) []byte {
	t.Helper()

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)

	mi := metainfo.MetaInfo{
		InfoBytes: infoBytes,
		Announce:  "https://example.invalid/announce",
	}

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

func TestParseTorrentBytes_MultiFilePrefixesRootFolder(t *testing.T) {
	torrentBytes := buildTorrentBytes(t, metainfo.Info{
		Name:        "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"Example.Show.S01E01.mkv"}, Length: 1},
			{Path: []string{"Example.Show.S01E02.mkv"}, Length: 1},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Equal(t, "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP", parsed.Name)
	require.Len(t, parsed.Files, 2)
	require.Equal(t, "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP/Example.Show.S01E01.mkv", parsed.Files[0].Path)
	require.Equal(t, "Example.Show.S01.1080p.WEB-DL.DDP5.1.x264-GROUP/Example.Show.S01E02.mkv", parsed.Files[1].Path)
}

func TestParseTorrentBytes_MultiFileDoesNotDoublePrefixRootFolder(t *testing.T) {
	torrentBytes := buildTorrentBytes(t, metainfo.Info{
		Name:        "Example.Show.S02.1080p.WEB-DL.x264-GROUP",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"Example.Show.S02.1080p.WEB-DL.x264-GROUP", "Example.Show.S02E01.mkv"}, Length: 1},
		},
	})

	parsed, err := ParseTorrentBytes(torrentBytes)
	require.NoError(t, err)
	require.Len(t, parsed.Files, 1)
	require.Equal(t, "Example.Show.S02.1080p.WEB-DL.x264-GROUP/Example.Show.S02E01.mkv", parsed.Files[0].Path)
}
