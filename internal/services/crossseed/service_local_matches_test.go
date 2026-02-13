// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
	"github.com/stretchr/testify/require"
)

func TestDetermineLocalMatchType_DoesNotTreatRootlessStorageDirAsCrossSeed(t *testing.T) {
	svc := &Service{
		releaseCache: NewReleaseCache(),
	}

	source := &qbt.Torrent{
		Name:        "Love.Island.Australia.S07E22.1080p.WEB.h264-EDITH",
		SavePath:    "/downloads",
		ContentPath: "/downloads",
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Name:        "WWE.NXT.2025.12.02.1080p.WEB.h264-KYR",
				SavePath:    "/downloads",
				ContentPath: "/downloads",
			},
		},
	}

	// nil matchCtx since we're testing the ambiguous case without file-level checking;
	// with no file overlap data, it should NOT match.
	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		nil,
	)

	require.Equal(t, "", matchType)
}

func TestDetermineLocalMatchType_ContentPathMatchWhenSpecific(t *testing.T) {
	svc := &Service{
		releaseCache: NewReleaseCache(),
	}

	source := &qbt.Torrent{
		Name:        "Some.Source.Release.1080p.WEB.h264-GROUP",
		SavePath:    "/downloads",
		ContentPath: "/downloads/Some.Source.Release.1080p.WEB.h264-GROUP.mkv",
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Name:        "Different.Name.Same.Data.1080p.WEB.h264-OTHER",
				SavePath:    "/downloads",
				ContentPath: "/downloads/Some.Source.Release.1080p.WEB.h264-GROUP.mkv",
			},
		},
	}

	// nil matchCtx is fine here since content_path != save_path (non-ambiguous case)
	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		nil,
	)

	require.Equal(t, matchTypeContentPath, matchType)
}

// localMatchSyncManager is a minimal fake for testing file overlap logic.
type localMatchSyncManager struct {
	files        map[string]qbt.TorrentFiles
	errorOnFetch error // If set, GetTorrentFilesBatch returns this error
}

//nolint:gocritic // Interface requires value type for TorrentFilterOptions
func (m *localMatchSyncManager) GetTorrents(_ context.Context, _ int, _ qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	return nil, nil
}

func (m *localMatchSyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	if m.errorOnFetch != nil {
		return nil, m.errorOnFetch
	}
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		normalized := normalizeHash(h)
		if files, ok := m.files[normalized]; ok {
			result[normalized] = files
		} else if files, ok := m.files[strings.ToLower(h)]; ok {
			result[normalized] = files
		}
	}
	return result, nil
}

func (*localMatchSyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (m *localMatchSyncManager) HasTorrentByAnyHash(_ context.Context, _ int, _ []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *localMatchSyncManager) GetTorrentProperties(_ context.Context, _ int, _ string) (*qbt.TorrentProperties, error) {
	return nil, nil
}

func (m *localMatchSyncManager) GetAppPreferences(_ context.Context, _ int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{}, nil
}

func (m *localMatchSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, _ map[string]string) error {
	return nil
}

func (m *localMatchSyncManager) BulkAction(_ context.Context, _ int, _ []string, _ string) error {
	return nil
}

func (m *localMatchSyncManager) GetCachedInstanceTorrents(_ context.Context, _ int) ([]qbittorrent.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (m *localMatchSyncManager) ExtractDomainFromURL(_ string) string {
	return ""
}

func (m *localMatchSyncManager) GetQBittorrentSyncManager(_ context.Context, _ int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (m *localMatchSyncManager) RenameTorrent(_ context.Context, _ int, _, _ string) error {
	return nil
}

func (m *localMatchSyncManager) RenameTorrentFile(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (m *localMatchSyncManager) RenameTorrentFolder(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (m *localMatchSyncManager) SetTags(_ context.Context, _ int, _ []string, _ string) error {
	return nil
}

func (m *localMatchSyncManager) GetCategories(_ context.Context, _ int) (map[string]qbt.Category, error) {
	return nil, nil
}

func (m *localMatchSyncManager) CreateCategory(_ context.Context, _ int, _, _ string) error {
	return nil
}

func TestDetermineLocalMatchType_AmbiguousDir_DifferentFiles_NoMatch(t *testing.T) {
	// When content_path == save_path (ambiguous directory) and the file lists
	// don't overlap, determineLocalMatchType should NOT return content_path.
	sourceHash := "aaaa1111aaaa1111aaaa1111aaaa1111aaaa1111"
	candidateHash := "bbbb2222bbbb2222bbbb2222bbbb2222bbbb2222"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(sourceHash): {
				{Name: "Movie.A.2023.1080p.WEB.mkv", Size: 1000000000},
			},
			normalizeHash(candidateHash): {
				{Name: "Movie.B.2024.720p.WEB.mkv", Size: 800000000},
			},
		},
	}

	svc := &Service{
		releaseCache: NewReleaseCache(),
		syncManager:  mockSync,
	}

	source := &qbt.Torrent{
		Hash:        sourceHash,
		Name:        "Movie.A.2023.1080p.WEB-GROUP",
		SavePath:    "/downloads",
		ContentPath: "/downloads", // Ambiguous: content_path == save_path
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Hash:        candidateHash,
				Name:        "Movie.B.2024.720p.WEB-OTHER",
				SavePath:    "/downloads",
				ContentPath: "/downloads", // Also ambiguous
			},
		},
		InstanceID: 1,
	}

	// Use lazy-loading matchCtx (files fetched on demand)
	matchCtx := &localMatchContext{
		ctx:              context.Background(),
		svc:              svc,
		sourceInstanceID: 1,
		sourceHash:       sourceHash,
	}

	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		matchCtx,
	)

	require.Equal(t, "", matchType, "Different file lists should not match as content_path")
}

