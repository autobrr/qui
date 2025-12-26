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
	"github.com/autobrr/qui/internal/services/jackett"
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
			name:               "partial-in-pack episode in season pack (folder source)",
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
			name:               "partial-in-pack single-file episode into season pack folder",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows", contentLayout: "Original",
			matchType:      "partial-in-pack",
			sourceFiles:    qbt.TorrentFiles{{Name: "ep.mkv"}}, // Single file, no folder
			candidateFiles: qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:       "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:    "Single-file TV episode uses season pack's ContentPath, not SavePath+Subfolder",
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
			wantPath:       "/movies",
			description:    "Loose file uses SavePath, Subfolder layout creates folder",
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
			description: "TV episode into season pack uses ContentPath, NoSubfolder layout",
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
			wantPath:    "/movies",
			description: "Loose movie uses SavePath, Subfolder layout creates folder",
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
		// Code normalizes all paths to forward slashes via strings.ReplaceAll
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

	// Step 2: Verify determineSavePath uses ContentPath for TV episode into season pack
	// TV episodes going into season packs should use the season pack's ContentPath directly
	// with NoSubfolder layout (not SavePath + Subfolder which would create wrong folder name)
	savePath := svc.determineSavePath(episodeName, seasonPackTorrent, seasonPackProps, matchType, episodeFiles, seasonPackFiles, "Original")
	require.Equal(t, "/downloads/tv/The.Show.S01.1080p.BluRay.x264-GRP", savePath,
		"TV episode into season pack uses ContentPath directly")
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

	// Step 2: Verify determineSavePath uses SavePath (Subfolder layout will create folder)
	savePath := svc.determineSavePath(movieName, collectionTorrent, collectionProps, matchType, movieFiles, collectionFiles, "Original")
	require.Equal(t, "/downloads/movies", savePath,
		"partial-in-pack single file into folder uses SavePath, Subfolder layout creates folder")
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
		name                  string
		request               *CrossSeedRequest
		matched               qbt.Torrent
		settings              *models.CrossSeedAutomationSettings
		inheritSourceTags     bool
		expectedBaseCategory  string
		expectedCrossCategory string
		expectedTags          []string
	}{
		{
			name: "inherit matched tags when inheritSourceTags enabled",
			request: &CrossSeedRequest{
				Category:          "",
				Tags:              []string{"cross-seed"},
				InheritSourceTags: true,
			},
			matched: qbt.Torrent{
				Category: "movies",
				Tags:     "tracker1,quality-1080p",
			},
			settings:              defaultSettings,
			inheritSourceTags:     true,
			expectedBaseCategory:  "movies",
			expectedCrossCategory: "movies.cross",
			expectedTags:          []string{"cross-seed", "tracker1", "quality-1080p"},
		},
		{
			name: "source tags without inheritance",
			request: &CrossSeedRequest{
				Category: "movies-4k",
				Tags:     []string{"custom", "cross-seed"},
			},
			matched: qbt.Torrent{
				Category: "movies",
				Tags:     "tracker1",
			},
			settings:              defaultSettings,
			inheritSourceTags:     false,
			expectedBaseCategory:  "movies-4k",
			expectedCrossCategory: "movies-4k.cross",
			expectedTags:          []string{"custom", "cross-seed"},
		},
		{
			name: "source tags with inheritance",
			request: &CrossSeedRequest{
				Category:          "",
				Tags:              []string{"cross-seed"},
				InheritSourceTags: true,
			},
			matched: qbt.Torrent{
				Category: "tv",
				Tags:     "sonarr",
			},
			settings:              defaultSettings,
			inheritSourceTags:     true,
			expectedBaseCategory:  "tv",
			expectedCrossCategory: "tv.cross",
			expectedTags:          []string{"cross-seed", "sonarr"},
		},
		{
			name: "use indexer category when enabled",
			request: &CrossSeedRequest{
				Category:    "",
				IndexerName: "IndexerCat",
				Tags:        []string{"cross-seed"},
			},
			matched: qbt.Torrent{
				Category: "fallback",
			},
			settings: &models.CrossSeedAutomationSettings{
				UseCategoryFromIndexer: true,
				UseCrossCategorySuffix: true,
			},
			inheritSourceTags:     false,
			expectedBaseCategory:  "IndexerCat",
			expectedCrossCategory: "IndexerCat.cross",
			expectedTags:          []string{"cross-seed"},
		},
		{
			name: "no tags when source tags empty",
			request: &CrossSeedRequest{
				Category: "",
				Tags:     []string{},
			},
			matched: qbt.Torrent{
				Category: "tv",
				Tags:     "",
			},
			settings:              defaultSettings,
			inheritSourceTags:     false,
			expectedBaseCategory:  "tv",
			expectedCrossCategory: "tv.cross",
			expectedTags:          []string{},
		},
		{
			name: "empty category stays empty",
			request: &CrossSeedRequest{
				Category: "",
				Tags:     []string{},
			},
			matched: qbt.Torrent{
				Category: "",
				Tags:     "",
			},
			settings:              defaultSettings,
			inheritSourceTags:     false,
			expectedBaseCategory:  "",
			expectedCrossCategory: "",
			expectedTags:          []string{},
		},
		{
			name: "no double suffix for already suffixed category",
			request: &CrossSeedRequest{
				Category: "movies.cross",
				Tags:     []string{},
			},
			matched: qbt.Torrent{
				Category: "movies",
				Tags:     "",
			},
			settings:              defaultSettings,
			inheritSourceTags:     false,
			expectedBaseCategory:  "movies.cross",
			expectedCrossCategory: "movies.cross",
			expectedTags:          []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := &Service{}

			// Use tt.settings if provided, otherwise use defaultSettings
			settings := tt.settings
			if settings == nil {
				settings = defaultSettings
			}

			baseCategory, crossCategory := svc.determineCrossSeedCategory(context.Background(), tt.request, &tt.matched, settings)
			assert.Equal(t, tt.expectedBaseCategory, baseCategory)
			assert.Equal(t, tt.expectedCrossCategory, crossCategory)

			tags := buildCrossSeedTags(tt.request.Tags, tt.matched.Tags, tt.inheritSourceTags)
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

func (f *fakeSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (f *fakeSyncManager) CreateCategory(_ context.Context, _ int, _, _ string) error {
	return nil
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

		// Single-file TV episode into season pack (the specific bug fix scenario)
		// These verify that single-file episodes use ContentPath (not SavePath+Subfolder)
		{
			name:               "original_layout_single_file_episode_into_season_pack",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows",
			contentLayout:      "Original",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "ep.mkv"}}, // Single file, no folder
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:           "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:        "Original layout: Single-file TV episode uses ContentPath, NoSubfolder layout required",
		},
		{
			name:               "subfolder_layout_single_file_episode_into_season_pack",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows",
			contentLayout:      "Subfolder",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "ep.mkv"}}, // Single file, no folder
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:           "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:        "Subfolder layout: Single-file TV episode uses ContentPath, NoSubfolder layout required",
		},
		{
			name:               "nosubfolder_layout_single_file_episode_into_season_pack",
			newTorrentName:     "Show.S01E05.1080p.WEB-DL.x264-GROUP",
			matchedTorrentName: "Show.S01.1080p.BluRay.x264-OTHER",
			matchedContentPath: "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			baseSavePath:       "/data/media/Shows",
			contentLayout:      "NoSubfolder",
			matchType:          "partial-in-pack",
			sourceFiles:        qbt.TorrentFiles{{Name: "ep.mkv"}}, // Single file, no folder
			candidateFiles:     qbt.TorrentFiles{{Name: "Show.S01.1080p.BluRay.x264-OTHER/ep1.mkv"}},
			wantPath:           "/data/media/Shows/Show.S01.1080p.BluRay.x264-OTHER",
			description:        "NoSubfolder layout: Single-file TV episode uses ContentPath",
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

func (m *mockRecoverSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (m *mockRecoverSyncManager) CreateCategory(_ context.Context, _ int, _, _ string) error {
	return nil
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

	// Should have batched pause, recheck, and resume on both errored torrents
	// Check that both hashes are in the batched calls (order may vary)
	var hasPauseBatch, hasRecheckBatch, hasResumeBatch bool
	for _, call := range mockSync.calls {
		if strings.HasPrefix(call, "BulkAction:pause:") && strings.Contains(call, "error123") && strings.Contains(call, "error456") {
			hasPauseBatch = true
		}
		if strings.HasPrefix(call, "BulkAction:recheck:") && strings.Contains(call, "error123") && strings.Contains(call, "error456") {
			hasRecheckBatch = true
		}
		if strings.HasPrefix(call, "BulkAction:resume:") && strings.Contains(call, "error123") && strings.Contains(call, "error456") {
			hasResumeBatch = true
		}
	}
	assert.True(t, hasPauseBatch, "expected batched pause call with both hashes")
	assert.True(t, hasRecheckBatch, "expected batched recheck call with both hashes")
	assert.True(t, hasResumeBatch, "expected batched resume call with both hashes")
	// Should not have touched the normal torrent
	for _, call := range mockSync.calls {
		assert.NotContains(t, call, "normal123", "normal torrent should not be in any calls")
	}
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

	// Should have batched pause, recheck, and resume only on errored and missing files torrents
	var hasPauseBatch, hasRecheckBatch, hasResumeBatch bool
	for _, call := range mockSync.calls {
		if strings.HasPrefix(call, "BulkAction:pause:") && strings.Contains(call, "error123") && strings.Contains(call, "missing123") {
			hasPauseBatch = true
		}
		if strings.HasPrefix(call, "BulkAction:recheck:") && strings.Contains(call, "error123") && strings.Contains(call, "missing123") {
			hasRecheckBatch = true
		}
		if strings.HasPrefix(call, "BulkAction:resume:") && strings.Contains(call, "error123") && strings.Contains(call, "missing123") {
			hasResumeBatch = true
		}
	}
	assert.True(t, hasPauseBatch, "expected batched pause call with both errored hashes")
	assert.True(t, hasRecheckBatch, "expected batched recheck call with both errored hashes")
	assert.True(t, hasResumeBatch, "expected batched resume call with both errored hashes")
	// Should not have touched downloading or completed torrents
	for _, call := range mockSync.calls {
		assert.NotContains(t, call, "download123", "downloading torrent should not be in any calls")
		assert.NotContains(t, call, "complete123", "completed torrent should not be in any calls")
	}
}

func TestRecoverErroredTorrents_EmptyList(t *testing.T) {
	mockSync := newMockRecoverSyncManager([]qbt.Torrent{})
	svc := &Service{syncManager: mockSync}

	err := svc.recoverErroredTorrents(context.Background(), 1, []qbt.Torrent{})
	require.NoError(t, err)

	// Should not have made any calls
	assert.Empty(t, mockSync.calls)
}

func TestExtractTorrentURLForCommentMatch(t *testing.T) {
	tests := []struct {
		name     string
		guid     string
		infoURL  string
		expected string
	}{
		{
			name:     "UNIT3D style URL in GUID",
			guid:     "https://seedpool.org/torrents/607803",
			infoURL:  "",
			expected: "https://seedpool.org/torrents/607803",
		},
		{
			name:     "BHD style URL in GUID",
			guid:     "https://beyond-hd.me/details/500790",
			infoURL:  "",
			expected: "https://beyond-hd.me/details/500790",
		},
		{
			name:     "Aither URL",
			guid:     "https://aither.cc/torrents/318093",
			infoURL:  "",
			expected: "https://aither.cc/torrents/318093",
		},
		{
			name:     "Blutopia URL",
			guid:     "https://blutopia.cc/torrents/294836",
			infoURL:  "",
			expected: "https://blutopia.cc/torrents/294836",
		},
		{
			name:     "Falls back to InfoURL when GUID empty",
			guid:     "",
			infoURL:  "https://seedpool.org/torrents/123456",
			expected: "https://seedpool.org/torrents/123456",
		},
		{
			name:     "Falls back to InfoURL when GUID not a torrent URL",
			guid:     "some-random-guid-12345",
			infoURL:  "https://seedpool.org/torrents/123456",
			expected: "https://seedpool.org/torrents/123456",
		},
		{
			name:     "HTTP URL rejected",
			guid:     "http://seedpool.org/torrents/607803",
			infoURL:  "",
			expected: "",
		},
		{
			name:     "Non-torrent URL rejected",
			guid:     "https://example.com/page/123",
			infoURL:  "",
			expected: "",
		},
		{
			name:     "Empty inputs",
			guid:     "",
			infoURL:  "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractTorrentURLForCommentMatch(tt.guid, tt.infoURL)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// infohashTestSyncManager extends episodeSyncManager with configurable HasTorrentByAnyHash behavior
type infohashTestSyncManager struct {
	torrents    map[int][]qbt.Torrent
	files       map[int]map[string]qbt.TorrentFiles
	props       map[int]map[string]*qbt.TorrentProperties
	hashResults map[int]*hashCheckResult // instanceID -> result
}

type hashCheckResult struct {
	torrent *qbt.Torrent
	exists  bool
	err     error
}

func newInfohashTestSyncManager() *infohashTestSyncManager {
	return &infohashTestSyncManager{
		torrents:    make(map[int][]qbt.Torrent),
		files:       make(map[int]map[string]qbt.TorrentFiles),
		props:       make(map[int]map[string]*qbt.TorrentProperties),
		hashResults: make(map[int]*hashCheckResult),
	}
}

func (f *infohashTestSyncManager) GetTorrents(_ context.Context, instanceID int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	list := f.torrents[instanceID]
	if list == nil {
		return nil, fmt.Errorf("instance %d has no torrents", instanceID)
	}
	copied := make([]qbt.Torrent, len(list))
	copy(copied, list)
	return copied, nil
}

func (f *infohashTestSyncManager) GetTorrentFilesBatch(_ context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	if instFiles, ok := f.files[instanceID]; ok {
		for _, h := range hashes {
			if files, ok := instFiles[strings.ToLower(h)]; ok {
				cp := make(qbt.TorrentFiles, len(files))
				copy(cp, files)
				result[normalizeHash(h)] = cp
			}
		}
	}
	return result, nil
}

func (f *infohashTestSyncManager) HasTorrentByAnyHash(_ context.Context, instanceID int, _ []string) (*qbt.Torrent, bool, error) {
	if result, ok := f.hashResults[instanceID]; ok {
		return result.torrent, result.exists, result.err
	}
	return nil, false, nil
}

func (f *infohashTestSyncManager) GetTorrentProperties(_ context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	if instProps, ok := f.props[instanceID]; ok {
		if props, ok := instProps[strings.ToLower(hash)]; ok {
			cp := *props
			return &cp, nil
		}
	}
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (f *infohashTestSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (f *infohashTestSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
}

func (f *infohashTestSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (f *infohashTestSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (f *infohashTestSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	// Build views from torrents
	if list, ok := f.torrents[instanceID]; ok {
		views := make([]internalqb.CrossInstanceTorrentView, len(list))
		for i, t := range list {
			views[i] = internalqb.CrossInstanceTorrentView{
				TorrentView: internalqb.TorrentView{
					Torrent: t,
				},
				InstanceID: instanceID,
			}
		}
		return views, nil
	}
	return nil, nil
}

func (f *infohashTestSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (f *infohashTestSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (f *infohashTestSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (f *infohashTestSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (f *infohashTestSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (f *infohashTestSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (f *infohashTestSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

type infohashTestInstanceStore struct {
	instances map[int]*models.Instance
}

func (f *infohashTestInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	inst, ok := f.instances[id]
	if !ok {
		return nil, fmt.Errorf("instance %d not found", id)
	}
	return inst, nil
}

func (f *infohashTestInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	list := make([]*models.Instance, 0, len(f.instances))
	for _, inst := range f.instances {
		list = append(list, inst)
	}
	return list, nil
}

func TestProcessAutomationCandidate_SkipsWhenInfohashExistsOnAllInstances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance1ID := 1
	instance2ID := 2
	testHash := "63e07ff523710ca268567dad344ce1e0e6b7e8a3"
	torrentName := "Show.S01.1080p.BluRay-GROUP"

	existingTorrent := qbt.Torrent{
		Hash:     testHash,
		Name:     torrentName,
		Progress: 1.0,
		Category: "tv",
	}

	sync := newInfohashTestSyncManager()
	// Set up torrents for both instances
	sync.torrents[instance1ID] = []qbt.Torrent{existingTorrent}
	sync.torrents[instance2ID] = []qbt.Torrent{existingTorrent}
	sync.files[instance1ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.files[instance2ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.props[instance1ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}
	sync.props[instance2ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}

	// Configure HasTorrentByAnyHash to return existing torrent for both instances
	sync.hashResults[instance1ID] = &hashCheckResult{
		torrent: &existingTorrent,
		exists:  true,
		err:     nil,
	}
	sync.hashResults[instance2ID] = &hashCheckResult{
		torrent: &existingTorrent,
		exists:  true,
		err:     nil,
	}

	downloadCalled := false
	service := &Service{
		instanceStore: &infohashTestInstanceStore{
			instances: map[int]*models.Instance{
				instance1ID: {ID: instance1ID, Name: "Instance1"},
				instance2ID: {ID: instance2ID, Name: "Instance2"},
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent"), nil
		},
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:       true,
		RSSAutomationTags: []string{"cross-seed"},
		IgnorePatterns:    []string{},
		TargetInstanceIDs: []int{instance1ID, instance2ID},
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                torrentName,
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 "guid-1",
		Size:                 1024,
		InfoHashV1:           testHash,
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, returnedHash, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})

	require.NoError(t, err)
	assert.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	assert.NotNil(t, returnedHash)
	assert.Equal(t, testHash, *returnedHash)
	assert.Equal(t, 2, run.TorrentsSkipped, "should skip for both instances")
	assert.False(t, downloadCalled, "should NOT download torrent when it exists on all instances")
}

func TestProcessAutomationCandidate_ProceedsWhenInfohashExistsOnSomeInstances(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance1ID := 1
	instance2ID := 2
	testHash := "63e07ff523710ca268567dad344ce1e0e6b7e8a3"
	torrentName := "Show.S01.1080p.BluRay-GROUP"

	existingTorrent := qbt.Torrent{
		Hash:     testHash,
		Name:     torrentName,
		Progress: 1.0,
		Category: "tv",
	}

	sync := newInfohashTestSyncManager()
	sync.torrents[instance1ID] = []qbt.Torrent{existingTorrent}
	sync.torrents[instance2ID] = []qbt.Torrent{existingTorrent} // Both have the torrent for candidate matching
	sync.files[instance1ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.files[instance2ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.props[instance1ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}
	sync.props[instance2ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}

	// Instance 1 has the torrent by hash, Instance 2 does not (simulating different torrent file)
	sync.hashResults[instance1ID] = &hashCheckResult{
		torrent: &existingTorrent,
		exists:  true,
		err:     nil,
	}
	sync.hashResults[instance2ID] = &hashCheckResult{
		torrent: nil,
		exists:  false,
		err:     nil,
	}

	downloadCalled := false
	service := &Service{
		instanceStore: &infohashTestInstanceStore{
			instances: map[int]*models.Instance{
				instance1ID: {ID: instance1ID, Name: "Instance1"},
				instance2ID: {ID: instance2ID, Name: "Instance2"},
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent"), nil
		},
	}

	// Mock crossSeedInvoker to avoid nil panic
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		return &CrossSeedResponse{
			Success: true,
			Results: []InstanceCrossSeedResult{
				{InstanceID: instance2ID, InstanceName: "Instance2", Success: true, Status: "added"},
			},
		}, nil
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:       true,
		RSSAutomationTags: []string{"cross-seed"},
		IgnorePatterns:    []string{},
		TargetInstanceIDs: []int{instance1ID, instance2ID},
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                torrentName,
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 "guid-1",
		Size:                 1024,
		InfoHashV1:           testHash,
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})

	require.NoError(t, err)
	// Should proceed with download since not all instances have it
	assert.True(t, downloadCalled, "should download torrent when not all instances have it")
	assert.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
}

func TestProcessAutomationCandidate_ProceedsOnHashCheckError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance1ID := 1
	testHash := "63e07ff523710ca268567dad344ce1e0e6b7e8a3"
	torrentName := "Show.S01.1080p.BluRay-GROUP"

	existingTorrent := qbt.Torrent{
		Hash:     testHash,
		Name:     torrentName,
		Progress: 1.0,
		Category: "tv",
	}

	sync := newInfohashTestSyncManager()
	sync.torrents[instance1ID] = []qbt.Torrent{existingTorrent}
	sync.files[instance1ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.props[instance1ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}

	// Configure HasTorrentByAnyHash to return an error
	sync.hashResults[instance1ID] = &hashCheckResult{
		torrent: nil,
		exists:  false,
		err:     errors.New("connection refused"),
	}

	downloadCalled := false
	service := &Service{
		instanceStore: &infohashTestInstanceStore{
			instances: map[int]*models.Instance{
				instance1ID: {ID: instance1ID, Name: "Instance1"},
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent"), nil
		},
	}

	// Mock crossSeedInvoker
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		return &CrossSeedResponse{
			Success: true,
			Results: []InstanceCrossSeedResult{
				{InstanceID: instance1ID, InstanceName: "Instance1", Success: true, Status: "added"},
			},
		}, nil
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:       true,
		RSSAutomationTags: []string{"cross-seed"},
		IgnorePatterns:    []string{},
		TargetInstanceIDs: []int{instance1ID},
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                torrentName,
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 "guid-1",
		Size:                 1024,
		InfoHashV1:           testHash,
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})

	require.NoError(t, err)
	// Should proceed with download on error (graceful degradation)
	assert.True(t, downloadCalled, "should download torrent when hash check fails")
	assert.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
}

func TestProcessAutomationCandidate_PropagatesContextCancellation(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance1ID := 1
	testHash := "63e07ff523710ca268567dad344ce1e0e6b7e8a3"
	torrentName := "Show.S01.1080p.BluRay-GROUP"

	existingTorrent := qbt.Torrent{
		Hash:     testHash,
		Name:     torrentName,
		Progress: 1.0,
		Category: "tv",
	}

	sync := newInfohashTestSyncManager()
	sync.torrents[instance1ID] = []qbt.Torrent{existingTorrent}
	sync.files[instance1ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.props[instance1ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}

	// Configure HasTorrentByAnyHash to return context.Canceled error
	sync.hashResults[instance1ID] = &hashCheckResult{
		torrent: nil,
		exists:  false,
		err:     context.Canceled,
	}

	downloadCalled := false
	service := &Service{
		instanceStore: &infohashTestInstanceStore{
			instances: map[int]*models.Instance{
				instance1ID: {ID: instance1ID, Name: "Instance1"},
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent"), nil
		},
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:       true,
		RSSAutomationTags: []string{"cross-seed"},
		IgnorePatterns:    []string{},
		TargetInstanceIDs: []int{instance1ID},
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                torrentName,
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 "guid-1",
		Size:                 1024,
		InfoHashV1:           testHash,
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})

	// Context cancellation should propagate as an error, not trigger fallback
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
	assert.Contains(t, err.Error(), "hash check canceled")
	assert.Equal(t, models.CrossSeedFeedItemStatusFailed, status)
	assert.Equal(t, 1, run.TorrentsFailed, "should increment TorrentsFailed on context cancellation")
	assert.False(t, downloadCalled, "should NOT download torrent when context is canceled")
}

func TestProcessAutomationCandidate_PropagatesContextDeadlineExceeded(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance1ID := 1
	testHash := "63e07ff523710ca268567dad344ce1e0e6b7e8a3"
	torrentName := "Show.S01.1080p.BluRay-GROUP"

	existingTorrent := qbt.Torrent{
		Hash:     testHash,
		Name:     torrentName,
		Progress: 1.0,
		Category: "tv",
	}

	sync := newInfohashTestSyncManager()
	sync.torrents[instance1ID] = []qbt.Torrent{existingTorrent}
	sync.files[instance1ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(testHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.props[instance1ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(testHash): {SavePath: "/downloads"},
	}

	// Configure HasTorrentByAnyHash to return context.DeadlineExceeded error
	sync.hashResults[instance1ID] = &hashCheckResult{
		torrent: nil,
		exists:  false,
		err:     context.DeadlineExceeded,
	}

	downloadCalled := false
	service := &Service{
		instanceStore: &infohashTestInstanceStore{
			instances: map[int]*models.Instance{
				instance1ID: {ID: instance1ID, Name: "Instance1"},
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent"), nil
		},
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:       true,
		RSSAutomationTags: []string{"cross-seed"},
		IgnorePatterns:    []string{},
		TargetInstanceIDs: []int{instance1ID},
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                torrentName,
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 "guid-1",
		Size:                 1024,
		InfoHashV1:           testHash,
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, _, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})

	// Context deadline exceeded should propagate as an error, not trigger fallback
	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Contains(t, err.Error(), "hash check canceled")
	assert.Equal(t, models.CrossSeedFeedItemStatusFailed, status)
	assert.Equal(t, 1, run.TorrentsFailed, "should increment TorrentsFailed on context deadline exceeded")
	assert.False(t, downloadCalled, "should NOT download torrent when context deadline exceeded")
}

func TestProcessAutomationCandidate_SkipsWhenCommentURLMatches(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instance1ID := 1
	commentURL := "https://seedpool.org/torrents/607803"
	torrentName := "Show.S01.1080p.BluRay-GROUP"
	torrentHash := "abc123def456abc123def456abc123def456abcd"

	// Torrent with matching comment
	existingTorrent := qbt.Torrent{
		Hash:     torrentHash,
		Name:     torrentName,
		Progress: 1.0,
		Category: "tv",
		Comment:  "Uploaded from https://seedpool.org/torrents/607803",
	}

	sync := newInfohashTestSyncManager()
	sync.torrents[instance1ID] = []qbt.Torrent{existingTorrent}
	sync.files[instance1ID] = map[string]qbt.TorrentFiles{
		strings.ToLower(torrentHash): {{Name: "Show.S01E01.1080p.BluRay-GROUP.mkv", Size: 1024}},
	}
	sync.props[instance1ID] = map[string]*qbt.TorrentProperties{
		strings.ToLower(torrentHash): {SavePath: "/downloads"},
	}

	// No hash results configured - will return nil, false, nil
	// This forces the fallback to comment URL matching

	downloadCalled := false
	service := &Service{
		instanceStore: &infohashTestInstanceStore{
			instances: map[int]*models.Instance{
				instance1ID: {ID: instance1ID, Name: "Instance1"},
			},
		},
		syncManager:      sync,
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			downloadCalled = true
			return []byte("torrent"), nil
		},
	}

	settings := &models.CrossSeedAutomationSettings{
		StartPaused:       true,
		RSSAutomationTags: []string{"cross-seed"},
		IgnorePatterns:    []string{},
		TargetInstanceIDs: []int{instance1ID},
	}

	run := &models.CrossSeedRun{}
	result := jackett.SearchResult{
		Indexer:              "Example",
		IndexerID:            10,
		Title:                torrentName,
		DownloadURL:          "https://example.invalid/download.torrent",
		GUID:                 commentURL, // UNIT3D style: GUID is the torrent details URL
		Size:                 1024,
		InfoHashV1:           "", // No infohash provided
		DownloadVolumeFactor: 1.0,
		UploadVolumeFactor:   1.0,
	}

	status, returnedHash, err := service.processAutomationCandidate(ctx, run, settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})

	require.NoError(t, err)
	assert.Equal(t, models.CrossSeedFeedItemStatusProcessed, status)
	assert.Nil(t, returnedHash, "should not return hash for comment URL match")
	assert.Equal(t, 1, run.TorrentsSkipped, "should skip for instance with matching comment")
	assert.False(t, downloadCalled, "should NOT download torrent when comment URL matches")
}

func TestCheckWebhook_WebhookSourceFilters(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{
		ID:   1,
		Name: "Test Instance",
	}
	instanceIDs := []int{instance.ID}

	tests := []struct {
		name               string
		request            *WebhookCheckRequest
		existingTorrents   []qbt.Torrent
		settings           *models.CrossSeedAutomationSettings
		wantCanCrossSeed   bool
		wantMatchCount     int
		wantRecommendation string
	}{
		{
			name: "exclude category filters out matching torrent",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Filter.Test.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "excluded", Name: "Filter.Test.2025.1080p.BluRay.x264-GRP", Category: "cross-seed-link", Progress: 1.0},
				{Hash: "included", Name: "Filter.Test.2025.1080p.BluRay.x264-GRP", Category: "movies", Progress: 1.0},
			},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link"},
				SizeMismatchTolerancePercent:   5.0,
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1, // Only movies category torrent matches
			wantRecommendation: "download",
		},
		{
			name: "exclude tag filters out matching torrent",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Tag.Filter.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "excluded", Name: "Tag.Filter.2025.1080p.BluRay.x264-GRP", Tags: "no-cross-seed, other", Progress: 1.0},
				{Hash: "included", Name: "Tag.Filter.2025.1080p.BluRay.x264-GRP", Tags: "cross-seed", Progress: 1.0},
			},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeTags:     []string{"no-cross-seed"},
				SizeMismatchTolerancePercent: 5.0,
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1,
			wantRecommendation: "download",
		},
		{
			name: "all torrents filtered out returns skip",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "All.Excluded.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "excluded1", Name: "All.Excluded.2025.1080p.BluRay.x264-GRP", Category: "cross-seed-link", Progress: 1.0},
				{Hash: "excluded2", Name: "All.Excluded.2025.1080p.BluRay.x264-GRP", Category: "cross-seed-link", Progress: 1.0},
			},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link"},
				SizeMismatchTolerancePercent:   5.0,
			},
			wantCanCrossSeed:   false,
			wantMatchCount:     0,
			wantRecommendation: "skip",
		},
		{
			name: "include category restricts matches",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "Include.Only.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "movies", Name: "Include.Only.2025.1080p.BluRay.x264-GRP", Category: "movies", Progress: 1.0},
				{Hash: "tv", Name: "Include.Only.2025.1080p.BluRay.x264-GRP", Category: "tv", Progress: 1.0},
			},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories:      []string{"movies"},
				SizeMismatchTolerancePercent: 5.0,
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     1, // Only movies category matches
			wantRecommendation: "download",
		},
		{
			name: "empty filters match all torrents",
			request: &WebhookCheckRequest{
				InstanceIDs: instanceIDs,
				TorrentName: "No.Filter.2025.1080p.BluRay.x264-GRP",
			},
			existingTorrents: []qbt.Torrent{
				{Hash: "cat1", Name: "No.Filter.2025.1080p.BluRay.x264-GRP", Category: "movies", Progress: 1.0},
				{Hash: "cat2", Name: "No.Filter.2025.1080p.BluRay.x264-GRP", Category: "tv", Progress: 1.0},
			},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories:        []string{},
				WebhookSourceExcludeCategories: []string{},
				SizeMismatchTolerancePercent:   5.0,
			},
			wantCanCrossSeed:   true,
			wantMatchCount:     2, // Both match
			wantRecommendation: "download",
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
				automationSettingsLoader: func(_ context.Context) (*models.CrossSeedAutomationSettings, error) {
					return tt.settings, nil
				},
			}

			resp, err := svc.CheckWebhook(context.Background(), tt.request)
			require.NoError(t, err)

			assert.Equal(t, tt.wantCanCrossSeed, resp.CanCrossSeed, "CanCrossSeed mismatch")
			assert.Equal(t, tt.wantMatchCount, len(resp.Matches), "Match count mismatch")
			assert.Equal(t, tt.wantRecommendation, resp.Recommendation, "Recommendation mismatch")
		})
	}
}

func TestMatchesWebhookSourceFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		torrent  *qbt.Torrent
		settings *models.CrossSeedAutomationSettings
		want     bool
	}{
		{
			name:     "nil torrent returns false",
			torrent:  nil,
			settings: &models.CrossSeedAutomationSettings{},
			want:     false,
		},
		{
			name:     "nil settings returns false",
			torrent:  &qbt.Torrent{Category: "movies"},
			settings: nil,
			want:     false,
		},
		{
			name:     "empty filters match all torrents",
			torrent:  &qbt.Torrent{Category: "movies", Tags: "cross-seed"},
			settings: &models.CrossSeedAutomationSettings{},
			want:     true,
		},
		{
			name:    "exclude category skips matching torrent",
			torrent: &qbt.Torrent{Category: "cross-seed-link"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link"},
			},
			want: false,
		},
		{
			name:    "exclude category allows non-matching torrent",
			torrent: &qbt.Torrent{Category: "movies"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link"},
			},
			want: true,
		},
		{
			name:    "multiple exclude categories work",
			torrent: &qbt.Torrent{Category: "temp"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link", "temp", "staging"},
			},
			want: false,
		},
		{
			name:    "exclude tag skips matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, temporary"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeTags: []string{"temporary"},
			},
			want: false,
		},
		{
			name:    "exclude tag allows non-matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, important"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeTags: []string{"temporary"},
			},
			want: true,
		},
		{
			name:    "include category requires match",
			torrent: &qbt.Torrent{Category: "tv"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies"},
			},
			want: false,
		},
		{
			name:    "include category allows matching torrent",
			torrent: &qbt.Torrent{Category: "movies"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies", "tv"},
			},
			want: true,
		},
		{
			name:    "include tag requires at least one match",
			torrent: &qbt.Torrent{Tags: "important"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags: []string{"important", "priority"},
			},
			want: true,
		},
		{
			name:    "include tag rejects when no match",
			torrent: &qbt.Torrent{Tags: "random"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags: []string{"important", "priority"},
			},
			want: false,
		},
		{
			name:    "exclude takes precedence over include",
			torrent: &qbt.Torrent{Category: "movies"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories:        []string{"movies", "tv"},
				WebhookSourceExcludeCategories: []string{"movies"},
			},
			want: false,
		},
		{
			name:    "empty category with exclude filter passes",
			torrent: &qbt.Torrent{Category: ""},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link"},
			},
			want: true,
		},
		{
			name:    "empty tags with include tag filter fails",
			torrent: &qbt.Torrent{Tags: ""},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags: []string{"important"},
			},
			want: false,
		},
		{
			name:    "tags are case-sensitive",
			torrent: &qbt.Torrent{Tags: "Important"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags: []string{"important"},
			},
			want: false,
		},
		{
			name:    "exclude tag takes precedence over include tag",
			torrent: &qbt.Torrent{Tags: "important, blocked"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags:        []string{"important"},
				WebhookSourceExcludeTags: []string{"blocked"},
			},
			want: false,
		},
		{
			name:    "category and tag filters both apply - passes both",
			torrent: &qbt.Torrent{Category: "movies", Tags: "important"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies"},
				WebhookSourceTags:       []string{"important"},
			},
			want: true,
		},
		{
			name:    "passes category filter but fails tag filter",
			torrent: &qbt.Torrent{Category: "movies", Tags: "random"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies"},
				WebhookSourceTags:       []string{"important"},
			},
			want: false,
		},
		{
			name:    "passes tag filter but fails category filter",
			torrent: &qbt.Torrent{Category: "tv", Tags: "important"},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies"},
				WebhookSourceTags:       []string{"important"},
			},
			want: false,
		},
		{
			name:    "empty category with include category filter fails",
			torrent: &qbt.Torrent{Category: ""},
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesWebhookSourceFilters(tt.torrent, tt.settings)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesRSSSourceFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		torrent  *qbt.Torrent
		settings *models.CrossSeedAutomationSettings
		want     bool
	}{
		{
			name:     "nil torrent returns false",
			torrent:  nil,
			settings: &models.CrossSeedAutomationSettings{},
			want:     false,
		},
		{
			name:     "nil settings returns false",
			torrent:  &qbt.Torrent{Category: "movies"},
			settings: nil,
			want:     false,
		},
		{
			name:     "empty filters match all torrents",
			torrent:  &qbt.Torrent{Category: "movies", Tags: "cross-seed"},
			settings: &models.CrossSeedAutomationSettings{},
			want:     true,
		},
		{
			name:    "exclude category skips matching torrent",
			torrent: &qbt.Torrent{Category: "AlphaRatio-Race"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceExcludeCategories: []string{"AlphaRatio-Race"},
			},
			want: false,
		},
		{
			name:    "exclude category allows non-matching torrent",
			torrent: &qbt.Torrent{Category: "AlphaRatio-LTS"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceExcludeCategories: []string{"AlphaRatio-Race"},
			},
			want: true,
		},
		{
			name:    "multiple exclude categories work",
			torrent: &qbt.Torrent{Category: "Mixed-Race"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceExcludeCategories: []string{"AlphaRatio-Race", "Mixed-Race", "TV-Race"},
			},
			want: false,
		},
		{
			name:    "exclude tag skips matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, temporary"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceExcludeTags: []string{"temporary"},
			},
			want: false,
		},
		{
			name:    "exclude tag allows non-matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, important"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceExcludeTags: []string{"temporary"},
			},
			want: true,
		},
		{
			name:    "include category requires match",
			torrent: &qbt.Torrent{Category: "TV-Race"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceCategories: []string{"TV-LTS", "Movies-LTS"},
			},
			want: false,
		},
		{
			name:    "include category allows matching torrent",
			torrent: &qbt.Torrent{Category: "TV-LTS"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceCategories: []string{"TV-LTS", "Movies-LTS"},
			},
			want: true,
		},
		{
			name:    "include tag requires at least one match",
			torrent: &qbt.Torrent{Tags: "important"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceTags: []string{"important", "priority"},
			},
			want: true,
		},
		{
			name:    "include tag rejects when no match",
			torrent: &qbt.Torrent{Tags: "random"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceTags: []string{"important", "priority"},
			},
			want: false,
		},
		{
			name:    "exclude takes precedence over include",
			torrent: &qbt.Torrent{Category: "TV-LTS"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceCategories:        []string{"TV-LTS", "Movies-LTS"},
				RSSSourceExcludeCategories: []string{"TV-LTS"},
			},
			want: false,
		},
		{
			name:    "empty category with exclude filter passes",
			torrent: &qbt.Torrent{Category: ""},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceExcludeCategories: []string{"AlphaRatio-Race"},
			},
			want: true,
		},
		{
			name:    "empty tags with include tag filter fails",
			torrent: &qbt.Torrent{Tags: ""},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceTags: []string{"important"},
			},
			want: false,
		},
		{
			name:    "tags are case-sensitive",
			torrent: &qbt.Torrent{Tags: "Important"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceTags: []string{"important"},
			},
			want: false,
		},
		{
			name:    "exclude tag takes precedence over include tag",
			torrent: &qbt.Torrent{Tags: "important, blocked"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceTags:        []string{"important"},
				RSSSourceExcludeTags: []string{"blocked"},
			},
			want: false,
		},
		{
			name:    "category and tag filters both apply - passes both",
			torrent: &qbt.Torrent{Category: "TV-LTS", Tags: "important"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceCategories: []string{"TV-LTS"},
				RSSSourceTags:       []string{"important"},
			},
			want: true,
		},
		{
			name:    "passes category filter but fails tag filter",
			torrent: &qbt.Torrent{Category: "TV-LTS", Tags: "random"},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceCategories: []string{"TV-LTS"},
				RSSSourceTags:       []string{"important"},
			},
			want: false,
		},
		{
			name:    "empty category with include category filter fails",
			torrent: &qbt.Torrent{Category: ""},
			settings: &models.CrossSeedAutomationSettings{
				RSSSourceCategories: []string{"TV-LTS"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesRSSSourceFilters(tt.torrent, tt.settings)
			assert.Equal(t, tt.want, got)
		})
	}
}

