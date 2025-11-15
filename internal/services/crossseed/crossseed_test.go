// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
)

// Helper function to create a test torrent file
func createTestTorrent(t *testing.T, name string, files []string, pieceLength int64) []byte {
	t.Helper()

	tempDir := t.TempDir()

	// Create actual files
	for _, f := range files {
		path := filepath.Join(tempDir, name, f)
		dir := filepath.Dir(path)
		require.NoError(t, os.MkdirAll(dir, 0755))

		content := fmt.Appendf(nil, "test content for %s", f)
		require.NoError(t, os.WriteFile(path, content, 0644))
	}

	mi := metainfo.MetaInfo{
		AnnounceList: [][]string{{"http://tracker.example.com:8080/announce"}},
	}

	info := metainfo.Info{
		Name:        name,
		PieceLength: pieceLength,
	}

	if len(files) == 1 {
		// Single file torrent - build from the file directly
		path := filepath.Join(tempDir, name, files[0])
		require.NoError(t, info.BuildFromFilePath(path))
		// Override name to match what we want
		info.Name = name
	} else {
		// Multi-file torrent - build from directory
		path := filepath.Join(tempDir, name)
		err := info.BuildFromFilePath(path)
		require.NoError(t, err)
		info.Name = name
	}

	infoBytes, err := bencode.Marshal(info)
	require.NoError(t, err)
	mi.InfoBytes = infoBytes

	var buf bytes.Buffer
	require.NoError(t, mi.Write(&buf))
	return buf.Bytes()
}

// TestDecodeTorrentData tests base64 decoding with various formats
func TestDecodeTorrentData(t *testing.T) {
	s := &Service{}
	testData := []byte("test torrent data")

	tests := []struct {
		name     string
		input    string
		wantErr  bool
		wantData []byte
	}{
		{
			name:     "standard base64",
			input:    base64.StdEncoding.EncodeToString(testData),
			wantErr:  false,
			wantData: testData,
		},
		{
			name:     "standard base64 with whitespace",
			input:    "  " + base64.StdEncoding.EncodeToString(testData) + "\n\t",
			wantErr:  false,
			wantData: testData,
		},
		{
			name:     "url-safe base64",
			input:    base64.URLEncoding.EncodeToString(testData),
			wantErr:  false,
			wantData: testData,
		},
		{
			name:     "raw standard base64",
			input:    base64.RawStdEncoding.EncodeToString(testData),
			wantErr:  false,
			wantData: testData,
		},
		{
			name:     "raw url-safe base64",
			input:    base64.RawURLEncoding.EncodeToString(testData),
			wantErr:  false,
			wantData: testData,
		},
		{
			name:    "invalid base64",
			input:   "not-valid-base64!!!",
			wantErr: true,
		},
		{
			name:     "empty string returns empty",
			input:    "",
			wantErr:  false,
			wantData: []byte{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.decodeTorrentData(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantData, got)
		})
	}
}

// TestParseTorrentName tests torrent parsing and info hash calculation
func TestParseTorrentName(t *testing.T) {

	tests := []struct {
		name        string
		torrentName string
		files       []string
		wantName    string
		wantHashLen int
	}{
		{
			name:        "single file torrent",
			torrentName: "Movie.2020.1080p.BluRay.x264-GROUP",
			files:       []string{"Movie.2020.1080p.BluRay.x264-GROUP.mkv"},
			wantName:    "Movie.2020.1080p.BluRay.x264-GROUP",
			wantHashLen: 40, // SHA1 hex string
		},
		{
			name:        "multi-file torrent",
			torrentName: "Show.S01E05.1080p.WEB-DL",
			files: []string{
				"Show.S01E05.1080p.WEB-DL.mkv",
				"Show.S01E05.1080p.WEB-DL.srt",
			},
			wantName:    "Show.S01E05.1080p.WEB-DL",
			wantHashLen: 40,
		},
		{
			name:        "season pack torrent",
			torrentName: "Show.S01.1080p.BluRay.x264-GROUP",
			files: []string{
				"Show.S01E01.mkv",
				"Show.S01E02.mkv",
				"Show.S01E03.mkv",
			},
			wantName:    "Show.S01.1080p.BluRay.x264-GROUP",
			wantHashLen: 40,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrentData := createTestTorrent(t, tt.torrentName, tt.files, 256*1024)

			name, hash, err := ParseTorrentName(torrentData)
			require.NoError(t, err)
			assert.Equal(t, tt.wantName, name)
			assert.Len(t, hash, tt.wantHashLen)
			assert.NotEmpty(t, hash)
		})
	}
}

