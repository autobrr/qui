// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/moistari/rls"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Discussion #2262: torrents in the wild encode announce-list as a bencode
// string instead of a list of tiers. The parse entry points must tolerate
// this; the leniency lives in autobrr/go-torrent, so this pins the contract
// across dependency bumps.
func TestParseTorrentTolerantOfMalformedAnnounceList(t *testing.T) {
	info := "d6:lengthi1e4:name8:Test.Rel12:piece lengthi16384e6:pieces20:" + strings.Repeat("a", 20) + "e"
	tests := []struct {
		name         string
		announceList string
		wantDomain   string
	}{
		// An empty-string announce-list is dropped, so the domain falls back
		// to announce; a bare URL string is salvaged as a single tier.
		{name: "empty string", announceList: "0:", wantDomain: "tracker.example.org"},
		{name: "url string", announceList: "29:https://other.example.net/ann", wantDomain: "other.example.net"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := []byte("d8:announce31:https://tracker.example.org/ann13:announce-list" + tt.announceList + "4:info" + info + "e")

			meta, err := ParseTorrentMetadataWithInfo(data)
			require.NoError(t, err)
			assert.Equal(t, "Test.Rel", meta.Name)
			// SHA-1 of the raw info substring: the hash must come from the
			// bytes as served, not a re-encoded form.
			assert.Equal(t, "1698fdcc4e79d4af9ed4406b72639ccdeec12c61", meta.HashV1)

			assert.Equal(t, tt.wantDomain, ParseTorrentAnnounceDomain(data))
		})
	}
}

