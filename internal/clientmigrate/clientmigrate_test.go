package clientmigrate

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/klauspost/compress/gzip"
	"github.com/stretchr/testify/assert"
)

// testInfo is a 3-piece multi-file torrent: file a spans pieces 0-1, file b
// spans pieces 1-2 (piece length 16384, total 42000 bytes)
func testInfo() metainfo.Info {
	return metainfo.Info{
		Name:        "Test.Torrent.2024.1080p.WEB-TEST",
		PieceLength: 16384,
		Pieces:      make([]byte, 3*20),
		Files: []metainfo.FileInfo{
			{Length: 30000, Path: []string{"a.mkv"}},
			{Length: 12000, Path: []string{"b.mkv"}},
		},
	}
}

func TestWantedPieces(t *testing.T) {
	t.Parallel()

	lengths := []int64{30000, 12000}

	assert.Equal(t, []bool{true, true, true}, wantedPieces(lengths, []bool{true, true}, 16384, 3))
	assert.Equal(t, []bool{true, true, false}, wantedPieces(lengths, []bool{true, false}, 16384, 3))
	assert.Equal(t, []bool{false, true, true}, wantedPieces(lengths, []bool{false, true}, 16384, 3))
	assert.Equal(t, []bool{false, false, false}, wantedPieces(lengths, []bool{false, false}, 16384, 3))
}

func TestDelugePieces(t *testing.T) {
	t.Parallel()

	info := testInfo()

	tests := []struct {
		name         string
		fr           Fastresume
		wantPieces   string
		wantComplete bool
	}{
		{
			name:         "complete",
			fr:           Fastresume{Pieces: "\x01\x01\x01", FilePriority: []int{1, 1}},
			wantPieces:   "\x01\x01\x01",
			wantComplete: true,
		},
		{
			name:         "missing wanted piece",
			fr:           Fastresume{Pieces: "\x01\x00\x01", FilePriority: []int{1, 1}},
			wantPieces:   "\x01\x00\x01",
			wantComplete: false,
		},
		{
			name:         "never checked, short pieces",
			fr:           Fastresume{Pieces: "", FilePriority: []int{1, 1}},
			wantComplete: false,
		},
		{
			name:         "deselected file, wanted pieces present",
			fr:           Fastresume{Pieces: "\x01\x01\x00", FilePriority: []int{1, 0}},
			wantPieces:   "\x01\x01\x00",
			wantComplete: true,
		},
		{
			name:         "verified seed-mode bit set",
			fr:           Fastresume{Pieces: "\x03\x03\x03", FilePriority: []int{1, 1}},
			wantPieces:   "\x01\x01\x01",
			wantComplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pieces, complete := delugePieces(&tt.fr, &info)
			assert.Equal(t, tt.wantComplete, complete)
			if tt.wantComplete {
				assert.Equal(t, tt.wantPieces, pieces)
			}
		})
	}
}

func TestRTorrentPieces(t *testing.T) {
	t.Parallel()

	info := testInfo()

	tests := []struct {
		name         string
		resume       RTorrentLibTorrentResumeFile
		wantPieces   string
		wantComplete bool
	}{
		{
			name:         "complete integer bitfield",
			resume:       RTorrentLibTorrentResumeFile{Bitfield: int64(3)},
			wantPieces:   "\x01\x01\x01",
			wantComplete: true,
		},
		{
			name:         "empty integer bitfield",
			resume:       RTorrentLibTorrentResumeFile{Bitfield: int64(0)},
			wantComplete: false,
		},
		{
			name:         "partial raw bitfield, all files wanted",
			resume:       RTorrentLibTorrentResumeFile{Bitfield: "\xc0"},
			wantComplete: false,
		},
		{
			name: "partial raw bitfield, missing pieces only in off file",
			resume: RTorrentLibTorrentResumeFile{
				Bitfield: "\xc0",
				Files:    []RTorrentResumeFileEntry{{Priority: 1}, {Priority: 0}},
			},
			wantPieces:   "\x01\x01\x00",
			wantComplete: true,
		},
		{
			name:         "absent bitfield",
			resume:       RTorrentLibTorrentResumeFile{},
			wantComplete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pieces, complete := rtorrentPieces(&tt.resume, &info)
			assert.Equal(t, tt.wantComplete, complete)
			if tt.wantComplete {
				assert.Equal(t, tt.wantPieces, pieces)
			}
		})
	}
}