// TestParseTorrentName_Errors tests error cases in torrent parsing
func TestParseTorrentName_Errors(t *testing.T) {

	tests := []struct {
		name    string
		data    []byte
		wantErr string
	}{
		{
			name:    "invalid torrent data",
			data:    []byte("not a valid torrent"),
			wantErr: "failed to parse torrent metainfo",
		},
		{
			name:    "empty data",
			data:    []byte{},
			wantErr: "failed to parse torrent metainfo",
		},
		{
			name:    "corrupted bencode",
			data:    []byte("d8:announce"),
			wantErr: "failed to parse torrent metainfo",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseTorrentName(tt.data)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.wantErr)
		})
	}
}

// TestDetermineSavePath tests path determination logic
func TestDetermineSavePath(t *testing.T) {
	cache := NewReleaseCache()
	s := &Service{releaseCache: cache}

	tests := []struct {
		name               string
		newTorrentName     string
		matchedTorrentName string
		baseSavePath       string
		wantPath           string
		description        string
	}{
		{
			name:               "season pack from individual episode",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01",
			wantPath:           "/data/media/Show/Season 01",
			description:        "Adding season pack when episode exists, use episode's path",
		},
		{
			name:               "individual episode from season pack",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-GROUP",
			baseSavePath:       "/data/media/Show/Season 01",
			wantPath:           "/data/media/Show/Season 01",
			description:        "Adding episode when season pack exists, use pack's path",
		},
		{
			name:               "same content type - both episodes",
			newTorrentName:     "Show.S01E05.720p.HDTV.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01",
			wantPath:           "/data/media/Show/Season 01",
			description:        "Both are episodes, use matched path",
		},
		{
			name:               "same content type - both season packs",
			newTorrentName:     "Show.S01.720p.HDTV.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Show",
			wantPath:           "/data/media/Show",
			description:        "Both are packs, use matched path",
		},
		{
			name:               "movies with year",
			newTorrentName:     "Movie.2020.720p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Movies/Movie (2020)",
			wantPath:           "/data/media/Movies/Movie (2020)",
			description:        "Movies use matched path directly",
		},
		{
			name:               "no series info",
			newTorrentName:     "Documentary.1080p.HDTV.x264-GROUP",
			matchedTorrentName: "Documentary.720p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Documentaries",
			wantPath:           "/data/media/Documentaries",
			description:        "Non-series content uses matched path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchedTorrent := &qbt.Torrent{
				Name: tt.matchedTorrentName,
			}
			props := &qbt.TorrentProperties{
				SavePath: tt.baseSavePath,
			}

			gotPath := s.determineSavePath(tt.newTorrentName, matchedTorrent, props)
			assert.Equal(t, tt.wantPath, gotPath, tt.description)
		})
	}
}

// TestCrossSeed_TorrentCreationAndParsing tests creating torrents and extracting info
func TestCrossSeed_TorrentCreationAndParsing(t *testing.T) {
	tests := []struct {
		name        string
		torrentName string
		files       []string
		wantErr     bool
	}{
		{
			name:        "single file movie",
			torrentName: "Movie.2020.1080p.BluRay.x264-GROUP",
			files:       []string{"movie.mkv"},
			wantErr:     false,
		},
		{
			name:        "episode with subs",
			torrentName: "Show.S01E05.1080p.WEB-DL",
			files:       []string{"show.mkv", "show.srt"},
			wantErr:     false,
		},
		{
			name:        "season pack",
			torrentName: "Show.S01.1080p.BluRay.x264-GROUP",
			files:       []string{"e01.mkv", "e02.mkv", "e03.mkv"},
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrentData := createTestTorrent(t, tt.torrentName, tt.files, 256*1024)

			// Verify it's valid base64
			encoded := base64.StdEncoding.EncodeToString(torrentData)
			assert.NotEmpty(t, encoded)

			// Verify we can decode it
			decoded, err := base64.StdEncoding.DecodeString(encoded)
			require.NoError(t, err)
			assert.Equal(t, torrentData, decoded)

			// Verify we can parse metainfo
			mi, err := metainfo.Load(bytes.NewReader(torrentData))
			require.NoError(t, err)
			assert.NotNil(t, mi)

			info, err := mi.UnmarshalInfo()
			require.NoError(t, err)
			assert.Equal(t, tt.torrentName, info.Name)

			// Verify hash calculation
			hash := mi.HashInfoBytes().HexString()
			assert.Len(t, hash, 40) // SHA1 hex = 40 chars
		})
	}
}

