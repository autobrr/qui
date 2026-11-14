package clientmigrate

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/autobrr/go-torrent/bencode"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUTorrentPieces(t *testing.T) {
	t.Parallel()

	info := testInfo()

	tests := []struct {
		name         string
		item         UTorrentResumeItem
		wantPieces   string
		wantComplete bool
	}{
		{
			name:         "complete",
			item:         UTorrentResumeItem{Have: "\x07", Prio: "\x08\x08"},
			wantPieces:   "\x01\x01\x01",
			wantComplete: true,
		},
		{
			name:         "missing wanted piece",
			item:         UTorrentResumeItem{Have: "\x05", Prio: "\x08\x08"},
			wantComplete: false,
		},
		{
			name:         "skipped file, wanted pieces present",
			item:         UTorrentResumeItem{Have: "\x03", Prio: "\x08\x00"},
			wantPieces:   "\x01\x01\x00",
			wantComplete: true,
		},
		{
			name:         "skip flag on high priority",
			item:         UTorrentResumeItem{Have: "\x03", Prio: "\x08\x8f"},
			wantPieces:   "\x01\x01\x00",
			wantComplete: true,
		},
		{
			// the same pieces read MSB-first must not pass as complete
			name:         "msb-first bitfield",
			item:         UTorrentResumeItem{Have: "\xe0", Prio: "\x08\x08"},
			wantComplete: false,
		},
		{
			name:         "missing have",
			item:         UTorrentResumeItem{Prio: "\x08\x08"},
			wantComplete: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			pieces, complete := utorrentPieces(&tt.item, &info)
			assert.Equal(t, tt.wantComplete, complete)
			if tt.wantComplete {
				assert.Equal(t, tt.wantPieces, pieces)
			}
		})
	}
}

func TestUTorrentFilePriorities(t *testing.T) {
	t.Parallel()

	assert.Equal(t, []int{0, 1, 1, 6, 6, 0}, utorrentFilePriorities("\x00\x01\x08\x09\x0f\x80", 6))
	assert.Equal(t, []int{1, 1}, utorrentFilePriorities("\x00", 2))
}