// mockCompletionFilterProvider is a test mock for CompletionFilterProvider interface.
type mockCompletionFilterProvider struct {
	categories        []string
	tags              []string
	excludeCategories []string
	excludeTags       []string
}

func (m *mockCompletionFilterProvider) GetCategories() []string        { return m.categories }
func (m *mockCompletionFilterProvider) GetTags() []string              { return m.tags }
func (m *mockCompletionFilterProvider) GetExcludeCategories() []string { return m.excludeCategories }
func (m *mockCompletionFilterProvider) GetExcludeTags() []string       { return m.excludeTags }

func TestMatchesCompletionFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		torrent  *qbt.Torrent
		settings models.CompletionFilterProvider
		want     bool
	}{
		{
			name:     "nil torrent returns false",
			torrent:  nil,
			settings: &mockCompletionFilterProvider{},
			want:     false,
		},
		{
			name:     "nil settings returns false",
			torrent:  &qbt.Torrent{Category: "movies"},
			settings: nil,
			want:     false,
		},
		{
			name:     "empty filters match all torrents",
			torrent:  &qbt.Torrent{Category: "movies", Tags: "cross-seed"},
			settings: &mockCompletionFilterProvider{},
			want:     true,
		},
		{
			name:    "exclude category skips matching torrent",
			torrent: &qbt.Torrent{Category: "AlphaRatio-Race"},
			settings: &mockCompletionFilterProvider{
				excludeCategories: []string{"AlphaRatio-Race"},
			},
			want: false,
		},
		{
			name:    "exclude category allows non-matching torrent",
			torrent: &qbt.Torrent{Category: "AlphaRatio-LTS"},
			settings: &mockCompletionFilterProvider{
				excludeCategories: []string{"AlphaRatio-Race"},
			},
			want: true,
		},
		{
			name:    "include category requires match",
			torrent: &qbt.Torrent{Category: "TV-Race"},
			settings: &mockCompletionFilterProvider{
				categories: []string{"TV-LTS", "Movies-LTS"},
			},
			want: false,
		},
		{
			name:    "include category allows matching torrent",
			torrent: &qbt.Torrent{Category: "TV-LTS"},
			settings: &mockCompletionFilterProvider{
				categories: []string{"TV-LTS", "Movies-LTS"},
			},
			want: true,
		},
		{
			name:    "exclude tag skips matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, temporary"},
			settings: &mockCompletionFilterProvider{
				excludeTags: []string{"temporary"},
			},
			want: false,
		},
		{
			name:    "include tag requires at least one match",
			torrent: &qbt.Torrent{Tags: "important"},
			settings: &mockCompletionFilterProvider{
				tags: []string{"important", "priority"},
			},
			want: true,
		},
		{
			name:    "include tag rejects when no match",
			torrent: &qbt.Torrent{Tags: "random"},
			settings: &mockCompletionFilterProvider{
				tags: []string{"important", "priority"},
			},
			want: false,
		},
		{
			name:    "exclude takes precedence over include",
			torrent: &qbt.Torrent{Category: "TV-LTS"},
			settings: &mockCompletionFilterProvider{
				categories:        []string{"TV-LTS", "Movies-LTS"},
				excludeCategories: []string{"TV-LTS"},
			},
			want: false,
		},
		{
			name:    "category and tag filters both apply - passes both",
			torrent: &qbt.Torrent{Category: "TV-LTS", Tags: "important"},
			settings: &mockCompletionFilterProvider{
				categories: []string{"TV-LTS"},
				tags:       []string{"important"},
			},
			want: true,
		},
		{
			name:    "passes category filter but fails tag filter",
			torrent: &qbt.Torrent{Category: "TV-LTS", Tags: "random"},
			settings: &mockCompletionFilterProvider{
				categories: []string{"TV-LTS"},
				tags:       []string{"important"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesCompletionFilters(tt.torrent, tt.settings)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMatchesSourceFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		torrent *qbt.Torrent
		req     *FindCandidatesRequest
		want    bool
	}{
		{
			name:    "nil torrent returns true (no filtering)",
			torrent: nil,
			req:     &FindCandidatesRequest{},
			want:    true,
		},
		{
			name:    "nil request returns true (no filtering)",
			torrent: &qbt.Torrent{Category: "movies"},
			req:     nil,
			want:    true,
		},
		{
			name:    "empty filters match all torrents",
			torrent: &qbt.Torrent{Category: "movies", Tags: "cross-seed"},
			req:     &FindCandidatesRequest{},
			want:    true,
		},
		{
			name:    "exclude category skips matching torrent",
			torrent: &qbt.Torrent{Category: "AlphaRatio-Race"},
			req: &FindCandidatesRequest{
				SourceFilterExcludeCategories: []string{"AlphaRatio-Race"},
			},
			want: false,
		},
		{
			name:    "exclude category allows non-matching torrent",
			torrent: &qbt.Torrent{Category: "AlphaRatio-LTS"},
			req: &FindCandidatesRequest{
				SourceFilterExcludeCategories: []string{"AlphaRatio-Race"},
			},
			want: true,
		},
		{
			name:    "include category requires match",
			torrent: &qbt.Torrent{Category: "TV-Race"},
			req: &FindCandidatesRequest{
				SourceFilterCategories: []string{"TV-LTS", "Movies-LTS"},
			},
			want: false,
		},
		{
			name:    "include category allows matching torrent",
			torrent: &qbt.Torrent{Category: "TV-LTS"},
			req: &FindCandidatesRequest{
				SourceFilterCategories: []string{"TV-LTS", "Movies-LTS"},
			},
			want: true,
		},
		{
			name:    "exclude tag skips matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, temporary"},
			req: &FindCandidatesRequest{
				SourceFilterExcludeTags: []string{"temporary"},
			},
			want: false,
		},
		{
			name:    "include tag requires at least one match",
			torrent: &qbt.Torrent{Tags: "important"},
			req: &FindCandidatesRequest{
				SourceFilterTags: []string{"important", "priority"},
			},
			want: true,
		},
		{
			name:    "include tag rejects when no match",
			torrent: &qbt.Torrent{Tags: "random"},
			req: &FindCandidatesRequest{
				SourceFilterTags: []string{"important", "priority"},
			},
			want: false,
		},
		{
			name:    "exclude takes precedence over include",
			torrent: &qbt.Torrent{Category: "TV-LTS"},
			req: &FindCandidatesRequest{
				SourceFilterCategories:        []string{"TV-LTS", "Movies-LTS"},
				SourceFilterExcludeCategories: []string{"TV-LTS"},
			},
			want: false,
		},
		{
			name:    "category and tag filters both apply - passes both",
			torrent: &qbt.Torrent{Category: "TV-LTS", Tags: "important"},
			req: &FindCandidatesRequest{
				SourceFilterCategories: []string{"TV-LTS"},
				SourceFilterTags:       []string{"important"},
			},
			want: true,
		},
		{
			name:    "passes category filter but fails tag filter",
			torrent: &qbt.Torrent{Category: "TV-LTS", Tags: "random"},
			req: &FindCandidatesRequest{
				SourceFilterCategories: []string{"TV-LTS"},
				SourceFilterTags:       []string{"important"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSourceFilters(tt.torrent, tt.req)
			assert.Equal(t, tt.want, got)
		})
	}
}

// TestProcessAutomationCandidate_RespectsRSSSourceFilters verifies that RSS automation
// passes RSS source filters through to the CrossSeedRequest. This is an integration test
// that catches the bug where filters worked in isolation but weren't passed through the flow.
func TestProcessAutomationCandidate_RespectsRSSSourceFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1

	tests := []struct {
		name                    string
		settings                *models.CrossSeedAutomationSettings
		expectCategories        []string
		expectTags              []string
		expectExcludeCategories []string
		expectExcludeTags       []string
	}{
		{
			name: "RSS include categories passed through",
			settings: &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:  []int{instanceID},
				RSSSourceCategories: []string{"movies-LTS", "tv-LTS"},
			},
			expectCategories:        []string{"movies-LTS", "tv-LTS"},
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name: "RSS include tags passed through",
			settings: &models.CrossSeedAutomationSettings{
				TargetInstanceIDs: []int{instanceID},
				RSSSourceTags:     []string{"cross-seed", "priority"},
			},
			expectCategories:        nil,
			expectTags:              []string{"cross-seed", "priority"},
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name: "RSS exclude categories passed through",
			settings: &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:          []int{instanceID},
				RSSSourceExcludeCategories: []string{"movies-Race", "tv-Race"},
			},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: []string{"movies-Race", "tv-Race"},
			expectExcludeTags:       nil,
		},
		{
			name: "RSS exclude tags passed through",
			settings: &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:      []int{instanceID},
				RSSSourceExcludeTags: []string{"no-cross-seed", "temporary"},
			},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       []string{"no-cross-seed", "temporary"},
		},
		{
			name: "all RSS filters passed through together",
			settings: &models.CrossSeedAutomationSettings{
				TargetInstanceIDs:          []int{instanceID},
				RSSSourceCategories:        []string{"movies-LTS"},
				RSSSourceTags:              []string{"important"},
				RSSSourceExcludeCategories: []string{"movies-Race"},
				RSSSourceExcludeTags:       []string{"temporary"},
			},
			expectCategories:        []string{"movies-LTS"},
			expectTags:              []string{"important"},
			expectExcludeCategories: []string{"movies-Race"},
			expectExcludeTags:       []string{"temporary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Set up sync manager with a torrent that matches
			sync := &rssFilterTestSyncManager{
				torrents: map[int][]qbt.Torrent{
					instanceID: {
						{
							Hash:     "testhash",
							Name:     "Test.Movie.2025.1080p.BluRay-GROUP",
							Progress: 1.0,
							Category: "movies-LTS",
						},
					},
				},
				files: map[int]map[string]qbt.TorrentFiles{
					instanceID: {
						"testhash": {{Name: "Test.Movie.2025.1080p.BluRay-GROUP.mkv", Size: 1024}},
					},
				},
				props: map[int]map[string]*qbt.TorrentProperties{
					instanceID: {
						"testhash": {SavePath: "/downloads"},
					},
				},
			}

			service := &Service{
				instanceStore: &fakeInstanceStore{
					instances: map[int]*models.Instance{
						instanceID: {ID: instanceID, Name: "TestInstance"},
					},
				},
				syncManager:      sync,
				releaseCache:     NewReleaseCache(),
				stringNormalizer: stringutils.NewDefaultNormalizer(),
				torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
					return []byte("torrent"), nil
				},
			}

			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{
					Success: true,
					Results: []InstanceCrossSeedResult{
						{InstanceID: instanceID, InstanceName: "TestInstance", Success: true, Status: "added"},
					},
				}, nil
			}

			run := &models.CrossSeedRun{}
			result := jackett.SearchResult{
				Indexer:     "TestIndexer",
				IndexerID:   1,
				Title:       "Test.Movie.2025.1080p.BluRay-GROUP",
				DownloadURL: "https://example.invalid/download.torrent",
				GUID:        "guid-1",
				Size:        1024,
			}

			_, _, err := service.processAutomationCandidate(ctx, run, tt.settings, nil, result, AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
			require.NoError(t, err)
			require.NotNil(t, captured, "CrossSeedRequest should have been captured")

			// Verify RSS source filters were passed through
			assert.Equal(t, tt.expectCategories, captured.SourceFilterCategories, "SourceFilterCategories mismatch")
			assert.Equal(t, tt.expectTags, captured.SourceFilterTags, "SourceFilterTags mismatch")
			assert.Equal(t, tt.expectExcludeCategories, captured.SourceFilterExcludeCategories, "SourceFilterExcludeCategories mismatch")
			assert.Equal(t, tt.expectExcludeTags, captured.SourceFilterExcludeTags, "SourceFilterExcludeTags mismatch")
		})
	}
}