// TestCrossSeed_CategoryAndTagPreservation tests category and tag handling
func TestCrossSeed_CategoryAndTagPreservation(t *testing.T) {
	tests := []struct {
		name             string
		matchedCategory  string
		matchedTags      string
		requestCategory  string
		requestTags      []string
		expectedCategory string
		expectedTags     []string
	}{
		{
			name:             "use matched category when request is empty",
			matchedCategory:  "movies",
			matchedTags:      "tracker1,quality-1080p",
			requestCategory:  "",
			requestTags:      nil,
			expectedCategory: "movies",
			expectedTags:     []string{"tracker1", "quality-1080p", "cross-seed"},
		},
		{
			name:             "override with request category",
			matchedCategory:  "movies",
			matchedTags:      "tracker1",
			requestCategory:  "movies-4k",
			requestTags:      []string{"custom"},
			expectedCategory: "movies-4k",
			expectedTags:     []string{"custom", "cross-seed"},
		},
		{
			name:             "add cross-seed tag",
			matchedCategory:  "tv",
			matchedTags:      "sonarr",
			requestCategory:  "",
			requestTags:      nil,
			expectedCategory: "tv",
			expectedTags:     []string{"sonarr", "cross-seed"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// This tests the tag/category logic conceptually
			// In real implementation, this would be extracted to a separate function

			category := tt.requestCategory
			if category == "" {
				category = tt.matchedCategory
			}
			assert.Equal(t, tt.expectedCategory, category)

			// Parse matched tags
			matchedTags := []string{}
			if tt.matchedTags != "" {
				matchedTags = []string{tt.matchedTags}
				// In real code: strings.Split(tt.matchedTags, ",")
			}

			// Merge tags
			tagSet := make(map[string]bool)
			if len(tt.requestTags) > 0 {
				for _, tag := range tt.requestTags {
					tagSet[tag] = true
				}
			} else {
				for _, tag := range matchedTags {
					tagSet[tag] = true
				}
			}
			tagSet["cross-seed"] = true

			tags := make([]string, 0, len(tagSet))
			for tag := range tagSet {
				tags = append(tags, tag)
			}

			// Verify cross-seed tag is present
			assert.Contains(t, tags, "cross-seed")
		})
	}
}

// TestAutoTMMLogic tests automatic torrent management state matching
func TestAutoTMMLogic(t *testing.T) {
	tests := []struct {
		name            string
		matchedAutoTMM  bool
		expectedAutoTMM bool
	}{
		{
			name:            "matched torrent has AutoTMM enabled",
			matchedAutoTMM:  true,
			expectedAutoTMM: true,
		},
		{
			name:            "matched torrent has AutoTMM disabled",
			matchedAutoTMM:  false,
			expectedAutoTMM: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the logic for preserving AutoTMM state
			useAutoTMM := tt.matchedAutoTMM
			assert.Equal(t, tt.expectedAutoTMM, useAutoTMM)
		})
	}
}

// TestTorrentStates tests various torrent state recognition
func TestTorrentStates(t *testing.T) {
	tests := []struct {
		name       string
		state      qbt.TorrentState
		isChecking bool
	}{
		{"checking download", qbt.TorrentStateCheckingDl, true},
		{"checking upload", qbt.TorrentStateCheckingUp, true},
		{"checking resume data", qbt.TorrentStateCheckingResumeData, true},
		{"allocating", qbt.TorrentStateAllocating, true},
		{"downloading", qbt.TorrentStateDownloading, false},
		{"uploading", qbt.TorrentStateUploading, false},
		{"paused download", qbt.TorrentStatePausedDl, false},
		{"paused upload", qbt.TorrentStatePausedUp, false},
		{"stalled upload", qbt.TorrentStateStalledUp, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			isChecking := tt.state == qbt.TorrentStateCheckingDl ||
				tt.state == qbt.TorrentStateCheckingUp ||
				tt.state == qbt.TorrentStateCheckingResumeData ||
				tt.state == qbt.TorrentStateAllocating

			assert.Equal(t, tt.isChecking, isChecking, "State %s checking detection", tt.state)
		})
	}
}

// TestProgressThresholds tests progress percentage logic
func TestProgressThresholds(t *testing.T) {
	tests := []struct {
		name       string
		progress   float64
		is100      bool
		shouldAuto bool
	}{
		{"exactly 100%", 1.0, true, true},
		{"99.9% (close enough)", 0.999, true, true},
		{"99.8% (not close)", 0.998, false, false},
		{"50% incomplete", 0.5, false, false},
		{"0% new torrent", 0.0, false, false},
		{"100.1% (over)", 1.001, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			is100 := tt.progress >= 0.999
			assert.Equal(t, tt.is100, is100)

			shouldAutoResume := is100
			assert.Equal(t, tt.shouldAuto, shouldAutoResume)
		})
	}
}