func TestTransmissionPieces(t *testing.T) {
	t.Parallel()

	info := testInfo()

	tests := []struct {
		name         string
		resume       TransmissionResumeFile
		wantPieces   string
		wantComplete bool
	}{
		{
			name:         "all blocks",
			resume:       TransmissionResumeFile{Progress: TransmissionResumeFileProgress{Blocks: "all"}},
			wantPieces:   "\x01\x01\x01",
			wantComplete: true,
		},
		{
			name:         "no blocks",
			resume:       TransmissionResumeFile{Progress: TransmissionResumeFileProgress{Blocks: "none"}},
			wantComplete: false,
		},
		{
			name:         "partial bitfield, all files wanted",
			resume:       TransmissionResumeFile{Progress: TransmissionResumeFileProgress{Blocks: "\xc0"}},
			wantComplete: false,
		},
		{
			name: "partial bitfield, missing blocks only in dnd file",
			resume: TransmissionResumeFile{
				Progress: TransmissionResumeFileProgress{Blocks: "\xc0"},
				Dnd:      []int{0, 1},
			},
			wantPieces:   "\x01\x01\x00",
			wantComplete: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pieces, complete := transmissionPieces(&tt.resume, &info)
			assert.Equal(t, tt.wantComplete, complete)
			if tt.wantComplete {
				assert.Equal(t, tt.wantPieces, pieces)
			}
		})
	}
}

func TestTransmissionPiecesMultiBlockPieces(t *testing.T) {
	t.Parallel()

	// 32 KiB pieces over a 48 KiB file: piece 0 = blocks 0-1, piece 1 = block 2
	info := metainfo.Info{
		Name:        "Test.Torrent.2024.1080p.WEB-TEST.mkv",
		PieceLength: 32768,
		Pieces:      make([]byte, 2*20),
		Length:      49152,
	}

	resume := TransmissionResumeFile{Progress: TransmissionResumeFileProgress{Blocks: "all"}}
	pieces, complete := transmissionPieces(&resume, &info)
	assert.True(t, complete)
	assert.Equal(t, "\x01\x01", pieces)

	// blocks 0 and 2 present, block 1 missing: piece 0 incomplete
	resume = TransmissionResumeFile{Progress: TransmissionResumeFileProgress{Blocks: "\xa0"}}
	pieces, complete = transmissionPieces(&resume, &info)
	assert.False(t, complete)
	assert.Equal(t, "\x00\x01", pieces)
}

func TestTransmissionFilePriorities(t *testing.T) {
	t.Parallel()

	resume := TransmissionResumeFile{
		Priority: []int{0, 1},
		Dnd:      []int{1, 0},
	}

	assert.Equal(t, []int{0, 6}, transmissionFilePriorities(&resume, 2))
	// mismatched lengths fall back to all-normal
	assert.Equal(t, []int{1, 1, 1}, transmissionFilePriorities(&resume, 3))
}

func TestRTorrentFilePriorities(t *testing.T) {
	t.Parallel()

	entries := []RTorrentResumeFileEntry{
		{Priority: 0},
		{Priority: 1},
		{Priority: 2},
	}

	assert.Equal(t, []int{0, 1, 6}, rtorrentFilePriorities(entries, 3))
	// length mismatch falls back to all-normal
	assert.Equal(t, []int{1, 1}, rtorrentFilePriorities(entries, 2))
	assert.Equal(t, []int{1}, rtorrentFilePriorities(nil, 1))
}

func TestRTorrentTrackers(t *testing.T) {
	t.Parallel()

	file := &metainfo.MetaInfo{
		Announce: "http://tracker.example/announce",
		AnnounceList: metainfo.AnnounceList{
			{"http://tracker.example/announce"},
			{"udp://backup.example/announce"},
		},
	}

	tests := []struct {
		name   string
		resume RTorrentLibTorrentResumeFile
		want   [][]string
	}{
		{
			name: "all enabled",
			resume: RTorrentLibTorrentResumeFile{Trackers: map[string]map[string]int{
				"http://tracker.example/announce": {"enabled": 1},
				"udp://backup.example/announce":   {"enabled": 1},
				"dht://":                          {"enabled": 1},
			}},
			want: [][]string{{"http://tracker.example/announce"}, {"udp://backup.example/announce"}},
		},
		{
			name: "disabled udp tracker dropped",
			resume: RTorrentLibTorrentResumeFile{Trackers: map[string]map[string]int{
				"http://tracker.example/announce": {"enabled": 1},
				"udp://backup.example/announce":   {"enabled": 0},
			}},
			want: [][]string{{"http://tracker.example/announce"}},
		},
		{
			name: "runtime-added tracker appended",
			resume: RTorrentLibTorrentResumeFile{Trackers: map[string]map[string]int{
				"http://tracker.example/announce": {"enabled": 1},
				"udp://backup.example/announce":   {"enabled": 1},
				"http://extra.example/announce":   {"enabled": 1, "extra_tracker": 1},
			}},
			want: [][]string{
				{"http://tracker.example/announce"},
				{"udp://backup.example/announce"},
				{"http://extra.example/announce"},
			},
		},
		{
			name: "all disabled keeps torrent trackers by staying nil",
			resume: RTorrentLibTorrentResumeFile{Trackers: map[string]map[string]int{
				"http://tracker.example/announce": {"enabled": 0},
				"udp://backup.example/announce":   {"enabled": 0},
			}},
			want: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, rtorrentTrackers(file, &tt.resume))
		})
	}
}

func TestTransmissionRatioLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		limit TransmissionResumeFileRatioLimit
		want  int64
	}{
		{name: "global", limit: TransmissionResumeFileRatioLimit{RatioMode: 0}, want: -2000},
		{name: "single", limit: TransmissionResumeFileRatioLimit{RatioMode: 1, RatioLimit: "2.500000"}, want: 2500},
		{name: "unlimited", limit: TransmissionResumeFileRatioLimit{RatioMode: 2}, want: -1000},
		{name: "unparseable ratio", limit: TransmissionResumeFileRatioLimit{RatioMode: 1, RatioLimit: "x"}, want: -2000},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, transmissionRatioLimit(tt.limit))
		})
	}
}

func TestTransmissionSpeedLimit(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(-1), transmissionSpeedLimit(TransmissionResumeFileSpeedLimit{SpeedBPS: 1000}))
	assert.Equal(t, int64(-1), transmissionSpeedLimit(TransmissionResumeFileSpeedLimit{SpeedBPS: 1000, UseSpeedLimit: 1, UseGlobalSpeedLimit: 1}))
	assert.Equal(t, int64(1000), transmissionSpeedLimit(TransmissionResumeFileSpeedLimit{SpeedBPS: 1000, UseSpeedLimit: 1}))
}

func TestReadDelugeLabels(t *testing.T) {
	t.Parallel()

	configDir := t.TempDir()
	stateDir := filepath.Join(configDir, "state")
	if err := os.MkdirAll(stateDir, 0o700); err != nil {
		t.Fatal(err)
	}

	labelConf := `{
  "file": 1,
  "format": 1
}{
  "labels": {"tv": {}, "movies": {}},
  "torrent_labels": {"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "tv"}
}`
	if err := os.WriteFile(filepath.Join(configDir, "label.conf"), []byte(labelConf), 0o600); err != nil {
		t.Fatal(err)
	}

	labels := readDelugeLabels(stateDir)
	assert.Equal(t, map[string]string{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa": "tv"}, labels)

	// absent file means no labels
	assert.Nil(t, readDelugeLabels(t.TempDir()))
}

func TestDelugeMigrateSkipsMismatchedInfoHash(t *testing.T) {
	t.Parallel()

	stateDir := t.TempDir()
	qbitDir := t.TempDir()
	saveDir := t.TempDir()

	info := map[string]any{
		"name":         "Test.Torrent.2024.1080p.WEB-TEST.mkv",
		"piece length": int64(16384),
		"pieces":       string(make([]byte, 20)),
		"length":       int64(1000),
	}
	infoBytes, err := bencode.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}

	mi := metainfo.MetaInfo{InfoBytes: infoBytes}
	torrentID := mi.HashInfoBytes().HexString()

	torrentFile, err := os.Create(filepath.Join(stateDir, torrentID+".torrent"))
	if err != nil {
		t.Fatal(err)
	}
	if err := mi.Write(torrentFile); err != nil {
		t.Fatal(err)
	}
	torrentFile.Close()

	writeResume := func(infoHash []byte) {
		resume := Fastresume{
			InfoHash:          infoHash,
			Pieces:            "\x01",
			FilePriority:      []int{1},
			SavePath:          saveDir,
			LibTorrentVersion: "2.0.14.0",
		}
		blob, err := bencode.Marshal(&resume)
		if err != nil {
			t.Fatal(err)
		}
		outer, err := bencode.Marshal(map[string]any{torrentID: string(blob)})
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(stateDir, "torrents.fastresume"), outer, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	opts := Options{Source: "deluge", SourceDir: stateDir, QbitDir: qbitDir}

	// valid length but belonging to different content: nothing is written
	writeResume(bytes.Repeat([]byte{0xaa}, 20))
	if err := NewDelugeImporter(opts).Migrate(); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(qbitDir)
	if err != nil {
		t.Fatal(err)
	}
	assert.Empty(t, entries)

	// control: the matching hash imports the torrent
	writeResume(mi.HashInfoBytes().Bytes())
	if err := NewDelugeImporter(opts).Migrate(); err != nil {
		t.Fatal(err)
	}
	entries, err = os.ReadDir(qbitDir)
	if err != nil {
		t.Fatal(err)
	}
	assert.Len(t, entries, 2)
}

func TestArchiveDirExcludesOwnArchive(t *testing.T) {
	t.Parallel()

	// --source-dir . puts qbt_backup inside the walked tree; the archive must
	// not be written into itself
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "torrents.fastresume"), []byte("data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "qbt_backup"), 0o700); err != nil {
		t.Fatal(err)
	}

	archiveName := filepath.Join(dir, "qbt_backup", "backup.tar.gz")
	if err := archiveDir(dir, archiveName); err != nil {
		t.Fatal(err)
	}

	f, err := os.Open(archiveName)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}

	var entries []string
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err != nil {
			break
		}
		entries = append(entries, hdr.Name)
	}

	assert.Equal(t, []string{"torrents.fastresume"}, entries)
}

func TestFirstNonZero(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(5), firstNonZero(0, 5, 3))
	assert.Equal(t, int64(0), firstNonZero(0, 0))
	assert.Equal(t, int64(1), firstNonZero(1))
}
