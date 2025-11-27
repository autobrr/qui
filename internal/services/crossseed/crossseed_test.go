// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/anacrolix/torrent/bencode"
	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
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
		matchedContentPath string
		baseSavePath       string
		contentLayout      string
		matchType          string
		sourceFiles        qbt.TorrentFiles
		candidateFiles     qbt.TorrentFiles
		wantPath           string
		description        string
	}{
		{
			name:               "season pack from individual episode",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01", contentLayout: "Original",

			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-OTHER/ep.mkv"}},
			wantPath:       "/data/media/Show/Season 01/Show.S01E05.1080p.WEB-DL.x264-OTHER",
			description:    "Different roots - use SavePath + candidateRoot (existing files are there)",
		},
		{
			name:               "individual episode from season pack",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-GROUP",
			baseSavePath:       "/data/media/Show/Season 01", contentLayout: "Original",

			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-OTHER/ep.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			wantPath:       "/data/media/Show/Season 01/Show.S01.1080p.BluRay.x264-GROUP",
			description:    "Different roots - use SavePath + candidateRoot (existing files are there)",
		},
		{
			name:               "same content type - both episodes",
			newTorrentName:     "Show.S01E05.720p.HDTV.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01E05.720p.HDTV.x264-GROUP/ep.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-OTHER/ep.mkv"}},
			wantPath:       "/data/media/Show/Season 01/Show.S01E05.1080p.WEB-DL.x264-OTHER",
			description:    "Different roots - use SavePath + candidateRoot (existing files are there)",
		},
		{
			name:               "same content type - both season packs with same root",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-GROUP",
			baseSavePath:       "/data/media/Show", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			wantPath:       "/data/media/Show",
			description:    "Same root folders, use SavePath (parent)",
		},
		{
			name:               "movies with year",
			newTorrentName:     "Movie.2020.720p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Movies/Movie (2020)", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2020.720p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2020.1080p.WEB-DL.x264-OTHER/movie.mkv"}},
			wantPath:       "/data/media/Movies/Movie (2020)/Movie.2020.1080p.WEB-DL.x264-OTHER",
			description:    "Different roots - use SavePath + candidateRoot (existing files are there)",
		},
		{
			name:               "no series info",
			newTorrentName:     "Documentary.1080p.HDTV.x264-GROUP",
			matchedTorrentName: "Documentary.720p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Documentaries", contentLayout: "Original",
			matchType:      "size",
			sourceFiles:    qbt.TorrentFiles{{Name: "Documentary.1080p.HDTV.x264-GROUP/doc.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Documentary.720p.WEB-DL.x264-OTHER/doc.mkv"}},
			wantPath:       "/data/media/Documentaries/Documentary.720p.WEB-DL.x264-OTHER",
			description:    "Different roots - use SavePath + candidateRoot (existing files are there)",
		},
		{
			name:               "partial-in-pack movie in collection",
			newTorrentName:     "Pulse.2001.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/data/media/Movies/Horror.Collection.2020",
			baseSavePath:       "/data/media/Movies", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "Pulse.2001.1080p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Horror.Collection.2020/Pulse.2001.mkv"}},
			wantPath:       "/data/media/Movies/Horror.Collection.2020",
			description:    "Partial-in-pack uses ContentPath, not SavePath",
		},
		{
			name:               "partial-in-pack episode in season pack",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-GROUP/ep.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:       "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:    "Partial-in-pack episode uses season pack's ContentPath",
		},
		{
			name:               "partial-in-pack with empty ContentPath uses candidateRoot",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Collection.2020",
			matchedContentPath: "",
			baseSavePath:       "/data/media/Movies", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Collection.2020/movie.mkv"}},
			wantPath:       "/data/media/Movies/Collection.2020",
			description:    "Partial-in-pack with empty ContentPath uses SavePath + candidateRoot",
		},
		{
			name:               "different root folders uses ContentPath",
			newTorrentName:     "SceneRelease.2020.BluRay.1080p-GRP",
			matchedTorrentName: "Movie (2020) [1080p]",
			matchedContentPath: "/data/media/Movies/Movie (2020) [1080p]",
			baseSavePath:       "/data/media/Movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "SceneRelease.2020.BluRay.1080p-GRP/movie.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie (2020) [1080p]/movie.mkv"}},
			wantPath:       "/data/media/Movies/Movie (2020) [1080p]",
			description:    "Different root folders should use ContentPath",
		},
		{
			name:               "single file torrents (no root) use SavePath",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.mkv"}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv"}},
			wantPath:       "/data/media/Movies",
			description:    "Single file torrents with no root folder use SavePath",
		},

		// ============================================================
		// MOVIES - Comprehensive folder structure scenarios
		// ============================================================

		// M1: We seed folder, match on loose file (partial-in-pack)
		// Seeding: The.Movie.2020-GRP/The.Movie.2020-GRP.mkv
		// Match:   The.Movie.2020-GRP.mkv (no folder)
		{
			name:               "M1: movie folder seeded, match loose file",
			newTorrentName:     "The.Movie.2020-GRP.mkv",
			matchedTorrentName: "The.Movie.2020-GRP",
			matchedContentPath: "/movies/The.Movie.2020-GRP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.2020-GRP.mkv", Size: 5 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Movie.2020-GRP/The.Movie.2020-GRP.mkv", Size: 5 << 30}},
			wantPath:       "/movies/The.Movie.2020-GRP",
			description:    "Loose file goes inside existing folder via ContentPath",
		},

		// M2: We seed loose file, match on folder
		// Seeding: movie.mkv (no folder, in /movies/)
		// Match:   The.Movie.2020-GRP/movie.mkv
		{
			name:               "M2: movie loose file seeded, match folder",
			newTorrentName:     "The.Movie.2020-GRP",
			matchedTorrentName: "The.Movie.2020-GRP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.2020-GRP/movie.mkv", Size: 5 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.mkv", Size: 5 << 30}},
			wantPath:       "/movies",
			description:    "Folder torrent points to SavePath, NoSubfolder strips root",
		},

		// M3: Same root folder names
		// Seeding: The.Movie.2020-GRP/movie.mkv
		// Match:   The.Movie.2020-GRP/movie.mkv (same structure, different tracker)
		{
			name:               "M3: movie same root folders",
			newTorrentName:     "The.Movie.2020-GRP",
			matchedTorrentName: "The.Movie.2020-GRP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.2020-GRP/movie.mkv", Size: 5 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Movie.2020-GRP/movie.mkv", Size: 5 << 30}},
			wantPath:       "/movies",
			description:    "Same roots use SavePath with Original layout",
		},

		// M4: Different root folders (spaces vs dots naming)
		// Seeding: Movie (2020) [1080p]/movie.mkv
		// Match:   The.Movie.2020.1080p-GRP/movie.mkv
		{
			name:               "M4: movie spaces vs dots naming",
			newTorrentName:     "The.Movie.2020.1080p-GRP",
			matchedTorrentName: "Movie (2020) [1080p]",
			matchedContentPath: "/movies/Movie (2020) [1080p]",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.2020.1080p-GRP/movie.mkv", Size: 5 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie (2020) [1080p]/movie.mkv", Size: 5 << 30}},
			wantPath:       "/movies/Movie (2020) [1080p]",
			description:    "Different roots use candidate folder path",
		},

		// M5: Both loose files (no folders)
		{
			name:               "M5: both movies loose files",
			newTorrentName:     "The.Movie.2020-GRP.mkv",
			matchedTorrentName: "The.Movie.2020-GRP.mkv",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.2020-GRP.mkv", Size: 5 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Movie.2020-GRP.mkv", Size: 5 << 30}},
			wantPath:       "/movies",
			description:    "Both loose files use SavePath directly",
		},

		// M6: Both loose files with partial-in-pack match (ContentPath is file path)
		// Bug scenario: ContentPath = /movies/Movie.mkv but we need SavePath = /movies
		{
			name:               "M6: partial-in-pack single file to single file",
			newTorrentName:     "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT",
			matchedTorrentName: "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv",
			matchedContentPath: "/mnt/storage/torrents/movies/Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv",
			baseSavePath:       "/mnt/storage/torrents/movies", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv", Size: 7 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv", Size: 7 << 30}},
			wantPath:       "/mnt/storage/torrents/movies",
			description:    "Single file partial-in-pack uses SavePath, not ContentPath (which is a file path)",
		},

		// M7: Folder-based source torrent matched against single file candidate
		// Real scenario: indexer returns folder torrent, we have single .mkv file
		{
			name:               "M7: partial-in-pack folder source to single file",
			newTorrentName:     "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT",
			matchedTorrentName: "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv",
			matchedContentPath: "/mnt/storage/torrents/movies/Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv",
			baseSavePath:       "/mnt/storage/torrents/movies", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{
				Name: "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT/Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv",
				Size: 7 << 30,
			}},
			candidateFiles: qbt.TorrentFiles{{Name: "Dracula.A.Love.Tale.2025.1080p.WEB.H264-SLOT.mkv", Size: 7 << 30}},
			wantPath:       "/mnt/storage/torrents/movies",
			description:    "Folder source to single file candidate uses SavePath",
		},

		// M8: Folder source → folder candidate (both have folders)
		// Both torrents have folder structure - should use ContentPath (folder)
		{
			name:               "M8: partial-in-pack folder source to folder candidate",
			newTorrentName:     "Movie.2020.1080p.WEB-GRP",
			matchedTorrentName: "Movie.2020.1080p.BluRay-OTHER",
			matchedContentPath: "/movies/Movie.2020.1080p.BluRay-OTHER",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{
				Name: "Movie.2020.1080p.WEB-GRP/Movie.2020.1080p.WEB-GRP.mkv",
				Size: 8 << 30,
			}},
			candidateFiles: qbt.TorrentFiles{{
				Name: "Movie.2020.1080p.BluRay-OTHER/Movie.2020.1080p.BluRay-OTHER.mkv",
				Size: 8 << 30,
			}},
			wantPath:    "/movies/Movie.2020.1080p.BluRay-OTHER",
			description: "Both have folders - uses ContentPath (folder path)",
		},

		// M9: Single file movie with extras folder matched against single file
		// Source has extras subfolder, candidate is single file
		{
			name:               "M9: movie with extras folder to single file",
			newTorrentName:     "Movie.2020.1080p.BluRay-GRP",
			matchedTorrentName: "Movie.2020.1080p.WEB.mkv",
			matchedContentPath: "/movies/Movie.2020.1080p.WEB.mkv",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2020.1080p.BluRay-GRP/Movie.2020.1080p.BluRay-GRP.mkv", Size: 8 << 30},
				{Name: "Movie.2020.1080p.BluRay-GRP/Extras/Behind.The.Scenes.mkv", Size: 1 << 30},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2020.1080p.WEB.mkv", Size: 8 << 30}},
			wantPath:       "/movies",
			description:    "Multi-file source with extras to single file uses SavePath",
		},

		// ============================================================
		// TV SHOWS - Episode and Season Pack scenarios
		// ============================================================

		// Additional TV partial-in-pack single file candidate tests:

		// T7: Season pack folder source → single loose episode file candidate
		// Indexer has season pack, we have a single episode file
		{
			name:               "T7: season pack source to single episode file",
			newTorrentName:     "The.Show.S01.1080p.BluRay-GRP",
			matchedTorrentName: "The.Show.S01E01.1080p.WEB.mkv",
			matchedContentPath: "/tv/The.Show.S01E01.1080p.WEB.mkv",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p.BluRay-GRP/The.Show.S01E01.1080p.BluRay-GRP.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p.BluRay-GRP/The.Show.S01E02.1080p.BluRay-GRP.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p.BluRay-GRP/The.Show.S01E03.1080p.BluRay-GRP.mkv", Size: 2 << 30},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Show.S01E01.1080p.WEB.mkv", Size: 2 << 30}},
			wantPath:       "/tv",
			description:    "Season pack source to single episode file uses SavePath",
		},

		// T8: Episode folder source → single episode file candidate
		// Indexer has episode with folder, we have single episode file
		{
			name:               "T8: episode folder source to single episode file",
			newTorrentName:     "The.Show.S01E05.1080p.BluRay-GRP",
			matchedTorrentName: "The.Show.S01E05.1080p.WEB.mkv",
			matchedContentPath: "/tv/The.Show.S01E05.1080p.WEB.mkv",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{
				Name: "The.Show.S01E05.1080p.BluRay-GRP/The.Show.S01E05.1080p.BluRay-GRP.mkv",
				Size: 2 << 30,
			}},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Show.S01E05.1080p.WEB.mkv", Size: 2 << 30}},
			wantPath:       "/tv",
			description:    "Episode folder source to single episode file uses SavePath",
		},

		// T9: Single episode file source → single episode file candidate
		// Both are single episode files without folders
		{
			name:               "T9: single episode file to single episode file",
			newTorrentName:     "The.Show.S01E05.1080p.BluRay.mkv",
			matchedTorrentName: "The.Show.S01E05.1080p.WEB.mkv",
			matchedContentPath: "/tv/The.Show.S01E05.1080p.WEB.mkv",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Show.S01E05.1080p.BluRay.mkv", Size: 2 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Show.S01E05.1080p.WEB.mkv", Size: 2 << 30}},
			wantPath:       "/tv",
			description:    "Both single episode files - uses SavePath",
		},

		// T10: Episode with subs folder source → single episode file candidate
		// Source has episode + subs in folder, candidate is single file
		{
			name:               "T10: episode with subs to single episode file",
			newTorrentName:     "The.Show.S01E05.1080p.BluRay-GRP",
			matchedTorrentName: "The.Show.S01E05.1080p.WEB.mkv",
			matchedContentPath: "/tv/The.Show.S01E05.1080p.WEB.mkv",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01E05.1080p.BluRay-GRP/The.Show.S01E05.1080p.BluRay-GRP.mkv", Size: 2 << 30},
				{Name: "The.Show.S01E05.1080p.BluRay-GRP/Subs/English.srt", Size: 100 << 10},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Show.S01E05.1080p.WEB.mkv", Size: 2 << 30}},
			wantPath:       "/tv",
			description:    "Episode with subs folder to single file uses SavePath",
		},

		// T1: Season pack seeded, match single episode (no folder)
		// Seeding: Show.S01-GRP/E01.mkv, E02.mkv, ...
		// Match:   Show.S01E01-GRP.mkv (no folder)
		{
			name:               "T1: season pack seeded, match loose episode",
			newTorrentName:     "The.Show.S01E01.1080p-GRP.mkv",
			matchedTorrentName: "The.Show.S01.1080p-GRP",
			matchedContentPath: "/tv/The.Show.S01.1080p-GRP",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType:   "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{Name: "The.Show.S01E01.1080p-GRP.mkv", Size: 2 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E01.1080p-GRP.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E02.1080p-GRP.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E03.1080p-GRP.mkv", Size: 2 << 30},
			},
			wantPath:    "/tv/The.Show.S01.1080p-GRP",
			description: "Loose episode goes inside season pack folder",
		},

		// T2: Single episode seeded (no folder), match season pack
		// Seeding: Show.S01E01-GRP.mkv (loose file)
		// Match:   Show.S01-GRP/E01.mkv, E02.mkv, ...
		{
			name:               "T2: loose episode seeded, match season pack",
			newTorrentName:     "The.Show.S01.1080p-GRP",
			matchedTorrentName: "The.Show.S01E01.1080p-GRP",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType: "partial-contains",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E01.1080p-GRP.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E02.1080p-GRP.mkv", Size: 2 << 30},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "The.Show.S01E01.1080p-GRP.mkv", Size: 2 << 30}},
			wantPath:       "/tv",
			description:    "Season pack uses SavePath, episode file exists there",
		},

		// T3: Season pack seeded, match single episode (with folder)
		// Seeding: Show.S01-GRP/E01.mkv, E02.mkv, ...
		// Match:   Show.S01E01-GRP/E01.mkv (has folder)
		{
			name:               "T3: season pack seeded, match episode with folder",
			newTorrentName:     "The.Show.S01E01.1080p-OTHER",
			matchedTorrentName: "The.Show.S01.1080p-GRP",
			matchedContentPath: "/tv/The.Show.S01.1080p-GRP",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType:   "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{Name: "The.Show.S01E01.1080p-OTHER/ep.mkv", Size: 2 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E01.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p-GRP/The.Show.S01E02.mkv", Size: 2 << 30},
			},
			wantPath:    "/tv/The.Show.S01.1080p-GRP",
			description: "Episode with folder placed inside season pack folder",
		},

		// T4: Season pack to season pack (same root)
		// Seeding: Show.S01.BluRay-GRP/...
		// Match:   Show.S01.BluRay-GRP/... (same name, different tracker)
		{
			name:               "T4: season pack same root",
			newTorrentName:     "The.Show.S01.1080p.BluRay-GRP",
			matchedTorrentName: "The.Show.S01.1080p.BluRay-GRP",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType: "exact",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p.BluRay-GRP/ep1.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p.BluRay-GRP/ep2.mkv", Size: 2 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p.BluRay-GRP/ep1.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p.BluRay-GRP/ep2.mkv", Size: 2 << 30},
			},
			wantPath:    "/tv",
			description: "Same root folders use SavePath with Original layout",
		},

		// T5: Season pack to season pack (different root)
		// Seeding: Show.S01.BluRay-GRP1/...
		// Match:   Show.S01.WEB-GRP2/...
		{
			name:               "T5: season pack different roots",
			newTorrentName:     "The.Show.S01.1080p.WEB-GRP",
			matchedTorrentName: "The.Show.S01.1080p.BluRay-GRP",
			matchedContentPath: "/tv/The.Show.S01.1080p.BluRay-GRP",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType: "exact",
			sourceFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p.WEB-GRP/ep1.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p.WEB-GRP/ep2.mkv", Size: 2 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Show.S01.1080p.BluRay-GRP/ep1.mkv", Size: 2 << 30},
				{Name: "The.Show.S01.1080p.BluRay-GRP/ep2.mkv", Size: 2 << 30},
			},
			wantPath:    "/tv/The.Show.S01.1080p.BluRay-GRP",
			description: "Different roots use candidate folder path",
		},

		// T6: Episode with spaces naming vs dots
		{
			name:               "T6: episode spaces vs dots",
			newTorrentName:     "The.Show.S01E01.1080p-GRP",
			matchedTorrentName: "The Show S01E01 [1080p]",
			matchedContentPath: "/tv/The Show S01E01 [1080p]",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Show.S01E01.1080p-GRP/ep.mkv", Size: 2 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "The Show S01E01 [1080p]/ep.mkv", Size: 2 << 30}},
			wantPath:       "/tv/The Show S01E01 [1080p]",
			description:    "Different naming conventions use candidate folder",
		},

		// ============================================================
		// COLLECTIONS - Movie and TV collections
		// ============================================================

		// C1: Collection seeded, match single movie (no folder)
		// Seeding: Horror.Collection/Pulse.mkv, Ring.mkv
		// Match:   Pulse.2001-GRP.mkv (no folder)
		{
			name:               "C1: collection seeded, match loose movie",
			newTorrentName:     "Pulse.2001.1080p-GRP.mkv",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/movies/Horror.Collection.2020",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:   "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{Name: "Pulse.2001.1080p-GRP.mkv", Size: 4 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Horror.Collection.2020/Pulse.2001.mkv", Size: 4 << 30},
				{Name: "Horror.Collection.2020/Ring.2002.mkv", Size: 4 << 30},
				{Name: "Horror.Collection.2020/Grudge.2004.mkv", Size: 4 << 30},
			},
			wantPath:    "/movies/Horror.Collection.2020",
			description: "Loose movie goes inside collection folder",
		},

		// C2: Single movie seeded (with folder), match collection
		// Seeding: Pulse.2001-GRP/Pulse.mkv
		// Match:   Horror.Collection/Pulse.mkv, Ring.mkv, ...
		// Note: Different roots, so we point to candidate's folder. File renaming aligns names.
		{
			name:               "C2: movie with folder seeded, match collection",
			newTorrentName:     "Horror.Collection.2020",
			matchedTorrentName: "Pulse.2001.1080p-GRP",
			matchedContentPath: "/movies/Pulse.2001.1080p-GRP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType: "partial-contains",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Horror.Collection.2020/Pulse.2001.mkv", Size: 4 << 30},
				{Name: "Horror.Collection.2020/Ring.2002.mkv", Size: 4 << 30},
			},
			candidateFiles: qbt.TorrentFiles{{Name: "Pulse.2001.1080p-GRP/Pulse.mkv", Size: 4 << 30}},
			wantPath:       "/movies",
			description:    "Collection points to existing movie folder, file renaming aligns",
		},

		// C3: Collection seeded, match single movie (with folder)
		{
			name:               "C3: collection seeded, match movie with folder",
			newTorrentName:     "Pulse.2001.1080p-GRP",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/movies/Horror.Collection.2020",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:   "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{Name: "Pulse.2001.1080p-GRP/movie.mkv", Size: 4 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Horror.Collection.2020/Pulse.2001.mkv", Size: 4 << 30},
				{Name: "Horror.Collection.2020/Ring.2002.mkv", Size: 4 << 30},
			},
			wantPath:    "/movies/Horror.Collection.2020",
			description: "Movie with folder placed inside collection",
		},

		// ============================================================
		// EDGE CASES
		// ============================================================

		// E1: Multi-file movie with extras, match single file
		{
			name:               "E1: movie with extras, match main file only",
			newTorrentName:     "The.Movie.2020-GRP",
			matchedTorrentName: "The.Movie.2020-OTHER",
			matchedContentPath: "/movies/The.Movie.2020-OTHER",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:   "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{{Name: "The.Movie.2020-GRP/movie.mkv", Size: 5 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Movie.2020-OTHER/movie.mkv", Size: 5 << 30},
				{Name: "The.Movie.2020-OTHER/Sample/sample.mkv", Size: 50 << 20},
				{Name: "The.Movie.2020-OTHER/Extras/behind_scenes.mkv", Size: 500 << 20},
			},
			wantPath:    "/movies/The.Movie.2020-OTHER",
			description: "Main movie file matches, extras ignored",
		},

		// E2: Nested folders in source
		{
			name:               "E2: nested folder structure",
			newTorrentName:     "Movie.Pack.2020",
			matchedTorrentName: "The.Movie.2020-GRP",
			matchedContentPath: "/movies/The.Movie.2020-GRP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.Pack.2020/The.Movie.2020/movie.mkv", Size: 5 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "The.Movie.2020-GRP/movie.mkv", Size: 5 << 30},
			},
			wantPath:    "/movies/The.Movie.2020-GRP",
			description: "Nested source structure matched to flat candidate",
		},

		// E3: Unicode/special characters in folder names
		{
			name:               "E3: special characters in names",
			newTorrentName:     "Amélie.2001.1080p-GRP",
			matchedTorrentName: "Amélie (2001) [1080p]",
			matchedContentPath: "/movies/Amélie (2001) [1080p]",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Amélie.2001.1080p-GRP/movie.mkv", Size: 4 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Amélie (2001) [1080p]/movie.mkv", Size: 4 << 30}},
			wantPath:       "/movies/Amélie (2001) [1080p]",
			description:    "Unicode characters handled correctly",
		},

		// E4: Very long folder names
		{
			name:               "E4: long folder names",
			newTorrentName:     "The.Movie.With.A.Very.Long.Title.That.Goes.On.And.On.2020.1080p.BluRay.x264.DTS-HD.MA.7.1-VERYLONGGROUP",
			matchedTorrentName: "Movie Long Title (2020)",
			matchedContentPath: "/movies/Movie Long Title (2020)",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.With.A.Very.Long.Title.That.Goes.On.And.On.2020.1080p.BluRay.x264.DTS-HD.MA.7.1-VERYLONGGROUP/movie.mkv", Size: 20 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie Long Title (2020)/movie.mkv", Size: 20 << 30}},
			wantPath:       "/movies/Movie Long Title (2020)",
			description:    "Long folder names handled correctly",
		},

		// E5: Prevent infinite recursion - matched torrent already in root-named folder
		{
			name:               "E5: prevent infinite recursion when save path already ends with candidate root",
			newTorrentName:     "Show.S01E01.720p.HDTV.x264-GROUP",
			matchedTorrentName: "Show.S01E01.1080p.WEB-DL.x264-OTHER",
			matchedContentPath: "/data/media/Show/Season 01/Show.S01E01.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01/Show.S01E01.1080p.WEB-DL.x264-OTHER", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01E01.720p.HDTV.x264-GROUP/ep.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E01.1080p.WEB-DL.x264-OTHER/ep.mkv", Size: 1 << 30}},
			wantPath:       "/data/media/Show/Season 01/Show.S01E01.1080p.WEB-DL.x264-OTHER",
			description:    "Save path already ends with candidate root, don't append again",
		},

		// E6: Deep nesting prevention - multiple levels of cross-seeding
		{
			name:               "E6: prevent deep nesting from chained cross-seeds",
			newTorrentName:     "Movie.2020.480p.WEB.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/movies/Movie.2020.1080p.BluRay.x264-OTHER/Movie.2020.720p.WEB.x264-SOME",
			baseSavePath:       "/movies/Movie.2020.1080p.BluRay.x264-OTHER/Movie.2020.720p.WEB.x264-SOME", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2020.480p.WEB.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2020.720p.WEB.x264-SOME/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/Movie.2020.1080p.BluRay.x264-OTHER/Movie.2020.720p.WEB.x264-SOME",
			description:    "Even with deep nesting in save path, don't append candidate root again",
		},

		// E7: Partial-in-pack with pre-existing root folder
		{
			name:               "E7: partial-in-pack single file into already-rooted folder",
			newTorrentName:     "Episode.S01E01.1080p-GROUP.mkv",
			matchedTorrentName: "Season.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/tv/Season.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/tv/Season.S01.1080p.BluRay.x264-OTHER", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "Episode.S01E01.1080p-GROUP.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Season.S01.1080p.BluRay.x264-OTHER/ep1.mkv", Size: 1 << 30}},
			wantPath:       "/tv/Season.S01.1080p.BluRay.x264-OTHER",
			description:    "Single file into season pack folder that's already named after root",
		},

		// E8: Cross-seed chain - episode from season pack that's in episode folder
		{
			name:               "E8: episode from season pack in episode-named folder",
			newTorrentName:     "Show.S01E02.720p.HDTV.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/tv/Show.S01E01.1080p.WEB.x264-SOME",
			baseSavePath:       "/tv/Show.S01E01.1080p.WEB.x264-SOME", contentLayout: "Original",
			matchType:   "partial-contains",
			sourceFiles: qbt.TorrentFiles{{Name: "Show.S01E02.720p.HDTV.x264-GROUP/ep.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv", Size: 1 << 30},
				{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep2.mkv", Size: 1 << 30},
			},
			wantPath:    "/tv/Show.S01E01.1080p.WEB.x264-SOME",
			description: "Season pack in episode folder should not create new subfolder",
		},

		// E9: Complex nesting with collection
		{
			name:               "E9: movie from collection in deeply nested path",
			newTorrentName:     "Movie.A.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/movies/Collections/Horror.2020/Horror.Collection.2020",
			baseSavePath:       "/movies/Collections/Horror.2020/Horror.Collection.2020", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.A.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Horror.Collection.2020/Movie.A.mkv", Size: 1 << 30}},
			wantPath:       "/movies/Collections/Horror.2020/Horror.Collection.2020",
			description:    "Collection in nested path should not add another level",
		},

		// E10: Root folder with special characters already in path
		{
			name:               "E10: special chars root already in complex path",
			newTorrentName:     "Show.S01E01.720p.HDTV.x264-GROUP",
			matchedTorrentName: "Show.S01E01.[1080p].WEB.x264-OTHER",
			matchedContentPath: "/tv/Shows/Season 01/Show.S01E01.[1080p].WEB.x264-OTHER",
			baseSavePath:       "/tv/Shows/Season 01/Show.S01E01.[1080p].WEB.x264-OTHER", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01E01.720p.HDTV.x264-GROUP/ep.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01E01.[1080p].WEB.x264-OTHER/ep.mkv", Size: 1 << 30}},
			wantPath:       "/tv/Shows/Season 01/Show.S01E01.[1080p].WEB.x264-OTHER",
			description:    "Special characters in root folder name already in path",
		},

		// E11: Empty save path (edge case)
		{
			name:               "E11: empty save path fallback",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "",
			baseSavePath:       "",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Movie.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:           "",
			description:        "Empty save path should be returned as-is",
		},

		// E12: Root folder with numbers and symbols
		{
			name:               "E12: numeric and symbolic root folders",
			newTorrentName:     "Show.2025.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.2025.S01.720p.WEB.x264-OTHER",
			matchedContentPath: "/tv/Show (2025) - Season 1",
			baseSavePath:       "/tv", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.2025.S01.1080p.BluRay.x264-GROUP/ep1.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.2025.S01.720p.WEB.x264-OTHER/ep1.mkv", Size: 1 << 30}},
			wantPath:       "/tv/Show.2025.S01.720p.WEB.x264-OTHER",
			description:    "Numeric season folders with different naming conventions",
		},

		// E13: Very deep nested paths
		{
			name:               "E13: extremely deep nested paths",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "/media/movies/action/2020/sci-fi/Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/media/movies/action/2020/sci-fi", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:       "/media/movies/action/2020/sci-fi/Movie.2020.720p.WEB.x264-OTHER",
			description:    "Deeply nested directory structures",
		},

		// E14: Case sensitivity differences
		{
			name:               "E14: case differences in root folders",
			newTorrentName:     "MOVIE.2020.1080P.BLU-RAY.X264-GROUP",
			matchedTorrentName: "movie.2020.720p.web.x264-other",
			matchedContentPath: "/movies/movie.2020.720p.web.x264-other",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "MOVIE.2020.1080P.BLU-RAY.X264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.2020.720p.web.x264-other/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/movie.2020.720p.web.x264-other",
			description:    "Case differences between source and candidate root folders",
		},

		// E15: Mixed separators in paths (edge case for cross-platform)
		// Code normalizes all paths to forward slashes via filepath.ToSlash
		// qBittorrent accepts forward slashes on all platforms including Windows
		{
			name:               "E15: mixed path separators",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01.720p.WEB.x264-OTHER",
			matchedContentPath: "/tv\\mixed\\Show.S01.720p.WEB.x264-OTHER",
			baseSavePath:       "/tv\\mixed", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01.720p.WEB.x264-OTHER/ep1.mkv", Size: 1 << 30}},
			wantPath:       "/tv/mixed/Show.S01.720p.WEB.x264-OTHER",
			description:    "Mixed separators normalized to forward slashes",
		},

		// E16: Root folders with spaces and punctuation
		{
			name:               "E16: complex punctuation in folder names",
			newTorrentName:     "The.Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "The Movie (2020) [1080p] BluRay x264-OTHER",
			matchedContentPath: "/movies/The Movie (2020) [1080p] BluRay x264-OTHER",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "The.Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "The Movie (2020) [1080p] BluRay x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/The Movie (2020) [1080p] BluRay x264-OTHER",
			description:    "Complex punctuation and spacing in folder names",
		},

		// E17: Single character root folders
		{
			name:               "E17: minimal root folder names",
			newTorrentName:     "A.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "B.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "/movies/B.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "A.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "B.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/B.2020.720p.WEB.x264-OTHER",
			description:    "Single character root folder names",
		},

		// E18: Paths with dots in directory names
		{
			name:               "E18: dots in directory paths",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "/movies/2020.Movies/Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/movies/2020.Movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/2020.Movies/Movie.2020.720p.WEB.x264-OTHER",
			description:    "Dots in directory names that could confuse parsing",
		},

		// E19: Unicode characters in different positions
		{
			name:               "E19: unicode in middle of folder names",
			newTorrentName:     "Film.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Film.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "/movies/Film.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Film.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Film.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/Film.2020.720p.WEB.x264-OTHER",
			description:    "Unicode characters properly handled in folder names",
		},

		// E20: Empty root detection (single file torrents)
		{
			name:               "E20: single file torrents with no root",
			newTorrentName:     "movie.2020.1080p.bluray.x264-group.mkv",
			matchedTorrentName: "movie.2020.720p.web.x264-other.mkv",
			matchedContentPath: "/movies",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "movie.2020.1080p.bluray.x264-group.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "movie.2020.720p.web.x264-other.mkv", Size: 1 << 30}},
			wantPath:       "/movies",
			description:    "Single file torrents with no root folder structure",
		},

		// E21: Nested root folders in source
		{
			name:               "E21: source with nested subfolders",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "/movies/Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType: "exact",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Movie.2020.1080p.BluRay.x264-GROUP/extras/trailer.mkv", Size: 50 << 20},
				{Name: "Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.2020.720p.WEB.x264-OTHER/extras/trailer.mkv", Size: 50 << 20},
				{Name: "Movie.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30},
			},
			wantPath:    "/movies/Movie.2020.720p.WEB.x264-OTHER",
			description: "Source torrent with nested subfolders in root",
		},

		// E22: Inconsistent root detection (mixed files)
		{
			name:               "E22: inconsistent file structures",
			newTorrentName:     "Collection.2020",
			matchedTorrentName: "Movie.A.2020.1080p.BluRay.x264-GROUP",
			matchedContentPath: "/movies/Movie.A.2020.1080p.BluRay.x264-GROUP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType: "partial-in-pack",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Collection.2020/Movie.A.mkv", Size: 1 << 30},
				{Name: "Collection.2020/Movie.B.mkv", Size: 1 << 30},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Movie.A.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30},
				{Name: "Movie.A.2020.1080p.BluRay.x264-GROUP/extras/trailer.mkv", Size: 50 << 20},
			},
			wantPath:    "/movies/Movie.A.2020.1080p.BluRay.x264-GROUP",
			description: "Inconsistent file structures between collection and individual movie",
		},

		// E23: Maximum path length simulation
		{
			name:               "E23: very long folder names",
			newTorrentName:     "Short.Name.2020",
			matchedTorrentName: "Very.Long.Folder.Name.That.Exceeds.Normal.Limits.And.Might.Cause.Issues.On.Some.File.Systems.2020.1080p.BluRay.x264-GROUP",
			matchedContentPath: "/movies/Very.Long.Folder.Name.That.Exceeds.Normal.Limits.And.Might.Cause.Issues.On.Some.File.Systems.2020.1080p.BluRay.x264-GROUP",
			baseSavePath:       "/movies", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Short.Name.2020/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Very.Long.Folder.Name.That.Exceeds.Normal.Limits.And.Might.Cause.Issues.On.Some.File.Systems.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			wantPath:       "/movies/Very.Long.Folder.Name.That.Exceeds.Normal.Limits.And.Might.Cause.Issues.On.Some.File.Systems.2020.1080p.BluRay.x264-GROUP",
			description:    "Very long folder names that might exceed filesystem limits",
		},

		// E24: Relative paths (edge case)
		{
			name:               "E24: relative save paths",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			matchedContentPath: "Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       ".", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "Movie.2020.1080p.BluRay.x264-GROUP/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "Movie.2020.720p.WEB.x264-OTHER/movie.mkv", Size: 1 << 30}},
			wantPath:       "Movie.2020.720p.WEB.x264-OTHER",
			description:    "Relative save paths and content paths",
		},

		// E25: Root folder conflicts with save path base
		{
			name:               "E25: root folder same as save path base",
			newTorrentName:     "movies.2020.collection",
			matchedTorrentName: "movies.2020.720p.web.x264-other",
			matchedContentPath: "/data/media/movies.2020.720p.web.x264-other",
			baseSavePath:       "/data/media", contentLayout: "Original",
			matchType:      "exact",
			sourceFiles:    qbt.TorrentFiles{{Name: "movies.2020.collection/movie.mkv", Size: 1 << 30}},
			candidateFiles: qbt.TorrentFiles{{Name: "movies.2020.720p.web.x264-other/movie.mkv", Size: 1 << 30}},
			wantPath:       "/data/media/movies.2020.720p.web.x264-other",
			description:    "Root folder name conflicts with save path base directory",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchedTorrent := &qbt.Torrent{
				Name:        tt.matchedTorrentName,
				ContentPath: tt.matchedContentPath,
			}
			props := &qbt.TorrentProperties{
				SavePath: tt.baseSavePath,
			}

			gotPath := s.determineSavePath(tt.newTorrentName, matchedTorrent, props, tt.matchType, tt.sourceFiles, tt.candidateFiles, tt.contentLayout)
			assert.Equal(t, filepath.ToSlash(tt.wantPath), filepath.ToSlash(gotPath), tt.description)
		})
	}
}