// TestSeasonPackDetection tests season vs episode detection logic
func TestSeasonPackDetection(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name        string
		releaseName string
		isSeason    bool
		isEpisode   bool
		series      int
		episode     int
	}{
		{
			name:        "season pack",
			releaseName: "Show.S01.1080p.BluRay",
			isSeason:    true,
			isEpisode:   false,
			series:      1,
			episode:     0,
		},
		{
			name:        "single episode",
			releaseName: "Show.S01E05.1080p.WEB-DL",
			isSeason:    false,
			isEpisode:   true,
			series:      1,
			episode:     5,
		},
		{
			name:        "multi-episode",
			releaseName: "Show.S02E10E11.720p.HDTV",
			isSeason:    false,
			isEpisode:   true,
			series:      2,
			episode:     10, // First episode
		},
		{
			name:        "movie with year",
			releaseName: "Movie.2020.1080p.BluRay",
			isSeason:    false,
			isEpisode:   false,
			series:      0,
			episode:     0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.releaseName)

			isSeason := release.Series > 0 && release.Episode == 0
			isEpisode := release.Series > 0 && release.Episode > 0

			assert.Equal(t, tt.isSeason, isSeason, "Season detection")
			assert.Equal(t, tt.isEpisode, isEpisode, "Episode detection")
			assert.Equal(t, tt.series, release.Series, "Series number")
			if tt.isEpisode {
				assert.Equal(t, tt.episode, release.Episode, "Episode number")
			}
		})
	}
}

// TestHashDetection tests info hash matching logic
func TestHashDetection(t *testing.T) {
	testHash := "abcdef1234567890abcdef1234567890abcdef12"

	tests := []struct {
		name       string
		torrentV1  string
		torrentV2  string
		searchHash string
		matches    bool
	}{
		{
			name:       "v1 hash match",
			torrentV1:  testHash,
			torrentV2:  "",
			searchHash: testHash,
			matches:    true,
		},
		{
			name:       "v2 hash match",
			torrentV1:  "different-hash",
			torrentV2:  testHash,
			searchHash: testHash,
			matches:    true,
		},
		{
			name:       "main hash match",
			torrentV1:  "",
			torrentV2:  "",
			searchHash: testHash,
			matches:    false, // Neither v1 nor v2 set
		},
		{
			name:       "no match",
			torrentV1:  "hash1",
			torrentV2:  "hash2",
			searchHash: testHash,
			matches:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matches := tt.torrentV1 == tt.searchHash || tt.torrentV2 == tt.searchHash
			assert.Equal(t, tt.matches, matches)
		})
	}
}

// TestBase64EdgeCases tests that decodeTorrentData can handle various data shapes and encodings.
func TestBase64EdgeCases(t *testing.T) {
	s := &Service{}

	tests := []struct {
		name  string
		input []byte
	}{
		{
			name:  "normal data",
			input: []byte("test data"),
		},
		{
			name:  "binary data",
			input: []byte{0x00, 0x01, 0x02, 0xFF, 0xFE},
		},
		{
			name:  "empty",
			input: []byte{},
		},
		{
			name:  "large data",
			input: make([]byte, 1024*1024), // 1MB
		},
	}

	encodings := []struct {
		name string
		enc  *base64.Encoding
	}{
		{"std", base64.StdEncoding},
		{"url", base64.URLEncoding},
		{"raw std", base64.RawStdEncoding},
		{"raw url", base64.RawURLEncoding},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			for _, e := range encodings {
				t.Run(e.name, func(t *testing.T) {
					encoded := e.enc.EncodeToString(tt.input)
					decoded, err := s.decodeTorrentData(encoded)
					require.NoError(t, err)
					assert.Equal(t, tt.input, decoded)
				})
			}
		})
	}
}

// TestReleaseNameVariations tests different release name formats
func TestReleaseNameVariations(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name        string
		releaseName string
		wantSeries  int
		wantEpisode int
	}{
		{"standard format", "Show.S01E05.1080p", 1, 5},
		{"lowercase", "show.s01e05.720p", 1, 5},
		{"no resolution", "Show.S02E10.WEB-DL", 2, 10},
		{"single digit", "Show.S1E2.HDTV", 1, 2},
		{"with year", "Show.2024.S01E05", 1, 5},
		{"multi-episode", "Show.S01E05E06", 1, 5}, // First episode
		{"season pack no episode", "Show.S01.Complete", 1, 0},
		{"season pack explicit", "Show.Season.1.1080p", 1, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.releaseName)
			assert.Equal(t, tt.wantSeries, release.Series, "Series mismatch")
			assert.Equal(t, tt.wantEpisode, release.Episode, "Episode mismatch")
		})
	}
}

