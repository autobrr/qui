// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/fsutil"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

// videoExtensions lists playable video file extensions used to identify
// episode files inside a season pack torrent.
var videoExtensions = map[string]struct{}{
	".mkv":  {},
	".mp4":  {},
	".avi":  {},
	".ts":   {},
	".m2ts": {},
	".wmv":  {},
	".flv":  {},
	".mov":  {},
}

// errLayoutMismatch signals that pack files could not be mapped to local episodes.
var errLayoutMismatch = errors.New("layout_mismatch")

// errSkippedRecheck signals a partial season pack that requires recheck, but recheck is disabled.
var errSkippedRecheck = errors.New("skipped_recheck")

// episodeIdentity uniquely identifies an episode within a show by season and episode number.
type episodeIdentity struct {
	series  int
	episode int
}

// episodeMatch records which local torrent provides a matched episode.
type episodeMatch struct {
	torrentHash string
	contentPath string // absolute path to the torrent content on disk
	category    string
	release     *rls.Release
}

type seasonPackLocalFile struct {
	sourcePath string
	size       int64
	release    *rls.Release
}

type seasonPackPlanBuild struct {
	plan              *hardlinktree.TreePlan
	materializedPaths map[string]struct{}
	linkedBytes       int64
	totalBytes        int64
	totalFiles        int
}

type seasonPackMatchOptions struct {
	skipRepackCompare  bool
	simplifyHDRCompare bool
	simplifyWEBCompare bool
	skipYearCompare    bool
}

func (b *seasonPackPlanBuild) hasPendingFiles() bool {
	if b == nil {
		return false
	}
	return len(b.materializedPaths) < b.totalFiles
}

func seasonPackMatchOptionsFromSettings(settings *models.CrossSeedAutomationSettings) seasonPackMatchOptions {
	opts := seasonPackMatchOptions{
		skipRepackCompare: true,
	}
	if settings == nil {
		return opts
	}

	opts.skipRepackCompare = settings.SeasonPackSkipRepackCompare
	opts.simplifyHDRCompare = settings.SeasonPackSimplifyHDRCompare
	opts.simplifyWEBCompare = settings.SeasonPackSimplifyWEBCompare
	opts.skipYearCompare = settings.SeasonPackSkipYearCompare

	return opts
}

// seasonPackPrep holds validated and parsed state shared between check and apply.
type seasonPackPrep struct {
	settings      *models.CrossSeedAutomationSettings
	packRelease   *rls.Release
	meta          TorrentMetadata
	torrentBytes  []byte // raw .torrent file content for AddTorrent
	packEpisodes  map[episodeIdentity]struct{}
	totalEpisodes int
	eligible      []*models.Instance
	threshold     float64
}

// prepareSeasonPack runs the shared validation pipeline for check and apply.
// Returns (nil, reason, message, nil) on expected early exit, or (nil, "", "", err) on internal error.
func (s *Service) prepareSeasonPack(ctx context.Context, torrentName, torrentData string, instanceIDs []int) (*seasonPackPrep, string, string, error) {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("load automation settings: %w", err)
	}
	if !settings.SeasonPackEnabled {
		return nil, "disabled", "", nil
	}
	if torrentName == "" || torrentData == "" {
		return nil, "invalid_payload", "torrentName and torrentData are required", nil
	}

	packRelease := s.releaseCache.Parse(torrentName)
	if !isTVSeasonPack(packRelease) {
		return nil, "not_season_pack", fmt.Sprintf("release %q is not a season pack", torrentName), nil
	}

	torrentBytes, decErr := base64.StdEncoding.DecodeString(torrentData)
	if decErr != nil {
		torrentBytes, decErr = decodeBase64Variants(torrentData)
	}
	if decErr != nil {
		return nil, "invalid_torrent", "failed to decode torrent data", nil
	}

	meta, parseErr := ParseTorrentMetadataWithInfo(torrentBytes)
	if parseErr != nil {
		return nil, "invalid_torrent", "failed to parse torrent metadata", nil
	}

	packEpisodes := extractPackEpisodes(meta.Files, packRelease)
	if len(packEpisodes) == 0 {
		return nil, "invalid_torrent", "no playable episode files found in torrent", nil
	}
	totalEpisodes := s.seasonPackCoverageTotal(ctx, torrentName, packRelease, len(packEpisodes))

	instances, resolveErr := s.resolveInstances(ctx, instanceIDs)
	if resolveErr != nil {
		return nil, "", "", fmt.Errorf("resolve instances: %w", resolveErr)
	}

	eligible := filterLinkEligible(instances)
	if len(eligible) == 0 {
		return nil, "no_eligible_instances", "no instances with local filesystem access and hardlink/reflink mode", nil
	}

	threshold := settings.SeasonPackCoverageThreshold
	if threshold <= 0 {
		threshold = 0.75
	}

	return &seasonPackPrep{
		settings:      settings,
		packRelease:   packRelease,
		meta:          meta,
		torrentBytes:  torrentBytes,
		packEpisodes:  packEpisodes,
		totalEpisodes: totalEpisodes,
		eligible:      eligible,
		threshold:     threshold,
	}, "", "", nil
}

// prepareSeasonPackCheck runs a lighter validation pipeline for the check endpoint.
// When torrentData is omitted, it skips torrent parsing and uses metadata providers
// for episode totals. Returns the same tuple convention as prepareSeasonPack.
func (s *Service) prepareSeasonPackCheck(ctx context.Context, torrentName, torrentData string, instanceIDs []int) (*seasonPackPrep, string, string, error) {
	// When torrentData is provided, use the full pipeline.
	if torrentData != "" {
		return s.prepareSeasonPack(ctx, torrentName, torrentData, instanceIDs)
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("load automation settings: %w", err)
	}
	if !settings.SeasonPackEnabled {
		return nil, "disabled", "", nil
	}
	if torrentName == "" {
		return nil, "invalid_payload", "torrentName is required", nil
	}

	packRelease := s.releaseCache.Parse(torrentName)
	if !isTVSeasonPack(packRelease) {
		return nil, "not_season_pack", fmt.Sprintf("release %q is not a season pack", torrentName), nil
	}

	instances, resolveErr := s.resolveInstances(ctx, instanceIDs)
	if resolveErr != nil {
		return nil, "", "", fmt.Errorf("resolve instances: %w", resolveErr)
	}

	eligible := filterLinkEligible(instances)
	if len(eligible) == 0 {
		return nil, "no_eligible_instances", "no instances with local filesystem access and hardlink/reflink mode", nil
	}

	// Try to get episode total from metadata chain (Sonarr -> TVDB/TVMaze).
	totalEpisodes := -1
	lookup := s.seasonPackEpisodeTotalLookup
	if lookup == nil {
		lookup = s.lookupSeasonPackEpisodeTotal
	}
	if total, ok := lookup(ctx, torrentName, packRelease); ok && total > 0 {
		totalEpisodes = total
	}

	threshold := settings.SeasonPackCoverageThreshold
	if threshold <= 0 {
		threshold = 0.75
	}

	return &seasonPackPrep{
		settings:      settings,
		packRelease:   packRelease,
		packEpisodes:  nil, // no torrent file = accept any matching episode
		totalEpisodes: totalEpisodes,
		eligible:      eligible,
		threshold:     threshold,
	}, "", "", nil
}