// TestPartialInPackIntegration verifies the full chain from matching to save path
// for the partial-in-pack scenario (e.g., episode matched against season pack).
// This ensures that when we seed a season pack and find an individual episode,
// the save path correctly uses the season pack's ContentPath.
func TestPartialInPackIntegration(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     releases.NewDefaultParser(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	// Season pack we're seeding
	seasonPackName := "The.Show.S01.1080p.BluRay.x264-GRP"
	seasonPackFiles := qbt.TorrentFiles{
		{Name: "The.Show.S01.1080p.BluRay.x264-GRP/The.Show.S01E01.1080p.BluRay.x264-GRP.mkv", Size: 2 << 30},
		{Name: "The.Show.S01.1080p.BluRay.x264-GRP/The.Show.S01E02.1080p.BluRay.x264-GRP.mkv", Size: 2 << 30},
		{Name: "The.Show.S01.1080p.BluRay.x264-GRP/The.Show.S01E03.1080p.BluRay.x264-GRP.mkv", Size: 2 << 30},
	}
	seasonPackTorrent := &qbt.Torrent{
		Hash:        "seasonpack123",
		Name:        seasonPackName,
		ContentPath: "/downloads/tv/The.Show.S01.1080p.BluRay.x264-GRP",
		Progress:    1.0,
	}
	seasonPackProps := &qbt.TorrentProperties{
		SavePath: "/downloads/tv",
	}

	// New episode torrent we found in search
	episodeName := "The.Show.S01E01.1080p.WEB-DL.x264-OTHER"
	episodeFiles := qbt.TorrentFiles{
		{Name: "The.Show.S01E01.1080p.WEB-DL.x264-OTHER.mkv", Size: 2 << 30},
	}

	// Parse releases
	episodeRelease := svc.releaseCache.Parse(episodeName)
	seasonPackRelease := svc.releaseCache.Parse(seasonPackName)

	// Step 1: Verify matching produces partial-in-pack
	// The episode's files should be found inside the season pack's files
	matchType := svc.getMatchType(episodeRelease, seasonPackRelease, episodeFiles, seasonPackFiles, nil)
	require.Equal(t, "partial-in-pack", matchType,
		"episode matched against season pack should produce partial-in-pack match type")

	// Step 2: Verify determineSavePath uses ContentPath (not SavePath)
	savePath := svc.determineSavePath(episodeName, seasonPackTorrent, seasonPackProps, matchType, episodeFiles, seasonPackFiles, "Original")
	require.Equal(t, "/downloads/tv/The.Show.S01.1080p.BluRay.x264-GRP", savePath,
		"partial-in-pack should use season pack's ContentPath, not SavePath")

	// Verify it's NOT using SavePath
	require.NotEqual(t, "/downloads/tv", savePath,
		"should NOT use SavePath for partial-in-pack matches")
}

// TestPartialInPackMovieCollectionIntegration verifies partial-in-pack for movie collections.
func TestPartialInPackMovieCollectionIntegration(t *testing.T) {
	t.Parallel()

	svc := &Service{
		releaseCache:     releases.NewDefaultParser(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	// Movie collection we're seeding
	collectionName := "Horror.Collection.2020.1080p.BluRay.x264-GRP"
	collectionFiles := qbt.TorrentFiles{
		{Name: "Horror.Collection.2020.1080p.BluRay.x264-GRP/Pulse.2001.1080p.BluRay.x264-GRP.mkv", Size: 4 << 30},
		{Name: "Horror.Collection.2020.1080p.BluRay.x264-GRP/Ring.1998.1080p.BluRay.x264-GRP.mkv", Size: 4 << 30},
	}
	collectionTorrent := &qbt.Torrent{
		Hash:        "collection456",
		Name:        collectionName,
		ContentPath: "/downloads/movies/Horror.Collection.2020.1080p.BluRay.x264-GRP",
		Progress:    1.0,
	}
	collectionProps := &qbt.TorrentProperties{
		SavePath: "/downloads/movies",
	}

	// New single movie torrent we found in search
	movieName := "Pulse.2001.1080p.BluRay.x264-GRP"
	movieFiles := qbt.TorrentFiles{
		{Name: "Pulse.2001.1080p.BluRay.x264-GRP.mkv", Size: 4 << 30},
	}

	// Parse releases
	movieRelease := svc.releaseCache.Parse(movieName)
	collectionRelease := svc.releaseCache.Parse(collectionName)

	// Step 1: Verify matching produces partial-in-pack
	matchType := svc.getMatchType(movieRelease, collectionRelease, movieFiles, collectionFiles, nil)
	require.Equal(t, "partial-in-pack", matchType,
		"movie matched against collection should produce partial-in-pack match type")

	// Step 2: Verify determineSavePath uses ContentPath
	savePath := svc.determineSavePath(movieName, collectionTorrent, collectionProps, matchType, movieFiles, collectionFiles, "Original")
	require.Equal(t, "/downloads/movies/Horror.Collection.2020.1080p.BluRay.x264-GRP", savePath,
		"partial-in-pack should use collection's ContentPath")
}

// TestCrossSeed_TorrentCreationAndParsing tests creating torrents and extracting info
func TestCrossSeed_TorrentCreationAndParsing(t *testing.T) {
	tests := []struct {
		name        string
		torrentName string
		files       []string
	}{
		{
			name:        "single file movie",
			torrentName: "Movie.2020.1080p.BluRay.x264-GROUP",
			files:       []string{"movie.mkv"},
		},
		{
			name:        "episode with subs",
			torrentName: "Show.S01E05.1080p.WEB-DL",
			files:       []string{"show.mkv", "show.srt"},
		},
		{
			name:        "season pack",
			torrentName: "Show.S01.1080p.BluRay.x264-GROUP",
			files:       []string{"e01.mkv", "e02.mkv", "e03.mkv"},
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
	defaultSettings := models.DefaultCrossSeedAutomationSettings()

	tests := []struct {
		name             string
		request          *CrossSeedRequest
		matched          qbt.Torrent
		settings         *models.CrossSeedAutomationSettings
		addCrossSeedTag  bool
		expectedCategory string
		expectedTags     []string
	}{
		{
			name: "use matched category when request is empty",
			request: &CrossSeedRequest{
				Category: "",
				Tags:     nil,
			},
			matched: qbt.Torrent{
				Category: "movies",
				Tags:     "tracker1,quality-1080p",
			},
			settings:         defaultSettings,
			addCrossSeedTag:  true,
			expectedCategory: "movies",
			expectedTags:     []string{"tracker1", "quality-1080p", "cross-seed"},
		},
		{
			name: "override with request category",
			request: &CrossSeedRequest{
				Category: "movies-4k",
				Tags:     []string{"custom"},
			},
			matched: qbt.Torrent{
				Category: "movies",
				Tags:     "tracker1",
			},
			settings:         defaultSettings,
			addCrossSeedTag:  true,
			expectedCategory: "movies-4k",
			expectedTags:     []string{"custom", "cross-seed"},
		},
		{
			name: "add cross-seed tag when matched tags exist",
			request: &CrossSeedRequest{
				Category: "",
				Tags:     nil,
			},
			matched: qbt.Torrent{
				Category: "tv",
				Tags:     "sonarr",
			},
			settings:         defaultSettings,
			addCrossSeedTag:  true,
			expectedCategory: "tv",
			expectedTags:     []string{"sonarr", "cross-seed"},
		},
		{
			name: "use indexer category when enabled",
			request: &CrossSeedRequest{
				Category:    "",
				IndexerName: "IndexerCat",
			},
			matched: qbt.Torrent{
				Category: "fallback",
			},
			settings: &models.CrossSeedAutomationSettings{
				UseCategoryFromIndexer: true,
			},
			addCrossSeedTag:  true,
			expectedCategory: "IndexerCat",
			expectedTags:     []string{"cross-seed"},
		},
		{
			name: "skip cross-seed tag when disabled",
			request: &CrossSeedRequest{
				Category: "",
				Tags:     []string{"keep"},
			},
			matched: qbt.Torrent{
				Category: "tv",
				Tags:     "",
			},
			settings:         defaultSettings,
			addCrossSeedTag:  false,
			expectedCategory: "tv",
			expectedTags:     []string{"keep"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					if tt.settings != nil {
						return tt.settings, nil
					}
					return defaultSettings, nil
				},
			}

			category := svc.determineCrossSeedCategory(context.Background(), tt.request, &tt.matched)
			assert.Equal(t, tt.expectedCategory, category)

			tags := buildCrossSeedTags(tt.request.Tags, tt.matched.Tags, tt.addCrossSeedTag)
			if len(tt.expectedTags) == 0 {
				assert.Empty(t, tags)
			} else {
				assert.ElementsMatch(t, tt.expectedTags, tags)
			}
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
		{"brackets", "Movie.2020.1080p.[GROUP]", ""},
		{"no group", "Movie.2020.1080p.BluRay.x264", ""},
		{"underscore", "Show_S01E05_1080p-GROUP", "GROUP"},
		{"multiple dashes", "Movie-2020-1080p-x264-GROUPName", "GROUPName"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			assert.Equalf(t, tt.wantGroup, release.Group, "group mismatch for release %q", tt.release)
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
			assert.Equalf(t, tt.wantResolution, release.Resolution, "resolution mismatch for release %q", tt.release)
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
		{"WEBRip", "Movie.2020.720p.WEBRip.x264", "WEBRiP"},
		{"HDTV", "Show.S01E05.720p.HDTV.x264", "HDTV"},
		{"DVD", "Movie.2000.480p.DVDRip", "DVDRiP"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			assert.Equalf(t, tt.wantSource, release.Source, "source mismatch for release %q", tt.release)
		})
	}
}

// TestCodecDetection tests video/audio codec detection
func TestCodecDetection(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name      string
		release   string
		wantCodec []string
	}{
		{"x264", "Movie.2020.1080p.x264", []string{"x264"}},
		{"x265/HEVC", "Movie.2020.1080p.x265", []string{"x265"}},
		{"H.264", "Movie.2020.1080p.H264", []string{"H.264"}},
		{"H.265", "Movie.2020.2160p.H265", []string{"H.265"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			release := cache.Parse(tt.release)
			assert.Equalf(t, tt.wantCodec, release.Codec, "codec mismatch for release %q", tt.release)
		})
	}
}

// TestSpecialCharacterHandling tests special characters in names
func TestSpecialCharacterHandling(t *testing.T) {
	cache := NewReleaseCache()

	tests := []struct {
		name    string
		release string
	}{
		{"ampersand", "Show.&.Title.S01E05"},
		{"apostrophe", "Show's.Title.S01E05"},
		{"parentheses", "Show.(US).S01E05"},
		{"dots", "Show...S01E05"},
		{"underscore", "Show_Title_S01E05"},
		{"mixed", "Show's.Title.(2024).S01E05"},
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
			assert.Equalf(t, tt.wantYear, release.Year, "year mismatch for release %q", tt.release)
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
	instanceIDs := []int{instance.ID}

	tests := []struct {
		name               string
		request            *WebhookCheckRequest
		existingTorrents   []qbt.Torrent
		wantCanCrossSeed   bool
		wantMatchCount     int
		wantRecommendation string
		wantMatchType      string
		expectPending      bool
	}{
		{
			name: "season pack does not match single episode without override",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Cool.Show.S02E05.MULTi.1080p.WEB.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "pack", Name: "Cool.Show.S02.MULTi.1080p.WEB.x264-GRP", Progress: 1.0},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "season pack matches single episode when override enabled",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Cool.Show.S02E05.MULTi.1080p.WEB.x264-GRP",
				FindIndividualEpisodes: func() *bool {
					v := true
					return &v
				}(),
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "pack", Name: "Cool.Show.S02.MULTi.1080p.WEB.x264-GRP", Progress: 1.0},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantMatchType:      "metadata",
		},
		{
			name: "movie match - identical release",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "That.Movie.2025.1080p.BluRay.x264-GROUP",
				Size:        8589934592, // 8GB
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "abc123def456",
					Name:     "That.Movie.2025.1080p.BluRay.x264-GROUP",
					Size:     8589934592,
					Progress: 1.0,
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
				InstanceIDs: instanceIDs,
				TorrentName: "Another.Movie.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "xyz789abc123",
					Name:     "Another.Movie.2025.1080p.BluRay.x264-GRP",
					Size:     9000000000,
					Progress: 1.0,
				},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantMatchType:      "metadata",
		},
		{
			name: "pending match when torrent still downloading",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Pending.Movie.2025.1080p.BluRay.x264-GRP",
				Size:        8589934592,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "pending",
					Name:     "Pending.Movie.2025.1080p.BluRay.x264-GRP",
					Size:     8589934592,
					Progress: 0.5,
				},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantMatchType:      "exact",
			expectPending:      true,
		},
		{
			name: "size mismatch rejects match",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Size.Test.2025.1080p.BluRay.x264-GRP",
				Size:        8589934592,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "size-mismatch",
					Name:     "Size.Test.2025.1080p.BluRay.x264-GRP",
					Size:     6500000000,
					Progress: 1.0,
				},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "different release group does not match",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Group.Change.2025.1080p.BluRay.x264-NEW",
				Size:        1073741824,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "old-group",
					Name:     "Group.Change.2025.1080p.BluRay.x264-OLD",
					Size:     1073741824,
					Progress: 1.0,
				},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "multiple matches return download recommendation",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Popular.Movie.2025.1080p.BluRay.x264-GROUP3",
				Size:        8589934592,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "match1",
					Name:     "Popular.Movie.2025.1080p.BluRay.x264-GROUP3",
					Size:     8589934592,
					Progress: 1.0,
				},
				{
					Hash:     "match2",
					Name:     "Popular.Movie.2025.1080p.BluRay.x264-GROUP3",
					Size:     8589934592,
					Progress: 1.0,
				},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     2,
			wantRecommendation: "download",
			wantMatchType:      "exact",
		},
		{
			name: "music release does not match unrelated torrents (regression from logs)",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Fictional Artist - B - Hidden Tracks [2020] [WEB 320]",
				Size:        123456789,
			},
			existingTorrents: []qbt.Torrent{
				{
					Hash:     "fangbone",
					Name:     "Galactic Tales!",
					Size:     234567890,
					Progress: 1.0,
				},
				{
					Hash:     "kiyosaki",
					Name:     "Author X - Imaginary Book (Narrated by Jane Doe)[2012]",
					Size:     345678901,
					Progress: 1.0,
				},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
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
				instanceStore:    store,
				syncManager:      newFakeSyncManager(instance, tt.existingTorrents, nil),
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
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
			if tt.expectPending && tt.wantMatchCount > 0 {
				for _, match := range resp.Matches {
					assert.Less(t, match.Progress, 1.0)
				}
			}
			if !tt.expectPending && tt.wantMatchCount > 0 {
				hasComplete := false
				for _, match := range resp.Matches {
					if match.Progress >= 1.0 {
						hasComplete = true
						break
					}
				}
				assert.True(t, hasComplete, "expected at least one completed match")
			}
		})
	}
}