func TestDetermineLocalMatchType_AmbiguousDir_OverlappingFiles_Match(t *testing.T) {
	// When content_path == save_path (ambiguous directory) but files overlap >= 90%,
	// determineLocalMatchType SHOULD return content_path.
	sourceHash := "cccc3333cccc3333cccc3333cccc3333cccc3333"
	candidateHash := "dddd4444dddd4444dddd4444dddd4444dddd4444"

	// Both torrents have the same files (100% overlap)
	sharedFiles := qbt.TorrentFiles{
		{Name: "TV.Show.S01E01.1080p.WEB.mkv", Size: 500000000},
		{Name: "TV.Show.S01E02.1080p.WEB.mkv", Size: 500000000},
	}

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(sourceHash):    sharedFiles,
			normalizeHash(candidateHash): sharedFiles,
		},
	}

	svc := &Service{
		releaseCache: NewReleaseCache(),
		syncManager:  mockSync,
	}

	source := &qbt.Torrent{
		Hash:        sourceHash,
		Name:        "TV.Show.S01.1080p.WEB-PACK",
		SavePath:    "/downloads",
		ContentPath: "/downloads", // Ambiguous: content_path == save_path
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Hash:        candidateHash,
				Name:        "TV.Show.S01.1080p.WEB-OTHER",
				SavePath:    "/downloads",
				ContentPath: "/downloads", // Also ambiguous
			},
		},
		InstanceID: 1,
	}

	// Use lazy-loading matchCtx (files fetched on demand)
	matchCtx := &localMatchContext{
		ctx:              context.Background(),
		svc:              svc,
		sourceInstanceID: 1,
		sourceHash:       sourceHash,
	}

	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		matchCtx,
	)

	require.Equal(t, matchTypeContentPath, matchType, "Overlapping file lists should match as content_path")
}

func TestDetermineLocalMatchType_AmbiguousDir_PartialOverlap_BelowThreshold(t *testing.T) {
	// When file overlap is below 90% threshold, should NOT match.
	sourceHash := "eeee5555eeee5555eeee5555eeee5555eeee5555"
	candidateHash := "ffff6666ffff6666ffff6666ffff6666ffff6666"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(sourceHash): {
				{Name: "shared.mkv", Size: 100000000},
				{Name: "source-only.mkv", Size: 900000000},
			},
			normalizeHash(candidateHash): {
				{Name: "shared.mkv", Size: 100000000},
				{Name: "candidate-only.mkv", Size: 900000000},
			},
		},
	}

	svc := &Service{
		releaseCache: NewReleaseCache(),
		syncManager:  mockSync,
	}

	source := &qbt.Torrent{
		Hash:        sourceHash,
		Name:        "Some.Release.2023-GROUP",
		SavePath:    "/downloads",
		ContentPath: "/downloads",
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Hash:        candidateHash,
				Name:        "Other.Release.2023-OTHER",
				SavePath:    "/downloads",
				ContentPath: "/downloads",
			},
		},
		InstanceID: 1,
	}

	// Use lazy-loading matchCtx (files fetched on demand)
	matchCtx := &localMatchContext{
		ctx:              context.Background(),
		svc:              svc,
		sourceInstanceID: 1,
		sourceHash:       sourceHash,
	}

	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		matchCtx,
	)

	// 10% overlap is below 90% threshold
	require.Equal(t, "", matchType, "10% file overlap should not match as content_path")
}

