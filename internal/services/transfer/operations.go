// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// doPrepare gathers source torrent info and prepares for transfer
func (s *Service) doPrepare(ctx context.Context, t *models.Transfer) {
	s.updateState(ctx, t, models.TransferStatePreparing, "")

	// 1. Validate source instance
	sourceInstance, err := s.instanceStore.Get(ctx, t.SourceInstanceID)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get source instance: %v", err))
		return
	}
	if !sourceInstance.HasLocalFilesystemAccess {
		s.fail(ctx, t, "source instance lacks local filesystem access")
		return
	}

	// 2. Validate target instance
	targetInstance, err := s.instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get target instance: %v", err))
		return
	}
	if !targetInstance.HasLocalFilesystemAccess {
		s.fail(ctx, t, "target instance lacks local filesystem access")
		return
	}

	// 3. Get source torrent info
	torrents, err := s.syncManager.GetTorrents(ctx, t.SourceInstanceID,
		qbt.TorrentFilterOptions{Hashes: []string{t.TorrentHash}})
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get source torrent: %v", err))
		return
	}
	if len(torrents) == 0 {
		s.fail(ctx, t, "torrent not found on source instance")
		return
	}
	sourceTorrent := torrents[0]

	// 4. Get source files
	files, err := s.syncManager.GetTorrentFiles(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get source files: %v", err))
		return
	}

	// 5. Get properties (save path)
	props, err := s.syncManager.GetTorrentProperties(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get source properties: %v", err))
		return
	}

	// 6. Check torrent is complete
	if sourceTorrent.Progress < 1.0 {
		s.fail(ctx, t, fmt.Sprintf("torrent is not complete (%.1f%%)", sourceTorrent.Progress*100))
		return
	}

	// 7. Update transfer with gathered info
	t.TorrentName = sourceTorrent.Name
	t.SourceSavePath = props.SavePath
	t.FilesTotal = len(*files)

	if t.PreserveCategory {
		t.TargetCategory = sourceTorrent.Category
	}
	if t.PreserveTags && sourceTorrent.Tags != "" {
		t.TargetTags = strings.Split(sourceTorrent.Tags, ",")
		for i := range t.TargetTags {
			t.TargetTags[i] = strings.TrimSpace(t.TargetTags[i])
		}
	}

	// 8. Compute target save path
	t.TargetSavePath = s.computeTargetPath(t.SourceSavePath, targetInstance, t.PathMappings)

	// 9. Determine link mode
	t.LinkMode = s.determineLinkMode(targetInstance, t.SourceSavePath, t.TargetSavePath)

	log.Info().
		Int64("id", t.ID).
		Str("name", t.TorrentName).
		Str("sourcePath", t.SourceSavePath).
		Str("targetPath", t.TargetSavePath).
		Str("linkMode", t.LinkMode).
		Int("files", t.FilesTotal).
		Msg("[TRANSFER] Prepared transfer")

	// 10. Save and continue
	if err := s.store.Update(ctx, t); err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to save transfer: %v", err))
		return
	}

	s.doCreateLinks(ctx, t)
}

// doCreateLinks creates hardlinks or reflinks for the torrent files
func (s *Service) doCreateLinks(ctx context.Context, t *models.Transfer) {
	s.updateState(ctx, t, models.TransferStateLinksCreating, "")

	// Direct mode - skip link creation
	if t.LinkMode == "direct" {
		log.Debug().Int64("id", t.ID).Msg("[TRANSFER] Direct mode - skipping link creation")
		s.updateState(ctx, t, models.TransferStateLinksCreated, "")
		s.doAddTorrent(ctx, t)
		return
	}

	// Get target instance for settings
	targetInstance, err := s.instanceStore.Get(ctx, t.TargetInstanceID)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get target instance: %v", err))
		return
	}

	// Get source files
	files, err := s.syncManager.GetTorrentFiles(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to get source files: %v", err))
		return
	}

	// Build file list for linking
	sourceFiles := make([]hardlinktree.TorrentFile, 0, len(*files))
	for _, f := range *files {
		sourceFiles = append(sourceFiles, hardlinktree.TorrentFile{
			Path: f.Name,
			Size: f.Size,
		})
	}

	// Build existing files list (source locations)
	existingFiles := make([]hardlinktree.ExistingFile, 0, len(*files))
	for _, f := range *files {
		existingFiles = append(existingFiles, hardlinktree.ExistingFile{
			AbsPath: filepath.Join(t.SourceSavePath, f.Name),
			RelPath: f.Name,
			Size:    f.Size,
		})
	}

	// Build destination directory
	destDir := s.buildDestDir(t, targetInstance)

	// Ensure destination directory exists
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to create destination directory: %v", err))
		return
	}

	// Build plan
	plan, err := hardlinktree.BuildPlan(sourceFiles, existingFiles, hardlinktree.LayoutOriginal, t.TorrentName, destDir)
	if err != nil {
		s.fail(ctx, t, fmt.Sprintf("failed to build link plan: %v", err))
		return
	}

	// Store plan info for potential rollback
	t.TargetSavePath = plan.RootDir

	// Execute based on link mode
	switch t.LinkMode {
	case "hardlink":
		if err := hardlinktree.Create(plan); err != nil {
			s.fail(ctx, t, fmt.Sprintf("failed to create hardlinks: %v", err))
			return
		}
	case "reflink":
		if err := reflinktree.Create(plan); err != nil {
			// Check if we should fall back
			if targetInstance.FallbackToRegularMode {
				log.Warn().
					Err(err).
					Int64("id", t.ID).
					Msg("[TRANSFER] Reflink failed, falling back to copy")
				// For now, fail - copy mode not implemented
				s.fail(ctx, t, fmt.Sprintf("reflink failed and copy fallback not implemented: %v", err))
				return
			}
			s.fail(ctx, t, fmt.Sprintf("failed to create reflinks: %v", err))
			return
		}
	}

	t.FilesLinked = len(plan.Files)
	if err := s.store.Update(ctx, t); err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to update progress")
	}

	log.Info().
		Int64("id", t.ID).
		Int("files", len(plan.Files)).
		Str("destDir", destDir).
		Msg("[TRANSFER] Created file links")

	s.updateState(ctx, t, models.TransferStateLinksCreated, "")
	s.doAddTorrent(ctx, t)
}