func TestCheckWebhook_NoInstancesAvailable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		store       *fakeInstanceStore
		request     *WebhookCheckRequest
		description string
	}{
		{
			name:  "globalScanWithNoInstancesConfigured",
			store: &fakeInstanceStore{instances: map[int]*models.Instance{}},
			request: &WebhookCheckRequest{
				TorrentName: "Missing.Instance.2025.1080p.BluRay.x264-GROUP",
			},
		},
		{
			name:  "targetedInstancesMissing",
			store: &fakeInstanceStore{instances: map[int]*models.Instance{}},
			request: &WebhookCheckRequest{
				TorrentName: "Missing.Instance.2025.1080p.BluRay.x264-GROUP",
				InstanceIDs: []int{99},
				FindIndividualEpisodes: func() *bool {
					v := true
					return &v
				}(),
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{
				instanceStore:    tt.store,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
			}

			resp, err := svc.CheckWebhook(context.Background(), tt.request)
			require.NoError(t, err)
			require.NotNil(t, resp)
			assert.False(t, resp.CanCrossSeed)
			assert.Empty(t, resp.Matches)
			assert.Equal(t, "skip", resp.Recommendation)
		})
	}
}

func TestCheckWebhook_MultiInstanceScan(t *testing.T) {
	t.Parallel()

	instanceA := &models.Instance{ID: 1, Name: "A"}
	instanceB := &models.Instance{ID: 2, Name: "B"}

	store := &fakeInstanceStore{
		instances: map[int]*models.Instance{
			instanceA.ID: instanceA,
			instanceB.ID: instanceB,
		},
	}

	torrentName := "Popular.Movie.2025.1080p.BluRay.x264-GRP"
	torrentSize := int64(8589934592)

	sync := &fakeSyncManager{
		cached: map[int][]internalqb.CrossInstanceTorrentView{
			instanceA.ID: buildCrossInstanceViews(instanceA, []qbt.Torrent{
				{Hash: "complete", Name: torrentName, Size: torrentSize, Progress: 1.0},
			}),
			instanceB.ID: buildCrossInstanceViews(instanceB, []qbt.Torrent{
				{Hash: "pending", Name: torrentName, Size: torrentSize, Progress: 0.6},
			}),
		},
	}

	svc := &Service{
		instanceStore:    store,
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	tests := []struct {
		name               string
		request            *WebhookCheckRequest
		wantCanCrossSeed   bool
		wantMatchCount     int
		wantRecommendation string
		wantInstanceIDs    []int
	}{
		{
			name: "globalScanUsesAllInstances",
			request: &WebhookCheckRequest{
				TorrentName: torrentName,
				Size:        uint64(torrentSize),
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     2,
			wantRecommendation: "download",
			wantInstanceIDs:    []int{instanceA.ID, instanceB.ID},
		},
		{
			name: "subsetScanTargetsSpecifiedInstances",
			request: &WebhookCheckRequest{
				TorrentName: torrentName,
				Size:        uint64(torrentSize),
				InstanceIDs: []int{instanceA.ID, instanceB.ID},
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     2,
			wantRecommendation: "download",
			wantInstanceIDs:    []int{instanceA.ID, instanceB.ID},
		},
		{
			name: "subsetScanWithIncompleteInstances",
			request: &WebhookCheckRequest{
				TorrentName: torrentName,
				Size:        uint64(torrentSize),
				InstanceIDs: []int{instanceB.ID},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantInstanceIDs:    []int{instanceB.ID},
		},
		{
			name: "subsetSkipsMissingInstances",
			request: &WebhookCheckRequest{
				TorrentName: torrentName,
				Size:        uint64(torrentSize),
				InstanceIDs: []int{instanceB.ID, 999},
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     1,
			wantRecommendation: "download",
			wantInstanceIDs:    []int{instanceB.ID},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp, err := svc.CheckWebhook(context.Background(), tt.request)
			require.NoError(t, err)
			assert.Equal(t, tt.wantCanCrossSeed, resp.CanCrossSeed)
			assert.Equal(t, tt.wantMatchCount, len(resp.Matches))
			assert.Equal(t, tt.wantRecommendation, resp.Recommendation)

			if tt.wantMatchCount > 0 && len(tt.wantInstanceIDs) > 0 {
				gotIDs := make([]int, 0, len(resp.Matches))
				for _, match := range resp.Matches {
					gotIDs = append(gotIDs, match.InstanceID)
				}
				assert.ElementsMatch(t, tt.wantInstanceIDs, gotIDs)
			}
		})
	}
}

func TestFindCandidates_NonTVDoesNotMatchUnrelatedTorrents(t *testing.T) {
	instance := &models.Instance{
		ID:   1,
		Name: "main",
	}

	torrents := []qbt.Torrent{
		{
			Hash:     "fangbone",
			Name:     "Galactic Tales!",
			Progress: 1.0,
		},
		{
			Hash:     "kiyosaki",
			Name:     "Author X - Imaginary Book (Narrated by Jane Doe)[2012]",
			Progress: 1.0,
		},
	}

	files := map[string]qbt.TorrentFiles{
		"fangbone": {
			{Name: "Galactic Tales!.mkv", Size: 2 << 30},
		},
		"kiyosaki": {
			{Name: "Author X - B - Imaginary Book (Narrated by Jane Doe)[2012].m4b", Size: 1 << 30},
		},
	}

	store := &fakeInstanceStore{
		instances: map[int]*models.Instance{
			instance.ID: instance,
		},
	}

	svc := &Service{
		instanceStore:    store,
		syncManager:      newFakeSyncManager(instance, torrents, files),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	resp, err := svc.FindCandidates(context.Background(), &FindCandidatesRequest{
		TorrentName:       "Fictional Artist - B - Hidden Tracks [2020] [WEB 320]",
		TargetInstanceIDs: []int{instance.ID},
	})
	require.NoError(t, err)
	require.Empty(t, resp.Candidates, "unrelated non-TV torrents should not be treated as matches")
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
	cached map[int][]internalqb.CrossInstanceTorrentView
	all    map[int][]qbt.Torrent
	files  map[string]qbt.TorrentFiles
}

func buildCrossInstanceViews(instance *models.Instance, torrents []qbt.Torrent) []internalqb.CrossInstanceTorrentView {
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
	return views
}

func newFakeSyncManager(instance *models.Instance, torrents []qbt.Torrent, files map[string]qbt.TorrentFiles) *fakeSyncManager {
	views := buildCrossInstanceViews(instance, torrents)
	cached := map[int][]internalqb.CrossInstanceTorrentView{
		instance.ID: views,
	}
	all := map[int][]qbt.Torrent{}
	if torrents != nil {
		all[instance.ID] = torrents
	}

	normalizedFiles := make(map[string]qbt.TorrentFiles, len(files))
	for hash, fl := range files {
		norm := normalizeHash(hash)
		cp := make(qbt.TorrentFiles, len(fl))
		copy(cp, fl)
		normalizedFiles[norm] = cp
	}

	return &fakeSyncManager{
		cached: cached,
		all:    all,
		files:  normalizedFiles,
	}
}

func (f *fakeSyncManager) GetTorrents(_ context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if torrents, ok := f.all[instanceID]; ok {
		return torrents, nil
	}
	return nil, fmt.Errorf("instance %d not found", instanceID)
}

func (f *fakeSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if len(f.files) == 0 {
		return nil, fmt.Errorf("files not configured")
	}
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		normalized := normalizeHash(h)
		files, ok := f.files[normalized]
		if !ok {
			if files, ok = f.files[strings.ToLower(h)]; !ok {
				if files, ok = f.files[h]; !ok {
					continue
				}
			}
		}
		copyFiles := make(qbt.TorrentFiles, len(files))
		copy(copyFiles, files)
		result[normalized] = copyFiles
	}
	return result, nil
}

func (f *fakeSyncManager) HasTorrentByAnyHash(_ context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error) {
	if torrents, ok := f.all[instanceID]; ok {
		targets := make(map[string]struct{}, len(hashes))
		for _, h := range hashes {
			if normalized := normalizeHash(h); normalized != "" {
				targets[normalized] = struct{}{}
			}
		}
		for i := range torrents {
			t := torrents[i]
			for _, candidate := range []string{t.Hash, t.InfohashV1, t.InfohashV2} {
				if candidate == "" {
					continue
				}
				if _, ok := targets[normalizeHash(candidate)]; ok {
					return &t, true, nil
				}
			}
		}
	}
	return nil, false, nil
}

func (f *fakeSyncManager) GetTorrentProperties(_ context.Context, _ int, _ string) (*qbt.TorrentProperties, error) {
	return nil, fmt.Errorf("GetTorrentProperties not implemented in fakeSyncManager")
}

func (f *fakeSyncManager) GetAppPreferences(_ context.Context, _ int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
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

func (f *fakeSyncManager) SetTags(_ context.Context, _ int, _ []string, _ string) error {
	return nil
}

func (f *fakeSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	if cached, ok := f.cached[instanceID]; ok {
		return cached, nil
	}
	return nil, fmt.Errorf("cached torrents not found for instance %d", instanceID)
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
			name: "valid request with explicit instance IDs",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceIDs: []int{1},
			},
			wantErr: false,
		},
		{
			name: "valid request without instance IDs (global)",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
			},
			wantErr: false,
		},
		{
			name: "valid full request",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceIDs: []int{1},
				Size:        8589934592,
			},
			wantErr: false,
		},
		{
			name: "invalid when instance IDs contain no positives",
			request: &WebhookCheckRequest{
				TorrentName: "Movie.2025.1080p.BluRay.x264-GROUP",
				InstanceIDs: []int{0, -1},
			},
			wantErr: true,
			errMsg:  "instanceIds must contain at least one positive integer",
		},
		{
			name: "missing torrent name",
			request: &WebhookCheckRequest{
				InstanceIDs: []int{1},
				Size:        8589934592,
			},
			wantErr: true,
			errMsg:  "torrentName is required",
		},
		{
			name: "empty torrent name",
			request: &WebhookCheckRequest{
				TorrentName: "",
				InstanceIDs: []int{1},
			},
			wantErr: true,
			errMsg:  "torrentName is required",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookCheckRequest(tt.request)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestWrapCrossSeedSearchErrorRateLimited(t *testing.T) {
	err := errors.New("torznab request rate-limited by tracker")
	wrapped := wrapCrossSeedSearchError(err)

	if wrapped == nil {
		t.Fatalf("expected wrapped error")
	}
	if !strings.Contains(wrapped.Error(), "temporarily unavailable") {
		t.Fatalf("expected friendly rate limit explanation, got %q", wrapped.Error())
	}
	if !strings.Contains(wrapped.Error(), err.Error()) {
		t.Fatalf("expected original error message to be included")
	}
}

func TestWrapCrossSeedSearchErrorGeneric(t *testing.T) {
	err := errors.New("unexpected search failure")
	wrapped := wrapCrossSeedSearchError(err)

	if wrapped == nil {
		t.Fatalf("expected wrapped error")
	}
	if !strings.Contains(wrapped.Error(), "torznab search failed") {
		t.Fatalf("expected generic torznab failure prefix, got %q", wrapped.Error())
	}
}

// TestDetermineSavePathContentLayoutScenarios tests determineSavePath with different
// qBittorrent content layout settings to ensure cross-seeding works correctly
// regardless of the user's content layout preference.
func TestDetermineSavePathContentLayoutScenarios(t *testing.T) {
	cache := NewReleaseCache()
	s := &Service{releaseCache: cache}

	// Test cases covering key scenarios with different contentLayout values
	tests := []struct {
		name               string
		newTorrentName     string
		matchedTorrentName string
		matchedContentPath string
		baseSavePath       string
		contentLayout      string
		matchType          string
		sourceFiles        qbt.TorrentFiles
		candidateFiles     qbt.TorrentFiles
		wantPath           string
		description        string
	}{
		// Original layout - files in subfolders based on torrent structure
		{
			name:               "original_layout_season_pack_from_episode",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01",
			contentLayout:      "Original",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-OTHER/ep.mkv"}},
			wantPath:           "/data/media/Show/Season 01/Show.S01E05.1080p.WEB-DL.x264-OTHER",
			description:        "Original layout: Different roots use SavePath + candidateRoot",
		},
		{
			name:               "subfolder_layout_season_pack_from_episode",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01",
			contentLayout:      "Subfolder",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-OTHER/ep.mkv"}},
			wantPath:           "/data/media/Show/Season 01/Show.S01E05.1080p.WEB-DL.x264-OTHER",
			description:        "Subfolder layout: Path determination should be identical",
		},
		{
			name:               "nosubfolder_layout_season_pack_from_episode",
			newTorrentName:     "Show.S01.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Show.S01E05.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Show/Season 01",
			contentLayout:      "NoSubfolder",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-GROUP/ep1.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-OTHER/ep.mkv"}},
			wantPath:           "/data/media/Show/Season 01/Show.S01E05.1080p.WEB-DL.x264-OTHER",
			description:        "NoSubfolder layout: Path determination should be identical",
		},

		// Partial-in-pack scenarios with different layouts
		{
			name:               "original_layout_partial_in_pack_episode",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows",
			contentLayout:      "Original",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-GROUP/ep.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:           "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:        "Original layout: Partial-in-pack uses ContentPath",
		},
		{
			name:               "subfolder_layout_partial_in_pack_episode",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows",
			contentLayout:      "Subfolder",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-GROUP/ep.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:           "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:        "Subfolder layout: Partial-in-pack uses ContentPath",
		},
		{
			name:               "nosubfolder_layout_partial_in_pack_episode",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows",
			contentLayout:      "NoSubfolder",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "Show.S01E05.1080p.WEB-DL.x264-GROUP/ep.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:           "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:        "NoSubfolder layout: Partial-in-pack uses ContentPath",
		},

		// Movie collection scenarios
		{
			name:               "original_layout_partial_in_pack_movie_collection",
			newTorrentName:     "Pulse.2001.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/data/media/Movies/Horror.Collection.2020",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "Original",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "Pulse.2001.1080p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Horror.Collection.2020/Pulse.2001.mkv"}},
			wantPath:           "/data/media/Movies/Horror.Collection.2020",
			description:        "Original layout: Movie in collection uses ContentPath",
		},
		{
			name:               "subfolder_layout_partial_in_pack_movie_collection",
			newTorrentName:     "Pulse.2001.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/data/media/Movies/Horror.Collection.2020",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "Subfolder",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "Pulse.2001.1080p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Horror.Collection.2020/Pulse.2001.mkv"}},
			wantPath:           "/data/media/Movies/Horror.Collection.2020",
			description:        "Subfolder layout: Movie in collection uses ContentPath",
		},
		{
			name:               "nosubfolder_layout_partial_in_pack_movie_collection",
			newTorrentName:     "Pulse.2001.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Horror.Collection.2020",
			matchedContentPath: "/data/media/Movies/Horror.Collection.2020",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "NoSubfolder",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "Pulse.2001.1080p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Horror.Collection.2020/Pulse.2001.mkv"}},
			wantPath:           "/data/media/Movies/Horror.Collection.2020",
			description:        "NoSubfolder layout: Movie in collection uses ContentPath",
		},

		// Same content type scenarios
		{
			name:               "original_layout_same_content_type_movies",
			newTorrentName:     "Movie.2020.720p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "Original",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Movie.2020.720p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Movie.2020.1080p.WEB-DL.x264-OTHER/movie.mkv"}},
			wantPath:           "/data/media/Movies/Movie.2020.1080p.WEB-DL.x264-OTHER",
			description:        "Original layout: Same type, different roots use candidate folder",
		},
		{
			name:               "subfolder_layout_same_content_type_movies",
			newTorrentName:     "Movie.2020.720p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "Subfolder",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Movie.2020.720p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Movie.2020.1080p.WEB-DL.x264-OTHER/movie.mkv"}},
			wantPath:           "/data/media/Movies/Movie.2020.1080p.WEB-DL.x264-OTHER",
			description:        "Subfolder layout: Same type, different roots use candidate folder",
		},
		{
			name:               "nosubfolder_layout_same_content_type_movies",
			newTorrentName:     "Movie.2020.720p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.1080p.WEB-DL.x264-OTHER",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "NoSubfolder",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "Movie.2020.720p.BluRay.x264-GROUP/movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "Movie.2020.1080p.WEB-DL.x264-OTHER/movie.mkv"}},
			wantPath:           "/data/media/Movies/Movie.2020.1080p.WEB-DL.x264-OTHER",
			description:        "NoSubfolder layout: Same type, different roots use candidate folder",
		},

		// Single file torrents (no root folders)
		{
			name:               "original_layout_single_file_torrents",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "Original",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "movie.mkv"}},
			wantPath:           "/data/media/Movies",
			description:        "Original layout: Single file torrents use SavePath directly",
		},
		{
			name:               "subfolder_layout_single_file_torrents",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "Subfolder",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "movie.mkv"}},
			wantPath:           "/data/media/Movies",
			description:        "Subfolder layout: Single file torrents use SavePath directly",
		},
		{
			name:               "nosubfolder_layout_single_file_torrents",
			newTorrentName:     "Movie.2020.1080p.BluRay.x264-GROUP",
			matchedTorrentName: "Movie.2020.720p.WEB.x264-OTHER",
			baseSavePath:       "/data/media/Movies",
			contentLayout:      "NoSubfolder",
			matchType:          "exact",
			sourceFiles:        qbt.TorrentFiles{{Name: "movie.mkv"}},
			candidateFiles:     qbt.TorrentFiles{{Name: "movie.mkv"}},
			wantPath:           "/data/media/Movies",
			description:        "NoSubfolder layout: Single file torrents use SavePath directly",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matchedTorrent := &qbt.Torrent{
				Name:        tt.matchedTorrentName,
				ContentPath: tt.matchedContentPath,
			}
			props := &qbt.TorrentProperties{
				SavePath: tt.baseSavePath,
			}

			gotPath := s.determineSavePath(tt.newTorrentName, matchedTorrent, props, tt.matchType, tt.sourceFiles, tt.candidateFiles, tt.contentLayout)
			assert.Equal(t, filepath.ToSlash(tt.wantPath), filepath.ToSlash(gotPath),
				"ContentLayout=%s: %s", tt.contentLayout, tt.description)
		})
	}
}