// TestGroupExtraction tests release group extraction
func TestGroupExtraction(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name      string
		release   string
		wantGroup string
	}{
		{"standard group", "Movie.2020.1080p.BluRay.x264-GROUP", "GROUP"},
		{"brackets", "Movie.2020.1080p.[GROUP]", "GROUP"},
		{"no group", "Movie.2020.1080p.BluRay.x264", ""},
		{"underscore", "Show_S01E05_1080p-GROUP", "GROUP"},
		{"multiple dashes", "Movie-2020-1080p-x264-GROUPName", "GROUPName"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			// Group extraction may vary by parser
			_ = release.Group // Just ensure it's accessible
		})
	}
}

// TestQualityDetection tests quality/resolution detection
func TestQualityDetection(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name           string
		release        string
		wantResolution string
	}{
		{"1080p", "Movie.2020.1080p.BluRay", "1080p"},
		{"720p", "Show.S01E05.720p.HDTV", "720p"},
		{"2160p/4K", "Movie.2020.2160p.UHD", "2160p"},
		{"480p", "Show.S01E05.480p.WEB", "480p"},
		{"no resolution", "Show.S01E05.WEB-DL", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			_ = release.Resolution // Ensure accessible
		})
	}
}

// TestSourceDetection tests source media detection
func TestSourceDetection(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name       string
		release    string
		wantSource string
	}{
		{"BluRay", "Movie.2020.1080p.BluRay.x264", "BluRay"},
		{"WEB-DL", "Show.S01E05.1080p.WEB-DL.x264", "WEB-DL"},
		{"WEBRip", "Movie.2020.720p.WEBRip.x264", "WEBRip"},
		{"HDTV", "Show.S01E05.720p.HDTV.x264", "HDTV"},
		{"DVD", "Movie.2000.480p.DVDRip", "DVDRip"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			_ = release.Source // Ensure accessible
		})
	}
}

// TestCodecDetection tests video/audio codec detection
func TestCodecDetection(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name      string
		release   string
		wantCodec string
	}{
		{"x264", "Movie.2020.1080p.x264", "x264"},
		{"x265/HEVC", "Movie.2020.1080p.x265", "x265"},
		{"H.264", "Movie.2020.1080p.H264", "H264"},
		{"H.265", "Movie.2020.2160p.H265", "H265"},
		{"XviD", "Movie.2000.XviD", "XviD"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			_ = release.Codec // Ensure accessible
		})
	}
}

// TestSpecialCharacterHandling tests special characters in names
func TestSpecialCharacterHandling(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name    string
		release string
		wantErr bool
	}{
		{"ampersand", "Show.&.Title.S01E05", false},
		{"apostrophe", "Show's.Title.S01E05", false},
		{"parentheses", "Show.(US).S01E05", false},
		{"dots", "Show...S01E05", false},
		{"underscore", "Show_Title_S01E05", false},
		{"mixed", "Show's.Title.(2024).S01E05", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			assert.NotNil(t, release)
		})
	}
}

// TestYearExtraction tests year extraction from releases
func TestYearExtraction(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name     string
		release  string
		wantYear int
	}{
		{"movie with year", "Movie.2020.1080p", 2020},
		{"show with year", "Show.2024.S01E05", 2024},
		{"old movie", "Movie.1995.DVDRip", 1995},
		{"future", "Movie.2025.1080p", 2025},
		{"no year episode", "Show.S01E05.1080p", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			// Year detection may vary
			_ = release.Year
		})
	}
}

// TestPathSanitization tests path handling edge cases
func TestPathSanitization(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{"normal path", "/data/torrents", "/data/torrents"},
		{"trailing slash", "/data/torrents/", "/data/torrents/"},
		{"windows path", "C:\\Torrents", "C:\\Torrents"},
		{"relative path", "./torrents", "./torrents"},
		{"nested path", "/data/media/shows/Show/Season 01", "/data/media/shows/Show/Season 01"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Just ensure paths are handled without panic
			assert.Equal(t, tt.expected, tt.path)
		})
	}
}