// CheckSeasonPackWebhook evaluates whether a season pack torrent can be
// reconstructed from existing individual episodes across eligible instances.
func (s *Service) CheckSeasonPackWebhook(ctx context.Context, req *SeasonPackCheckRequest) (*SeasonPackCheckResponse, error) {
	prep, reason, message, prepErr := s.prepareSeasonPackCheck(ctx, req.TorrentName, req.TorrentData, req.InstanceIDs)
	if prep == nil {
		if prepErr != nil {
			return nil, prepErr
		}
		resp := &SeasonPackCheckResponse{Reason: reason, Message: message}
		s.recordCheckRun(ctx, req.TorrentName, resp, nil, 0)
		return resp, nil
	}

	// When totalEpisodes == -1, no metadata source was available and no torrent data
	// was provided. Skip threshold enforcement and just check for any matches.
	if prep.totalEpisodes == -1 {
		return s.checkSeasonPackNoThreshold(ctx, req.TorrentName, prep)
	}

	matches, err := s.computeCoverage(ctx, prep.eligible, prep.packRelease, prep.packEpisodes, prep.totalEpisodes, prep.settings)
	if err != nil {
		return nil, err
	}

	var passing []SeasonPackCheckMatch
	for _, m := range matches {
		if m.Coverage >= prep.threshold {
			passing = append(passing, m)
		}
	}

	resp := buildCheckResponse(passing, matches, prep.totalEpisodes, prep.threshold)
	s.recordCheckRun(ctx, req.TorrentName, resp, passing, prep.totalEpisodes)
	return resp, nil
}

// checkSeasonPackNoThreshold handles the check path when no episode total is available.
// Returns ready=true if any instance has matching episodes.
func (s *Service) checkSeasonPackNoThreshold(ctx context.Context, torrentName string, prep *seasonPackPrep) (*SeasonPackCheckResponse, error) {
	var matches []SeasonPackCheckMatch

	for _, inst := range prep.eligible {
		cached, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			return nil, fmt.Errorf("load cached torrents for instance %d: %w", inst.ID, err)
		}

		matched := s.matchEpisodesOnInstance(cached, prep.packRelease, prep.packEpisodes, prep.settings)
		if len(matched) == 0 {
			continue
		}

		matches = append(matches, SeasonPackCheckMatch{
			InstanceID:      inst.ID,
			MatchedEpisodes: len(matched),
		})
	}

	if len(matches) > 0 {
		best := matches[0]
		for i := range matches[1:] {
			if matches[i+1].MatchedEpisodes > best.MatchedEpisodes {
				best = matches[i+1]
			}
		}
		resp := &SeasonPackCheckResponse{
			Ready:            true,
			Message:          fmt.Sprintf("%d instance(s) have matching episodes (threshold skipped, no episode total available)", len(matches)),
			Matches:          matches,
			ThresholdSkipped: true,
		}
		s.recordCheckRunNoThreshold(ctx, torrentName, best.MatchedEpisodes, best.InstanceID)
		return resp, nil
	}

	resp := &SeasonPackCheckResponse{
		Reason:           "no_matches",
		Message:          "no matching episodes found on any instance",
		ThresholdSkipped: true,
	}
	s.recordCheckRun(ctx, torrentName, resp, nil, 0)
	return resp, nil
}