// mockRecoverSyncManager simulates torrent state changes during recheck operations
type mockRecoverSyncManager struct {
	torrents                    map[string]*qbt.Torrent // hash -> torrent
	calls                       []string                // track method calls for verification
	recheckCompletes            bool                    // whether recheck should complete torrents
	disappearAfterRecheck       bool                    // whether torrent disappears after recheck
	bulkActionFails             bool                    // whether BulkAction should fail
	keepInCheckingState         bool                    // whether to keep torrent in checking state
	failGetTorrentsAfterRecheck bool                    // whether GetTorrents should fail after recheck
	setProgressToThreshold      bool                    // whether to set progress exactly at threshold
	hasRechecked                bool                    // track if recheck has been called
	secondRecheckCompletes      bool                    // whether second recheck should complete torrents
	recheckCount                int                     // count of recheck calls
}

func newMockRecoverSyncManager(initialTorrents []qbt.Torrent) *mockRecoverSyncManager {
	torrents := make(map[string]*qbt.Torrent)
	for _, t := range initialTorrents {
		torrent := t // copy
		torrents[t.Hash] = &torrent
	}
	return &mockRecoverSyncManager{
		torrents:                    torrents,
		calls:                       []string{},
		recheckCompletes:            true, // default to completing
		disappearAfterRecheck:       false,
		bulkActionFails:             false,
		keepInCheckingState:         false,
		failGetTorrentsAfterRecheck: false,
		setProgressToThreshold:      false,
		hasRechecked:                false,
		secondRecheckCompletes:      false,
		recheckCount:                0,
	}
}

