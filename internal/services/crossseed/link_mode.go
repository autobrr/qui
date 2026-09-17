// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// linkMode lists everything that differs between hardlink and reflink mode.
// processLinkMode is the one body behind both; see docs/adr/0006.
type linkMode struct {
	name    string
	enabled func(*models.Instance) bool
	// materialize creates the link tree. Reflink mode probes filesystem support
	// first and returns *linkUnsupportedError when the probe fails.
	materialize func(ctx context.Context, backend fsops.Backend, baseDir string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error)
	poolMode    string
	// poolReplaceable registers the cloned targets as replaceable in the partial
	// pool. A hardlink cannot be replaced in place without touching the source.
	poolReplaceable bool
	// linkedPathsForResume hands the linked file set to the recheck-resume worker,
	// which refuses to download into a linked file (ADR 0004). A reflink clone is
	// copy-on-write, so reflink mode stays exempt.
	linkedPathsForResume bool
	// recoverMissingFilesWithResume lets the recheck-resume worker nudge a
	// missingFiles torrent back to life.
	recoverMissingFilesWithResume bool
	// regularFallbackOnCreateError falls back to regular mode when the plan build
	// or the tree creation fails. Reflink mode refuses regular fallback there so
	// the add never lands inside the matched torrent's path.
	regularFallbackOnCreateError bool
	// belowThresholdNote appends the manual-review note to the status message of
	// a partial add.
	belowThresholdNote bool
}

func (s *Service) processHardlinkMode(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentHashV2 string,
	torrentName string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	matchType string,
	sourceFiles, candidateFiles qbt.TorrentFiles,
	props *qbt.TorrentProperties,
	baseCategory, crossCategory string,
) linkModeResult {
	return s.processLinkMode(ctx, candidate, torrentBytes, torrentHash, torrentHashV2, torrentName, req,
		matchedTorrent, matchType, sourceFiles, candidateFiles, props, baseCategory, crossCategory,
		linkMode{
			name:    "hardlink",
			enabled: func(instance *models.Instance) bool { return instance.UseHardlinks },
			materialize: func(ctx context.Context, backend fsops.Backend, _ string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
				return backend.HardlinkTree(ctx, plan)
			},
			poolMode:                     models.CrossSeedPartialPoolModeHardlink,
			linkedPathsForResume:         true,
			regularFallbackOnCreateError: true,
		})
}

func (s *Service) processReflinkMode(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentHashV2 string,
	torrentName string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	matchType string,
	sourceFiles, candidateFiles qbt.TorrentFiles,
	props *qbt.TorrentProperties,
	baseCategory, crossCategory string,
) linkModeResult {
	return s.processLinkMode(ctx, candidate, torrentBytes, torrentHash, torrentHashV2, torrentName, req,
		matchedTorrent, matchType, sourceFiles, candidateFiles, props, baseCategory, crossCategory,
		linkMode{
			name:                          "reflink",
			enabled:                       func(instance *models.Instance) bool { return instance.UseReflinks },
			materialize:                   s.materializeReflink,
			poolMode:                      models.CrossSeedPartialPoolModeReflink,
			poolReplaceable:               true,
			recoverMissingFilesWithResume: true,
			belowThresholdNote:            true,
		})
}

func shouldWarnForReflinkCreateError(err error) bool {
	if !errors.Is(err, reflinktree.ErrReflinkUnsupported) {
		return false
	}

	type multiUnwrapper interface {
		Unwrap() []error
	}

	var joined multiUnwrapper
	return !errors.As(err, &joined)
}

type linkUnsupportedError struct {
	reason string
}

func (e *linkUnsupportedError) Error() string {
	return e.reason
}

// materializeReflink owns the filesystem-dependent support probe and clone
// operation so link-mode policy tests can exercise the portable code around it.
func (s *Service) materializeReflink(ctx context.Context, backend fsops.Backend, baseDir string, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	if s.reflinkMaterializer != nil {
		return s.reflinkMaterializer(ctx, baseDir, plan)
	}

	supported, reason, err := backend.SupportsReflink(ctx, baseDir)
	if err != nil {
		supported, reason = false, err.Error()
	}
	if !supported {
		return nil, &linkUnsupportedError{reason: reason}
	}
	return backend.ReflinkTree(ctx, plan)
}

