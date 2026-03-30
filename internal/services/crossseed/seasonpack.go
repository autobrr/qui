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

	"github.com/anacrolix/torrent/metainfo"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
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

func (b *seasonPackPlanBuild) hasPendingFiles() bool {
	if b == nil {
		return false
	}
	return len(b.materializedPaths) < b.totalFiles
}

func (b *seasonPackPlanBuild) recheckThreshold() float64 {
	if b == nil || b.totalBytes <= 0 {
		return 1.0
	}
	threshold := float64(b.linkedBytes) / float64(b.totalBytes)
	if threshold <= 0 {
		return 0.01
	}
	if threshold > 1 {
		return 1.0
	}
	return threshold
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
	settings, err := s.automationSettingsLoader(ctx)
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

// CheckSeasonPackWebhook evaluates whether a season pack torrent can be
// reconstructed from existing individual episodes across eligible instances.
func (s *Service) CheckSeasonPackWebhook(ctx context.Context, req *SeasonPackCheckRequest) (*SeasonPackCheckResponse, error) {
	prep, reason, message, prepErr := s.prepareSeasonPack(ctx, req.TorrentName, req.TorrentData, req.InstanceIDs)
	if prep == nil {
		if prepErr != nil {
			return nil, prepErr
		}
		resp := &SeasonPackCheckResponse{Reason: reason, Message: message}
		s.recordCheckRun(ctx, req.TorrentName, resp, nil, 0)
		return resp, nil
	}

	matches := s.computeCoverage(ctx, prep.eligible, prep.packRelease, prep.packEpisodes, prep.totalEpisodes, prep.settings)

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

	matches := s.computeCoverage(ctx, prep.eligible, prep.packRelease, prep.packEpisodes, prep.totalEpisodes, prep.settings)

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

	_, crossCategory := s.determineCrossSeedCategory(ctx, &CrossSeedRequest{
		IndexerName: req.Indexer,
	}, &qbt.Torrent{
		Category: firstMatchedEpisodeCategory(episodes),
	}, prep.settings)

	opts := seasonPackAddOptions(planBuild.plan, crossCategory, planBuild.hasPendingFiles())
	if tags := prep.settings.SeasonPackTags; len(tags) > 0 {
		opts["tags"] = strings.Join(tags, ",")
	}
	if err := s.syncManager.AddTorrent(ctx, inst.ID, torrentBytes, opts); err != nil {
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
		case s.syncManager.BulkAction(ctx, inst.ID, recheckHashes, "recheck") != nil:
			message = "torrent added paused; automatic recheck failed"
		default:
			activeHash := seasonPackActiveHash(prep.meta)
			if activeHash == "" {
				message = "torrent added paused; automatic resume could not be queued"
			} else if s.recheckResumeChan == nil {
				message = "torrent added paused; automatic resume is unavailable"
			} else if err := s.queueRecheckResumeWithThreshold(ctx, inst.ID, activeHash, planBuild.recheckThreshold()); err != nil {
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

	episodes := s.matchEpisodesDetailed(cached, prep.packRelease, prep.packEpisodes, prep.settings)
	if len(episodes) < winner.MatchedEpisodes {
		return nil, nil, nil, fmt.Errorf("%w: episode count drifted during apply", errLayoutMismatch)
	}

	localFiles, err := s.resolveSeasonPackLocalFiles(ctx, inst.ID, episodes)
	if err != nil {
		return nil, nil, nil, err
	}

	planBuild, err := buildSeasonPackPlan(
		prep.meta.Files, prep.packRelease, prep.meta.Name,
		inst.HardlinkBaseDir, localFiles, seasonPackNormalizer(s),
	)
	if err != nil {
		return nil, nil, nil, err
	}
	if planBuild.hasPendingFiles() && prep.settings.SkipRecheck {
		return nil, nil, nil, fmt.Errorf("%w: incomplete season pack requires recheck, but Skip Recheck is enabled", errSkippedRecheck)
	}

	if linkMode == "hardlink" {
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
	return stringutils.NewDefaultNormalizer()
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

func (s *Service) lookupSeasonPackEpisodeTotal(ctx context.Context, torrentName string, packRelease *rls.Release) (int, bool) {
	if s == nil || s.arrService == nil || packRelease == nil || packRelease.Series <= 0 || torrentName == "" {
		return 0, false
	}

	result, err := s.arrService.LookupSeasonEpisodeTotal(ctx, torrentName, packRelease.Series)
	if err != nil {
		log.Debug().Err(err).Str("torrentName", torrentName).Int("season", packRelease.Series).
			Msg("season pack: failed to resolve Sonarr season total")
		return 0, false
	}
	if result == nil || result.TotalEpisodes <= 0 {
		return 0, false
	}

	return result.TotalEpisodes, true
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
) []SeasonPackCheckMatch {
	var matches []SeasonPackCheckMatch

	for _, inst := range instances {
		cached, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", inst.ID).
				Msg("failed to get cached torrents for season pack coverage")
			continue
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

	return matches
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
	matched := make(map[episodeIdentity]episodeMatch)
	matcher := s
	if matcher.stringNormalizer == nil {
		matcher = &Service{stringNormalizer: stringutils.NewDefaultNormalizer()}
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

		if !matcher.releasesMatch(packRelease, parsed, true) {
			continue
		}

		id := episodeIdentity{series: parsed.Series, episode: parsed.Episode}
		if _, inPack := packEpisodes[id]; !inPack {
			continue
		}
		if _, already := matched[id]; already {
			continue
		}

		matched[id] = episodeMatch{
			torrentHash: torrent.Hash,
			contentPath: torrent.ContentPath,
			category:    torrent.Category,
			release:     parsed,
		}
	}

	return matched
}

func (s *Service) resolveSeasonPackLocalFiles(
	ctx context.Context,
	instanceID int,
	episodes map[episodeIdentity]episodeMatch,
) (map[episodeIdentity]seasonPackLocalFile, error) {
	hashes := make([]string, 0, len(episodes))
	for _, episode := range episodes {
		hashes = append(hashes, episode.torrentHash)
	}

	filesByHash, err := s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
	if err != nil {
		return nil, fmt.Errorf("load matched episode files: %w", err)
	}

	normalizer := seasonPackNormalizer(s)
	resolved := make(map[episodeIdentity]seasonPackLocalFile, len(episodes))

	for id, episode := range episodes {
		files, ok := filesByHash[normalizeHash(episode.torrentHash)]
		if !ok || len(files) == 0 {
			return nil, fmt.Errorf("%w: no file list for torrent %s", errLayoutMismatch, episode.torrentHash)
		}

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
			return nil, fmt.Errorf("%w: expected exactly one playable episode file in torrent %s", errLayoutMismatch, episode.torrentHash)
		}

		resolved[id] = seasonPackLocalFile{
			sourcePath: matchedSourcePath,
			size:       matchedFileSize,
			release:    matchedRelease,
		}
	}

	return resolved, nil
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
		if !matcher.releasesMatch(packFileRelease, localFile.release, false) {
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
	cleanedPath := filepath.Clean(filepath.FromSlash(strings.ReplaceAll(relativePath, "\\", "/")))
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

	if err := os.RemoveAll(plan.RootDir); err != nil && !errors.Is(err, os.ErrNotExist) {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
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