func (m *mockRecoverSyncManager) GetTorrents(_ context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	m.calls = append(m.calls, "GetTorrents")

	if m.failGetTorrentsAfterRecheck && m.hasRechecked {
		// Return empty list to simulate torrent disappearing
		return []qbt.Torrent{}, nil
	}

	var result []qbt.Torrent
	if len(filter.Hashes) > 0 {
		for _, hash := range filter.Hashes {
			if torrent, ok := m.torrents[hash]; ok {
				result = append(result, *torrent)
			}
		}
	} else {
		for _, torrent := range m.torrents {
			result = append(result, *torrent)
		}
	}
	return result, nil
}

func (m *mockRecoverSyncManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	m.calls = append(m.calls, fmt.Sprintf("BulkAction:%s:%v", action, hashes))

	if m.bulkActionFails {
		return errors.New("bulk action failed")
	}

	if action == "pause" {
		// Pause torrents
		for _, hash := range hashes {
			if torrent, ok := m.torrents[hash]; ok {
				torrent.State = qbt.TorrentStatePausedDl
			}
		}
	} else if action == "resume" {
		// Resume torrents
		for _, hash := range hashes {
			if torrent, ok := m.torrents[hash]; ok {
				torrent.State = qbt.TorrentStateDownloading
			}
		}
	} else if action == "recheck" {
		m.hasRechecked = true
		m.recheckCount++
		for _, hash := range hashes {
			if torrent, ok := m.torrents[hash]; ok {
				if m.disappearAfterRecheck {
					delete(m.torrents, hash)
				} else if m.keepInCheckingState {
					torrent.State = qbt.TorrentStateCheckingDl
				} else if m.recheckCompletes || (m.secondRecheckCompletes && m.recheckCount >= 2) {
					torrent.State = qbt.TorrentStatePausedDl
					torrent.Progress = 1.0
				} else if m.setProgressToThreshold {
					torrent.State = qbt.TorrentStatePausedDl
					torrent.Progress = 0.95 // Exactly at threshold with 5% tolerance
				} else {
					// Leave incomplete
					torrent.State = qbt.TorrentStatePausedDl
					torrent.Progress = 0.5 // incomplete
				}
			}
		}
	}
	return nil
}