func TestCandidateSharesSourceFiles_ExactMatch(t *testing.T) {
	candHash := "2222222222222222222222222222222222222222"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(candHash): {
				{Name: "file1.mkv", Size: 1000},
				{Name: "file2.mkv", Size: 2000},
			},
		},
	}

	svc := &Service{syncManager: mockSync}

	// Precomputed source keys
	srcFileKeys := map[string]int64{
		"file1.mkv|1000": 1000,
		"file2.mkv|2000": 2000,
	}
	srcTotalBytes := int64(3000)

	shares, _, err := svc.candidateSharesSourceFiles(context.Background(), srcFileKeys, srcTotalBytes, 1, candHash)
	require.NoError(t, err)
	require.True(t, shares, "Identical file lists should share files")
}

func TestCandidateSharesSourceFiles_NoOverlap(t *testing.T) {
	candHash := "4444444444444444444444444444444444444444"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(candHash): {
				{Name: "fileB.mkv", Size: 1000},
			},
		},
	}

	svc := &Service{syncManager: mockSync}

	// Precomputed source keys (different files)
	srcFileKeys := map[string]int64{
		"filea.mkv|1000": 1000,
	}
	srcTotalBytes := int64(1000)

	shares, _, err := svc.candidateSharesSourceFiles(context.Background(), srcFileKeys, srcTotalBytes, 1, candHash)
	require.NoError(t, err)
	require.False(t, shares, "Non-overlapping file lists should not share files")
}

func TestCandidateSharesSourceFiles_EpisodeInPack(t *testing.T) {
	// Test episode-in-pack scenario: candidate is a single episode contained in source pack
	candHash := "6666666666666666666666666666666666666666"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(candHash): {
				{Name: "Show.S01E01.mkv", Size: 500000000}, // Single episode
			},
		},
	}

	svc := &Service{syncManager: mockSync}

	// Precomputed source keys (full season pack)
	srcFileKeys := map[string]int64{
		"show.s01e01.mkv|500000000": 500000000,
		"show.s01e02.mkv|500000000": 500000000,
		"show.s01e03.mkv|500000000": 500000000,
	}
	srcTotalBytes := int64(1500000000)

	shares, _, err := svc.candidateSharesSourceFiles(context.Background(), srcFileKeys, srcTotalBytes, 1, candHash)
	require.NoError(t, err)
	require.True(t, shares, "Single episode contained in pack should share files (100% of smaller)")
}

func TestGetSourceFiles_EmptyFileListReturnsError(t *testing.T) {
	// When qBittorrent returns an empty file list for a valid torrent,
	// getSourceFiles should return an error to avoid silent false-negatives.
	sourceHash := "7777777777777777777777777777777777777777"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(sourceHash): {}, // Empty file list
		},
	}

	svc := &Service{
		releaseCache: NewReleaseCache(),
		syncManager:  mockSync,
	}

	matchCtx := &localMatchContext{
		ctx:              context.Background(),
		svc:              svc,
		sourceInstanceID: 1,
		sourceHash:       sourceHash,
	}

	fileKeys, totalBytes, err := matchCtx.getSourceFiles()
	require.Error(t, err, "Empty source file list should return an error")
	require.Contains(t, err.Error(), "empty file list")
	require.Nil(t, fileKeys)
	require.Zero(t, totalBytes)
	require.Equal(t, err, matchCtx.sourceFilesErr, "Error should be stored on matchCtx.sourceFilesErr")
}

