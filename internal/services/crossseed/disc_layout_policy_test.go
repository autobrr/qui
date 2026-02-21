// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"maps"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

// discPolicySyncManager is a mock sync manager for disc layout policy tests.
// It records AddTorrent options and BulkAction calls to verify policy enforcement.
type discPolicySyncManager struct {
	files          map[string]qbt.TorrentFiles
	props          map[string]*qbt.TorrentProperties
	addedOptions   map[string]string
	bulkActions    []string // records "action:hash" for each BulkAction call
	matchedTorrent *qbt.Torrent
}

func (m *discPolicySyncManager) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(filter.Hashes) > 0 {
		torrents := make([]qbt.Torrent, 0, len(filter.Hashes))
		for _, hash := range filter.Hashes {
			if m.matchedTorrent != nil && strings.EqualFold(m.matchedTorrent.Hash, hash) {
				torrents = append(torrents, *m.matchedTorrent)
			} else {
				torrents = append(torrents, qbt.Torrent{Hash: hash})
			}
		}
		return torrents, nil
	}
	if m.matchedTorrent != nil {
		return []qbt.Torrent{*m.matchedTorrent}, nil
	}
	return []qbt.Torrent{{Hash: "dummy"}}, nil
}

func (m *discPolicySyncManager) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	result := make(map[string]qbt.TorrentFiles, len(hashes))
	for _, h := range hashes {
		if files, ok := m.files[strings.ToLower(h)]; ok {
			cp := make(qbt.TorrentFiles, len(files))
			copy(cp, files)
			result[normalizeHash(h)] = cp
		}
	}
	return result, nil
}

func (*discPolicySyncManager) ExportTorrent(context.Context, int, string) ([]byte, string, string, error) {
	return nil, "", "", errors.New("not implemented")
}

func (*discPolicySyncManager) HasTorrentByAnyHash(context.Context, int, []string) (*qbt.Torrent, bool, error) {
	return nil, false, nil
}

func (m *discPolicySyncManager) GetTorrentProperties(_ context.Context, _ int, hash string) (*qbt.TorrentProperties, error) {
	if props, ok := m.props[strings.ToLower(hash)]; ok {
		cp := *props
		return &cp, nil
	}
	return &qbt.TorrentProperties{SavePath: "/downloads"}, nil
}

func (*discPolicySyncManager) GetAppPreferences(context.Context, int) (qbt.AppPreferences, error) {
	return qbt.AppPreferences{TorrentContentLayout: "Original"}, nil
}

func (m *discPolicySyncManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) error {
	m.addedOptions = make(map[string]string, len(options))
	maps.Copy(m.addedOptions, options)
	return nil
}

func (m *discPolicySyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	for _, h := range hashes {
		m.bulkActions = append(m.bulkActions, action+":"+h)
	}
	return nil
}

func (*discPolicySyncManager) SetTags(context.Context, int, []string, string) error {
	return nil
}

func (*discPolicySyncManager) GetCachedInstanceTorrents(context.Context, int) ([]internalqb.CrossInstanceTorrentView, error) {
	return nil, nil
}

func (*discPolicySyncManager) ExtractDomainFromURL(string) string {
	return ""
}

func (*discPolicySyncManager) GetQBittorrentSyncManager(context.Context, int) (*qbt.SyncManager, error) {
	return nil, nil
}

func (*discPolicySyncManager) RenameTorrent(context.Context, int, string, string) error {
	return nil
}

func (*discPolicySyncManager) RenameTorrentFile(context.Context, int, string, string, string) error {
	return nil
}

func (*discPolicySyncManager) RenameTorrentFolder(context.Context, int, string, string, string) error {
	return nil
}

func (*discPolicySyncManager) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	return map[string]qbt.Category{}, nil
}

func (*discPolicySyncManager) CreateCategory(context.Context, int, string, string) error {
	return nil
}

type discPolicyInstanceStore struct {
	instances map[int]*models.Instance
}

func (m *discPolicyInstanceStore) Get(_ context.Context, id int) (*models.Instance, error) {
	if inst, ok := m.instances[id]; ok {
		return inst, nil
	}
	return &models.Instance{
		ID:           id,
		UseHardlinks: false,
		UseReflinks:  false,
	}, nil
}