// Simulate state progression after recheck
func (m *mockRecoverSyncManager) simulateRecheckComplete(hash string, finalProgress float64, finalState qbt.TorrentState) {
	if torrent, ok := m.torrents[hash]; ok {
		torrent.Progress = finalProgress
		torrent.State = finalState
	}
}

func (m *mockRecoverSyncManager) GetTorrentFilesBatch(context.Context, int, []string) (map[string]qbt.TorrentFiles, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) GetTorrentProperties(context.Context, int, string) (*qbt.TorrentProperties, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{
		DiskCacheTTL: 1, // 1 second for tests
	}, nil
}

func (m *mockRecoverSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (m *mockRecoverSyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, fmt.Errorf("not implemented")
}

func (m *mockRecoverSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (m *mockRecoverSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, fmt.Errorf("not implemented")
}

func TestRecoverErroredTorrents_NoErroredTorrents(t *testing.T) {
	// Test with no errored torrents
	normalTorrent := qbt.Torrent{
		Hash:     "normal123",
		Name:     "normal.torrent",
		State:    qbt.TorrentStateDownloading,
		Progress: 0.5,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{normalTorrent})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{normalTorrent})
	require.NoError(t, err)

	// Should not have made any calls
	assert.Empty(t, mockSync.calls)
}

func TestRecoverErroredTorrents_SingleErroredTorrent(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{erroredTorrent})
	require.NoError(t, err)

	// Should have attempted pause, recheck, and resume
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[error123]")
}