func TestDetermineLocalMatchType_EmptyCandidateFiles_StoresError(t *testing.T) {
	// When candidate file list is empty, no content_path match should occur and error should be stored.
	sourceHash := "8888888888888888888888888888888888888888"
	candidateHash := "9999999999999999999999999999999999999999"

	mockSync := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			// Source has files, but candidate returns empty
			normalizeHash(sourceHash): {
				{Name: "Movie.2023.1080p.mkv", Size: 1000000000},
			},
			normalizeHash(candidateHash): {}, // Empty file list
		},
	}

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		syncManager:      mockSync,
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := &qbt.Torrent{
		Hash:        sourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    "/downloads",
		ContentPath: "/downloads", // Ambiguous
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Hash:        candidateHash,
				Name:        "Movie.2023.1080p.WEB-OTHER",
				SavePath:    "/downloads",
				ContentPath: "/downloads", // Also ambiguous
			},
		},
		InstanceID: 1,
	}

	matchCtx := &localMatchContext{
		ctx:              context.Background(),
		svc:              svc,
		sourceInstanceID: 1,
		sourceHash:       sourceHash,
	}

	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		matchCtx,
	)

	// Empty candidate file list now returns an error from candidateSharesSourceFiles,
	// so no content_path match occurs and error is stored for strict mode.
	require.NotEqual(t, matchTypeContentPath, matchType, "Should not match as content_path when candidate files are empty")
	require.Error(t, matchCtx.candidateFilesErr, "Empty candidate files should store error on matchCtx.candidateFilesErr")
	require.Contains(t, matchCtx.candidateFilesErr.Error(), "empty file list")
}

func TestDetermineLocalMatchType_CandidateFetchError_StoresError(t *testing.T) {
	// When candidate file fetch fails, the error should be stored on matchCtx
	// so FindLocalMatches can bubble it up to the UI.
	sourceHash := "aaaa0000aaaa0000aaaa0000aaaa0000aaaa0000"
	candidateHash := "bbbb1111bbbb1111bbbb1111bbbb1111bbbb1111"
	fetchErr := errors.New("qbittorrent API unavailable")

	// First mock returns source files successfully
	sourceMock := &localMatchSyncManager{
		files: map[string]qbt.TorrentFiles{
			normalizeHash(sourceHash): {
				{Name: "Movie.2023.1080p.mkv", Size: 1000000000},
			},
		},
	}

	svc := &Service{
		releaseCache:     NewReleaseCache(),
		syncManager:      sourceMock,
		stringNormalizer: stringutils.NewDefaultNormalizer(),
	}

	source := &qbt.Torrent{
		Hash:        sourceHash,
		Name:        "Movie.2023.1080p.WEB-GROUP",
		SavePath:    "/downloads",
		ContentPath: "/downloads", // Ambiguous
	}

	candidate := &qbittorrent.CrossInstanceTorrentView{
		TorrentView: qbittorrent.TorrentView{
			Torrent: qbt.Torrent{
				Hash:        candidateHash,
				Name:        "Movie.2023.1080p.WEB-OTHER",
				SavePath:    "/downloads",
				ContentPath: "/downloads", // Also ambiguous
			},
		},
		InstanceID: 1,
	}

	// Fetch source files first
	matchCtx := &localMatchContext{
		ctx:              context.Background(),
		svc:              svc,
		sourceInstanceID: 1,
		sourceHash:       sourceHash,
	}
	_, _, err := matchCtx.getSourceFiles()
	require.NoError(t, err)

	// Now switch to a mock that returns errors for candidate fetches
	svc.syncManager = &localMatchSyncManager{
		files:        map[string]qbt.TorrentFiles{},
		errorOnFetch: fetchErr,
	}

	matchType := svc.determineLocalMatchType(
		source,
		svc.releaseCache.Parse(source.Name),
		candidate,
		strings.ToLower(normalizePath(source.ContentPath)),
		matchCtx,
	)

	// Should not match as content_path since candidate fetch failed
	require.NotEqual(t, matchTypeContentPath, matchType)
	// Error should be stored on matchCtx.candidateFilesErr for bubbling
	require.Error(t, matchCtx.candidateFilesErr, "Candidate fetch error should be stored on matchCtx.candidateFilesErr")
	require.ErrorIs(t, matchCtx.candidateFilesErr, fetchErr)
}