// ApplySeasonPackWebhook attempts to apply a season pack by selecting the best
// instance, assembling a link tree from local episode files, and adding the torrent.
func (s *Service) ApplySeasonPackWebhook(ctx context.Context, req *SeasonPackApplyRequest) (*SeasonPackApplyResponse, error) {
	prep, reason, message, prepErr := s.prepareSeasonPack(ctx, req.TorrentName, req.TorrentData, req.InstanceIDs)
	if prep == nil {
		if prepErr != nil {
			return nil, prepErr
		}
		s.recordApplyRun(ctx, req.TorrentName, reason, message, 0, 0, 0, 0, "")
		return &SeasonPackApplyResponse{Reason: reason, Message: message}, nil
	}

	// Check if torrent already exists on any eligible instance.
	hashes := collectHashes(prep.meta)
	for _, inst := range prep.eligible {
		if _, found, err := s.syncManager.HasTorrentByAnyHash(ctx, inst.ID, hashes); err != nil {
			message := fmt.Sprintf("failed to check existing torrents on instance %d: %v", inst.ID, err)
			s.recordApplyRun(ctx, req.TorrentName, "existing_check_failed", message, inst.ID, 0, prep.totalEpisodes, 0, "")
			return &SeasonPackApplyResponse{
				Reason:  "existing_check_failed",
				Message: message,
			}, nil
		} else if found {
			s.recordApplyRun(ctx, req.TorrentName, "already_exists", "", inst.ID, 0, prep.totalEpisodes, 0, "")
			return &SeasonPackApplyResponse{
				Reason:  "already_exists",
				Message: fmt.Sprintf("torrent already exists on instance %d", inst.ID),
			}, nil
		}
	}

	matches, err := s.computeCoverage(ctx, prep.eligible, prep.packRelease, prep.packEpisodes, prep.totalEpisodes, prep.settings)
	if err != nil {
		message := err.Error()
		s.recordApplyRun(ctx, req.TorrentName, "coverage_check_failed", message, 0, 0, prep.totalEpisodes, 0, "")
		return &SeasonPackApplyResponse{
			Reason:  "coverage_check_failed",
			Message: message,
		}, nil
	}

	winner := selectWinner(matches, prep.threshold)
	if winner == nil {
		s.recordApplyRun(ctx, req.TorrentName, "drifted", "no instance meets coverage threshold at apply time", 0, 0, prep.totalEpisodes, 0, "")
		return &SeasonPackApplyResponse{
			Reason:  "drifted",
			Message: "coverage no longer meets threshold",
		}, nil
	}

	linkMode := determineLinkMode(prep.eligible, winner.InstanceID)
	inst := findInstance(prep.eligible, winner.InstanceID)

	planBuild, torrentBytes, episodes, err := s.assembleSeasonPack(ctx, prep, inst, winner, linkMode)
	if err != nil {
		return s.failApply(ctx, req.TorrentName, err, prep, winner)
	}

	crossCategory := s.resolveSeasonPackCategory(ctx, prep, req.Indexer, episodes)
	if _, err := s.ensureCrossCategory(ctx, inst.ID, crossCategory, "", false); err != nil {
		log.Warn().Err(err).Str("torrentName", req.TorrentName).Str("category", crossCategory).Msg("season pack: failed to ensure cross-seed category exists")
	}

	opts := seasonPackAddOptions(planBuild.plan, crossCategory, planBuild.hasPendingFiles())
	if tags := prep.settings.SeasonPackTags; len(tags) > 0 {
		opts["tags"] = strings.Join(tags, ",")
	}
	if _, err := s.syncManager.AddTorrent(ctx, inst.ID, torrentBytes, opts); err != nil {
		if rollbackErr := rollbackSeasonPackTree(linkMode, planBuild.plan); rollbackErr != nil {
			log.Warn().Err(rollbackErr).Str("torrentName", req.TorrentName).Msg("season pack: failed to rollback after add failure")
		}
		s.recordApplyRun(ctx, req.TorrentName, "add_failed", err.Error(), winner.InstanceID, winner.MatchedEpisodes, prep.totalEpisodes, winner.Coverage, linkMode)
		return &SeasonPackApplyResponse{Reason: "add_failed", Message: "failed to add torrent to qbittorrent"}, nil
	}

	message = ""
	if planBuild.hasPendingFiles() {
		recheckHashes := collectHashes(prep.meta)
		switch {
		case len(recheckHashes) == 0:
			message = "torrent added paused; missing files require manual recheck"
		case s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), inst.ID, recheckHashes, "recheck") != nil:
			message = "torrent added paused; automatic recheck failed"
		default:
			activeHash := seasonPackActiveHash(prep.meta)
			if activeHash == "" {
				message = "torrent added paused; automatic resume could not be queued"
			} else if s.recheckResumeChan == nil {
				message = "torrent added paused; automatic resume is unavailable"
			} else if err := s.queueRecheckResumeWithThreshold(ctx, inst.ID, activeHash, prep.threshold); err != nil {
				message = "torrent added paused; automatic resume queue is full"
			} else {
				message = "torrent added paused; recheck queued"
			}
		}
	}

	s.recordApplyRun(ctx, req.TorrentName, "applied", message, winner.InstanceID, winner.MatchedEpisodes, prep.totalEpisodes, winner.Coverage, linkMode)

	return &SeasonPackApplyResponse{
		Applied:         true,
		Message:         message,
		InstanceID:      winner.InstanceID,
		MatchedEpisodes: winner.MatchedEpisodes,
		TotalEpisodes:   prep.totalEpisodes,
		Coverage:        winner.Coverage,
		LinkMode:        linkMode,
	}, nil
}

// assembleSeasonPack builds the link tree for a season pack apply.
func (s *Service) assembleSeasonPack(
	ctx context.Context,
	prep *seasonPackPrep,
	inst *models.Instance,
	winner *SeasonPackCheckMatch,
	linkMode string,
) (*seasonPackPlanBuild, []byte, map[episodeIdentity]episodeMatch, error) {
	if inst == nil {
		return nil, nil, nil, fmt.Errorf("%w: no instance found for winner", errLayoutMismatch)
	}
	if inst.HardlinkBaseDir == "" {
		return nil, nil, nil, fmt.Errorf("%w: hardlink base dir not configured on instance %d", errLayoutMismatch, inst.ID)
	}

	cached, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("link_failed: %w", err)
	}

	candidates := s.matchEpisodeCandidatesDetailed(cached, prep.packRelease, prep.packEpisodes, prep.settings)
	if len(candidates) < winner.MatchedEpisodes {
		return nil, nil, nil, fmt.Errorf("%w: episode count drifted during apply", errLayoutMismatch)
	}

	episodes, localFiles, err := s.resolveSeasonPackLocalFilesForCandidates(ctx, inst.ID, candidates, prep.meta.Files, prep.packRelease, prep.settings)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(episodes) < winner.MatchedEpisodes {
		return nil, nil, nil, fmt.Errorf("%w: episode file validation drifted during apply", errLayoutMismatch)
	}

	selectedBaseDir, err := selectSeasonPackBaseDir(inst.HardlinkBaseDir, localFiles)
	if err != nil {
		return nil, nil, nil, err
	}

	planBuild, err := buildSeasonPackPlan(
		prep.meta.Files, prep.packRelease, prep.meta.Name,
		selectedBaseDir, localFiles, seasonPackNormalizer(s), prep.settings,
	)
	if err != nil {
		return nil, nil, nil, err
	}
	if planBuild.hasPendingFiles() && prep.settings.SkipRecheck {
		return nil, nil, nil, fmt.Errorf("%w: incomplete season pack requires recheck, but Skip Recheck is enabled", errSkippedRecheck)
	}

	if linkMode == "hardlink" && !prep.settings.SkipPieceBoundarySafetyCheck {
		if unsafe, result := hasUnsafeSeasonPackPendingFiles(prep.meta.Info, planBuild.materializedPaths); unsafe {
			return nil, nil, nil, fmt.Errorf("%w: unsafe piece boundary with pending files: %s", errLayoutMismatch, result.Reason)
		}
	}

	createFn := s.seasonPackLinkCreator
	if createFn == nil {
		createFn = linkCreatorForMode(linkMode)
	}
	if err := createFn(planBuild.plan); err != nil {
		if rollbackErr := rollbackSeasonPackTree(linkMode, planBuild.plan); rollbackErr != nil {
			return nil, nil, nil, fmt.Errorf("link_failed: %w", errors.Join(err, fmt.Errorf("rollback failed: %w", rollbackErr)))
		}
		return nil, nil, nil, fmt.Errorf("link_failed: %w", err)
	}

	return planBuild, prep.torrentBytes, episodes, nil
}