// TestDetermineContentType tests the unified content type detection including
// expanded JAV/RIAJ/date/xxx corner cases.
func TestDetermineContentType(t *testing.T) {
	tests := []struct {
		name        string
		release     rls.Release
		wantType    string
		wantCats    []int
		wantSearch  string
		wantCaps    []string
		wantIsMusic bool
	}{
		{
			name:        "Movie",
			release:     rls.Release{Type: rls.Movie, Title: "Test Movie", Year: 2024},
			wantType:    "movie",
			wantCats:    []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "TV Episode",
			release:     rls.Release{Type: rls.Episode, Title: "Test Show", Series: 1, Episode: 1},
			wantType:    "tv",
			wantCats:    []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "TV Series",
			release:     rls.Release{Type: rls.Series, Title: "Test Show", Series: 1},
			wantType:    "tv",
			wantCats:    []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "Music",
			release:     rls.Release{Type: rls.Music, Artist: "Test Artist", Title: "Test Album"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "Audiobook",
			release:     rls.Release{Type: rls.Audiobook, Title: "Test Audiobook"},
			wantType:    "audiobook",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "Book",
			release:     rls.Release{Type: rls.Book, Title: "Test Book"},
			wantType:    "book",
			wantCats:    []int{7000, 7010, 7020, 7040, 7050, 7060},
			wantSearch:  "book",
			wantCaps:    []string{"book-search"},
			wantIsMusic: false,
		},
		{
			name:        "Comic",
			release:     rls.Release{Type: rls.Comic, Title: "Test Comic"},
			wantType:    "comic",
			wantCats:    []int{7000, 7030},
			wantSearch:  "book",
			wantCaps:    []string{"book-search"},
			wantIsMusic: false,
		},
		{
			name:        "Game",
			release:     rls.Release{Type: rls.Game, Title: "Test Game"},
			wantType:    "game",
			wantCats:    []int{4000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "App",
			release:     rls.Release{Type: rls.App, Title: "Test App"},
			wantType:    "app",
			wantCats:    []int{4000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Unknown with Series/Episode (TV fallback)",
			release:     rls.Release{Type: rls.Unknown, Title: "Test", Series: 1, Episode: 1},
			wantType:    "tv",
			wantCats:    []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "Unknown with Year (Movie fallback)",
			release:     rls.Release{Type: rls.Unknown, Title: "Test", Year: 2024},
			wantType:    "movie",
			wantCats:    []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "Adult content (date pattern)",
			release:     rls.Release{Type: rls.Episode, Title: "FakeStudioZ 010124_001-1PON", Series: 1, Episode: 1},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "JAV (4-letter) -> strip -> parse as TV",
			release:     rls.Release{Type: rls.Unknown, Title: "AAEJ-123 Some Show S01E02 1080p"},
			wantType:    "tv",
			wantCats:    []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "JAV (3-letter) -> strip -> parse as Movie",
			release:     rls.Release{Type: rls.Unknown, Title: "IPX-123 Big Movie 1080p"},
			wantType:    "movie",
			wantCats:    []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "lowercase jav code -> TV",
			release:     rls.Release{Type: rls.Unknown, Title: "ipx-123 Some Show S02E03 720p"},
			wantType:    "tv",
			wantCats:    []int{5000, 5010, 5020, 5030, 5040, 5045, 5070, 5080},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "JAV-strip -> music detection",
			release:     rls.Release{Type: rls.Unknown, Title: "IPX-123 Test Artist - Test Album (2020) [GROUP]"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "RIAJ code -> music detection",
			release:     rls.Release{Type: rls.Unknown, Title: "ABCD-1234 Some Album"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "Mainstream movie xXx franchise",
			release:     rls.Release{Type: rls.Movie, Title: "xXx", Year: 2002},
			wantType:    "movie",
			wantCats:    []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "Mainstream movie xXx Return of Xander Cage",
			release:     rls.Release{Type: rls.Movie, Title: "xXx return of xander cage", Year: 2017},
			wantType:    "movie",
			wantCats:    []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "Music artist XXXTentacion not adult",
			release:     rls.Release{Type: rls.Music, Artist: "XXXTentacion", Title: "17"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "xxx inside word not adult",
			release:     rls.Release{Type: rls.Unknown, Title: "fooxxxbar sample"},
			wantType:    "unknown",
			wantCats:    []int{},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Date pattern (adult) without extra markers",
			release:     rls.Release{Type: rls.Unknown, Title: "010124_001 Some title"},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Bracketed date pattern triggers adult",
			release:     rls.Release{Type: rls.Unknown, Title: "[2023.08.01] Some Title"},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "xxx in subtitle triggers adult",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioX", Subtitle: "25 11 21 FakeActress XXX 2160p MP4-WRB"},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "xxx in collection triggers adult",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioX", Collection: "XXX", Year: 2025},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Porn scene naming with XXX in title",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioX XXX FakeActress 2160p MP4-WRB", Year: 2025},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Porn scene naming 2 with XXX in title",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioY XXX FakeActress2 1080p MP4-WRB", Year: 2025},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Unknown without hints",
			release:     rls.Release{Type: rls.Unknown, Title: "Test"},
			wantType:    "unknown",
			wantCats:    []int{},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineContentType(&tt.release)

			assert.Equal(t, tt.wantType, result.ContentType)
			assert.Equal(t, tt.wantCats, result.Categories)
			assert.Equal(t, tt.wantSearch, result.SearchType)
			assert.Equal(t, tt.wantCaps, result.RequiredCaps)
			assert.Equal(t, tt.wantIsMusic, result.IsMusic)
		})
	}
}

// TestDetermineContentTypeWithFiles covers discussion #1734: file extensions
// are ground truth for the audio/video split, so a byte-weighted extension
// signal overrides the name-based type when the two disagree.
func TestDetermineContentTypeWithFiles(t *testing.T) {
	audioFiles := qbt.TorrentFiles{
		{Name: "Artist - Album (2019) [FLAC]/01 - Track.flac", Size: 40_000_000},
		{Name: "Artist - Album (2019) [FLAC]/cover.jpg", Size: 900_000},
	}
	videoFiles := qbt.TorrentFiles{
		{Name: "Release/release.mkv", Size: 8_000_000_000},
		{Name: "Release/release.nfo", Size: 4_000},
	}

	tests := []struct {
		name        string
		release     rls.Release
		files       qbt.TorrentFiles
		wantType    string
		wantIsMusic bool
	}{
		{
			name:        "audio files force music over a tv parse",
			release:     rls.Release{Type: rls.Episode, Title: "Test Album", Series: 1, Episode: 1},
			files:       audioFiles,
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			name:        "audio files force music over a movie parse",
			release:     rls.Release{Type: rls.Movie, Title: "Test Album", Year: 2019},
			files:       audioFiles,
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			name:        "audio files force music over an unknown parse",
			release:     rls.Release{Type: rls.Unknown, Title: "weird_name_2019"},
			files:       audioFiles,
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			// The music-to-video name rescue must not undo the file signal:
			// a music parse with a video-looking token stays music when the
			// bytes are audio.
			name:        "audio files beat video tokens in the name",
			release:     rls.Release{Type: rls.Music, Title: "Test Album", Resolution: "1080p"},
			files:       audioFiles,
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			name:        "video files rewrite a music parse with episode metadata to tv",
			release:     rls.Release{Type: rls.Music, Title: "Test Show", Series: 2, Episode: 3},
			files:       videoFiles,
			wantType:    "tv",
			wantIsMusic: false,
		},
		{
			name:        "video files rewrite a music parse without episode metadata to movie",
			release:     rls.Release{Type: rls.Music, Title: "Test Title"},
			files:       videoFiles,
			wantType:    "movie",
			wantIsMusic: false,
		},
		{
			name:        "video files keep a movie parse unchanged",
			release:     rls.Release{Type: rls.Movie, Title: "Test Movie", Year: 2019},
			files:       videoFiles,
			wantType:    "movie",
			wantIsMusic: false,
		},
		{
			name:        "audio files keep a music parse music",
			release:     rls.Release{Type: rls.Music, Artist: "Test Artist", Title: "Test Album"},
			files:       audioFiles,
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			name:        "audio files keep an audiobook parse audiobook",
			release:     rls.Release{Type: rls.Audiobook, Title: "Test Audiobook"},
			files:       qbt.TorrentFiles{{Name: "Author - Book/book.m4b", Size: 300_000_000}},
			wantType:    "audiobook",
			wantIsMusic: true,
		},
		{
			name:    "video bytes dominate a movie with a soundtrack folder",
			release: rls.Release{Type: rls.Music, Title: "Test Title"},
			files: qbt.TorrentFiles{
				{Name: "Movie/movie.mkv", Size: 8_000_000_000},
				{Name: "Movie/Soundtrack/01 - theme.mp3", Size: 9_000_000},
			},
			wantType:    "movie",
			wantIsMusic: false,
		},
		{
			// Booklet scans outweigh individual tracks in many lossy releases;
			// the signal weighs total bytes per class, not the largest file.
			name:    "audio bytes dominate an album with booklet scans",
			release: rls.Release{Type: rls.Unknown, Title: "Test Album"},
			files: qbt.TorrentFiles{
				{Name: "Album/01.mp3", Size: 9_000_000},
				{Name: "Album/02.mp3", Size: 9_000_000},
				{Name: "Album/Scans/booklet.tif", Size: 12_000_000},
			},
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			// Ties go to the content class, mirroring the RED/OPS gate.
			name:    "audio wins an exact byte tie",
			release: rls.Release{Type: rls.Unknown, Title: "Test Album"},
			files: qbt.TorrentFiles{
				{Name: "Album/01.flac", Size: 10_000_000},
				{Name: "Album/booklet.pdf", Size: 10_000_000},
			},
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			name:    "video wins an exact byte tie",
			release: rls.Release{Type: rls.Music, Title: "Test Title"},
			files: qbt.TorrentFiles{
				{Name: "Release/release.mkv", Size: 10_000_000},
				{Name: "Release/cover.jpg", Size: 10_000_000},
			},
			wantType:    "movie",
			wantIsMusic: false,
		},
		{
			name:        "uppercase audio extension still counts",
			release:     rls.Release{Type: rls.Episode, Title: "Test Album", Series: 1},
			files:       qbt.TorrentFiles{{Name: "Album/01 - Track.FLAC", Size: 40_000_000}},
			wantType:    "music",
			wantIsMusic: true,
		},
		{
			name:        "no files falls back to the name-based rescue",
			release:     rls.Release{Type: rls.Music, Title: "Test Title", Resolution: "1080p"},
			files:       qbt.TorrentFiles{},
			wantType:    "movie",
			wantIsMusic: false,
		},
		{
			// #2336: the file names of a disc rip say nothing, and the folder of
			// this one has no year or resolution, so only the layout is left.
			name:    "a disc layout classifies as a movie when the name gives nothing",
			release: rls.Release{Type: rls.Unknown, Title: "Some Feature DVDR"},
			files: qbt.TorrentFiles{
				{Name: "Some Feature DVDR/VIDEO_TS/VTS_01_1.VOB", Size: 4_000_000_000},
				{Name: "Some Feature DVDR/VIDEO_TS/VIDEO_TS.IFO", Size: 20_000},
			},
			wantType:    "movie",
			wantIsMusic: false,
		},
		{
			// The layout must not overrule structure the name does supply.
			name:    "a disc layout keeps the tv structure of the name",
			release: rls.Release{Type: rls.Unknown, Title: "Some Show", Series: 2},
			files: qbt.TorrentFiles{
				{Name: "Some Show S02 DVDR/VIDEO_TS/VTS_01_1.VOB", Size: 4_000_000_000},
			},
			wantType:    "tv",
			wantIsMusic: false,
		},
		{
			name:    "no disc layout leaves an unknown name unknown",
			release: rls.Release{Type: rls.Unknown, Title: "Some Feature DVDR"},
			files: qbt.TorrentFiles{
				{Name: "Some Feature DVDR/feature.vob", Size: 4_000_000_000},
			},
			wantType:    "unknown",
			wantIsMusic: false,
		},
		{
			name:        "neither class dominant falls back to the name",
			release:     rls.Release{Type: rls.Book, Title: "Test Book"},
			files:       qbt.TorrentFiles{{Name: "Author - Book.epub", Size: 2_000_000}},
			wantType:    "book",
			wantIsMusic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineContentTypeWithFiles(&tt.release, tt.files)

			assert.Equal(t, tt.wantType, result.ContentType)
			assert.Equal(t, tt.wantIsMusic, result.IsMusic)
			if tt.wantIsMusic {
				assert.Equal(t, []int{3000}, result.Categories)
			}
		})
	}
}

// TestRuleContentTypeInfo covers the category mapping rules from discussion
// #1734: a rule names a content type as a string, and the classification must
// come out identical to a release that parsed as that type.
func TestRuleContentTypeInfo(t *testing.T) {
	tests := []struct {
		contentType string
		wantSearch  string
		wantIsMusic bool
	}{
		{contentType: "movie", wantSearch: "movie"},
		{contentType: "tv", wantSearch: "tvsearch"},
		{contentType: "music", wantSearch: "music", wantIsMusic: true},
		{contentType: "audiobook", wantSearch: "music", wantIsMusic: true},
		{contentType: "book", wantSearch: "book"},
		{contentType: "comic", wantSearch: "book"},
		{contentType: "game", wantSearch: "search"},
		{contentType: "app", wantSearch: "search"},
		{contentType: "adult"},
		{contentType: "unknown"},
		{contentType: ""},
	}

	for _, tt := range tests {
		wantOK := tt.wantSearch != ""
		t.Run("contentType="+tt.contentType, func(t *testing.T) {
			info, ok := RuleContentTypeInfo(tt.contentType)
			assert.Equal(t, wantOK, ok)
			if !wantOK {
				return
			}
			assert.Equal(t, tt.contentType, info.ContentType)
			assert.Equal(t, tt.wantSearch, info.SearchType)
			assert.Equal(t, tt.wantIsMusic, info.IsMusic)
			assert.NotEmpty(t, info.Categories)
		})
	}
}

// TestGameSceneGroupDetection verifies that releases from known game scene groups
// are correctly detected as games via the rls library's group detection.
func TestGameSceneGroupDetection(t *testing.T) {
	tests := []struct {
		name     string
		release  string
		wantType string
		wantCats []int
	}{
		{
			name:     "RUNE game release",
			release:  "Oddsparks.An.Automation.Adventure.Coaster.Rush-RUNE",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "CODEX game release",
			release:  "Some.Game.v1.0-CODEX",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "SKIDROW game release",
			release:  "Another.Game-SKIDROW",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "PLAZA game release",
			release:  "Game.Update.v1.2-PLAZA",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "Movie release unchanged",
			release:  "Random.Movie.2024.1080p.BluRay.x264-GROUP",
			wantType: "movie",
			wantCats: []int{2000, 2010, 2020, 2030, 2040, 2045, 2050, 2060, 2070, 2080},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := rls.ParseString(tt.release)
			result := DetermineContentType(&parsed)

			assert.Equal(t, tt.wantType, result.ContentType, "content type mismatch for %s", tt.release)
			assert.Equal(t, tt.wantCats, result.Categories, "categories mismatch for %s", tt.release)
		})
	}
}

func TestBuildTorrentFilesFromInfo_EmptyPathComponents(t *testing.T) {
	// libtorrent maps an empty path component to "_" instead of dropping it, so the
	// hardlink/reflink tree has to be built at that same path or qBittorrent reports
	// the injected torrent as missing data.
	info := metainfo.Info{
		Name:        "Release",
		PieceLength: 262144,
		Files: []metainfo.FileInfo{
			{Path: []string{"", "file.mkv"}, Length: 1},
			{Path: []string{"sub", "", "other.mkv"}, Length: 1},
		},
	}

	files := BuildTorrentFilesFromInfo("Release", info)

	require.Len(t, files, 2)
	assert.Equal(t, "Release/_/file.mkv", files[0].Name)
	assert.Equal(t, "Release/sub/_/other.mkv", files[1].Name)
}