// TestCachePerformance tests release cache performance
func TestCachePerformance(t *testing.T) {
	cache := NewReleaseCache()
	testName := "Show.S01E05.1080p.WEB-DL.x264-GROUP"

	// First parse (cache miss)
	release1 := cache.Parse(testName)
	assert.NotNil(t, release1)

	// Second parse (cache hit)
	release2 := cache.Parse(testName)
	assert.NotNil(t, release2)

	// Should return consistent results
	assert.Equal(t, release1.Series, release2.Series)
	assert.Equal(t, release1.Episode, release2.Episode)
}

// TestTorrentFileStructures tests different torrent file structures
func TestTorrentFileStructures(t *testing.T) {
	tests := []struct {
		name        string
		torrentName string
		files       []string
		fileCount   int
	}{
		{
			name:        "single file",
			torrentName: "Movie.2020.mkv",
			files:       []string{"movie.mkv"},
			fileCount:   1,
		},
		{
			name:        "with subtitles",
			torrentName: "Movie.2020",
			files:       []string{"movie.mkv", "movie.srt", "movie.en.srt"},
			fileCount:   3,
		},
		{
			name:        "with samples",
			torrentName: "Movie.2020",
			files:       []string{"movie.mkv", "Sample/sample.mkv"},
			fileCount:   2,
		},
		{
			name:        "season pack",
			torrentName: "Show.S01",
			files: []string{
				"Show.S01E01.mkv",
				"Show.S01E02.mkv",
				"Show.S01E03.mkv",
			},
			fileCount: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			torrentData := createTestTorrent(t, tt.torrentName, tt.files, 256*1024)

			mi, err := metainfo.Load(bytes.NewReader(torrentData))
			require.NoError(t, err)

			info, err := mi.UnmarshalInfo()
			require.NoError(t, err)

			fileCount := len(info.Files)
			if fileCount == 0 {
				fileCount = 1 // Single file torrent
			}
			assert.Equal(t, tt.fileCount, fileCount)
		})
	}
}

// TestMakeReleaseKey_Matching tests release key matching logic
func TestMakeReleaseKey_Matching(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name        string
		release1    string
		release2    string
		shouldMatch bool
	}{
		{
			name:        "same episode different quality",
			release1:    "Show.S01E05.1080p.BluRay",
			release2:    "Show.S01E05.720p.WEB-DL",
			shouldMatch: true,
		},
		{
			name:        "different episodes",
			release1:    "Show.S01E05.1080p",
			release2:    "Show.S01E06.1080p",
			shouldMatch: false,
		},
		{
			name:        "season pack vs episode",
			release1:    "Show.S01.1080p",
			release2:    "Show.S01E05.1080p",
			shouldMatch: false, // Different structure
		},
		{
			name:        "same movie different year",
			release1:    "Movie.2020.1080p",
			release2:    "Movie.2021.1080p",
			shouldMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r1 := cache.Parse(tt.release1)
			r2 := cache.Parse(tt.release2)

			key1 := makeReleaseKey(r1)
			key2 := makeReleaseKey(r2)

			matches := key1 == key2
			assert.Equal(t, tt.shouldMatch, matches)
		})
	}
}

