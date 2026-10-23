// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/stringutils"
)

// renameOnlySyncManager extends the reflink fallback mock with rename bookkeeping:
// RenameTorrentFile mutates the files map so renameFileWithVerification can verify
// the new path via GetTorrentFilesBatch, and BulkAction records actions.
type renameOnlySyncManager struct {
	*reflinkFallbackSafetySyncManager
	bulkActions []string // "action:hash"
}

func (m *renameOnlySyncManager) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	for _, h := range hashes {
		m.bulkActions = append(m.bulkActions, action+":"+normalizeHash(h))
	}
	return nil
}

func (m *renameOnlySyncManager) RenameTorrentFile(_ context.Context, _ int, hash, oldPath, newPath string) error {
	key := normalizeHash(hash)
	files := m.files[key]
	for i := range files {
		if files[i].Name == oldPath {
			files[i].Name = newPath
		}
	}
	m.files[key] = files
	return nil
}

// Synthetic names per repo rule; only the DoVi/DV token differs, which survives
// normalizeFileKey as distinct keys, so only the size-only sole-candidate
// fallback can pair them (issue #2272).
const (
	renameOnlySourceFile    = "Example.Film.2024.2160p.WEB-DL.DDP5.1.Atmos.DoVi.HDR10.H.265-EXGRP.mkv"
	renameOnlyCandidateFile = "Example.Film.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR10.H.265-EXGRP.mkv"
	renameOnlySize          = int64(12_240_306_439)
)

func newRenameOnlyService(t *testing.T, instance *models.Instance, matchedHash, matchedName string, candidateFiles qbt.TorrentFiles, newHash string, sourceFiles qbt.TorrentFiles) (*Service, *renameOnlySyncManager, CrossSeedCandidate) {
	t.Helper()

	matchedTorrent := qbt.Torrent{
		Hash:        matchedHash,
		Name:        matchedName,
		Progress:    1.0,
		Category:    "movies",
		ContentPath: "/downloads/movies/" + matchedName,
	}

	sync := &renameOnlySyncManager{
		reflinkFallbackSafetySyncManager: &reflinkFallbackSafetySyncManager{
			files: map[string]qbt.TorrentFiles{
				normalizeHash(matchedHash): candidateFiles,
				normalizeHash(newHash):     sourceFiles,
			},
			props: map[string]*qbt.TorrentProperties{
				normalizeHash(matchedHash): {SavePath: "/downloads/movies"},
			},
		},
	}

	service := &Service{
		syncManager:      sync,
		instanceStore:    &reflinkFallbackSafetyInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	service.settler = idleSettler(sync)

	candidate := CrossSeedCandidate{
		InstanceID:   instance.ID,
		InstanceName: "Test",
		Torrents:     []qbt.Torrent{matchedTorrent},
	}

	return service, sync, candidate
}

func TestProcessCrossSeedCandidate_RenameOnlySingleFileSkipsRecheckSkip(t *testing.T) {
	t.Parallel()

	instance := &models.Instance{ID: 1}
	sourceFiles := qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
	candidateFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
	newHash := "newhash"
	service, sync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlyCandidateFile, candidateFiles, newHash, sourceFiles)

	startPaused := false
	req := &CrossSeedRequest{SkipRecheck: true, StartPaused: &startPaused}

	withAdder(service)
	result := service.processCrossSeedCandidate(context.Background(), candidate, []byte("torrent"), newHash, "", renameOnlySourceFile, req, service.releaseCache.Parse(renameOnlySourceFile), sourceFiles, nil)

	require.True(t, result.Success, "message: %s", result.Message)
	require.Equal(t, "added", result.Status)
	require.Contains(t, sync.bulkActions, "resume:"+normalizeHash(newHash))
	for _, action := range sync.bulkActions {
		require.NotContains(t, action, "recheck:", "rename-only exact-size add must not recheck")
	}
	require.Equal(t, renameOnlyCandidateFile, sync.files[normalizeHash(newHash)][0].Name, "file entry must be renamed to the on-disk name")
}