// processLinkMode attempts to add a cross-seed torrent by creating a linked
// file tree that matches the incoming torrent's layout, which removes the need
// for reuse+rename alignment.
//
// Returns Used=false if the mode is not applicable (disabled, instance lacks
// local access, filesystem mismatch, etc). Returns Used=true with the final
// result when the mode is attempted.
func (s *Service) processLinkMode(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentHashV2 string,
	torrentName string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	matchType string,
	sourceFiles, candidateFiles qbt.TorrentFiles,
	props *qbt.TorrentProperties,
	_, crossCategory string, // baseCategory unused, crossCategory used for torrent options
	mode linkMode,
) linkModeResult {
	notUsed := linkModeResult{Used: false}
	label := strings.ToUpper(mode.name[:1]) + mode.name[1:]
	logPrefix := "[CROSSSEED] " + label + " mode: "

	// Helper to create error result when link mode is enabled but fails
	linkError := func(message string) linkModeResult {
		return linkModeResult{
			Used:    true,
			Success: false,
			Result: InstanceCrossSeedResult{
				InstanceID:   candidate.InstanceID,
				InstanceName: candidate.InstanceName,
				Success:      false,
				Status:       mode.name + "_error",
				Message:      message,
			},
		}
	}

	// Get instance to check link settings (per-instance)
	instance, err := s.instanceStore.Get(ctx, candidate.InstanceID)
	if err != nil || instance == nil {
		// If we can't get the instance, we can't check if the mode is enabled.
		// This is not a link error, just return notUsed
		return notUsed
	}

	// Check if the mode is disabled for this instance - only case where we return notUsed
	if !mode.enabled(instance) {
		return notUsed
	}

	// From here on, link mode is ENABLED
	// Check if fallback is enabled - if so, errors return notUsed instead of <mode>_error
	fallbackEnabled := instance.FallbackToRegularMode

	// Helper to handle errors based on fallback setting
	handleError := func(message string) linkModeResult {
		if fallbackEnabled {
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("reason", message).
				Msg("[CROSSSEED] " + label + " mode failed, falling back to regular mode")
			return linkModeResult{FallbackToRegular: true}
		}
		return linkError(message)
	}
	handleFullRecheckFallback := func(message string) linkModeResult {
		if fallbackEnabled {
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("reason", message).
				Msg("[CROSSSEED] " + label + " mode filesystem fallback requires full regular-mode recheck")
			return linkModeResult{RequiresFullRecheck: true, FallbackToRegular: true}
		}
		return linkError(message)
	}
	handleMaterializationError := func(message string) linkModeResult {
		if fallbackEnabled {
			log.Warn().
				Int("instanceID", candidate.InstanceID).
				Str("reason", message).
				Msg("[CROSSSEED] " + label + " materialization failed; regular fallback disabled to avoid adding into the matched torrent path")
		}
		return linkError(message)
	}
	handlePlanError := handleMaterializationError
	handleCreateError := handleMaterializationError
	if mode.regularFallbackOnCreateError {
		handlePlanError = handleError
		handleCreateError = handleFullRecheckFallback
	}

	// Build LINKABLE source files list. The selector keeps rename-friendly
	// matching for content files but does not materialize sidecars on size alone.
	// Files omitted by the selector will be downloaded by qBittorrent after recheck.
	candidateTorrentFilesToLink := selectExistingSourceFiles(sourceFiles, candidateFiles)
	hasExtras := hasUnmaterializedSourceFiles(sourceFiles, candidateTorrentFilesToLink)
	verifyBeforeSeed := candidateRequiresVerification(candidate, matchedTorrent.Hash, req)

	// Early guard: if SkipRecheck is enabled and we have extras, or the match must
	// be verified first, skip before any plan building
	if req.SkipRecheck && (hasExtras || verifyBeforeSeed) {
		return linkModeResult{
			Used:    true,
			Success: false,
			Result: InstanceCrossSeedResult{
				InstanceID:   candidate.InstanceID,
				InstanceName: candidate.InstanceName,
				Success:      false,
				Status:       "skipped_recheck",
				Message:      skippedRecheckMessage,
			},
		}
	}

	// Validate base directory is configured (reflink mode reuses the hardlink base dir)
	if instance.HardlinkBaseDir == "" {
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Msg("[CROSSSEED] " + label + " mode enabled but base directory is empty")
		return handleError(label + " mode enabled but base directory is not configured")
	}

	// Verify instance has local filesystem access (required for links)
	if !instance.HasLocalFilesystemAccess {
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Msg("[CROSSSEED] " + label + " mode enabled but instance lacks local filesystem access")
		return handleError(fmt.Sprintf("Instance '%s' does not have local filesystem access enabled", candidate.InstanceName))
	}

	// Need a valid file path from matched torrent to check filesystem
	if len(candidateFiles) == 0 {
		return handleError("No candidate files available for " + mode.name + " matching")
	}

	if len(candidateTorrentFilesToLink) == 0 {
		return handleError("No linkable files found (all source files are extras)")
	}

	coverageThreshold := coverageThresholdFromTolerance(defaultSizeMismatchTolerancePercent)
	coverage, linkedBytes, totalBytes := materializedCoverage(sourceFiles, candidateTorrentFilesToLink)
	linkedFiles := len(candidateTorrentFilesToLink)
	totalFiles := len(sourceFiles)
	if hasExtras && coverage < coverageThreshold {
		message := belowThresholdMessage(mode.name, coverage, coverageThreshold, linkedBytes, totalBytes, linkedFiles, totalFiles)
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Float64("coverage", coverage).
			Float64("threshold", coverageThreshold).
			Int64("linkedBytes", linkedBytes).
			Int64("totalBytes", totalBytes).
			Int("linkedFiles", linkedFiles).
			Int("totalFiles", totalFiles).
			Msg(logPrefix + "skipping below-threshold match before add")
		return linkModeResult{
			Used:    true,
			Success: false,
			Result: InstanceCrossSeedResult{
				InstanceID:   candidate.InstanceID,
				InstanceName: candidate.InstanceName,
				Success:      false,
				Status:       "below_threshold",
				Message:      message,
			},
		}
	}
	resumeBudget := s.resumeBudgetBytes(ctx)

	backend, err := s.getBackendForInstance(ctx, candidate.InstanceID)
	if err != nil {
		return handleError(fmt.Sprintf("no filesystem backend: %v", err))
	}

	// Pick an actual matched file when available so symlinked file sources are
	// resolved before choosing the link base directory.
	existingFilePath, ok := matchedFilesystemProbePath(ctx, backend, matchedTorrent, props, candidateFiles)
	if !ok {
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Str("matchedHash", matchedTorrent.Hash).
			Msg(logPrefix + "no content path or save path available")
		return handleError("No content path or save path available for matched torrent")
	}

	selectedBaseDir, err := FindMatchingBaseDir(ctx, instance.HardlinkBaseDir, existingFilePath, backend)
	if err != nil {
		log.Warn().
			Err(err).
			Str("configuredDirs", instance.HardlinkBaseDir).
			Str("existingPath", existingFilePath).
			Msg(logPrefix + "no suitable base directory found")
		return handleFullRecheckFallback(fmt.Sprintf("No suitable base directory: %v", err))
	}

	// Link mode always uses Original layout to match the incoming torrent's structure exactly.
	// We'll also set contentLayout=Original when adding the torrent to qBittorrent to avoid
	// double-folder nesting issues when the instance default is Subfolder.
	layout := hardlinktree.LayoutOriginal

	// Build ALL source files list (for destDir calculation - reflects full torrent structure)
	candidateTorrentFilesAll := make([]hardlinktree.TorrentFile, 0, len(sourceFiles))
	for _, f := range sourceFiles {
		candidateTorrentFilesAll = append(candidateTorrentFilesAll, hardlinktree.TorrentFile{
			Path: f.Name,
			Size: f.Size,
		})
	}

	// Extract incoming tracker domain from torrent bytes (for "by-tracker" preset)
	incomingTrackerDomain := ParseTorrentAnnounceDomain(torrentBytes)

	// Build destination directory based on preset and torrent structure
	destDir := s.buildHardlinkDestDir(ctx, instance, selectedBaseDir, torrentHash, torrentName, candidate, incomingTrackerDomain, req, candidateTorrentFilesAll)

	// Ensure cross-seed category exists with the correct save path derived from
	// the base directory and directory preset, rather than the matched torrent's save path.
	categoryCreationFailed := false
	if crossCategory != "" {
		categorySavePath := s.buildCategorySavePath(ctx, instance, selectedBaseDir, incomingTrackerDomain, candidate, req)
		if _, err := s.ensureCrossCategory(ctx, candidate.InstanceID, crossCategory, categorySavePath, false); err != nil {
			log.Warn().Err(err).
				Str("category", crossCategory).
				Str("savePath", categorySavePath).
				Msg(logPrefix + "failed to ensure category exists, continuing without category")
			crossCategory = ""
			categoryCreationFailed = true
		}
	}

	// Build existing files list (all files on disk from matched torrent).
	// We pass all existing files to BuildPlan so it can use path/name matching
	// to select the correct source file for each target.
	existingFiles := make([]hardlinktree.ExistingFile, 0, len(candidateFiles))
	for _, f := range candidateFiles {
		existingFiles = append(existingFiles, hardlinktree.ExistingFile{
			AbsPath: filepath.Join(props.SavePath, f.Name),
			RelPath: f.Name,
			Size:    f.Size,
		})
	}

	// Build link tree plan with only the linkable files
	plan, err := hardlinktree.BuildPlan(candidateTorrentFilesToLink, existingFiles, layout, torrentName, destDir)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Str("destDir", destDir).
			Msg(logPrefix + "failed to build plan, aborting")
		return handlePlanError(fmt.Sprintf("Failed to build %s plan: %v", mode.name, err))
	}
	addPolicy := PolicyForSourceFiles(sourceFiles)
	recheckPolicy := linkModeRecheckPolicy(hasExtras, verifyBeforeSeed, addPolicy.DiscLayout)
	pooledCompletion := s.partialPoolAdmissionEnabled(ctx, instance, hasExtras, req, recheckPolicy.requireComplete)
	var poolDescriptors []partialPoolFileDescriptor
	var poolDescriptorErr error
	var poolReplaceablePaths map[string]struct{}
	if pooledCompletion {
		_, _, _, poolDescriptors, poolDescriptorErr = partialPoolParsedIdentity(torrentBytes)
	}

	// Materialize only after the coverage and plan gates so clearly invalid
	// partial matches are skipped before probing filesystem capabilities.
	created, err := mode.materialize(ctx, backend, selectedBaseDir, plan)
	if unsupportedErr, ok := errors.AsType[*linkUnsupportedError](err); ok {
		log.Warn().
			Str("reason", unsupportedErr.reason).
			Str("baseDir", selectedBaseDir).
			Msg(logPrefix + "filesystem does not support " + mode.name + "s")
		return handleFullRecheckFallback(label + " not supported: " + unsupportedErr.reason)
	}
	if err != nil {
		logEvent := log.Error()
		if shouldWarnForReflinkCreateError(err) {
			logEvent = log.Warn()
		}
		logEvent.
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Str("destDir", destDir).
			Msg(logPrefix + "failed to create " + mode.name + " tree, aborting")
		return handleCreateError(fmt.Sprintf("Failed to create %s tree: %v", mode.name, err))
	}

	log.Info().
		Int("instanceID", candidate.InstanceID).
		Str("torrentName", torrentName).
		Str("destDir", destDir).
		Int("fileCount", len(plan.Files)).
		Msg(logPrefix + "created " + mode.name + " tree")

	if pooledCompletion && poolDescriptorErr == nil && mode.poolReplaceable {
		poolReplaceablePaths = partialPoolReplaceableTargets(plan.RootDir, poolDescriptors)
	}

	// Build options for adding torrent
	options := make(map[string]string)

	// Set category if available
	if crossCategory != "" {
		options["category"] = crossCategory
	}

	// Set tags
	finalTags := buildCrossSeedTags(req.Tags, matchedTorrent.Tags, req.InheritSourceTags)
	if len(finalTags) > 0 {
		options["tags"] = strings.Join(finalTags, ",")
	}

	// Link mode: files are pre-created, so use savepath pointing to tree root
	// Force contentLayout=Original to match the link tree layout exactly
	// and avoid double-folder nesting when instance default is Subfolder
	options["autoTMM"] = "false"
	options["savepath"] = plan.RootDir
	options["contentLayout"] = "Original"

	if addPolicy.DiscLayout {
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Str("torrentHash", torrentHash).
			Str("discMarker", addPolicy.DiscMarker).
			Msg(logPrefix + "disc layout detected - torrent will be added paused and only resumed after full recheck")
	}

	if req.SkipRecheck && addPolicy.DiscLayout {
		return linkModeResult{
			Used:    true,
			Success: false,
			Result: InstanceCrossSeedResult{
				InstanceID:   candidate.InstanceID,
				InstanceName: candidate.InstanceName,
				Success:      false,
				Status:       "skipped_recheck",
				Message:      skippedRecheckMessage,
			},
		}
	}

	// Handle skip_checking and pause behavior based on extras:
	// - No extras: skip_checking=true, start immediately (100% complete)
	// - With extras: skip_checking=true, add paused, then recheck to find missing pieces
	// - Verify-before-seed matches (title rescue, relaxed season/episode/group): same as extras
	// - Disc layout: policy will override to paused via ApplyToAddOptions
	options["skip_checking"] = "true"
	switch {
	case recheckPolicy.requiresRecheck:
		// With extras, or a match that must be verified first: add paused, then
		// trigger recheck.
		options["stopped"] = "true"
		options["paused"] = "true"
	case req.SkipAutoResume:
		// No extras but user wants paused
		options["stopped"] = "true"
		options["paused"] = "true"
	default:
		// No extras: start immediately
		options["stopped"] = "false"
		options["paused"] = "false"
	}

	// Apply add policy (e.g., disc layout forces paused)
	addPolicy.ApplyToAddOptions(options)

	log.Debug().
		Int("instanceID", candidate.InstanceID).
		Str("torrentName", torrentName).
		Str("savepath", plan.RootDir).
		Str("category", crossCategory).
		Bool("hasExtras", hasExtras).
		Bool("discLayout", addPolicy.DiscLayout).
		Int("linkedFiles", linkedFiles).
		Int("totalFiles", totalFiles).
		Msg(logPrefix + "adding torrent")

	// Add the torrent
	addResponse, err := s.syncManager.AddTorrent(ctx, candidate.InstanceID, torrentBytes, options)
	if err != nil {
		// Rollback only what this attempt created: the destination can be shared
		// with an earlier successful add for the same release (discussion #2282).
		// WithoutCancel: a cancelled run must still roll back its partial tree.
		if rollbackErr := backend.RemoveTree(context.WithoutCancel(ctx), created); rollbackErr != nil {
			log.Warn().
				Err(rollbackErr).
				Str("destDir", destDir).
				Msg(logPrefix + "failed to rollback " + mode.name + " tree")
		}
		log.Error().
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Int("rolledBackFiles", len(created.Files)).
			Msg(logPrefix + "failed to add torrent, aborting")
		return handleError(fmt.Sprintf("Failed to add torrent: %v", err))
	}
	var addedTorrentIDs []string
	if addResponse != nil {
		addedTorrentIDs = append([]string(nil), addResponse.AddedTorrentIds...)
	}

	var pooledMember *models.CrossSeedPartialPoolMember
	var poolRegistrationErr error
	if pooledCompletion {
		if poolDescriptorErr != nil {
			poolRegistrationErr = fmt.Errorf("describe partial pool files: %w", poolDescriptorErr)
		} else {
			_, pooledMember, poolRegistrationErr = s.registerPartialPoolAdmission(
				ctx,
				candidate,
				torrentBytes,
				addedTorrentIDs,
				req,
				matchedTorrent,
				mode.poolMode,
				plan.RootDir,
				candidateTorrentFilesToLink,
				poolReplaceablePaths,
				poolDescriptors,
			)
		}
	}
	if poolRegistrationErr != nil {
		poolRegistrationErr = fmt.Errorf("register partial pool admission: %w", poolRegistrationErr)
		log.Warn().
			Err(poolRegistrationErr).
			Str("mode", mode.poolMode).
			Int("instanceID", candidate.InstanceID).
			Str("torrentHash", torrentHash).
			Msg("[CROSSSEED] Partial pool registration failed after qBittorrent add")
	}

	// Build result message
	var statusMsg string
	switch {
	case categoryCreationFailed:
		statusMsg = fmt.Sprintf("Added via %s mode WITHOUT category isolation (match: %s, files: %d/%d)", mode.name, matchType, linkedFiles, totalFiles)
	case crossCategory != "":
		statusMsg = fmt.Sprintf("Added via %s mode (match: %s, category: %s, files: %d/%d)", mode.name, matchType, crossCategory, linkedFiles, totalFiles)
	default:
		statusMsg = fmt.Sprintf("Added via %s mode (match: %s, files: %d/%d)", mode.name, matchType, linkedFiles, totalFiles)
	}
	if addPolicy.DiscLayout {
		statusMsg += addPolicy.StatusSuffix()
	}

	// Handle recheck and auto-resume when extras exist, or disc layout or the
	// search decision requires verification
	if recheckPolicy.requiresRecheck {
		switch {
		case poolRegistrationErr != nil:
		case pooledMember != nil:
			s.signalPartialPoolWake(partialPoolWake{poolID: pooledMember.PoolID})
			statusMsg += " - pooled completion pending"
		default:
			recheckHashes := []string{torrentHash}
			if torrentHashV2 != "" && !strings.EqualFold(torrentHash, torrentHashV2) {
				recheckHashes = append(recheckHashes, torrentHashV2)
			}

			// Trigger recheck so qBittorrent discovers which pieces are present (linked)
			// and which are missing (extras to download)
			recheckCtx := qbittorrent.WithPostAddBulkActionRetry(ctx)
			recheckErr := s.syncManager.BulkAction(recheckCtx, candidate.InstanceID, recheckHashes, "recheck")
			switch {
			case recheckErr != nil:
				log.Warn().
					Err(recheckErr).
					Int("instanceID", candidate.InstanceID).
					Str("torrentHash", torrentHash).
					Msg(logPrefix + "failed to trigger recheck after add")
				statusMsg += " - recheck failed, manual intervention required"
			case req.SkipAutoResume:
				statusMsg += s.titleRescueMonitorSuffix(candidate.titleRescue, candidate.InstanceID, torrentHash)
				// User requested to skip auto-resume - leave paused after recheck
				log.Debug().
					Int("instanceID", candidate.InstanceID).
					Str("torrentHash", torrentHash).
					Msg(logPrefix + "skipping auto-resume per user settings")
				statusMsg += " - auto-resume skipped per settings"
			default:
				// Queue for background resume - worker will resume when recheck completes within budget
				log.Debug().
					Int("instanceID", candidate.InstanceID).
					Str("torrentHash", torrentHash).
					Int("extraFiles", totalFiles-linkedFiles).
					Msg(logPrefix + "queuing torrent for recheck resume")
				var linkedPaths map[string]struct{}
				if mode.linkedPathsForResume {
					linkedPaths = linkedTreePaths(candidateTorrentFilesToLink)
				}
				queueErr := error(nil)
				switch {
				case verifyBeforeSeed:
					queueErr = s.queueVerificationRecheckResume(candidate.InstanceID, torrentHash)
				case recheckPolicy.requireComplete:
					queueErr = s.queueRecheckResumeWithBudget(candidate.InstanceID, torrentHash, 0, false, linkedPaths)
				default:
					queueErr = s.queueRecheckResumeWithBudget(candidate.InstanceID, torrentHash, resumeBudget, mode.recoverMissingFilesWithResume, linkedPaths)
				}
				if queueErr != nil {
					statusMsg += " - auto-resume queue full, manual resume required"
				}
			}
		}
	}
	if poolRegistrationErr != nil {
		statusMsg += fmt.Sprintf(" - qBittorrent added torrent, but pooled registration failed: %v; torrent remains stopped for manual intervention", poolRegistrationErr)
	}

	// Add note about low completion behavior
	if mode.belowThresholdNote && hasExtras {
		statusMsg += " (below threshold = remains paused for manual review)"
	}

	s.runPostInjectionHooks(ctx, candidate.InstanceID, torrentHash)
	success := poolRegistrationErr == nil
	status := "added_" + mode.name
	if !success {
		status = partialPoolRegistrationErrorStatus
	}

	return linkModeResult{
		Used:    true,
		Success: success,
		Result: InstanceCrossSeedResult{
			InstanceID:         candidate.InstanceID,
			InstanceName:       candidate.InstanceName,
			Success:            success,
			Status:             status,
			Message:            statusMsg,
			partialPoolPending: pooledMember != nil,
			MatchedTorrent: &MatchedTorrent{
				Hash:     matchedTorrent.Hash,
				Name:     matchedTorrent.Name,
				Progress: matchedTorrent.Progress,
				Size:     matchedTorrent.Size,
			},
		},
	}
}