// doAddTorrent adds the torrent to the target instance
func (s *Service) doAddTorrent(ctx context.Context, t *models.Transfer) {
	s.updateState(ctx, t, models.TransferStateAddingTorrent, "")

	// Export torrent from source
	torrentBytes, _, _, err := s.syncManager.ExportTorrent(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		s.rollbackLinks(ctx, t)
		s.fail(ctx, t, fmt.Sprintf("failed to export torrent: %v", err))
		return
	}

	// Ensure category exists on target
	if t.TargetCategory != "" {
		if err := s.ensureCategory(ctx, t.TargetInstanceID, t.TargetCategory, t.TargetSavePath); err != nil {
			log.Warn().
				Err(err).
				Int64("id", t.ID).
				Str("category", t.TargetCategory).
				Msg("[TRANSFER] Failed to create category, continuing without")
			t.TargetCategory = ""
		}
	}

	// Build add options
	options := map[string]string{
		"autoTMM":       "false",
		"savepath":      t.TargetSavePath,
		"skip_checking": "true", // Files already exist via links
	}

	if t.LinkMode != "direct" {
		options["contentLayout"] = "Original"
	}

	if t.TargetCategory != "" {
		options["category"] = t.TargetCategory
	}

	if len(t.TargetTags) > 0 {
		options["tags"] = strings.Join(t.TargetTags, ",")
	}

	// Start paused to verify before resuming
	options["paused"] = "true"
	options["stopped"] = "true"

	// Add torrent to target
	if err := s.syncManager.AddTorrent(ctx, t.TargetInstanceID, torrentBytes, options); err != nil {
		s.rollbackLinks(ctx, t)
		s.fail(ctx, t, fmt.Sprintf("failed to add torrent to target: %v", err))
		return
	}

	log.Info().
		Int64("id", t.ID).
		Str("hash", t.TorrentHash).
		Int("targetInstance", t.TargetInstanceID).
		Msg("[TRANSFER] Added torrent to target instance")

	// Wait for torrent to be visible, then resume
	if s.waitForTorrent(ctx, t.TargetInstanceID, t.TorrentHash, 15*time.Second) {
		// Trigger recheck and resume
		if err := s.syncManager.BulkAction(ctx, t.TargetInstanceID, []string{t.TorrentHash}, "recheck"); err != nil {
			log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to trigger recheck")
		}
		// Resume after a brief delay for recheck to start
		time.Sleep(2 * time.Second)
		if err := s.syncManager.BulkAction(ctx, t.TargetInstanceID, []string{t.TorrentHash}, "resume"); err != nil {
			log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to resume torrent")
		}
	}

	s.updateState(ctx, t, models.TransferStateTorrentAdded, "")

	if t.DeleteFromSource {
		s.doDeleteSource(ctx, t)
	} else {
		s.markCompleted(ctx, t)
	}
}

// doDeleteSource removes the torrent from the source instance
func (s *Service) doDeleteSource(ctx context.Context, t *models.Transfer) {
	s.updateState(ctx, t, models.TransferStateDeletingSource, "")

	// Delete torrent from source (keep files - they're hardlinked)
	if err := s.syncManager.DeleteTorrents(ctx, t.SourceInstanceID, []string{t.TorrentHash}, false); err != nil {
		// Don't fail the whole transfer for this - log and complete
		log.Warn().
			Err(err).
			Int64("id", t.ID).
			Msg("[TRANSFER] Failed to delete from source, completing anyway")
	} else {
		log.Info().
			Int64("id", t.ID).
			Int("sourceInstance", t.SourceInstanceID).
			Msg("[TRANSFER] Deleted torrent from source instance")
	}

	s.markCompleted(ctx, t)
}