func selectSeasonPackBaseDir(configuredDirs string, localFiles map[episodeIdentity]seasonPackLocalFile) (string, error) {
	dirs := parseSeasonPackBaseDirs(configuredDirs)
	if len(dirs) == 0 {
		return "", fmt.Errorf("%w: hardlink base dir not configured", errLayoutMismatch)
	}

	sourcePaths := seasonPackSourcePaths(localFiles)
	if len(sourcePaths) == 0 {
		return "", fmt.Errorf("%w: no resolved episode files for base dir selection", errLayoutMismatch)
	}
	if len(dirs) == 1 {
		matchesAllSources, err := seasonPackBaseDirMatchesAllSources(dirs[0], sourcePaths)
		if err != nil {
			return "", fmt.Errorf("%w: no base directory on same filesystem as season pack sources (last error: %w)", errLayoutMismatch, err)
		}
		if matchesAllSources {
			return dirs[0], nil
		}
		return "", fmt.Errorf("%w: no base directory on same filesystem as season pack sources", errLayoutMismatch)
	}

	var lastErr error
	for _, dir := range dirs {
		matchesAllSources, err := seasonPackBaseDirMatchesAllSources(dir, sourcePaths)
		if err != nil {
			lastErr = err
			continue
		}
		if matchesAllSources {
			return dir, nil
		}
	}

	if lastErr != nil {
		return "", fmt.Errorf("%w: no base directory on same filesystem as season pack sources (last error: %w)", errLayoutMismatch, lastErr)
	}
	return "", fmt.Errorf("%w: no base directory on same filesystem as season pack sources", errLayoutMismatch)
}

func seasonPackSourcePaths(localFiles map[episodeIdentity]seasonPackLocalFile) []string {
	sourcePaths := make([]string, 0, len(localFiles))
	for _, localFile := range localFiles {
		if localFile.sourcePath != "" {
			sourcePaths = append(sourcePaths, localFile.sourcePath)
		}
	}
	sort.Strings(sourcePaths)
	return sourcePaths
}

func seasonPackBaseDirMatchesAllSources(dir string, sourcePaths []string) (bool, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return false, fmt.Errorf("failed to create directory %s: %w", dir, err)
	}

	for _, sourcePath := range sourcePaths {
		sameFS, err := fsutil.SameFilesystem(sourcePath, dir)
		if err != nil && errors.Is(err, os.ErrNotExist) {
			existingParent := nearestExistingParent(sourcePath)
			if existingParent != "" {
				sameFS, err = fsutil.SameFilesystem(existingParent, dir)
			}
		}
		if err != nil {
			return false, fmt.Errorf("failed to check filesystem for %s: %w", dir, err)
		}
		if !sameFS {
			return false, nil
		}
	}
	return true, nil
}

