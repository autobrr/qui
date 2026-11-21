// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/linktree"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

// linkModeResult represents the outcome of hardlink or reflink mode processing.
type linkModeResult struct {
	// Used indicates whether link mode was used for this cross-seed.
	Used bool
	// Success indicates the link mode completed successfully.
	Success bool
	// RequiresFullRecheck indicates that fallback to regular mode is allowed,
	// but the regular add must only auto-resume after a full 100% recheck.
	RequiresFullRecheck bool
	// FallbackToRegular indicates link mode explicitly fell through to regular mode.
	FallbackToRegular bool
	// Result is the final InstanceCrossSeedResult when link mode is used.
	// Only valid when Used is true.
	Result InstanceCrossSeedResult
}

// linkMode lists everything that differs between hardlink and reflink mode.
// processLinkMode is the one body behind both; see docs/adr/0006.
type linkMode struct {
	name string
	// addedStatus and errorStatus are the result statuses of the mode, written
	// out so that a code search finds each status.
	addedStatus, errorStatus string
	enabled                  func(*models.Instance) bool
	// poolMode is the persisted partial-pool mode value, kept apart from name so
	// a display rename never changes a stored row.
	poolMode string
	// poolReplaceable allows the pool to replace placeholders that qBittorrent
	// creates at paths missing before the add. Existing files stay protected.
	poolReplaceable bool
	// linkedPathsForResume hands the linked file set to Settle,
	// which refuses to download into a linked file (ADR 0004). A reflink clone is
	// copy-on-write, so reflink mode stays exempt.
	linkedPathsForResume bool
	// recoverMissingFilesWithResume lets Settle nudge a missingFiles torrent
	// back to life.
	recoverMissingFilesWithResume bool
	// regularFallbackOnCreateError falls back to regular mode when the plan build
	// or the tree creation fails. Reflink mode refuses regular fallback there so
	// the add never lands inside the matched torrent's path.
	regularFallbackOnCreateError bool
}

func (s *Service) processHardlinkMode(
	ctx context.Context,
	check PreAddCheck,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentName string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	matchType string,
	sourceFiles, candidateFiles qbt.TorrentFiles,
	props *qbt.TorrentProperties,
	baseCategory, crossCategory string,
) linkModeResult {
	return s.processLinkMode(ctx, check, candidate, torrentBytes, torrentHash, torrentName, req,
		matchedTorrent, matchType, sourceFiles, candidateFiles, props, baseCategory, crossCategory,
		linkMode{
			name:                         "hardlink",
			addedStatus:                  "added_hardlink",
			errorStatus:                  "hardlink_error",
			enabled:                      func(instance *models.Instance) bool { return instance.UseHardlinks },
			poolMode:                     models.CrossSeedPartialPoolModeHardlink,
			linkedPathsForResume:         true,
			regularFallbackOnCreateError: true,
		})
}

func (s *Service) processReflinkMode(
	ctx context.Context,
	check PreAddCheck,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentName string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	matchType string,
	sourceFiles, candidateFiles qbt.TorrentFiles,
	props *qbt.TorrentProperties,
	baseCategory, crossCategory string,
) linkModeResult {
	// A reflink clone is copy-on-write, so qBittorrent can repair bytes in the
	// clone without touching the source. That is why reflink mode skips the
	// linked-file resume gate and never falls back to regular mode on a create error.
	return s.processLinkMode(ctx, check, candidate, torrentBytes, torrentHash, torrentName, req,
		matchedTorrent, matchType, sourceFiles, candidateFiles, props, baseCategory, crossCategory,
		linkMode{
			name:                          "reflink",
			addedStatus:                   "added_reflink",
			errorStatus:                   "reflink_error",
			enabled:                       func(instance *models.Instance) bool { return instance.UseReflinks },
			poolMode:                      models.CrossSeedPartialPoolModeReflink,
			poolReplaceable:               true,
			recoverMissingFilesWithResume: true,
		})
}

func shouldWarnForReflinkCreateError(err error) bool {
	if !errors.Is(err, reflinktree.ErrReflinkUnsupported) {
		return false
	}

	type multiUnwrapper interface {
		error
		Unwrap() []error
	}

	_, joined := errors.AsType[multiUnwrapper](err)
	return !joined
}