// rssFilterTestSyncManager implements qbittorrentSync for RSS filter tests
type rssFilterTestSyncManager struct {
	torrents map[int][]qbt.Torrent
	files    map[int]map[string]qbt.TorrentFiles
	props    map[int]map[string]*qbt.TorrentProperties
}

func (m *rssFilterTestSyncManager) GetTorrents(_ context.Context, instanceID int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	list := m.torrents[instanceID]
	if list == nil {
		return nil, fmt.Errorf("instance %d has no torrents", instanceID)
	}
	copied := make([]qbt.Torrent, len(list))
	copy(copied, list)
	return copied, nil
}

func (m *rssFilterTestSyncManager) GetTorrentFilesBatch(_ context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	if instFiles, ok := m.files[instanceID]; ok {
		for _, h := range hashes {
			if files, ok := instFiles[strings.ToLower(h)]; ok {
				cp := make(qbt.TorrentFiles, len(files))
				copy(cp, files)
				result[normalizeHash(h)] = cp
			}
		}
	}
	return result, nil
}

func (m *rssFilterTestSyncManager) HasTorrentByAnyHash(_ context.Context, _ int, _ []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *rssFilterTestSyncManager) GetTorrentProperties(_ context.Context, instanceID int, hash string) (*qbt.TorrentProperties, error) {
	if instProps, ok := m.props[instanceID]; ok {
		if props, ok := instProps[strings.ToLower(hash)]; ok {
			cp := *props
			return &cp, nil
		}
	}
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (m *rssFilterTestSyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (m *rssFilterTestSyncManager) AddTorrent(context.Context, int, []byte, map[string]string) error {
	return nil
}

func (m *rssFilterTestSyncManager) BulkAction(context.Context, int, []string, string) error {
	return nil
}

func (m *rssFilterTestSyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (m *rssFilterTestSyncManager) GetCachedInstanceTorrents(_ context.Context, instanceID int) ([]internalqb.CrossInstanceTorrentView, error) {
	if list, ok := m.torrents[instanceID]; ok {
		views := make([]internalqb.CrossInstanceTorrentView, len(list))
		for i, t := range list {
			views[i] = internalqb.CrossInstanceTorrentView{
				TorrentView: internalqb.TorrentView{
					Torrent: t,
				},
				InstanceID: instanceID,
			}
		}
		return views, nil
	}
	return nil, nil
}

func (m *rssFilterTestSyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (m *rssFilterTestSyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (m *rssFilterTestSyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (m *rssFilterTestSyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (m *rssFilterTestSyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (m *rssFilterTestSyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (m *rssFilterTestSyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

// TestExecuteCrossSeedSearchAttempt_RespectsCompletionFilters verifies that completion source
// filters are passed through to the CrossSeedRequest. This tests the path from
// executeCompletionSearch → executeCrossSeedSearchAttempt → CrossSeed where completion settings
// filters should be propagated to FindCandidates.
func TestExecuteCrossSeedSearchAttempt_RespectsCompletionFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1

	// Create a minimal valid torrent file for encoding
	createTorrentData := func() []byte {
		info := metainfo.Info{
			Name:        "Test.Movie.2025.1080p.BluRay-GROUP",
			PieceLength: 262144,
			Pieces:      make([]byte, 20), // Minimal piece hash
			Length:      1024,
		}
		mi := metainfo.MetaInfo{
			InfoBytes: bencode.MustMarshal(info),
		}
		var buf bytes.Buffer
		if err := mi.Write(&buf); err != nil {
			t.Fatalf("failed to create torrent data: %v", err)
		}
		return buf.Bytes()
	}

	tests := []struct {
		name                    string
		opts                    SearchRunOptions
		expectCategories        []string
		expectTags              []string
		expectExcludeCategories []string
		expectExcludeTags       []string
	}{
		{
			name: "completion include categories passed through",
			opts: SearchRunOptions{
				InstanceID: instanceID,
				Categories: []string{"movies-LTS", "tv-LTS"},
			},
			expectCategories:        []string{"movies-LTS", "tv-LTS"},
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name: "completion include tags passed through",
			opts: SearchRunOptions{
				InstanceID: instanceID,
				Tags:       []string{"cross-seed", "priority"},
			},
			expectCategories:        nil,
			expectTags:              []string{"cross-seed", "priority"},
			expectExcludeCategories: nil,
			expectExcludeTags:       nil,
		},
		{
			name: "completion exclude categories passed through",
			opts: SearchRunOptions{
				InstanceID:        instanceID,
				ExcludeCategories: []string{"movies-Race", "tv-Race"},
			},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: []string{"movies-Race", "tv-Race"},
			expectExcludeTags:       nil,
		},
		{
			name: "completion exclude tags passed through",
			opts: SearchRunOptions{
				InstanceID:  instanceID,
				ExcludeTags: []string{"no-cross-seed", "temporary"},
			},
			expectCategories:        nil,
			expectTags:              nil,
			expectExcludeCategories: nil,
			expectExcludeTags:       []string{"no-cross-seed", "temporary"},
		},
		{
			name: "all completion filters passed through together",
			opts: SearchRunOptions{
				InstanceID:        instanceID,
				Categories:        []string{"movies-LTS"},
				Tags:              []string{"important"},
				ExcludeCategories: []string{"movies-Race"},
				ExcludeTags:       []string{"temporary"},
			},
			expectCategories:        []string{"movies-LTS"},
			expectTags:              []string{"important"},
			expectExcludeCategories: []string{"movies-Race"},
			expectExcludeTags:       []string{"temporary"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			torrentData := createTorrentData()

			service := &Service{
				torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
					return torrentData, nil
				},
			}

			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{
					Success: true,
					Results: []InstanceCrossSeedResult{
						{InstanceID: instanceID, InstanceName: "TestInstance", Success: true, Status: "added"},
					},
				}, nil
			}

			state := &searchRunState{opts: tt.opts}
			torrent := &qbt.Torrent{
				Hash:     "testhash",
				Name:     "Test.Movie.2025.1080p.BluRay-GROUP",
				Progress: 1.0,
				Category: "movies-LTS",
			}
			match := TorrentSearchResult{
				Indexer:     "TestIndexer",
				IndexerID:   1,
				Title:       "Test.Movie.2025.1080p.BluRay-GROUP",
				DownloadURL: "https://example.invalid/download.torrent",
				GUID:        "guid-1",
				Size:        1024,
			}

			_, err := service.executeCrossSeedSearchAttempt(ctx, state, torrent, match, time.Now().UTC())
			require.NoError(t, err)
			require.NotNil(t, captured, "CrossSeedRequest should have been captured")

			// Verify completion source filters were passed through
			assert.Equal(t, tt.expectCategories, captured.SourceFilterCategories, "SourceFilterCategories mismatch")
			assert.Equal(t, tt.expectTags, captured.SourceFilterTags, "SourceFilterTags mismatch")
			assert.Equal(t, tt.expectExcludeCategories, captured.SourceFilterExcludeCategories, "SourceFilterExcludeCategories mismatch")
			assert.Equal(t, tt.expectExcludeTags, captured.SourceFilterExcludeTags, "SourceFilterExcludeTags mismatch")
		})
	}
}

func TestMatchesSearchFilters(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		torrent *qbt.Torrent
		opts    SearchRunOptions
		want    bool
	}{
		{
			name:    "nil torrent returns false",
			torrent: nil,
			opts:    SearchRunOptions{},
			want:    false,
		},
		{
			name:    "empty filters match all torrents",
			torrent: &qbt.Torrent{Category: "movies", Tags: "cross-seed"},
			opts:    SearchRunOptions{},
			want:    true,
		},
		{
			name:    "exclude category skips matching torrent",
			torrent: &qbt.Torrent{Category: "movies-Race"},
			opts: SearchRunOptions{
				ExcludeCategories: []string{"movies-Race"},
			},
			want: false,
		},
		{
			name:    "exclude category allows non-matching torrent",
			torrent: &qbt.Torrent{Category: "movies-LTS"},
			opts: SearchRunOptions{
				ExcludeCategories: []string{"movies-Race"},
			},
			want: true,
		},
		{
			name:    "include category requires match",
			torrent: &qbt.Torrent{Category: "tv-Race"},
			opts: SearchRunOptions{
				Categories: []string{"movies-LTS", "tv-LTS"},
			},
			want: false,
		},
		{
			name:    "include category allows matching torrent",
			torrent: &qbt.Torrent{Category: "movies-LTS"},
			opts: SearchRunOptions{
				Categories: []string{"movies-LTS", "tv-LTS"},
			},
			want: true,
		},
		{
			name:    "exclude tag skips matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, temporary"},
			opts: SearchRunOptions{
				ExcludeTags: []string{"temporary"},
			},
			want: false,
		},
		{
			name:    "exclude tag allows non-matching torrent",
			torrent: &qbt.Torrent{Tags: "cross-seed, important"},
			opts: SearchRunOptions{
				ExcludeTags: []string{"temporary"},
			},
			want: true,
		},
		{
			name:    "include tag requires at least one match",
			torrent: &qbt.Torrent{Tags: "important"},
			opts: SearchRunOptions{
				Tags: []string{"important", "priority"},
			},
			want: true,
		},
		{
			name:    "include tag rejects when no match",
			torrent: &qbt.Torrent{Tags: "random"},
			opts: SearchRunOptions{
				Tags: []string{"important", "priority"},
			},
			want: false,
		},
		{
			name:    "exclude takes precedence over include",
			torrent: &qbt.Torrent{Category: "movies-LTS"},
			opts: SearchRunOptions{
				Categories:        []string{"movies-LTS", "tv-LTS"},
				ExcludeCategories: []string{"movies-LTS"},
			},
			want: false,
		},
		{
			name:    "category and tag filters both apply - passes both",
			torrent: &qbt.Torrent{Category: "movies-LTS", Tags: "important"},
			opts: SearchRunOptions{
				Categories: []string{"movies-LTS"},
				Tags:       []string{"important"},
			},
			want: true,
		},
		{
			name:    "passes category filter but fails tag filter",
			torrent: &qbt.Torrent{Category: "movies-LTS", Tags: "random"},
			opts: SearchRunOptions{
				Categories: []string{"movies-LTS"},
				Tags:       []string{"important"},
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := matchesSearchFilters(tt.torrent, tt.opts)
			assert.Equal(t, tt.want, got)
		})
	}
}