func nearestExistingParent(path string) string {
	for dir := filepath.Dir(path); dir != "." && dir != ""; dir = filepath.Dir(dir) {
		if _, err := os.Stat(dir); err == nil {
			return dir
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return ""
}

func parseSeasonPackBaseDirs(configuredDirs string) []string {
	parts := strings.Split(configuredDirs, ",")
	dirs := make([]string, 0, len(parts))
	for _, part := range parts {
		dir := strings.TrimSpace(part)
		if dir != "" {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// failApply handles apply errors, categorizing by reason prefix.
func (s *Service) failApply(
	ctx context.Context,
	torrentName string,
	err error,
	prep *seasonPackPrep,
	winner *SeasonPackCheckMatch,
) (*SeasonPackApplyResponse, error) {
	reason := "link_failed"
	if errors.Is(err, errLayoutMismatch) {
		reason = "layout_mismatch"
	} else if errors.Is(err, errSkippedRecheck) {
		reason = "skipped_recheck"
	}
	s.recordApplyRun(ctx, torrentName, reason, err.Error(), winner.InstanceID, winner.MatchedEpisodes, prep.totalEpisodes, winner.Coverage, "")
	return &SeasonPackApplyResponse{Reason: reason, Message: err.Error()}, nil
}

// seasonPackAddOptions returns qBittorrent add options for a season pack torrent.
func seasonPackAddOptions(plan *hardlinktree.TreePlan, category string, paused bool) map[string]string {
	options := map[string]string{
		"autoTMM":       "false",
		"contentLayout": "Original",
		"savepath":      plan.RootDir,
		"skip_checking": "true",
	}
	if paused {
		options["paused"] = "true"
		options["stopped"] = "true"
	} else {
		options["paused"] = "false"
		options["stopped"] = "false"
	}
	if category != "" {
		options["category"] = category
	}
	return options
}

// linkCreatorForMode returns the appropriate link-tree creator function.
func linkCreatorForMode(mode string) func(*hardlinktree.TreePlan) error {
	if mode == "reflink" {
		return reflinktree.Create
	}
	return hardlinktree.Create
}

// findInstance returns the instance with the given ID, or nil.
func findInstance(instances []*models.Instance, id int) *models.Instance {
	for _, inst := range instances {
		if inst.ID == id {
			return inst
		}
	}
	return nil
}

func seasonPackNormalizer(s *Service) *stringutils.Normalizer[string, string] {
	if s != nil && s.stringNormalizer != nil {
		return s.stringNormalizer
	}
	// Shared singleton: see normalizerForService - a fresh normalizer
	// leaks a never-terminating ttlcache goroutine.
	return stringutils.DefaultNormalizer
}

func parseSeasonPackEpisodePayload(
	fileName string,
	torrentRelease *rls.Release,
	normalizer *stringutils.Normalizer[string, string],
) (*rls.Release, bool) {
	ext := strings.ToLower(filepath.Ext(fileName))
	if _, ok := videoExtensions[ext]; !ok {
		return nil, false
	}
	if shouldIgnoreFile(fileName, normalizer) {
		return nil, false
	}

	parsed := rls.ParseString(filepath.Base(fileName))
	if !isTVEpisode(&parsed) {
		return nil, false
	}

	enriched := enrichReleaseFromTorrent(&parsed, torrentRelease)
	if torrentRelease != nil && torrentRelease.Series > 0 && enriched.Series != torrentRelease.Series {
		return nil, false
	}

	return enriched, true
}

// extractPackEpisodes returns the set of unique episode identities from the
// torrent's file list, considering only playable video files.
func extractPackEpisodes(files qbt.TorrentFiles, packRelease *rls.Release) map[episodeIdentity]struct{} {
	episodes := make(map[episodeIdentity]struct{})
	normalizer := seasonPackNormalizer(nil)

	for _, f := range files {
		parsed, ok := parseSeasonPackEpisodePayload(f.Name, packRelease, normalizer)
		if !ok {
			continue
		}

		id := episodeIdentity{series: parsed.Series, episode: parsed.Episode}
		episodes[id] = struct{}{}
	}

	return episodes
}

// filterLinkEligible returns instances that have local filesystem access
// and either hardlink or reflink mode enabled.
func filterLinkEligible(instances []*models.Instance) []*models.Instance {
	var eligible []*models.Instance
	for _, inst := range instances {
		if !inst.HasLocalFilesystemAccess {
			continue
		}
		switch {
		case inst.UseHardlinks && inst.HardlinkBaseDir != "":
			eligible = append(eligible, inst)
		case inst.UseReflinks && inst.HardlinkBaseDir != "":
			eligible = append(eligible, inst)
		}
	}
	return eligible
}

func (s *Service) seasonPackCoverageTotal(ctx context.Context, torrentName string, packRelease *rls.Release, packEpisodes int) int {
	if packEpisodes <= 0 {
		return 0
	}

	lookup := s.seasonPackEpisodeTotalLookup
	if lookup == nil {
		lookup = s.lookupSeasonPackEpisodeTotal
	}
	totalEpisodes, ok := lookup(ctx, torrentName, packRelease)
	if !ok || totalEpisodes < packEpisodes {
		return packEpisodes
	}
	return totalEpisodes
}

func (s *Service) seasonPackReleasesMatch(
	source *rls.Release,
	candidate *rls.Release,
	findIndividualEpisodes bool,
	settings *models.CrossSeedAutomationSettings,
) bool {
	if source == nil || candidate == nil {
		return false
	}

	sourceCopy := *source
	candidateCopy := *candidate
	opts := seasonPackMatchOptionsFromSettings(settings)

	if opts.skipRepackCompare {
		sourceCopy.Other = stripSeasonPackRepackTags(sourceCopy.Other)
		candidateCopy.Other = stripSeasonPackRepackTags(candidateCopy.Other)
	}
	if opts.simplifyHDRCompare {
		sourceCopy.HDR = simplifySeasonPackHDRTags(sourceCopy.HDR)
		candidateCopy.HDR = simplifySeasonPackHDRTags(candidateCopy.HDR)
	}
	if opts.simplifyWEBCompare {
		simplifySeasonPackWEBSource(&sourceCopy)
		simplifySeasonPackWEBSource(&candidateCopy)
	}
	if !seasonPackNonPackVariantsMatch(&sourceCopy, &candidateCopy) {
		return false
	}
	if !seasonPackSourcesMatchExactly(&sourceCopy, &candidateCopy) {
		return false
	}
	if opts.skipYearCompare {
		sourceCopy.Year, sourceCopy.Month, sourceCopy.Day = 0, 0, 0
		candidateCopy.Year, candidateCopy.Month, candidateCopy.Day = 0, 0, 0
	}

	return s.releasesMatch(&sourceCopy, &candidateCopy, findIndividualEpisodes)
}

func stripSeasonPackRepackTags(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeVariant(value)
		switch {
		case normalized == "":
			continue
		case variantValueMatches(normalized, "REPACK"):
			continue
		case variantValueMatches(normalized, "PROPER"):
			continue
		default:
			filtered = append(filtered, value)
		}
	}

	return filtered
}

func simplifySeasonPackHDRTags(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	simplified := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeVariant(value)
		if normalized == "" {
			continue
		}
		if strings.Contains(normalized, "HDR") {
			normalized = "HDR"
		}
		if _, ok := seen[normalized]; ok {
			continue
		}
		seen[normalized] = struct{}{}
		simplified = append(simplified, normalized)
	}

	return simplified
}

func simplifySeasonPackWEBSource(release *rls.Release) {
	if release == nil {
		return
	}

	if normalizeSource(release.Source) == "WEBDL" {
		release.Source = "WEB"
	}
}

func seasonPackSourcesMatchExactly(source *rls.Release, candidate *rls.Release) bool {
	if source == nil || candidate == nil {
		return false
	}

	sourceSource := normalizeSource(source.Source)
	candidateSource := normalizeSource(candidate.Source)
	if sourceSource == "" || candidateSource == "" {
		return true
	}

	return sourceSource == candidateSource
}

func seasonPackNonPackVariantsMatch(source *rls.Release, candidate *rls.Release) bool {
	if source == nil || candidate == nil {
		return false
	}
	if mismatch := nonPackVariantOverrides.findMismatch(source, candidate); mismatch != "" {
		return false
	}
	if mismatch := nonPackVariantOverrides.findMismatch(candidate, source); mismatch != "" {
		return false
	}

	return true
}

func (s *Service) lookupSeasonPackEpisodeTotal(ctx context.Context, torrentName string, packRelease *rls.Release) (int, bool) {
	if s == nil || packRelease == nil || packRelease.Series <= 0 || torrentName == "" {
		return 0, false
	}

	// 1. Try Sonarr first.
	if s.arrService != nil {
		result, err := s.arrService.LookupSeasonEpisodeTotal(ctx, torrentName, packRelease.Series)
		if err != nil {
			log.Debug().Err(err).Str("torrentName", torrentName).Int("season", packRelease.Series).
				Msg("season pack: failed to resolve Sonarr season total")
		} else if result != nil && result.TotalEpisodes > 0 {
			return result.TotalEpisodes, true
		}
	}

	// 2. Try metadata providers (TVDB/TVMaze).
	// Use the parsed show title (e.g. "Cool Show") instead of the raw torrent name
	// (e.g. "Cool.Show.S01.1080p.WEB.x264-GRP") since metadata APIs expect clean titles.
	metaSvc := s.getMetadataService(ctx)
	if metaSvc != nil {
		showTitle := strings.ReplaceAll(packRelease.Title, ".", " ")
		total, err := metaSvc.LookupEpisodeTotal(ctx, showTitle, packRelease.Series)
		if err != nil {
			log.Debug().Err(err).Str("torrentName", torrentName).Int("season", packRelease.Series).
				Msg("season pack: metadata provider lookup failed")
		} else if total > 0 {
			return total, true
		}
	}

	return 0, false
}

// computeCoverage calculates episode coverage for each instance by scanning
// cached torrents and matching them against the pack's expected episodes.
func (s *Service) computeCoverage(
	ctx context.Context,
	instances []*models.Instance,
	packRelease *rls.Release,
	packEpisodes map[episodeIdentity]struct{},
	totalEpisodes int,
	settings *models.CrossSeedAutomationSettings,
) ([]SeasonPackCheckMatch, error) {
	var matches []SeasonPackCheckMatch

	for _, inst := range instances {
		cached, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			return nil, fmt.Errorf("load cached torrents for instance %d: %w", inst.ID, err)
		}

		matched := s.matchEpisodesOnInstance(cached, packRelease, packEpisodes, settings)
		if len(matched) == 0 {
			continue
		}

		coverage := float64(len(matched)) / float64(totalEpisodes)
		matches = append(matches, SeasonPackCheckMatch{
			InstanceID:      inst.ID,
			MatchedEpisodes: len(matched),
			TotalEpisodes:   totalEpisodes,
			Coverage:        coverage,
		})
	}

	return matches, nil
}

// matchEpisodesOnInstance finds which pack episodes are present as individual
// episode torrents on a given instance.
func (s *Service) matchEpisodesOnInstance(
	cached []qbittorrent.CrossInstanceTorrentView,
	packRelease *rls.Release,
	packEpisodes map[episodeIdentity]struct{},
	settings *models.CrossSeedAutomationSettings,
) map[episodeIdentity]struct{} {
	rich := s.matchEpisodesDetailed(cached, packRelease, packEpisodes, settings)
	result := make(map[episodeIdentity]struct{}, len(rich))
	for id := range rich {
		result[id] = struct{}{}
	}
	return result
}

// matchEpisodesDetailed returns per-episode match info including the owning torrent.
func (s *Service) matchEpisodesDetailed(
	cached []qbittorrent.CrossInstanceTorrentView,
	packRelease *rls.Release,
	packEpisodes map[episodeIdentity]struct{},
	settings *models.CrossSeedAutomationSettings,
) map[episodeIdentity]episodeMatch {
	candidates := s.matchEpisodeCandidatesDetailed(cached, packRelease, packEpisodes, settings)
	matched := make(map[episodeIdentity]episodeMatch, len(candidates))
	for id, episodeCandidates := range candidates {
		if len(episodeCandidates) > 0 {
			matched[id] = episodeCandidates[0]
		}
	}

	return matched
}

func (s *Service) matchEpisodeCandidatesDetailed(
	cached []qbittorrent.CrossInstanceTorrentView,
	packRelease *rls.Release,
	packEpisodes map[episodeIdentity]struct{},
	settings *models.CrossSeedAutomationSettings,
) map[episodeIdentity][]episodeMatch {
	candidates := make(map[episodeIdentity][]episodeMatch)
	matcher := s
	if matcher.stringNormalizer == nil {
		matcher = &Service{stringNormalizer: stringutils.DefaultNormalizer}
	}

	for i := range cached {
		view := &cached[i]
		if view.TorrentView == nil || view.Torrent == nil {
			continue
		}
		torrent := view.Torrent

		if !matchesWebhookSourceFilters(torrent, settings) {
			continue
		}
		if torrent.Progress < 1.0 {
			continue
		}

		parsed := s.releaseCache.Parse(torrent.Name)
		if !isTVEpisode(parsed) {
			continue
		}

		if !matcher.seasonPackReleasesMatch(packRelease, parsed, true, settings) {
			continue
		}

		id := episodeIdentity{series: parsed.Series, episode: parsed.Episode}
		if packEpisodes != nil {
			if _, inPack := packEpisodes[id]; !inPack {
				continue
			}
		}

		candidates[id] = append(candidates[id], episodeMatch{
			torrentHash: torrent.Hash,
			contentPath: torrent.ContentPath,
			category:    torrent.Category,
			release:     parsed,
		})
	}

	return candidates
}

func (s *Service) resolveSeasonPackLocalFilesForCandidates(
	ctx context.Context,
	instanceID int,
	candidates map[episodeIdentity][]episodeMatch,
	packFiles qbt.TorrentFiles,
	packRelease *rls.Release,
	settings *models.CrossSeedAutomationSettings,
) (map[episodeIdentity]episodeMatch, map[episodeIdentity]seasonPackLocalFile, error) {
	hashes := make([]string, 0)
	seenHashes := make(map[string]struct{})
	for _, episodeCandidates := range candidates {
		for _, candidate := range episodeCandidates {
			normalized := normalizeHash(candidate.torrentHash)
			if normalized == "" {
				continue
			}
			if _, seen := seenHashes[normalized]; seen {
				continue
			}
			seenHashes[normalized] = struct{}{}
			hashes = append(hashes, candidate.torrentHash)
		}
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, nil, fmt.Errorf("load matched episode files: %w", err)
	}

	normalizer := seasonPackNormalizer(s)
	expected := seasonPackExpectedFiles(packFiles, packRelease, normalizer)
	matcher := &Service{stringNormalizer: normalizer}
	selected := make(map[episodeIdentity]episodeMatch, len(candidates))
	localFiles := make(map[episodeIdentity]seasonPackLocalFile, len(candidates))
	ids := sortedEpisodeCandidateIDs(candidates)

	for _, id := range ids {
		expectedFile, ok := expected[id]
		if !ok {
			continue
		}

		var lastErr error
		for _, candidate := range candidates[id] {
			files, ok := filesByHash[normalizeHash(candidate.torrentHash)]
			if !ok || len(files) == 0 {
				lastErr = fmt.Errorf("%w: no file list for torrent %s", errLayoutMismatch, candidate.torrentHash)
				continue
			}

			localFile, err := resolveSeasonPackLocalFileCandidate(id, candidate, files, normalizer)
			if err != nil {
				lastErr = err
				continue
			}
			if localFile.size != expectedFile.file.Size {
				lastErr = fmt.Errorf("%w: file size mismatch for %s", errLayoutMismatch, expectedFile.file.Name)
				continue
			}
			if !matcher.seasonPackReleasesMatch(expectedFile.release, localFile.release, false, settings) {
				lastErr = fmt.Errorf("%w: release mismatch for %s", errLayoutMismatch, expectedFile.file.Name)
				continue
			}

			selected[id] = candidate
			localFiles[id] = localFile
			break
		}

		if _, ok := selected[id]; !ok {
			if lastErr != nil {
				return nil, nil, lastErr
			}
			return nil, nil, fmt.Errorf("%w: no valid episode file in matched candidates", errLayoutMismatch)
		}
	}

	return selected, localFiles, nil
}

type seasonPackExpectedFile struct {
	file    qbt.TorrentFile
	release *rls.Release
}

func seasonPackExpectedFiles(
	packFiles qbt.TorrentFiles,
	packRelease *rls.Release,
	normalizer *stringutils.Normalizer[string, string],
) map[episodeIdentity]seasonPackExpectedFile {
	expected := make(map[episodeIdentity]seasonPackExpectedFile)
	for _, file := range packFiles {
		release, ok := parseSeasonPackEpisodePayload(file.Name, packRelease, normalizer)
		if !ok {
			continue
		}
		expected[episodeIdentity{series: release.Series, episode: release.Episode}] = seasonPackExpectedFile{
			file:    file,
			release: release,
		}
	}
	return expected
}

func sortedEpisodeCandidateIDs(candidates map[episodeIdentity][]episodeMatch) []episodeIdentity {
	ids := make([]episodeIdentity, 0, len(candidates))
	for id := range candidates {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		if ids[i].series != ids[j].series {
			return ids[i].series < ids[j].series
		}
		return ids[i].episode < ids[j].episode
	})
	return ids
}

func resolveSeasonPackLocalFileCandidate(
	id episodeIdentity,
	episode episodeMatch,
	files qbt.TorrentFiles,
	normalizer *stringutils.Normalizer[string, string],
) (seasonPackLocalFile, error) {
	var matchedFileSize int64
	var matchedRelease *rls.Release
	matchedSourcePath := ""
	matchCount := 0

	for i := range files {
		file := &files[i]
		parsed, ok := parseSeasonPackEpisodePayload(file.Name, episode.release, normalizer)
		if !ok {
			continue
		}
		if parsed.Series != id.series || parsed.Episode != id.episode {
			continue
		}

		matchCount++
		matchedFileSize = file.Size
		matchedRelease = parsed
		matchedSourcePath = resolveSeasonPackSourcePath(episode.contentPath, files, file.Name)
	}

	if matchCount != 1 || matchedRelease == nil || matchedSourcePath == "" {
		return seasonPackLocalFile{}, fmt.Errorf("%w: expected exactly one playable episode file in torrent %s", errLayoutMismatch, episode.torrentHash)
	}

	return seasonPackLocalFile{
		sourcePath: matchedSourcePath,
		size:       matchedFileSize,
		release:    matchedRelease,
	}, nil
}

func resolveSeasonPackSourcePath(contentPath string, files qbt.TorrentFiles, fileName string) string {
	rootDir := resolveRootlessContentDir(&qbt.Torrent{ContentPath: contentPath}, files)
	if rootDir == "" {
		return ""
	}

	relativePath := strings.ReplaceAll(fileName, "\\", "/")
	if commonRoot := detectCommonRoot(files); commonRoot != "" && filepath.Base(normalizePath(rootDir)) == commonRoot {
		if trimmed, found := strings.CutPrefix(relativePath, commonRoot+"/"); found {
			relativePath = trimmed
		}
	}

	candidatePath, ok := safeSeasonPackJoin(rootDir, relativePath)
	if !ok {
		return ""
	}
	return candidatePath
}

func hasUnsafeSeasonPackPendingFiles(
	info *metainfo.Info,
	materializedPaths map[string]struct{},
) (bool, PieceBoundarySafetyResult) {
	return HasUnsafeIgnoredExtras(info, func(path string) bool {
		_, ok := materializedPaths[path]
		return !ok
	})
}

func firstMatchedEpisodeCategory(episodes map[episodeIdentity]episodeMatch) string {
	ids := make([]episodeIdentity, 0, len(episodes))
	for id := range episodes {
		ids = append(ids, id)
	}

	sort.Slice(ids, func(i, j int) bool {
		if ids[i].series != ids[j].series {
			return ids[i].series < ids[j].series
		}
		return ids[i].episode < ids[j].episode
	})

	for _, id := range ids {
		if category := strings.TrimSpace(episodes[id].category); category != "" {
			return category
		}
	}

	return ""
}

// buildSeasonPackPlan builds a TreePlan that assembles episode files from multiple
// local torrents into the layout expected by the season pack torrent.
func buildSeasonPackPlan(
	packFiles qbt.TorrentFiles,
	packRelease *rls.Release,
	packName string,
	destDir string,
	localFiles map[episodeIdentity]seasonPackLocalFile,
	normalizer *stringutils.Normalizer[string, string],
	settings *models.CrossSeedAutomationSettings,
) (*seasonPackPlanBuild, error) {
	rootDir, ok := safeSeasonPackJoin(destDir, packName)
	if !ok {
		return nil, fmt.Errorf("%w: invalid pack root path %q", errLayoutMismatch, packName)
	}
	plan := &hardlinktree.TreePlan{
		RootDir: rootDir,
		Files:   make([]hardlinktree.FilePlan, 0, len(packFiles)),
	}
	matcher := &Service{stringNormalizer: normalizer}
	build := &seasonPackPlanBuild{
		plan:              plan,
		materializedPaths: make(map[string]struct{}, len(packFiles)),
		totalFiles:        len(packFiles),
	}

	for _, pf := range packFiles {
		build.totalBytes += pf.Size

		packFileRelease, ok := parseSeasonPackEpisodePayload(pf.Name, packRelease, normalizer)
		if !ok {
			continue
		}

		id := episodeIdentity{series: packFileRelease.Series, episode: packFileRelease.Episode}
		localFile, ok := localFiles[id]
		if !ok {
			continue
		}

		if localFile.size != pf.Size {
			return nil, fmt.Errorf("%w: file size mismatch for %s", errLayoutMismatch, pf.Name)
		}
		if !matcher.seasonPackReleasesMatch(packFileRelease, localFile.release, false, settings) {
			return nil, fmt.Errorf("%w: release mismatch for %s", errLayoutMismatch, pf.Name)
		}

		targetPath, ok := safeSeasonPackJoin(plan.RootDir, pf.Name)
		if !ok {
			return nil, fmt.Errorf("%w: invalid pack target path %q", errLayoutMismatch, pf.Name)
		}
		plan.Files = append(plan.Files, hardlinktree.FilePlan{
			SourcePath: localFile.sourcePath,
			TargetPath: targetPath,
		})
		build.materializedPaths[pf.Name] = struct{}{}
		build.linkedBytes += pf.Size
	}

	if len(plan.Files) == 0 {
		return nil, fmt.Errorf("%w: no pack files could be mapped to local episodes", errLayoutMismatch)
	}

	sort.Slice(plan.Files, func(i, j int) bool {
		return plan.Files[i].TargetPath < plan.Files[j].TargetPath
	})

	return build, nil
}

func safeSeasonPackJoin(rootDir, relativePath string) (string, bool) {
	slashPath := strings.ReplaceAll(relativePath, "\\", "/")
	if strings.HasPrefix(slashPath, "/") {
		return "", false
	}

	cleanedPath := filepath.Clean(filepath.FromSlash(slashPath))
	if cleanedPath == "." ||
		filepath.IsAbs(cleanedPath) ||
		isWindowsDriveAbs(filepath.ToSlash(cleanedPath)) ||
		cleanedPath == ".." ||
		strings.HasPrefix(cleanedPath, ".."+string(filepath.Separator)) {
		return "", false
	}

	candidatePath := filepath.Join(rootDir, cleanedPath)
	relativeToRoot, err := filepath.Rel(rootDir, candidatePath)
	if err != nil {
		return "", false
	}
	if relativeToRoot == ".." ||
		strings.HasPrefix(relativeToRoot, ".."+string(filepath.Separator)) ||
		filepath.IsAbs(relativeToRoot) ||
		isWindowsDriveAbs(filepath.ToSlash(relativeToRoot)) {
		return "", false
	}

	return candidatePath, true
}

// rollbackSeasonPackTree removes a created link tree on failure.
func rollbackSeasonPackTree(linkMode string, plan *hardlinktree.TreePlan) error {
	if plan == nil || plan.RootDir == "" {
		return nil
	}

	var errs []error
	switch linkMode {
	case "hardlink":
		if err := hardlinktree.Rollback(plan); err != nil {
			errs = append(errs, err)
		}
	case "reflink":
		if err := reflinktree.Rollback(plan); err != nil {
			errs = append(errs, err)
		}
	default:
		return nil
	}

	if err := os.Remove(plan.RootDir); err != nil &&
		!errors.Is(err, os.ErrNotExist) &&
		!seasonPackDirNotEmpty(err) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func seasonPackDirNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) ||
		strings.Contains(err.Error(), "not empty") ||
		strings.Contains(err.Error(), "directory not empty") ||
		strings.Contains(err.Error(), "The directory is not empty")
}