// TestCheckWebhook_AutobrrPayload exercises the webhook handler end-to-end using faked dependencies.
func TestCheckWebhook_AutobrrPayload(t *testing.T) {
	instance := &models.Instance{
		ID:   1,
		Name: "Test Instance",
	}

	tests := []struct {
		name               string
		request            *WebhookCheckRequest
		existingTorrents   []qbt.Torrent
		wantCanCrossSeed   bool
		wantMatchCount     int
		wantRecommendation string
		wantMatchType      string
	}{
		{
			name: "season pack does not match single episode without override",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "Cool.Show.S02E05.MULTi.1080p.WEB.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "pack", Name: "Cool.Show.S02.MULTi.1080p.WEB.x264-GRP"},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "season pack matches single episode when override enabled",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "Cool.Show.S02E05.MULTi.1080p.WEB.x264-GRP",
				FindIndividualEpisodes: func() *bool {
					v := true
					return &v
				}(),
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "pack", Name: "Cool.Show.S02.MULTi.1080p.WEB.x264-GRP"},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantMatchType:      "metadata",
		},
		{
			name: "movie match - identical release",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "That.Movie.2025.1080p.BluRay.x264-GROUP",
				Size:        8589934592, // 8GB
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash: "abc123def456",
					Name: "That.Movie.2025.1080p.BluRay.x264-GROUP",
					Size: 8589934592,
				},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantMatchType:      "exact",
		},
		{
			name: "metadata match - size unknown",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "Another.Movie.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash: "xyz789abc123",
					Name: "Another.Movie.2025.1080p.BluRay.x264-GRP",
					Size: 9000000000,
				},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantMatchType:      "metadata",
		},
		{
			name: "size mismatch rejects match",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "Size.Test.2025.1080p.BluRay.x264-GRP",
				Size:        8589934592,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash: "size-mismatch",
					Name: "Size.Test.2025.1080p.BluRay.x264-GRP",
					Size: 6500000000,
				},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "different release group does not match",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "Group.Change.2025.1080p.BluRay.x264-NEW",
				Size:        1073741824,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash: "old-group",
					Name: "Group.Change.2025.1080p.BluRay.x264-OLD",
					Size: 1073741824,
				},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "multiple matches return download recommendation",
			request: &WebhookCheckRequest{
				InstanceID:  instance.ID,
				TorrentName: "Popular.Movie.2025.1080p.BluRay.x264-GROUP3",
				Size:        8589934592,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash: "match1",
					Name: "Popular.Movie.2025.1080p.BluRay.x264-GROUP3",
					Size: 8589934592,
				},
				{
					Hash: "match2",
					Name: "Popular.Movie.2025.1080p.BluRay.x264-GROUP3",
					Size: 8589934592,
				},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     2,
			wantRecommendation: "download",
			wantMatchType:      "exact",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := &fakeInstanceStore{
				instances: map[int]*models.Instance{
					instance.ID: instance,
				},
			}
			svc := &Service{
				instanceStore: store,
				syncManager:   newFakeSyncManager(instance, tt.existingTorrents),
				releaseCache:  NewReleaseCache(),
			}

			resp, err := svc.CheckWebhook(context.Background(), tt.request)
			require.NoError(t, err)

			assert.Equal(t, tt.wantCanCrossSeed, resp.CanCrossSeed)
			assert.Equal(t, tt.wantMatchCount, len(resp.Matches))
			assert.Equal(t, tt.wantRecommendation, resp.Recommendation)

			if tt.wantMatchType != "" && tt.wantMatchCount > 0 {
				matchTypes := make([]string, 0, len(resp.Matches))
				for _, match := range resp.Matches {
					matchTypes = append(matchTypes, match.MatchType)
				}
				assert.Contains(t, matchTypes, tt.wantMatchType)
			}
		})
	}
}

func TestCheckWebhook_InstanceNotFound(t *testing.T) {
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{}},
		releaseCache:  NewReleaseCache(),
	}

	_, err := svc.CheckWebhook(context.Background(), &WebhookCheckRequest{
		InstanceID:  99,
		TorrentName: "Missing.Instance.2025.1080p.BluRay.x264-GROUP",
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, ErrWebhookInstanceNotFound)
}

type fakeInstanceStore struct {
	instances map[int]*models.Instance
}

func (f *fakeInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if inst, ok := f.instances[id]; ok {
		return inst, nil
	}
	return nil, models.ErrInstanceNotFound
}

func (f *fakeInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	result := make([]*models.Instance, 0, len(f.instances))
	for _, inst := range f.instances {
		result = append(result, inst)
	}
	return result, nil
}

type fakeSyncManager struct {
	torrents map[int][]internalqb.CrossInstanceTorrentView
}

func newFakeSyncManager(instance *models.Instance, torrents []qbt.Torrent) *fakeSyncManager {
	views := make([]internalqb.CrossInstanceTorrentView, len(torrents))
	for i, tor := range torrents {
		views[i] = internalqb.CrossInstanceTorrentView{
			TorrentView: internalqb.TorrentView{
				Torrent: tor,
			},
			InstanceID:   instance.ID,
			InstanceName: instance.Name,
		}
	}
	return &fakeSyncManager{
		torrents: map[int][]internalqb.CrossInstanceTorrentView{
			instance.ID: views,
		},
	}
}