func TestUTorrentLayout(t *testing.T) {
	t.Parallel()

	base := t.TempDir()
	info := testInfo()
	single := metainfo.Info{Name: "Test.Torrent.2024.1080p.WEB-TEST.mkv", PieceLength: 16384, Length: 1000}
	moved := filepath.Join(base, "elsewhere", "b.mkv")

	tests := []struct {
		name       string
		info       *metainfo.Info
		item       UTorrentResumeItem
		rules      []PathReplace
		path       string
		wantMapped []string
	}{
		{
			name: "single file unchanged",
			info: &single,
			path: filepath.Join(base, single.Name),
		},
		{
			name:       "single file renamed",
			info:       &single,
			path:       filepath.Join(base, "renamed.mkv"),
			wantMapped: []string{"renamed.mkv"},
		},
		{
			name: "multi file unchanged",
			info: &info,
			path: filepath.Join(base, info.Name),
		},
		{
			name:       "root folder renamed",
			info:       &info,
			path:       filepath.Join(base, "Test Torrent (2024)"),
			wantMapped: []string{"Test Torrent (2024)/a.mkv", "Test Torrent (2024)/b.mkv"},
		},
		{
			name:       "one file renamed into a subfolder",
			info:       &info,
			item:       UTorrentResumeItem{Targets: [][]any{{int64(1), filepath.Join("extras", "c.mkv")}}},
			path:       filepath.Join(base, info.Name),
			wantMapped: []string{info.Name + "/a.mkv", info.Name + "/extras/c.mkv"},
		},
		{
			name:       "one file moved to another Windows drive",
			info:       &info,
			item:       UTorrentResumeItem{Targets: [][]any{{int64(1), `E:\Extras\b.mkv`}}},
			rules:      []PathReplace{{From: `E:\`, To: filepath.Join(base, "elsewhere")}},
			path:       filepath.Join(base, info.Name),
			wantMapped: []string{info.Name + "/a.mkv", filepath.Join(base, "elsewhere", "Extras", "b.mkv")},
		},
		{
			name:       "one file moved out of the content root",
			info:       &info,
			item:       UTorrentResumeItem{Targets: [][]any{{int64(1), moved}}},
			path:       filepath.Join(base, info.Name),
			wantMapped: []string{info.Name + "/a.mkv", moved},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			savePath, mapped, err := utorrentLayout(&tt.item, tt.path, tt.info, tt.rules)
			require.NoError(t, err)
			assert.Equal(t, base, savePath)
			assert.Equal(t, tt.wantMapped, mapped)
		})
	}
}

func TestUTorrentLayoutUnmappedTarget(t *testing.T) {
	t.Parallel()

	info := testInfo()
	contentPath := filepath.Join(t.TempDir(), info.Name)

	// a file moved to a drive no rule maps would land under the content
	// root as Name/E:/Extras/b.mkv on Unix
	for _, target := range []string{`E:\Extras\b.mkv`, `\\nas\share\b.mkv`} {
		item := UTorrentResumeItem{Targets: [][]any{{int64(1), target}}}
		_, _, err := utorrentLayout(&item, contentPath, &info, nil)
		if runtime.GOOS == "windows" {
			assert.NoError(t, err, target)
		} else {
			assert.Error(t, err, target)
		}
	}
}

func TestUTorrentTrackers(t *testing.T) {
	t.Parallel()

	file := &metainfo.MetaInfo{
		AnnounceList: [][]string{
			{"https://tracker-a.test/announce", "https://tracker-b.test/announce"},
			{"https://tracker-c.test/announce"},
		},
	}

	// no resume list keeps the torrent's own tiers
	assert.Equal(t, [][]string(file.UpvertedAnnounceList()), utorrentTrackers(file, nil))

	// removed trackers drop out, added ones get their own tier
	got := utorrentTrackers(file, []any{
		"https://tracker-a.test/announce",
		"https://tracker-c.test/announce https://tracker-d.test/announce",
	})
	assert.Equal(t, [][]string{
		{"https://tracker-a.test/announce"},
		{"https://tracker-c.test/announce"},
		{"https://tracker-d.test/announce"},
	}, got)
}

func TestDecodeCESU8(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "Test 😀 Torrent", decodeCESU8("Test \xed\xa0\xbd\xed\xb8\x80 Torrent"))
	assert.Equal(t, "Test Torrent ö", decodeCESU8("Test Torrent ö"))
	// a lone surrogate is left as-is
	assert.Equal(t, "Test \xed\xa0\xbd", decodeCESU8("Test \xed\xa0\xbd"))
}

func TestUTorrentMigrate(t *testing.T) {
	t.Parallel()

	sourceDir := t.TempDir()
	qbitDir := t.TempDir()
	saveDir := t.TempDir()

	writeTorrent := func(name string) *metainfo.MetaInfo {
		info := map[string]any{
			"name":         name,
			"piece length": int64(16384),
			"pieces":       string(make([]byte, 3*20)),
			"files": []any{
				map[string]any{"length": int64(30000), "path": []any{"a.mkv"}},
				map[string]any{"length": int64(12000), "path": []any{"b.mkv"}},
			},
		}
		infoBytes, err := bencode.Marshal(info)
		require.NoError(t, err)

		mi := &metainfo.MetaInfo{InfoBytes: infoBytes, Announce: "https://tracker-a.test/announce"}
		f, err := os.Create(filepath.Join(sourceDir, name+".torrent"))
		require.NoError(t, err)
		require.NoError(t, mi.Write(f))
		require.NoError(t, f.Close())

		return mi
	}

	done := writeTorrent("Test.Torrent.2024.1080p.WEB-TEST")
	partial := writeTorrent("Other.Torrent.2023.720p.WEB-TEST")

	resume := map[string]any{
		".fileguard": "deadbeef",
		"rec":        map[string]any{"x": int64(1)},
		"Test.Torrent.2024.1080p.WEB-TEST.torrent": map[string]any{
			"added_on":              int64(1700000000),
			"caption":               "Test Torrent (2024)",
			"completed_on":          int64(1700000500),
			"downloaded":            int64(42000),
			"have":                  "\x07",
			"info":                  string(done.HashInfoBytes().Bytes()),
			"label":                 "tv",
			"override_seedsettings": int64(1),
			"path":                  `D:\Downloads\Test.Torrent.2024.1080p.WEB-TEST`,
			"prio":                  "\x08\x08",
			"runtime":               int64(7200),
			"seedtime":              int64(3600),
			"started":               int64(3),
			"trackers":              []any{"https://tracker-a.test/announce"},
			"uploaded":              int64(84000),
			"wanted_ratio":          int64(2500),
		},
		"Other.Torrent.2023.720p.WEB-TEST.torrent": map[string]any{
			"have": "\x03",
			"info": string(partial.HashInfoBytes().Bytes()),
			"path": filepath.Join(saveDir, "Other.Torrent.2023.720p.WEB-TEST"),
			"prio": "\x08\x08",
		},
	}
	blob, err := bencode.Marshal(resume)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(sourceDir, "resume.dat"), blob, 0o600))

	// the Windows save path only resolves through the replace rule
	opts := Options{
		Source:       ClientTypeUTorrent,
		SourceDir:    sourceDir,
		QbitDir:      qbitDir,
		PathReplaces: []PathReplace{{From: `D:\Downloads`, To: saveDir}},
	}
	err = NewUTorrentImporter(opts).Migrate()
	require.NoError(t, err)

	entries, err := os.ReadDir(qbitDir)
	require.NoError(t, err)
	assert.Len(t, entries, 2, "only the complete torrent is imported")

	dat, err := os.ReadFile(filepath.Join(qbitDir, done.HashInfoBytes().HexString()+".fastresume"))
	require.NoError(t, err)

	var fr Fastresume
	require.NoError(t, bencode.Unmarshal(dat, &fr))

	assert.Equal(t, saveDir, fr.SavePath)
	assert.Equal(t, saveDir, fr.QbtSavePath)
	assert.Nil(t, fr.MappedFiles)
	assert.Equal(t, "Test Torrent (2024)", fr.QbtName)
	assert.Equal(t, "tv", fr.QbtCategory)
	assert.Equal(t, int64(1), fr.Paused)
	assert.Equal(t, int64(0), fr.AutoManaged)
	assert.Equal(t, int64(2500), fr.QbtRatioLimit)
	assert.Equal(t, int64(3600), fr.SeedingTime)
	assert.Equal(t, int64(7200), fr.ActiveTime)
	assert.Equal(t, int64(1700000500), fr.CompletedTime)
	assert.Equal(t, int64(84000), fr.TotalUploaded)
	assert.Equal(t, "\x01\x01\x01", fr.Pieces)
	assert.Equal(t, []int{1, 1}, fr.FilePriority)
	assert.Equal(t, [][]string{{"https://tracker-a.test/announce"}}, fr.Trackers)
}