// selectWinner picks the best instance from the coverage matches using
// deterministic ordering: highest coverage, then highest matched episodes,
// then lowest instance ID.
func selectWinner(matches []SeasonPackCheckMatch, threshold float64) *SeasonPackCheckMatch {
	var best *SeasonPackCheckMatch

	for i := range matches {
		m := &matches[i]
		if m.Coverage < threshold {
			continue
		}
		if best == nil || isBetterMatch(m, best) {
			best = m
		}
	}

	return best
}

func isBetterMatch(a, b *SeasonPackCheckMatch) bool {
	if a.Coverage != b.Coverage {
		return a.Coverage > b.Coverage
	}
	if a.MatchedEpisodes != b.MatchedEpisodes {
		return a.MatchedEpisodes > b.MatchedEpisodes
	}
	return a.InstanceID < b.InstanceID
}

// determineLinkMode returns the link mode string for the winning instance.
func determineLinkMode(instances []*models.Instance, instanceID int) string {
	for _, inst := range instances {
		if inst.ID == instanceID {
			if inst.UseReflinks {
				return "reflink"
			}
			return "hardlink"
		}
	}
	return ""
}

// collectHashes returns all non-empty hashes from parsed torrent metadata.
func collectHashes(meta TorrentMetadata) []string {
	var hashes []string
	if meta.HashV1 != "" {
		hashes = append(hashes, meta.HashV1)
	}
	if meta.HashV2 != "" {
		hashes = append(hashes, meta.HashV2)
	}
	return hashes
}