func (m *discPolicyInstanceStore) List(_ context.Context) ([]*models.Instance, error) {
	result := make([]*models.Instance, 0, len(m.instances))
	for _, inst := range m.instances {
		result = append(result, inst)
	}
	return result, nil
}

// TestDiscLayoutPolicy_ForcePausedEvenWhenStartPausedFalse verifies that disc layout torrents
// are always added paused, even when StartPaused is false.
func TestDiscLayoutPolicy_ForcePausedEvenWhenStartPausedFalse(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Movie.2024.BluRay.1080p"

	// Candidate files (existing on disk) - a Blu-ray disc structure
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.BluRay.1080p/BDMV/index.bdmv", Size: 100},
		{Name: "Movie.2024.BluRay.1080p/BDMV/STREAM/00000.m2ts", Size: 30_000_000_000},
	}
	// Source files (incoming torrent) - same structure
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.BluRay.1080p/BDMV/index.bdmv", Size: 100},
		{Name: "Movie.2024.BluRay.1080p/BDMV/STREAM/00000.m2ts", Size: 30_000_000_000},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		ContentPath: "/downloads/movies/" + matchedName,
		Progress:    1.0,
		Size:        30_000_000_100,
	}

	mockSync := &discPolicySyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {
				SavePath: "/downloads/movies",
			},
		},
		matchedTorrent: &matchedTorrent,
	}

	mockInstances := &discPolicyInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, UseHardlinks: false, UseReflinks: false},
		},
	}

	service := &Service{
		syncManager:       mockSync,
		instanceStore:     mockInstances,
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		releaseCache:      NewReleaseCache(),
		recheckResumeChan: make(chan *pendingResume, 10),
		recheckResumeCtx:  context.Background(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "test-instance",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	// Request with StartPaused = false - normally would auto-resume
	startPausedFalse := false
	req := &CrossSeedRequest{
		StartPaused: &startPausedFalse, // Explicitly set to false
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)

	// Verify the torrent was added successfully
	require.True(t, result.Success, "Expected success, got: %s", result.Message)
	require.Equal(t, "added", result.Status)

	// Even though StartPaused=false, disc layout policy should force paused
	assert.Equal(t, "true", mockSync.addedOptions["paused"], "Disc layout should force paused=true")
	assert.Equal(t, "true", mockSync.addedOptions["stopped"], "Disc layout should force stopped=true")

	// Verify the status message mentions disc layout
	assert.Contains(t, result.Message, "disc layout", "Result message should mention disc layout")
	assert.Contains(t, result.Message, "BDMV", "Result message should mention the marker")

	// Disc-layout torrents should be queued for resume only after a full (100%) recheck
	select {
	case pending := <-service.recheckResumeChan:
		require.NotNil(t, pending)
		assert.Equal(t, instanceID, pending.instanceID)
		assert.Equal(t, newHash, pending.hash)
		assert.Equal(t, 1.0, pending.threshold)
	default:
		require.Fail(t, "expected disc-layout torrent to be queued for recheck resume")
	}
}

// TestDiscLayoutPolicy_NoAutoResumeForPerfectMatch verifies that disc layout torrents
// never get auto-resumed, even for a perfect match scenario that would normally resume.
func TestDiscLayoutPolicy_ResumeOnlyAfterFullRecheck(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Movie.2024.DVD-GROUP"

	// DVD disc structure
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.DVD-GROUP/VIDEO_TS/VIDEO_TS.VOB", Size: 5_000_000_000},
		{Name: "Movie.2024.DVD-GROUP/VIDEO_TS/VTS_01_0.VOB", Size: 1_000_000_000},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.DVD-GROUP/VIDEO_TS/VIDEO_TS.VOB", Size: 5_000_000_000},
		{Name: "Movie.2024.DVD-GROUP/VIDEO_TS/VTS_01_0.VOB", Size: 1_000_000_000},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		ContentPath: "/downloads/movies/" + matchedName,
		Progress:    1.0,
		Size:        6_000_000_000,
	}

	mockSync := &discPolicySyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {
				SavePath: "/downloads/movies",
			},
		},
		matchedTorrent: &matchedTorrent,
		bulkActions:    make([]string, 0),
	}

	mockInstances := &discPolicyInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, UseHardlinks: false, UseReflinks: false},
		},
	}

	service := &Service{
		syncManager:       mockSync,
		instanceStore:     mockInstances,
		stringNormalizer:  stringutils.NewDefaultNormalizer(),
		releaseCache:      NewReleaseCache(),
		recheckResumeChan: make(chan *pendingResume, 10),
		recheckResumeCtx:  context.Background(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "test-instance",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	// Request with StartPaused = true and no SkipAutoResume - normally would auto-resume
	startPausedTrue := true
	req := &CrossSeedRequest{
		StartPaused:    &startPausedTrue,
		SkipAutoResume: false, // Would normally allow auto-resume
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)

	// Verify the torrent was added successfully
	require.True(t, result.Success, "Expected success, got: %s", result.Message)

	// We should not resume immediately; it must wait for a full recheck.
	for _, action := range mockSync.bulkActions {
		assert.NotContains(t, action, "resume", "No resume action should be called for disc layout torrents")
	}

	// Verify the status message mentions disc layout
	assert.Contains(t, result.Message, "disc layout", "Result message should mention disc layout")
	assert.Contains(t, result.Message, "VIDEO_TS", "Result message should mention the marker")

	select {
	case pending := <-service.recheckResumeChan:
		require.NotNil(t, pending)
		assert.Equal(t, instanceID, pending.instanceID)
		assert.Equal(t, newHash, pending.hash)
		assert.Equal(t, 1.0, pending.threshold)
	default:
		require.Fail(t, "expected disc-layout torrent to be queued for recheck resume")
	}
}

// TestDiscLayoutPolicy_NonDiscTorrentAllowsAutoResume verifies that non-disc torrents
// still get auto-resumed as expected (regression test).
func TestDiscLayoutPolicy_NonDiscTorrentAllowsAutoResume(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	instanceID := 1
	matchedHash := "matchedhash"
	newHash := "newhash"
	matchedName := "Movie.2024.1080p.BluRay.x264-GROUP"

	// Regular movie file (not disc structure)
	candidateFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv", Size: 8_000_000_000},
	}
	sourceFiles := qbt.TorrentFiles{
		{Name: "Movie.2024.1080p.BluRay.x264-GROUP.mkv", Size: 8_000_000_000},
	}

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		ContentPath: "/downloads/movies/" + matchedName + ".mkv",
		Progress:    1.0,
		Size:        8_000_000_000,
	}

	mockSync := &discPolicySyncManager{
		files: map[string]qbt.TorrentFiles{
			matchedHash: candidateFiles,
		},
		props: map[string]*qbt.TorrentProperties{
			matchedHash: {
				SavePath: "/downloads/movies",
			},
		},
		matchedTorrent: &matchedTorrent,
		bulkActions:    make([]string, 0),
	}

	mockInstances := &discPolicyInstanceStore{
		instances: map[int]*models.Instance{
			instanceID: {ID: instanceID, UseHardlinks: false, UseReflinks: false},
		},
	}

	service := &Service{
		syncManager:      mockSync,
		instanceStore:    mockInstances,
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		releaseCache:     NewReleaseCache(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	candidate := CrossSeedCandidate{
		InstanceID:   instanceID,
		InstanceName: "test-instance",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	// Request with StartPaused = true and no SkipAutoResume - should auto-resume
	startPausedTrue := true
	req := &CrossSeedRequest{
		StartPaused:    &startPausedTrue,
		SkipAutoResume: false,
	}

	result := service.processCrossSeedCandidate(ctx, candidate, []byte("torrent"), newHash, "", matchedName, req, service.releaseCache.Parse(matchedName), sourceFiles, nil)

	// Verify the torrent was added successfully
	require.True(t, result.Success, "Expected success, got: %s", result.Message)

	// Non-disc torrent should get resumed
	resumeCalled := false
	for _, action := range mockSync.bulkActions {
		if strings.HasPrefix(action, "resume:") {
			resumeCalled = true
			break
		}
	}
	assert.True(t, resumeCalled, "Resume should be called for non-disc perfect match torrents")

	// Status message should NOT mention disc layout
	assert.NotContains(t, result.Message, "disc layout", "Non-disc torrent message should not mention disc layout")
}
