// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

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

func (m *localMatchSyncManager) AddTorrent(_ context.Context, _ int, _ []byte, _ map[string]string) (*qbt.TorrentAddResponse, error) {
	return nil, nil
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

	err := matchCtx.getSourceFiles()
	require.Error(t, err, "Empty source file list should return an error")
	require.Contains(t, err.Error(), "empty file list")
	require.Equal(t, err, matchCtx.sourceFilesErr, "Error should be stored on matchCtx.sourceFilesErr")
}