func seasonPackActiveHash(meta TorrentMetadata) string {
	if meta.HashV1 != "" {
		return meta.HashV1
	}
	return meta.HashV2
}

// buildCheckResponse constructs the check response from computed matches.
func buildCheckResponse(
	passing []SeasonPackCheckMatch,
	allMatches []SeasonPackCheckMatch,
	totalEpisodes int,
	threshold float64,
) *SeasonPackCheckResponse {
	if len(passing) > 0 {
		return &SeasonPackCheckResponse{
			Ready:   true,
			Message: fmt.Sprintf("%d instance(s) meet %.0f%% coverage threshold", len(passing), threshold*100),
			Matches: allMatches,
		}
	}

	if len(allMatches) > 0 {
		best := allMatches[0]
		for i := range allMatches[1:] {
			if isBetterMatch(&allMatches[i+1], &best) {
				best = allMatches[i+1]
			}
		}
		return &SeasonPackCheckResponse{
			Reason:  "below_threshold",
			Message: fmt.Sprintf("best coverage %.0f%% on instance %d (%d/%d episodes)", best.Coverage*100, best.InstanceID, best.MatchedEpisodes, totalEpisodes),
			Matches: allMatches,
		}
	}

	return &SeasonPackCheckResponse{
		Reason:  "no_matches",
		Message: "no matching episodes found on any instance",
	}
}