func TestProcessCrossSeedCandidate_RenameOnlyLinkFallbackSkipsRecheckSkip(t *testing.T) {
	t.Parallel()

	// Reflink mode with an empty base dir forces the link-mode bail-out into the
	// regular-mode fallback (reflink_fallback_safety_test.go pattern); the
	// rename-only pair must clear the fallback's full-recheck demand.
	instance := &models.Instance{
		ID:                       1,
		UseReflinks:              true,
		FallbackToRegularMode:    true,
		HasLocalFilesystemAccess: true,
		HardlinkBaseDir:          "",
	}
	sourceFiles := qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
	candidateFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
	newHash := "newhash"
	service, sync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlyCandidateFile, candidateFiles, newHash, sourceFiles)

	req := &CrossSeedRequest{SkipRecheck: true}

	withAdder(service)
	result := service.processCrossSeedCandidate(context.Background(), candidate, []byte("torrent"), newHash, "", renameOnlySourceFile, req, service.releaseCache.Parse(renameOnlySourceFile), sourceFiles, nil)

	require.True(t, result.Success, "message: %s", result.Message)
	require.Equal(t, "added", result.Status)
	require.NotContains(t, result.Message, "full recheck required")
	require.Contains(t, sync.bulkActions, "resume:"+normalizeHash(newHash))
	for _, action := range sync.bulkActions {
		require.NotContains(t, action, "recheck:")
	}
}

func TestProcessCrossSeedCandidate_ExactLinkFullRecheckFallback(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		skipRecheck bool
		extraFile   bool
		wantStatus  string
		wantAction  string
	}{
		{name: "skip recheck adds and resumes", skipRecheck: true, wantStatus: "added", wantAction: "resume:"},
		{name: "recheck adds paused and rechecks", wantStatus: "added", wantAction: "recheck:"},
		{name: "skip recheck with extra file skips", skipRecheck: true, extraFile: true, wantStatus: "skipped_recheck"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// The matched files do not exist on disk, so no base dir passes the
			// same-filesystem probe: the full-recheck fallback, not the soft one.
			instance := &models.Instance{
				ID:                       1,
				UseReflinks:              true,
				FallbackToRegularMode:    true,
				HasLocalFilesystemAccess: true,
				HardlinkBaseDir:          t.TempDir(),
			}
			sourceFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
			if tt.extraFile {
				sourceFiles = append(sourceFiles, qbt.TorrentFile{Name: "Example.Film.2024.2160p.WEB-DL.DDP5.1.Atmos.DV.HDR10.H.265-EXGRP.nfo", Size: 700})
			}
			candidateFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
			newHash := "newhash"
			service, sync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlyCandidateFile, candidateFiles, newHash, sourceFiles)
			service.SetBackendPool(fsops.NewPool(service.instanceStore, local.NewBackend()))

			req := &CrossSeedRequest{SkipRecheck: tt.skipRecheck}

			withAdder(service)
			result := service.processCrossSeedCandidate(t.Context(), candidate, []byte("torrent"), newHash, "", renameOnlyCandidateFile, req, service.releaseCache.Parse(renameOnlyCandidateFile), sourceFiles, nil)

			require.Equal(t, tt.wantStatus, result.Status, "message: %s", result.Message)
			if tt.wantAction == "" {
				require.Nil(t, sync.addTorrentOpts, "AddTorrent must not be called")
				return
			}
			require.Equal(t, []string{tt.wantAction + normalizeHash(newHash)}, sync.bulkActions)
		})
	}
}

func TestProcessCrossSeedCandidate_RenameOnlyStillRechecksWithoutSkipRecheck(t *testing.T) {
	t.Parallel()

	// SkipRecheck OFF is the default config: a rename-only pair must keep the
	// post-alignment recheck — the bypass is scoped strictly to SkipRecheck ON.
	instance := &models.Instance{ID: 1}
	sourceFiles := qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
	candidateFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
	newHash := "newhash"
	service, sync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlyCandidateFile, candidateFiles, newHash, sourceFiles)

	req := &CrossSeedRequest{SkipRecheck: false, SkipAutoResume: true}

	withAdder(service)
	result := service.processCrossSeedCandidate(context.Background(), candidate, []byte("torrent"), newHash, "", renameOnlySourceFile, req, service.releaseCache.Parse(renameOnlySourceFile), sourceFiles, nil)

	require.True(t, result.Success, "message: %s", result.Message)
	require.Equal(t, "added", result.Status)
	require.Contains(t, sync.bulkActions, "recheck:"+normalizeHash(newHash))
}