func TestRecoverErroredTorrents_MultipleErroredTorrents(t *testing.T) {
	erroredTorrent1 := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored1.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}
	erroredTorrent2 := qbt.Torrent{
		Hash:     "error456",
		Name:     "errored2.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}
	normalTorrent := qbt.Torrent{
		Hash:     "normal123",
		Name:     "normal.torrent",
		State:    qbt.TorrentStateDownloading,
		Progress: 0.5,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent1, erroredTorrent2, normalTorrent})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{erroredTorrent1, erroredTorrent2, normalTorrent})
	require.NoError(t, err)

	// Should have attempted pause, recheck, and resume on both errored torrents
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error456]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error456]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[error456]")
	// Should not have touched the normal torrent
	assert.NotContains(t, mockSync.calls, "BulkAction:pause:[normal123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:recheck:[normal123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:resume:[normal123]")
}

func TestRecoverErroredTorrents_MissingFilesState(t *testing.T) {
	missingFilesTorrent := qbt.Torrent{
		Hash:     "missing123",
		Name:     "missing.torrent",
		State:    qbt.TorrentStateMissingFiles,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{missingFilesTorrent})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{missingFilesTorrent})
	require.NoError(t, err)

	// Should have attempted pause, recheck, and resume
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[missing123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[missing123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[missing123]")
}

func TestRecoverSingleErroredTorrent_RemainsIncomplete(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Configure mock to not complete the torrent after recheck
	mockSync.recheckCompletes = false
	svc := &Service{syncManager: mockSync}

	err := svc.recoverSingleErroredTorrent(context.Background(), 1, erroredTorrent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "torrent not complete after second recheck")

	// Should have attempted pause and recheck
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
}

func TestRecoverSingleErroredTorrent_DisappearsDuringRecheck(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Configure mock to make torrent disappear after recheck
	mockSync.disappearAfterRecheck = true
	svc := &Service{syncManager: mockSync}

	err := svc.recoverSingleErroredTorrent(context.Background(), 1, erroredTorrent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "torrent disappeared during recheck")

	// Should have attempted pause and recheck
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
}

func TestRecoverSingleErroredTorrent_BulkActionFails(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Configure mock to fail BulkAction
	mockSync.bulkActionFails = true
	svc := &Service{syncManager: mockSync}

	err := svc.recoverSingleErroredTorrent(context.Background(), 1, erroredTorrent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "pause torrent")

	// Should have attempted pause (recheck not attempted since pause failed)
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:recheck:[error123]")
}

func TestRecoverErroredTorrents_ContextCancelled(t *testing.T) {
	erroredTorrent1 := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored1.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}
	erroredTorrent2 := qbt.Torrent{
		Hash:     "error456",
		Name:     "errored2.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	ctx, cancel := context.WithCancel(context.Background())
	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent1, erroredTorrent2})
	svc := &Service{syncManager: mockSync}

	// Cancel context immediately
	cancel()

	err := svc.recoverErroredTorrents(ctx, 1, []qbt.Torrent{erroredTorrent1, erroredTorrent2})
	require.Error(t, err)
	assert.Equal(t, context.Canceled, err)
}