// recordCheckRun persists a check phase run row.
func (s *Service) recordCheckRun(
	ctx context.Context,
	torrentName string,
	resp *SeasonPackCheckResponse,
	passing []SeasonPackCheckMatch,
	totalEpisodes int,
) {
	if s.seasonPackRunStore == nil {
		return
	}

	run := &models.SeasonPackRun{
		TorrentName:   torrentName,
		Phase:         "check",
		TotalEpisodes: totalEpisodes,
	}

	if resp.Ready && len(passing) > 0 {
		run.Status = "ready"
		best := passing[0]
		for i := range passing[1:] {
			if isBetterMatch(&passing[i+1], &best) {
				best = passing[i+1]
			}
		}
		run.MatchedEpisodes = best.MatchedEpisodes
		run.Coverage = best.Coverage
		instID := best.InstanceID
		run.InstanceID = &instID
	} else {
		run.Status = "skipped"
		run.Reason = resp.Reason
		run.Message = resp.Message
	}

	if _, err := s.seasonPackRunStore.Create(ctx, run); err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).
			Msg("failed to record season pack check run")
	}
}

// recordCheckRunNoThreshold persists a check row when threshold was skipped.
func (s *Service) recordCheckRunNoThreshold(ctx context.Context, torrentName string, matchedEpisodes, instanceID int) {
	if s.seasonPackRunStore == nil {
		return
	}

	run := &models.SeasonPackRun{
		TorrentName:     torrentName,
		Phase:           "check",
		Status:          "ready_no_threshold",
		Reason:          "no_episode_total",
		MatchedEpisodes: matchedEpisodes,
	}
	if instanceID > 0 {
		run.InstanceID = &instanceID
	}

	if _, err := s.seasonPackRunStore.Create(ctx, run); err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).
			Msg("failed to record season pack check run")
	}
}

// recordApplyRun persists an apply phase run row.
func (s *Service) recordApplyRun(
	ctx context.Context,
	torrentName, reason, message string,
	instanceID, matchedEpisodes, totalEpisodes int,
	coverage float64,
	linkMode string,
) {
	if s.seasonPackRunStore == nil {
		return
	}

	run := &models.SeasonPackRun{
		TorrentName:     torrentName,
		Phase:           "apply",
		Reason:          reason,
		Message:         message,
		MatchedEpisodes: matchedEpisodes,
		TotalEpisodes:   totalEpisodes,
		Coverage:        coverage,
		LinkMode:        linkMode,
	}

	switch reason {
	case "applied":
		run.Status = "applied"
	case "already_exists", "skipped_recheck":
		run.Status = "skipped"
	default:
		run.Status = "failed"
	}

	if instanceID > 0 {
		run.InstanceID = &instanceID
	}

	if _, err := s.seasonPackRunStore.Create(ctx, run); err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).
			Msg("failed to record season pack apply run")
	}
}