func TestProcessCrossSeedCandidate_SkipRecheckStillSkipsNonRenameOnly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		sourceFiles    qbt.TorrentFiles
		candidateFiles qbt.TorrentFiles
		matchedName    string
		wantStatus     string
	}{
		{
			// Episode into season pack: alignment deliberately skips file renames
			// (shouldAlignFilesWithCandidate false), so the bypass must not apply
			// even though the episode pairs with a same-size pack file.
			name:        "episode into season pack still skips",
			sourceFiles: qbt.TorrentFiles{{Name: "Example.Show.S01E01.2160p.WEB-DL.DDP5.1.DV.H.265-EXGRP.mkv", Size: renameOnlySize}},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Example.Show.S01.2160p.WEB-DL.DDP5.1.DoVi.H.265-EXGRP/Example.Show.S01E01.2160p.WEB-DL.DDP5.1.DoVi.H.265-EXGRP.mkv", Size: renameOnlySize},
				{Name: "Example.Show.S01.2160p.WEB-DL.DDP5.1.DoVi.H.265-EXGRP/Example.Show.S01E02.2160p.WEB-DL.DDP5.1.DoVi.H.265-EXGRP.mkv", Size: renameOnlySize - 5},
			},
			matchedName: "Example.Show.S01.2160p.WEB-DL.DDP5.1.DoVi.H.265-EXGRP",
			wantStatus:  "skipped_recheck",
		},
		{
			// The nfo has no candidate counterpart: genuinely missing content.
			name: "genuine extra file still skips",
			sourceFiles: qbt.TorrentFiles{
				{Name: renameOnlySourceFile, Size: renameOnlySize},
				{Name: "Example.Film.2024.2160p.WEB-DL.DDP5.1.Atmos.DoVi.HDR10.H.265-EXGRP.nfo", Size: 700},
			},
			candidateFiles: qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}},
			wantStatus:     "skipped_recheck",
		},
		{
			// Two same-size candidates: the size-only fallback must refuse the
			// ambiguous bucket, so the renamed files stay unmatched.
			name: "same-size ambiguity still skips",
			sourceFiles: qbt.TorrentFiles{
				{Name: "Example.Show.S01/Example.Show.S01E01.2160p.DV.WEB-DL-EXGRP.mkv", Size: renameOnlySize},
				{Name: "Example.Show.S01/Example.Show.S01E02.2160p.DV.WEB-DL-EXGRP.mkv", Size: renameOnlySize},
			},
			candidateFiles: qbt.TorrentFiles{
				{Name: "Example.Show.S01/Example.Show.S01E01.2160p.DoVi.WEB-DL-EXGRP.mkv", Size: renameOnlySize},
				{Name: "Example.Show.S01/Example.Show.S01E02.2160p.DoVi.WEB-DL-EXGRP.mkv", Size: renameOnlySize},
			},
			wantStatus: "skipped_recheck",
		},
		{
			name:           "size mismatch still rejects",
			sourceFiles:    qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}},
			candidateFiles: qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize + 1}},
			wantStatus:     "rejected",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			instance := &models.Instance{ID: 1}
			newHash := "newhash"
			matchedName := tt.matchedName
			if matchedName == "" {
				matchedName = renameOnlyCandidateFile
			}
			service, sync, candidate := newRenameOnlyService(t, instance, "matchedhash", matchedName, tt.candidateFiles, newHash, tt.sourceFiles)

			req := &CrossSeedRequest{SkipRecheck: true}

			withAdder(service)
			result := service.processCrossSeedCandidate(context.Background(), candidate, []byte("torrent"), newHash, "", tt.sourceFiles[0].Name, req, service.releaseCache.Parse(tt.sourceFiles[0].Name), tt.sourceFiles, nil)

			require.False(t, result.Success)
			require.Equal(t, tt.wantStatus, result.Status, "message: %s", result.Message)
			require.Nil(t, sync.addTorrentOpts, "AddTorrent must not be called")
		})
	}
}