// Helper methods

func (s *Service) updateState(ctx context.Context, t *models.Transfer, state models.TransferState, errorMsg string) {
	t.State = state
	t.Error = errorMsg
	if err := s.store.UpdateState(ctx, t.ID, state, errorMsg); err != nil {
		log.Error().Err(err).Int64("id", t.ID).Str("state", string(state)).Msg("[TRANSFER] Failed to update state")
	}
}

func (s *Service) fail(ctx context.Context, t *models.Transfer, errorMsg string) {
	log.Error().Int64("id", t.ID).Str("error", errorMsg).Msg("[TRANSFER] Transfer failed")
	s.updateState(ctx, t, models.TransferStateFailed, errorMsg)
}

func (s *Service) markCompleted(ctx context.Context, t *models.Transfer) {
	now := time.Now().UTC()
	t.CompletedAt = &now
	t.State = models.TransferStateCompleted
	if err := s.store.Update(ctx, t); err != nil {
		log.Error().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to mark completed")
		return
	}
	log.Info().
		Int64("id", t.ID).
		Str("name", t.TorrentName).
		Msg("[TRANSFER] Transfer completed successfully")
}

func (s *Service) checkTorrentExists(ctx context.Context, instanceID int, hash string) bool {
	_, exists, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, []string{hash})
	if err != nil {
		return false
	}
	return exists
}

func (s *Service) waitForTorrent(ctx context.Context, instanceID int, hash string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if s.checkTorrentExists(ctx, instanceID, hash) {
			return true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(500 * time.Millisecond):
		}
	}
	return false
}

func (s *Service) rollbackLinks(ctx context.Context, t *models.Transfer) {
	if t.LinkMode == "direct" || t.TargetSavePath == "" {
		return
	}

	log.Debug().Int64("id", t.ID).Str("path", t.TargetSavePath).Msg("[TRANSFER] Rolling back links")

	// Get source files to build rollback plan
	files, err := s.syncManager.GetTorrentFiles(ctx, t.SourceInstanceID, t.TorrentHash)
	if err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Failed to get files for rollback")
		return
	}

	// Build plan just for rollback
	sourceFiles := make([]hardlinktree.TorrentFile, 0, len(*files))
	for _, f := range *files {
		sourceFiles = append(sourceFiles, hardlinktree.TorrentFile{
			Path: f.Name,
			Size: f.Size,
		})
	}

	plan := &hardlinktree.TreePlan{
		RootDir: t.TargetSavePath,
		Files:   make([]hardlinktree.FilePlan, 0, len(sourceFiles)),
	}
	for _, f := range sourceFiles {
		plan.Files = append(plan.Files, hardlinktree.FilePlan{
			TargetPath: filepath.Join(t.TargetSavePath, f.Path),
		})
	}

	if err := hardlinktree.Rollback(plan); err != nil {
		log.Warn().Err(err).Int64("id", t.ID).Msg("[TRANSFER] Rollback failed")
	}
}

func (s *Service) buildDestDir(t *models.Transfer, instance *models.Instance) string {
	baseDir := t.TargetSavePath
	if instance.HardlinkBaseDir != "" {
		baseDir = instance.HardlinkBaseDir
	}

	// Use preset if configured
	switch instance.HardlinkDirPreset {
	case "flat":
		// All torrents in base dir with isolation folder
		shortHash := t.TorrentHash
		if len(shortHash) > 8 {
			shortHash = shortHash[:8]
		}
		return filepath.Join(baseDir, fmt.Sprintf("%s--%s", sanitizeName(t.TorrentName), shortHash))
	case "by-instance":
		return filepath.Join(baseDir, fmt.Sprintf("instance-%d", t.SourceInstanceID))
	default:
		return baseDir
	}
}

func (s *Service) ensureCategory(ctx context.Context, instanceID int, category, savePath string) error {
	if category == "" {
		return nil
	}

	key := fmt.Sprintf("%d:%s", instanceID, category)

	// Fast path: already created in this session
	if _, ok := s.createdCategories.Load(key); ok {
		return nil
	}

	// Use singleflight to deduplicate concurrent calls
	_, err, _ := s.categoryCreationGroup.Do(key, func() (any, error) {
		// Double-check
		if _, ok := s.createdCategories.Load(key); ok {
			return nil, nil
		}

		// Check if exists
		categories, err := s.syncManager.GetCategories(ctx, instanceID)
		if err != nil {
			return nil, err
		}

		if _, exists := categories[category]; exists {
			s.createdCategories.Store(key, true)
			return nil, nil
		}

		// Create category
		if err := s.syncManager.CreateCategory(ctx, instanceID, category, savePath); err != nil {
			return nil, err
		}

		s.createdCategories.Store(key, true)
		log.Debug().
			Int("instanceID", instanceID).
			Str("category", category).
			Msg("[TRANSFER] Created category")

		return nil, nil
	})

	return err
}

func sanitizeName(name string) string {
	// Replace characters that might cause issues in paths
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(name)
}