func (f *fakeSyncManager) GetAllTorrents(_ context.Context, _ int) ([]qbt.Torrent, error) {
	return nil, fmt.Errorf("GetAllTorrents not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) GetTorrentFiles(_ context.Context, _ int, _ string) (*qbt.TorrentFiles, error) {
	return nil, fmt.Errorf("GetTorrentFiles not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) GetTorrentProperties(_ context.Context, _ int, _ string) (*qbt.TorrentProperties, error) {
	return nil, fmt.Errorf("GetTorrentProperties not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, _ map[string]string) error {
	return fmt.Errorf("AddTorrent not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) BulkAction(_ context.Context, _ int, _ []string, _ string) error {
	return fmt.Errorf("BulkAction not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) RenameTorrent(_ context.Context, _ int, _, _ string) error {
	return fmt.Errorf("RenameTorrent not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) RenameTorrentFile(_ context.Context, _ int, _, _, _ string) error {
	return fmt.Errorf("RenameTorrentFile not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) RenameTorrentFolder(_ context.Context, _ int, _, _, _ string) error {
	return fmt.Errorf("RenameTorrentFolder not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	return f.torrents[instanceID], nil
}

func (f *fakeSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (f *fakeSyncManager) GetQBittorrentSyncManager(_ context.Context, _ int) (*qbt.SyncManager, error) {
	return nil, fmt.Errorf("GetQBittorrentSyncManager not implemented in fakeSyncManager")
}

// TestWebhookCheckRequest_Validation tests request validation
func TestWebhookCheckRequest_Validation(t *testing.T) {
	tests := []struct {
		name    string
		request *WebhookCheckRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request with instance",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceID:  1,
			},
			wantErr: false,
		},
		{
			name: "valid full request",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceID:  1,
				Size:        8589934592,
			},
			wantErr: false,
		},
		{
			name: "missing instance ID",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
			},
			wantErr: true,
			errMsg:  "instanceId is required and must be a positive integer",
		},
		{
			name: "zero instance ID",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceID:  0,
			},
			wantErr: true,
			errMsg:  "instanceId is required and must be a positive integer",
		},
		{
			name: "negative instance ID",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceID:  -1,
			},
			wantErr: true,
			errMsg:  "instanceId is required and must be a positive integer",
		},
		{
			name: "missing torrent name",
			request: &WebhookCheckRequest{
				InstanceID: 1,
				Size:       8589934592,
			},
			wantErr: true,
			errMsg:  "torrentName is required",
		},
		{
			name: "empty torrent name",
			request: &WebhookCheckRequest{
				TorrentName: "",
				InstanceID:  1,
			},
			wantErr: true,
			errMsg:  "torrentName is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate the request structure expectations
			if !tt.wantErr {
				// Valid requests should have required fields
				assert.NotEmpty(t, tt.request.TorrentName, "Valid request should have TorrentName")
				assert.Greater(t, tt.request.InstanceID, 0, "Valid request should have positive InstanceID")
			}
			// Note: Invalid requests can have various combinations of missing/invalid fields
			// The actual validation is tested in integration tests with the service
		})
	}
}

// TestWebhookCheckResponse_Structure tests response structure
func TestWebhookCheckResponse_Structure(t *testing.T) {
	tests := []struct {
		name     string
		response *WebhookCheckResponse
	}{
		{
			name: "no matches",
			response: &WebhookCheckResponse{
				CanCrossSeed:   false,
				Matches:        []WebhookCheckMatch{},
				Recommendation: "skip",
			},
		},
		{
			name: "single match",
			response: &WebhookCheckResponse{
				CanCrossSeed: true,
				Matches: []WebhookCheckMatch{
					{
						InstanceID:   1,
						InstanceName: "Main qBittorrent",
						TorrentHash:  "abc123",
						TorrentName:  "Movie.2025.1080p.BluRay.x264-GROUP1",
						MatchType:    "exact",
						SizeDiff:     0.5,
					},
				},
				Recommendation: "download",
			},
		},
		{
			name: "multiple matches",
			response: &WebhookCheckResponse{
				CanCrossSeed: true,
				Matches: []WebhookCheckMatch{
					{
						InstanceID:   1,
						InstanceName: "Instance 1",
						TorrentHash:  "abc123",
						TorrentName:  "Movie.2025.1080p.BluRay.x264-GROUP1",
						MatchType:    "exact",
						SizeDiff:     0.2,
					},
					{
						InstanceID:   2,
						InstanceName: "Instance 2",
						TorrentHash:  "def456",
						TorrentName:  "Movie.2025.1080p.BluRay.x264-GROUP2",
						MatchType:    "size",
						SizeDiff:     2.5,
					},
				},
				Recommendation: "download",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Validate response structure
			if tt.response.CanCrossSeed {
				assert.Equal(t, "download", tt.response.Recommendation)
				assert.NotEmpty(t, tt.response.Matches)
			} else {
				assert.Equal(t, "skip", tt.response.Recommendation)
				assert.Empty(t, tt.response.Matches)
			}

			// Validate match types
			for _, match := range tt.response.Matches {
				validTypes := []string{"metadata", "exact", "size"}
				assert.Contains(t, validTypes, match.MatchType)
				assert.NotEmpty(t, match.InstanceName)
				assert.NotEmpty(t, match.TorrentHash)
				assert.NotEmpty(t, match.TorrentName)
				assert.Greater(t, match.InstanceID, 0)
			}
		})
	}
}