// failingRecheckSyncManager fails the recheck while recheckErr is set, the way a
// stalled qBittorrent fails the post-add readiness wait (issue #2821).
type failingRecheckSyncManager struct {
	qbittorrentSync
	recheckErr error
}

func (m *failingRecheckSyncManager) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	if action == "recheck" && m.recheckErr != nil {
		return m.recheckErr
	}
	return m.qbittorrentSync.BulkAction(ctx, instanceID, hashes, action)
}

func TestProcessCrossSeedCandidate_FailedRecheckIsQueued(t *testing.T) {
	t.Parallel()

	for _, recheckErr := range []error{
		errors.New("synthetic recheck failure"),
		context.DeadlineExceeded,
	} {
		t.Run(recheckErr.Error(), func(t *testing.T) {
			t.Parallel()
			instance := &models.Instance{ID: 1}
			sourceFiles := qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
			candidateFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
			newHash := "newhash"
			service, renameSync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlyCandidateFile, candidateFiles, newHash, sourceFiles)
			service.syncManager = &failingRecheckSyncManager{qbittorrentSync: renameSync, recheckErr: recheckErr}
			synctest.Test(t, func(t *testing.T) {
				stopped := scripted(newHash, qbt.TorrentStateStoppedUp, 1, 0)
				script := startSettleScript(t, service, stopped, stopped)

				req := &CrossSeedRequest{}
				withAdder(service)
				result := service.processCrossSeedCandidate(t.Context(), candidate, []byte("torrent"), newHash, "", renameOnlySourceFile, req, service.releaseCache.Parse(renameOnlySourceFile), sourceFiles, nil)

				require.True(t, result.Success, "message: %s", result.Message)
				require.NotContains(t, result.Message, "manual intervention required")
				// skip_checking reports 100% before any recheck, so each poll sends
				// the recheck again and never resumes.
				actions, _ := script.wait()
				require.Equal(t, []string{"recheck:" + newHash, "recheck:" + newHash, "recheck:" + newHash}, actions)
			})
		})
	}
}

func TestProcessCrossSeedCandidate_FailedRecheckWithSkipAutoResumeIsRecheckedNotResumed(t *testing.T) {
	t.Parallel()
	instance := &models.Instance{ID: 1}
	sourceFiles := qbt.TorrentFiles{{Name: renameOnlySourceFile, Size: renameOnlySize}}
	candidateFiles := qbt.TorrentFiles{{Name: renameOnlyCandidateFile, Size: renameOnlySize}}
	newHash := "newhash"
	service, renameSync, candidate := newRenameOnlyService(t, instance, "matchedhash", renameOnlyCandidateFile, candidateFiles, newHash, sourceFiles)
	service.syncManager = &failingRecheckSyncManager{qbittorrentSync: renameSync, recheckErr: context.DeadlineExceeded}
	synctest.Test(t, func(t *testing.T) {
		stopped := scripted(newHash, qbt.TorrentStateStoppedUp, 1, 0)
		script := startSettleScript(t, service, stopped, stopped)

		req := &CrossSeedRequest{SkipAutoResume: true}
		withAdder(service)
		result := service.processCrossSeedCandidate(t.Context(), candidate, []byte("torrent"), newHash, "", renameOnlySourceFile, req, service.releaseCache.Parse(renameOnlySourceFile), sourceFiles, nil)

		require.True(t, result.Success, "message: %s", result.Message)
		require.NotContains(t, result.Message, "manual intervention required")
		require.Contains(t, result.Message, "auto-resume skipped per settings")
		actions, _ := script.wait()
		require.Equal(t, []string{"recheck:" + newHash, "recheck:" + newHash, "recheck:" + newHash}, actions)
	})
}
