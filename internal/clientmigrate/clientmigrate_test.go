package clientmigrate

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/internal/qbittorrent"

	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
)

func TestDelugeTorrentComplete(t *testing.T) {
	t.Parallel()

	unfinished := []any{map[string]any{"piece": int64(3), "bitmask": "\x0f"}}

	tests := []struct {
		name      string
		fr        qbittorrent.Fastresume
		numPieces int
		want      bool
	}{
		{
			name:      "complete",
			fr:        qbittorrent.Fastresume{Pieces: "\x01\x01\x01", FilePriority: []int{1, 1}},
			numPieces: 3,
			want:      true,
		},
		{
			name:      "missing piece",
			fr:        qbittorrent.Fastresume{Pieces: "\x01\x00\x01", FilePriority: []int{1, 1}},
			numPieces: 3,
			want:      false,
		},
		{
			name:      "never checked, short pieces",
			fr:        qbittorrent.Fastresume{Pieces: "", FilePriority: []int{1}},
			numPieces: 3,
			want:      false,
		},
		{
			name:      "unfinished pieces present",
			fr:        qbittorrent.Fastresume{Pieces: "\x01\x01\x01", Unfinished: &unfinished},
			numPieces: 3,
			want:      false,
		},
		{
			name:      "do-not-download file",
			fr:        qbittorrent.Fastresume{Pieces: "\x01\x01\x01", FilePriority: []int{1, 0}},
			numPieces: 3,
			want:      false,
		},
		{
			name:      "verified seed-mode bit set",
			fr:        qbittorrent.Fastresume{Pieces: "\x03\x03\x03", FilePriority: []int{1}},
			numPieces: 3,
			want:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, delugeTorrentComplete(&tt.fr, tt.numPieces))
		})
	}
}

func TestRTorrentTorrentComplete(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		bitfield any
		want     bool
	}{
		{name: "complete", bitfield: int64(10), want: true},
		{name: "empty", bitfield: int64(0), want: false},
		{name: "partial raw bitfield", bitfield: "\xff\xc0", want: false},
		{name: "absent", bitfield: nil, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			resume := &RTorrentLibTorrentResumeFile{Bitfield: tt.bitfield}
			assert.Equal(t, tt.want, rtorrentTorrentComplete(resume, 10))
		})
	}
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

func TestFirstNonZero(t *testing.T) {
	t.Parallel()

	assert.Equal(t, int64(5), firstNonZero(0, 5, 3))
	assert.Equal(t, int64(0), firstNonZero(0, 0))
	assert.Equal(t, int64(1), firstNonZero(1))
}