// linkSources finds the source file of each torrent file to link: the
// validated pairs of a catalog match, or else the match by path, name, and
// size.
func linkSources(d fsops.PathDialect, pairs []catalogFilePair, toLink []hardlinktree.TorrentFile, existing []hardlinktree.ExistingFile) ([]linktree.Link, error) {
	if len(pairs) > 0 {
		return catalogLinks(d, pairs, existing)
	}
	matched, err := hardlinktree.MatchFiles(toLink, existing)
	if err != nil {
		return nil, err
	}
	links := make([]linktree.Link, 0, len(matched))
	for _, m := range matched {
		links = append(links, linktree.Link{TorrentPath: m.RelPath, SourcePath: m.AbsPath})
	}
	return links, nil
}

type singleAddTorrent struct {
	hash, name                 string
	files                      qbt.TorrentFiles
	trackerDomain, indexerName string
}

func (s *Service) planSingleAddTree(
	ctx context.Context,
	backend fsops.Backend,
	instance *models.Instance,
	candidate CrossSeedCandidate,
	torrent singleAddTorrent,
	toLink []hardlinktree.TorrentFile,
	matchedFiles qbt.TorrentFiles,
	savePath string,
) (*linktree.TreePlan, error) {
	d := backend.Paths()
	existing := make([]hardlinktree.ExistingFile, 0, len(matchedFiles))
	for _, f := range matchedFiles {
		existing = append(existing, hardlinktree.ExistingFile{
			AbsPath: d.Join(savePath, d.FromSlash(f.Name)),
			RelPath: f.Name,
			Size:    f.Size,
		})
	}
	links, err := linkSources(d, candidate.filePairs, toLink, existing)
	if err != nil {
		return nil, &linktree.Error{Kind: linktree.InvalidPlan, Err: err}
	}

	torrentFiles := make([]string, 0, len(torrent.files))
	for _, f := range torrent.files {
		torrentFiles = append(torrentFiles, f.Name)
	}
	cfg, _ := linktree.ConfigOf(instance)
	var trackerFolder string
	if cfg.Preset == linktree.PresetByTracker {
		trackerFolder = linktree.TrackerFolderName(ctx, s.trackerCustomizationStore, torrent.trackerDomain, torrent.indexerName)
	}
	_, receiverName := candidate.receiver()
	return linktree.Plan(ctx, linktree.Request{
		Backend:        backend,
		Config:         cfg,
		TrackerFolder:  trackerFolder,
		InstanceFolder: receiverName,
		Torrent:        linktree.Torrent{InfoHash: torrent.hash, Name: torrent.name, Files: torrentFiles},
		Links:          links,
	})
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
	check PreAddCheck,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	torrentHash string,
	torrentName string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	matchType string,
	sourceFiles, candidateFiles qbt.TorrentFiles,
	props *qbt.TorrentProperties,
	_, crossCategory string, // baseCategory unused, crossCategory used for torrent options
	mode linkMode,
) linkModeResult {
	logPrefix := "[CROSSSEED] " + mode.name + " mode: "

	// failure is a Used=true result that stops the dispatcher from trying regular mode.
	failure := func(status, message string) linkModeResult {
		return linkModeResult{Used: true, Result: candidate.newResult(status, message)}
	}
	linkError := func(message string) linkModeResult { return failure(mode.errorStatus, message) }

	var instance *models.Instance
	var err error
	if candidate.route != nil {
		// A routed add uses the Source instance's row from its one capability read.
		instance = candidate.route.source
	} else if instance, err = s.instanceStore.Get(ctx, candidate.InstanceID); err != nil || instance == nil {
		// If we can't get the instance, we can't check if the mode is enabled.
		// This is not a link error, just return linkModeResult{}
		return linkModeResult{}
	}

	// Check if the mode is disabled for this instance - only case where we return linkModeResult{}
	if !mode.enabled(instance) {
		return linkModeResult{}
	}

	// Reject unsafe paths before any error can fall back to regular mode.
	for _, file := range candidateFiles {
		if _, ok := safeTorrentRelativeFilePath(file.Name); !ok {
			return linkError(fmt.Sprintf("Unsafe candidate file path: %q", file.Name))
		}
	}

	// From here on, link mode is ENABLED
	// Check if fallback is enabled - if so, errors return linkModeResult{} instead of <mode>_error.
	// A routed add ignores the setting and never falls back (ADR 0035).
	fallbackEnabled := instance.FallbackToRegularMode && candidate.route == nil

	// Helper to handle errors based on fallback setting
	handleError := func(message string) linkModeResult {
		if fallbackEnabled {
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("reason", message).
				Msg(logPrefix + "failed, falling back to regular mode")
			return linkModeResult{FallbackToRegular: true}
		}
		return linkError(message)
	}
	handleFullRecheckFallback := func(message string) linkModeResult {
		if fallbackEnabled {
			log.Info().
				Int("instanceID", candidate.InstanceID).
				Str("reason", message).
				Msg(logPrefix + "filesystem fallback requires full regular-mode recheck")
			return linkModeResult{RequiresFullRecheck: true, FallbackToRegular: true}
		}
		return linkError(message)
	}
	handleMaterializationError := func(message string) linkModeResult {
		if fallbackEnabled {
			log.Warn().
				Int("instanceID", candidate.InstanceID).
				Str("reason", message).
				Msg(logPrefix + "materialization failed; regular fallback disabled to avoid adding into the matched torrent path")
		}
		return linkError(message)
	}
	handlePlanError, handleCreateError := handleMaterializationError, handleMaterializationError
	if mode.regularFallbackOnCreateError {
		handlePlanError, handleCreateError = handleError, handleFullRecheckFallback
	}

	// Build LINKABLE source files list. The selector keeps rename-friendly
	// matching for content files but does not materialize sidecars on size alone.
	// Files omitted by the selector will be downloaded by qBittorrent after recheck.
	candidateTorrentFilesToLink := selectExistingSourceFiles(sourceFiles, candidateFiles)
	if len(candidate.filePairs) > 0 {
		candidateTorrentFilesToLink = make([]hardlinktree.TorrentFile, 0, len(candidate.filePairs))
		for _, pair := range candidate.filePairs {
			candidateTorrentFilesToLink = append(candidateTorrentFilesToLink, hardlinktree.TorrentFile{Path: pair.sourcePath, Size: sourceFiles[pair.sourceIndex].Size})
		}
	}
	hasExtras := hasUnmaterializedSourceFiles(sourceFiles, candidateTorrentFilesToLink)
	addPolicy := PolicyForSourceFiles(sourceFiles)
	facts := AddFacts{
		LinkTree:            true,
		PendingFiles:        hasExtras,
		DiscMarker:          addPolicy.DiscMarker,
		Verify:              verifyReason(candidate, matchedTorrent.Hash, req),
		RecoverMissingFiles: mode.recoverMissingFilesWithResume,
		SkipRecheck:         req.SkipRecheck,
		SkipAutoResume:      req.SkipAutoResume,
	}

	// Refuse before any plan building, so a refused add leaves no link tree.
	if v := facts.Verdict(); v.Refused {
		return failure(v.Status, v.Message)
	}

	// Validate base directory is configured (reflink mode reuses the hardlink base dir)
	if instance.HardlinkBaseDir == "" {
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Msg(logPrefix + "enabled but base directory is empty")
		return handleError(mode.name + " mode enabled but base directory is not configured")
	}

	// Verify qui may write link trees on the instance
	if !models.FilesystemCapabilitiesOf(instance).Write {
		log.Warn().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Msg(logPrefix + "enabled but instance lacks filesystem write access")
		return handleError(fmt.Sprintf("Instance '%s' does not have filesystem write access", candidate.InstanceName))
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
		return failure("below_threshold", message)
	}

	var backend fsops.Backend
	if candidate.route != nil {
		backend = candidate.route.backend
	} else if backend, err = s.getBackendForInstance(ctx, candidate.InstanceID, models.CapabilityWrite); err != nil {
		return handleError(fmt.Sprintf("no filesystem backend: %v", err))
	}
	d := backend.Paths()

	// The partial pool registers its members on the Source instance, so a routed add stays out.
	pooledCompletion := candidate.route == nil && s.partialPool.joinEnabled(ctx, instance, hasExtras, req, facts.Verify != VerifyNone || addPolicy.DiscLayout)
	var poolDescriptors []partialPoolFileDescriptor
	var poolDescriptorErr error
	var poolReplaceablePaths map[string]struct{}
	if pooledCompletion {
		_, _, _, poolDescriptors, poolDescriptorErr = partialPoolParsedIdentity(torrentBytes)
	}

	const planFailed = "failed to build plan, aborting"
	logLinkError := func(level zerolog.Level, err error, what string) {
		log.WithLevel(level).
			Err(err).
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Str("configuredDirs", instance.HardlinkBaseDir).
			Msg(logPrefix + what)
	}

	plan, err := s.planSingleAddTree(ctx, backend, instance, candidate, singleAddTorrent{
		hash:          torrentHash,
		name:          torrentName,
		files:         sourceFiles,
		trackerDomain: ParseTorrentAnnounceDomain(torrentBytes),
		indexerName:   req.IndexerName,
	}, candidateTorrentFilesToLink, candidateFiles, props.SavePath)
	var tree *linktree.Tree
	if err == nil {
		tree, err = plan.Create(ctx)
	}
	if treeErr, failed := errors.AsType[*linktree.Error](err); failed {
		message := fmt.Sprintf("Failed to create %s tree: %v", mode.name, treeErr.Err)
		handle, level, what := handleCreateError, zerolog.ErrorLevel, "failed to create link tree, aborting"
		switch treeErr.Kind {
		case linktree.NoBaseDir:
			message, handle = fmt.Sprintf("No suitable base directory: %v", treeErr.Err), handleFullRecheckFallback
			level, what = zerolog.WarnLevel, "no suitable base directory found"
		case linktree.NotSupported:
			message, handle = fmt.Sprintf("%s not supported: %v", mode.name, treeErr.Err), handleFullRecheckFallback
			level, what = zerolog.WarnLevel, "filesystem does not support "+mode.name
		case linktree.InvalidPlan:
			message, handle, what = fmt.Sprintf("Failed to build %s plan: %v", mode.name, treeErr.Err), handlePlanError, planFailed
		default:
			if shouldWarnForReflinkCreateError(treeErr.Err) {
				level = zerolog.WarnLevel
			}
		}
		if treeErr.Partial {
			message += "; a partial link tree can remain in " + treeErr.Dir
		}
		// The target dir already holds a file: a crashed earlier run or a
		// second tracker sharing the link path. Falling back would add the
		// torrent in regular mode inside the matched torrent's path and leave
		// the partial tree, so refuse and let the user clean up (#2942). The
		// cause, not the kind, decides: a lost connection on another base
		// directory changes the kind.
		if errors.Is(treeErr.Err, hardlinktree.ErrTargetExists) {
			handle = handleMaterializationError
		}
		logLinkError(level, err, what)
		return handle(message)
	}

	// Rollback removes only what this add created: the destination can be shared
	// with an earlier successful add for the same release (discussion #2282).
	rollback := func() {
		if rollbackErr := tree.Rollback(ctx); rollbackErr != nil {
			log.Warn().
				Err(rollbackErr).
				Str("destDir", tree.SavePath).
				Msg(logPrefix + "failed to rollback link tree")
		}
	}

	// savePath is the save path of the tree, mapped to the Cross-seed instance for a routed add.
	savePath := tree.SavePath
	if candidate.route != nil {
		var mapped bool
		if savePath, mapped = crossseedroute.ToCrossSeed(d, candidate.route.rows, tree.BaseDir, tree.SavePath); !mapped {
			rollback()
			err := fmt.Errorf("the cross-seed route has no row for base folder %s", tree.BaseDir)
			logLinkError(zerolog.ErrorLevel, err, planFailed)
			return handlePlanError(fmt.Sprintf("Failed to build %s plan: %v", mode.name, err))
		}
		if status, message := s.checkVisible(ctx, candidate.route, d, tree.BaseDir, tree.Files[0].LinkPath); status != "" {
			rollback()
			log.Warn().
				Int("instanceID", candidate.InstanceID).
				Str("torrentName", torrentName).
				Str("reason", message).
				Msg(logPrefix + "routed add stopped by the visibility check")
			return failure(status, message)
		}
	}

	// Ensure cross-seed category exists with the correct save path derived from
	// the base directory and directory preset, rather than the matched torrent's save path.
	categoryCreationFailed := false
	if crossCategory != "" {
		categorySavePath := linkCategorySavePath(d, tree, candidate.route)
		receiverID, _ := candidate.receiver()
		if _, err := s.ensureCrossCategory(ctx, receiverID, crossCategory, categorySavePath, false); err != nil {
			log.Warn().Err(err).
				Str("category", crossCategory).
				Str("savePath", categorySavePath).
				Msg(logPrefix + "failed to ensure category exists, continuing without category")
			crossCategory = ""
			categoryCreationFailed = true
		}
	}

	createdFiles := 0
	for _, f := range tree.Files {
		if f.Created {
			createdFiles++
		}
	}
	log.Info().
		Int("instanceID", candidate.InstanceID).
		Str("torrentName", torrentName).
		Str("destDir", tree.SavePath).
		Int("fileCount", len(tree.Files)).
		Int("skippedExists", len(tree.Files)-createdFiles).
		Msg(logPrefix + "created link tree")

	if pooledCompletion && poolDescriptorErr == nil && mode.poolReplaceable {
		poolReplaceablePaths = partialPoolReplaceableTargets(tree.SavePath, poolDescriptors)
	}

	// Link mode: files are pre-created, so the save path points to the tree root.
	// Force contentLayout=Original to match the link tree layout exactly
	// and avoid double-folder nesting when instance default is Subfolder.
	placement := AddPlacement{
		SavePath:      savePath,
		Category:      crossCategory,
		Tags:          buildCrossSeedTags(req.Tags, matchedTorrent.Tags, req.InheritSourceTags),
		ContentLayout: "Original",
	}

	if addPolicy.DiscLayout {
		log.Info().
			Int("instanceID", candidate.InstanceID).
			Str("instanceName", candidate.InstanceName).
			Str("torrentHash", torrentHash).
			Str("discMarker", addPolicy.DiscMarker).
			Msg(logPrefix + "disc layout detected - torrent will be added paused and only resumed after full recheck")
	}

	log.Debug().
		Int("instanceID", candidate.InstanceID).
		Str("torrentName", torrentName).
		Str("savepath", savePath).
		Str("category", crossCategory).
		Bool("hasExtras", hasExtras).
		Bool("discLayout", addPolicy.DiscLayout).
		Int("linkedFiles", linkedFiles).
		Int("totalFiles", totalFiles).
		Msg(logPrefix + "adding torrent")

	facts.PoolMember = pooledCompletion
	if mode.linkedPathsForResume {
		facts.LinkedPaths = linkedTreePaths(candidateTorrentFilesToLink)
	}
	var pooledMember *models.CrossSeedPartialPoolMember
	var poolRegistrationErr error
	added := s.adder.Add(ctx, AddRequest{
		Check:     check,
		Torrent:   torrentBytes,
		Facts:     facts,
		Placement: placement,
		AfterAdd: func(ctx context.Context, addedTorrentIDs []string) (string, bool) {
			if !pooledCompletion {
				return "", true
			}
			if poolDescriptorErr != nil {
				poolRegistrationErr = fmt.Errorf("describe partial pool files: %w", poolDescriptorErr)
			} else {
				_, pooledMember, poolRegistrationErr = s.partialPool.join(
					ctx,
					candidate,
					torrentBytes,
					addedTorrentIDs,
					req,
					matchedTorrent,
					mode.poolMode,
					tree.SavePath,
					candidateTorrentFilesToLink,
					poolReplaceablePaths,
					poolDescriptors,
				)
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
			return "", poolRegistrationErr == nil
		},
	})
	if added.Outcome == AddRefused {
		rollback()
		log.Error().
			Str("reason", added.Message).
			Int("instanceID", candidate.InstanceID).
			Str("torrentName", torrentName).
			Int("rolledBackFiles", createdFiles).
			Msg(logPrefix + "failed to add torrent, aborting")
		return handleError(added.Message)
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
	statusMsg += added.Message
	if added.Outcome == AddPooled {
		s.partialPool.wake(partialPoolWake{poolID: pooledMember.PoolID})
		statusMsg += " - pooled completion pending"
	}
	// Add note about low completion behavior
	if hasExtras {
		statusMsg += " (below threshold = remains paused for manual review)"
	}
	if poolRegistrationErr != nil {
		statusMsg += fmt.Sprintf(" - qBittorrent added torrent, but pooled registration failed: %v; torrent remains stopped for manual intervention", poolRegistrationErr)
	}

	success := poolRegistrationErr == nil
	status := mode.addedStatus
	if !success {
		status = partialPoolRegistrationErrorStatus
	}

	result := candidate.newResult(status, statusMsg)
	result.Success = success
	result.partialPoolPending = pooledMember != nil
	result.MatchedTorrent = &MatchedTorrent{
		Hash:     matchedTorrent.Hash,
		Name:     matchedTorrent.Name,
		Progress: matchedTorrent.Progress,
		Size:     matchedTorrent.Size,
	}
	return linkModeResult{Used: true, Success: success, Result: result}
}