func TestRecoverErroredTorrents_MixedStates(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}
	missingFilesTorrent := qbt.Torrent{
		Hash:     "missing123",
		Name:     "missing.torrent",
		State:    qbt.TorrentStateMissingFiles,
		Progress: 0.0,
	}
	downloadingTorrent := qbt.Torrent{
		Hash:     "download123",
		Name:     "downloading.torrent",
		State:    qbt.TorrentStateDownloading,
		Progress: 0.3,
	}
	completedTorrent := qbt.Torrent{
		Hash:     "complete123",
		Name:     "completed.torrent",
		State:    qbt.TorrentStatePausedDl,
		Progress: 1.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent, missingFilesTorrent, downloadingTorrent, completedTorrent})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{erroredTorrent, missingFilesTorrent, downloadingTorrent, completedTorrent})
	require.NoError(t, err)

	// Should have attempted pause, recheck, and resume only on errored and missing files torrents
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[missing123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[missing123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[missing123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:pause:[download123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:recheck:[download123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:resume:[download123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:pause:[complete123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:recheck:[complete123]")
	assert.NotContains(t, mockSync.calls, "BulkAction:resume:[complete123]")
}

func TestRecoverSingleErroredTorrent_Timeout(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Configure mock to keep torrent in checking state forever
	mockSync.keepInCheckingState = true
	svc := &Service{syncManager: mockSync}

	// Use a short timeout for testing - should cancel before the 5-minute internal timeout
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()

	err := svc.recoverSingleErroredTorrent(ctx, 1, erroredTorrent)
	require.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)

	// Should have attempted pause and recheck
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
}

func TestRecoverSingleErroredTorrent_GetTorrentsFailsDuringWait(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Configure mock to fail GetTorrents after recheck
	mockSync.failGetTorrentsAfterRecheck = true
	svc := &Service{syncManager: mockSync}

	err := svc.recoverSingleErroredTorrent(context.Background(), 1, erroredTorrent)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "torrent disappeared during recheck")

	// Should have attempted pause and recheck
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
}

func TestRecoverSingleErroredTorrent_ProgressAtThreshold(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Configure mock to set progress exactly at threshold (0.95 with 5% tolerance)
	mockSync.setProgressToThreshold = true
	svc := &Service{syncManager: mockSync}

	err := svc.recoverSingleErroredTorrent(context.Background(), 1, erroredTorrent)
	require.NoError(t, err)

	// Should have attempted pause, recheck, and resume
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[error123]")
}

func TestRecoverErroredTorrents_EmptyList(t *testing.T) {
	mockSync := newMockRecoverSyncManager([]qbt.Torrent{})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{})
	require.NoError(t, err)

	// Should not have made any calls
	assert.Empty(t, mockSync.calls)
}

func TestRecoverSingleErroredTorrent_SettingsLoadFailure(t *testing.T) {
	erroredTorrent := qbt.Torrent{
		Hash:     "error123",
		Name:     "errored.torrent",
		State:    qbt.TorrentStateError,
		Progress: 0.0,
	}

	mockSync := newMockRecoverSyncManager([]qbt.Torrent{erroredTorrent})
	// Create service without settings store to simulate failure
	svc := &Service{syncManager: mockSync}

	err := svc.recoverSingleErroredTorrent(context.Background(), 1, erroredTorrent)
	require.NoError(t, err) // Should still work with defaults

	// Should have attempted pause, recheck, and resume
	assert.Contains(t, mockSync.calls, "BulkAction:pause:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:recheck:[error123]")
	assert.Contains(t, mockSync.calls, "BulkAction:resume:[error123]")
}
