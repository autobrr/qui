// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"iter"
	"path"
	"path/filepath"
	"slices"
	"sort"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/releases"
)

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

var errLayoutMismatch = errors.New("layout_mismatch")

var errUnsafePieceBoundary = fmt.Errorf("%w: unsafe piece boundary with pending files", errLayoutMismatch)

var errSkippedRecheck = errors.New("skipped_recheck")

var errCoverageDrifted = errors.New("coverage_drifted")

const seasonPackResumeSlack = 0.98

type episodeIdentity struct {
	series  int
	episode int
}

func rangeEpisodeIdentity(r *rls.Release) episodeIdentity {
	eps := r.SeriesEpisodes()
	first := eps[0]
	if i := slices.IndexFunc(eps, func(se []int) bool { return se[1] > 0 }); i >= 0 {
		first = eps[i]
	}
	return episodeIdentity{series: first[0], episode: first[1]}
}

type episodeMatch struct {
	manual      bool
	torrentHash string
	contentPath string
	category    string
	name        string
	release     *rls.Release
	titles      []string
}

type seasonPackLocalFile struct {
	sourcePath string
	size       int64
	release    *rls.Release
	packFile   string
}

type seasonPackPlanBuild struct {
	plan              *hardlinktree.TreePlan
	created           *fsops.TreeCreateResult
	backend           fsops.Backend
	packDir           string
	baseDir           string
	materializedPaths map[string]struct{}
	linkedBytes       int64
	totalBytes        int64
	totalFiles        int
	nextBaseDir       func() (*seasonPackPlanBuild, error)
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

type seasonPackPrep struct {
	manual         bool
	heldOnly       bool
	skipAutoResume bool
	rejected       map[string]string
	settings       *models.CrossSeedAutomationSettings
	packName       string
	packRelease    *rls.Release
	meta           TorrentMetadata
	torrentBytes   []byte
	packEpisodes   map[episodeIdentity]packEpisodeOrigin
	totalEpisodes  int
	eligible       []*models.Instance
	threshold      float64
	aliasTitles    []string
	sonarrAnswers  bool
}

const seasonPackNoEligibleInstances = "no instances with hardlink/reflink mode and filesystem write access (Local Filesystem Access, or SSH access, in instance settings)"

func (s *Service) prepareSeasonPackLocally(ctx context.Context, torrentName, torrentData string, instanceIDs []int, autonomous, needsTorrent bool) (*seasonPackPrep, string, string, error) {
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, "", "", fmt.Errorf("load automation settings: %w", err)
	}
	if autonomous {
		if !settings.SeasonPackAutomationEnabled {
			return nil, "disabled", "", nil
		}
	} else if !settings.SeasonPackEnabled {
		return nil, "disabled", "", nil
	}
	switch {
	case needsTorrent && (torrentName == "" || torrentData == ""):
		return nil, "invalid_payload", "torrentName and torrentData are required", nil
	case torrentName == "":
		return nil, "invalid_payload", "torrentName is required", nil
	}

	prep := s.newSeasonPackPrep(torrentName, settings)
	if prep == nil {
		return nil, "not_season_pack", fmt.Sprintf("release %q is not a season pack", torrentName), nil
	}
	if needsTorrent {
		if message := s.readSeasonPackTorrent(prep, torrentData); message != "" {
			return nil, "invalid_torrent", message, nil
		}
	}

	instances, resolveErr := s.resolveInstances(ctx, instanceIDs)
	if resolveErr != nil {
		return nil, "", "", fmt.Errorf("resolve instances: %w", resolveErr)
	}

	prep.eligible = filterLinkEligible(instances)
	if len(prep.eligible) == 0 {
		return nil, "no_eligible_instances", seasonPackNoEligibleInstances, nil
	}
	return prep, "", "", nil
}

func (s *Service) newSeasonPackPrep(name string, settings *models.CrossSeedAutomationSettings) *seasonPackPrep {
	packRelease := s.matcher().parseSeasonPackName(name)
	if !isTVSeasonPack(packRelease) {
		return nil
	}
	prep := &seasonPackPrep{
		settings:    settings,
		packName:    name,
		packRelease: packRelease,
		threshold:   settings.SeasonPackCoverageThreshold,
	}
	if prep.threshold <= 0 {
		prep.threshold = 0.75
	}
	return prep
}

func (s *Service) readSeasonPackTorrent(prep *seasonPackPrep, torrentData string) (problem string) {
	torrentBytes, err := base64.StdEncoding.DecodeString(torrentData)
	if err != nil {
		torrentBytes, err = decodeBase64Variants(torrentData)
	}
	if err != nil {
		return "failed to decode torrent data"
	}
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return "failed to parse torrent metadata"
	}
	prep.torrentBytes, prep.meta = torrentBytes, meta
	prep.packEpisodes = s.matcher().extractPackEpisodes(meta.Files, prep.packRelease)
	if len(prep.packEpisodes) == 0 {
		return "no playable episode files found in torrent"
	}
	return ""
}

func (s *Service) lookUpSeason(ctx context.Context, prep *seasonPackPrep) {
	season := s.seasonPackSeasonLookup(ctx, prep.packName, prep.packRelease)
	prep.aliasTitles, prep.sonarrAnswers = season.titles, season.fromSonarr
	prep.totalEpisodes = max(len(prep.packEpisodes), season.total)
	if prep.totalEpisodes == 0 {
		prep.totalEpisodes = -1
	}
}

func (s *Service) checkSeasonPack(ctx context.Context, req seasonPackRequest) (*seasonPackCheckResult, error) {
	prep, reason, message, prepErr := s.prepareSeasonPackLocally(ctx, req.name, req.torrentData, req.instanceIDs, false, req.torrentData != "")
	if prep == nil {
		if prepErr != nil {
			return nil, prepErr
		}
		resp := &seasonPackCheckResult{reason: reason, message: message}
		s.recordCheckRun(ctx, req.name, resp, 0)
		return resp, nil
	}

	lookup := s.seasonPackLookup(ctx, prep, prep.eligible)
	defer lookup.refreshStale()
	if seasonPackNotHeld(ctx, lookup) {
		log.Trace().Str("torrentName", req.name).Msg("season pack: check found no held episode")
		return buildCheckResponse(nil, nil, 0, prep.threshold), nil
	}
	return s.checkPreparedSeasonPack(ctx, prep, lookup)
}

func (s *Service) checkPreparedSeasonPack(ctx context.Context, prep *seasonPackPrep, lookup *heldLookup) (*seasonPackCheckResult, error) {
	s.lookUpSeason(ctx, prep)
	lookup.addTitles(prep.aliasTitles)

	matches, _, err := s.computeCoverage(ctx, prep, lookup)
	if err == nil && len(prep.passing(matches)) == 0 && lookup.awaitDueSyncs(ctx) {
		matches, _, err = s.computeCoverage(ctx, prep, lookup)
	}
	if err != nil {
		return nil, err
	}

	if prep.totalEpisodes == -1 {
		return s.checkSeasonPackNoThreshold(ctx, prep.packName, matches), nil
	}
	passing := prep.passing(matches)
	resp := buildCheckResponse(passing, matches, prep.totalEpisodes, prep.threshold)
	s.recordCheckRun(ctx, prep.packName, resp, prep.totalEpisodes)
	return resp, nil
}

func (prep *seasonPackPrep) passing(matches []seasonPackCoverage) []seasonPackCoverage {
	if prep.totalEpisodes == -1 {
		return matches
	}
	var passing []seasonPackCoverage
	for _, m := range matches {
		if m.coverage >= prep.threshold {
			passing = append(passing, m)
		}
	}
	return passing
}

func (s *Service) checkSeasonPackNoThreshold(ctx context.Context, torrentName string, matches []seasonPackCoverage) *seasonPackCheckResult {
	if len(matches) > 0 {
		best := matches[0]
		for i := range matches[1:] {
			if matches[i+1].matched > best.matched {
				best = matches[i+1]
			}
		}
		resp := &seasonPackCheckResult{
			ready:            true,
			message:          fmt.Sprintf("%d instance(s) have matching episodes (threshold skipped, no episode total available)", len(matches)),
			matches:          matches,
			best:             best,
			thresholdSkipped: true,
		}
		s.recordCheckRunNoThreshold(ctx, torrentName, best.matched, best.instanceID)
		return resp
	}

	resp := &seasonPackCheckResult{
		reason:           "no_matches",
		message:          "no matching episodes found on any instance",
		thresholdSkipped: true,
	}
	s.recordCheckRun(ctx, torrentName, resp, 0)
	return resp
}

func (s *Service) applySeasonPack(ctx context.Context, req seasonPackRequest) (*seasonPackApplyResult, error) {
	prep, reason, message, prepErr := s.prepareSeasonPackLocally(ctx, req.name, req.torrentData, req.instanceIDs, req.autonomous, true)
	if prep == nil {
		if prepErr != nil {
			return nil, prepErr
		}
		s.recordApplyRun(ctx, req.name, reason, message, 0, 0, 0, 0, 0, "")
		return &seasonPackApplyResult{reason: reason, message: message}, nil
	}

	return s.applyPreparedSeasonPack(ctx, prep, req.indexer)
}

func (s *Service) applyPreparedSeasonPack(ctx context.Context, prep *seasonPackPrep, indexer string) (*seasonPackApplyResult, error) {
	hashes := collectHashes(prep.meta)
	pack, release := s.seasonPackApplies.claim(hashes[0])
	defer release()
	s.lookUpSeason(ctx, prep)
	giveUp, err := pack.takeTurn(ctx)
	if err != nil {
		return nil, err
	}
	defer giveUp()

	checks := make(map[int]PreAddCheck, len(prep.eligible))
	routes := make(map[int]*candidateRoute)
	unblocked := make([]*models.Instance, 0, len(prep.eligible))
	blockedID := 0
	for _, inst := range prep.eligible {
		route, routeErr := s.routeOf(ctx, inst.ID)
		if routeErr != nil {
			status, message := routeFailure(routeErr)
			reason := seasonPackRouteReason(status)
			s.recordApplyRun(ctx, prep.packName, reason, message, inst.ID, route.receiverOf(0), 0, prep.totalEpisodes, 0, "")
			return &seasonPackApplyResult{reason: reason, message: message, instanceID: inst.ID}, nil
		}
		receiverID := route.receiverOf(inst.ID)
		routes[inst.ID] = route
		check, refusal := s.adder.Check(ctx, receiverID, AddFacts{}, hashes...)
		if refusal == nil && pack.addedTo[receiverID] {
			refusal = &AddResult{Status: StatusExists, Message: existsInThisInstance}
		}
		switch {
		case refusal == nil:
			checks[inst.ID] = check
			unblocked = append(unblocked, inst)
		case refusal.Status == StatusBlocked:
			blockedID = cmp.Or(blockedID, inst.ID)
		case refusal.Status == StatusExists:
			message := fmt.Sprintf("torrent already exists on instance %d", receiverID)
			if refusal.Message != existsInThisInstance {
				message = refusal.Message
			}
			s.recordApplyRun(ctx, prep.packName, "already_exists", "", inst.ID, receiverID, 0, prep.totalEpisodes, 0, "")
			return &seasonPackApplyResult{reason: "already_exists", message: message, instanceID: inst.ID}, nil
		default:
			reason, message := "existing_check_failed", fmt.Sprintf("failed to check existing torrents on instance %d: %v", receiverID, refusal.Err)
			if strings.HasPrefix(refusal.Message, blocklistCheckFailed) {
				reason, message = "blocklist_check_failed", fmt.Sprintf("failed to check cross-seed blocklist on instance %d: %v", receiverID, refusal.Err)
			}
			s.recordApplyRun(ctx, prep.packName, reason, message, inst.ID, receiverID, 0, prep.totalEpisodes, 0, "")
			return &seasonPackApplyResult{reason: reason, message: message, instanceID: inst.ID}, nil
		}
	}
	if len(unblocked) == 0 {
		blockedOn := routes[blockedID].receiverOf(blockedID)
		s.recordApplyRun(ctx, prep.packName, "blocked", "", blockedID, blockedOn, 0, prep.totalEpisodes, 0, "")
		return &seasonPackApplyResult{
			reason:     "blocked",
			message:    fmt.Sprintf("torrent is on the cross-seed blocklist for instance %d", blockedOn),
			instanceID: blockedID,
		}, nil
	}
	prep.eligible = unblocked

	lookup := s.seasonPackLookup(ctx, prep, prep.eligible)
	defer lookup.refreshStale()
	matches, matchedTitles, err := s.computeCoverage(ctx, prep, lookup)
	if err != nil {
		message := err.Error()
		s.recordApplyRun(ctx, prep.packName, "coverage_check_failed", message, 0, 0, 0, prep.totalEpisodes, 0, "")
		return &seasonPackApplyResult{
			reason:  "coverage_check_failed",
			message: message,
		}, nil
	}

	winner := selectWinner(matches, prep.threshold)
	if winner == nil {
		best := selectWinner(matches, 0)
		bestInstance, bestMatched, bestCoverage := 0, 0, 0.0
		if best != nil {
			bestInstance, bestMatched, bestCoverage = best.instanceID, best.matched, best.coverage
		}
		s.recordApplyRun(ctx, prep.packName, "drifted", "no instance meets coverage threshold at apply time", bestInstance, routes[bestInstance].receiverOf(bestInstance), bestMatched, prep.totalEpisodes, bestCoverage, "")
		return &seasonPackApplyResult{
			reason:     "drifted",
			message:    "coverage no longer meets threshold",
			instanceID: bestInstance,
		}, nil
	}

	linkMode := determineLinkMode(prep.eligible, winner.instanceID)
	inst := findInstance(prep.eligible, winner.instanceID)
	route := routes[winner.instanceID]
	if route != nil {
		if status, message := s.loadSource(ctx, route, winner.instanceID); status != "" {
			reason := seasonPackRouteReason(status)
			s.recordApplyRun(ctx, prep.packName, reason, message, winner.instanceID, route.crossSeed.ID, winner.matched, prep.totalEpisodes, winner.coverage, "")
			return &seasonPackApplyResult{reason: reason, message: message, instanceID: winner.instanceID}, nil
		}
		inst = route.source
		linkMode = determineLinkMode([]*models.Instance{inst}, inst.ID)
	}

	planBuild, episodes, err := s.assembleSeasonPack(ctx, prep, inst, route, winner, matchedTitles[winner.instanceID], linkMode, indexer)
	if err != nil {
		return s.failApply(ctx, prep.packName, err, prep, winner, route.receiverOf(winner.instanceID))
	}

	crossCategory := s.resolveSeasonPackCategory(ctx, prep, indexer, episodes)
	result, err := s.addSeasonPack(ctx, prep, inst, route, checks[inst.ID], planBuild, episodes, crossCategory, prep.settings.SeasonPackTags, linkMode, prep.packName)
	if err == nil && result.applied {
		pack.addedTo[result.addedInstanceID] = true
	}
	return result, err
}

func seasonPackRouteReason(status string) string {
	if status == "error" {
		return "link_failed"
	}
	return status
}

func seasonPackSavePath(route *candidateRoute, build *seasonPackPlanBuild) (string, bool) {
	if route == nil {
		return build.plan.RootDir, true
	}
	return crossseedroute.ToCrossSeed(build.backend.Paths(), route.rows, build.baseDir, build.plan.RootDir)
}

func (s *Service) addSeasonPack(
	ctx context.Context, prep *seasonPackPrep, inst *models.Instance, route *candidateRoute, check PreAddCheck, planBuild *seasonPackPlanBuild,
	episodes map[episodeIdentity]episodeMatch, crossCategory string, tags []string, linkMode, torrentName string,
) (*seasonPackApplyResult, error) {
	receiverID := route.receiverOf(inst.ID)
	fail := func(reason, cause, message string) (*seasonPackApplyResult, error) {
		if rollbackErr := rollbackSeasonPackTree(ctx, planBuild.backend, planBuild.created, planBuild.packDir); rollbackErr != nil {
			log.Warn().Err(rollbackErr).Str("torrentName", torrentName).Msg("season pack: failed to rollback after add failure")
		}
		s.recordApplyRun(ctx, torrentName, reason, cause, inst.ID, receiverID, len(episodes), prep.totalEpisodes, float64(len(episodes))/float64(prep.totalEpisodes), linkMode)
		return &seasonPackApplyResult{reason: reason, message: message, instanceID: inst.ID}, nil
	}
	savePath, mapped := seasonPackSavePath(route, planBuild)
	if !mapped {
		message := "the cross-seed route has no row for base folder " + planBuild.baseDir
		return fail("link_failed", message, message)
	}
	if route != nil {
		if status, message := s.checkVisible(ctx, route, planBuild.backend.Paths(), planBuild.baseDir, planBuild.plan.Files[0].TargetPath); status != "" {
			return fail(seasonPackRouteReason(status), message, message)
		}
	}

	if _, err := s.ensureCrossCategory(ctx, receiverID, crossCategory, "", false); err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).Str("category", crossCategory).Msg("season pack: failed to ensure cross-seed category exists")
	}

	facts := seasonPackFacts(prep, planBuild)
	if linkMode == "hardlink" {
		facts.LinkedPaths = planBuild.materializedPaths
		facts.SeasonPackRun = &models.SeasonPackRun{
			TorrentName:     torrentName,
			Phase:           "resume",
			InstanceID:      &inst.ID,
			AddedInstanceID: &receiverID,
			MatchedEpisodes: len(episodes),
			TotalEpisodes:   prep.totalEpisodes,
			Coverage:        float64(len(episodes)) / float64(prep.totalEpisodes),
			LinkMode:        linkMode,
		}
	}
	added := s.adder.Add(ctx, AddRequest{
		Check:   check,
		Torrent: prep.torrentBytes,
		Facts:   facts,
		Placement: AddPlacement{
			SavePath:      savePath,
			Category:      crossCategory,
			Tags:          tags,
			ContentLayout: "Original",
		},
	})
	if added.Outcome == AddRefused {
		cause := added.Message
		if added.Err != nil {
			cause = added.Err.Error()
		}
		return fail("add_failed", cause, "failed to add torrent to qbittorrent")
	}

	message := ""
	if added.Outcome == AddSettling {
		switch outcome := added.Settled; {
		case outcome.QueueFull && outcome.RecheckFailed:
			message = "torrent added paused; automatic recheck failed"
		case outcome.QueueFull:
			message = "torrent added paused; automatic resume queue is full"
		case outcome.RecheckFailed:
			message = "torrent added paused; automatic recheck failed, retry queued"
		default:
			message = "torrent added paused; recheck queued"
		}
	}

	matched := len(episodes)
	coverage := float64(matched) / float64(prep.totalEpisodes)
	s.recordApplyRun(ctx, torrentName, "applied", message, inst.ID, receiverID, matched, prep.totalEpisodes, coverage, linkMode)

	return &seasonPackApplyResult{
		applied:         true,
		message:         message,
		instanceID:      inst.ID,
		addedInstanceID: receiverID,
		matched:         matched,
		total:           prep.totalEpisodes,
		coverage:        coverage,
		linkMode:        linkMode,
	}, nil
}

func seasonPackFacts(prep *seasonPackPrep, planBuild *seasonPackPlanBuild) AddFacts {
	return AddFacts{
		LinkTree:         true,
		PendingFiles:     planBuild.hasPendingFiles(),
		ManualAssembly:   prep.manual,
		SeasonPackLinked: float64(planBuild.linkedBytes) / float64(planBuild.totalBytes),
		SkipRecheck:      prep.settings.SkipRecheck,
		SkipAutoResume:   prep.skipAutoResume,
	}
}

func (s *Service) assembleSeasonPack(
	ctx context.Context,
	prep *seasonPackPrep,
	inst *models.Instance,
	route *candidateRoute,
	winner *seasonPackCoverage,
	matchedTitles map[string][]string,
	linkMode string,
	indexer string,
) (*seasonPackPlanBuild, map[episodeIdentity]episodeMatch, error) {
	if inst == nil {
		return nil, nil, fmt.Errorf("%w: no instance found for winner", errLayoutMismatch)
	}
	if inst.HardlinkBaseDir == "" {
		return nil, nil, fmt.Errorf("%w: hardlink base dir not configured on instance %d", errLayoutMismatch, inst.ID)
	}

	lookup := s.seasonPackLookup(ctx, prep, []*models.Instance{inst})
	torrents, err := s.seasonPackTorrents(ctx, prep, lookup, matchedTitles)(inst.ID)
	if err != nil {
		return nil, nil, fmt.Errorf("link_failed: %w", err)
	}

	candidates := s.matchEpisodeTorrents(torrents, prep.packName, prep.packRelease, prep.packEpisodes, prep.settings, prep.aliasTitles)
	if len(candidates) < winner.matched {
		return nil, nil, fmt.Errorf("%w: episode count drifted during apply", errLayoutMismatch)
	}

	planBuild, episodes, err := s.planSeasonPack(ctx, prep, inst, route, candidates, linkMode, indexer)
	if err != nil {
		return nil, nil, err
	}
	if err := s.createSeasonPackTree(ctx, planBuild, linkMode); err != nil {
		return nil, nil, err
	}
	return planBuild, episodes, nil
}

func (s *Service) planSeasonPack(
	ctx context.Context, prep *seasonPackPrep, inst *models.Instance, route *candidateRoute,
	candidates map[episodeIdentity][]episodeMatch, linkMode, indexer string,
) (*seasonPackPlanBuild, map[episodeIdentity]episodeMatch, error) {
	backend, presetName := fsops.Backend(nil), inst.Name
	if route != nil {
		backend, presetName = route.backend, route.crossSeed.Name
	} else {
		var err error
		if backend, err = s.getBackendForInstance(ctx, inst.ID, models.CapabilityWrite); err != nil {
			return nil, nil, fmt.Errorf("no filesystem backend: %w", err)
		}
	}
	d := backend.Paths()

	episodes, localFiles, err := s.resolveSeasonPackLocalFilesForCandidates(ctx, d, inst.ID, candidates, prep.meta.Files, prep.packRelease, prep.packName, prep.settings, prep.aliasTitles, prep.rejected)
	if err != nil {
		return nil, nil, err
	}
	if !prep.manual && float64(len(episodes)) < float64(prep.totalEpisodes)*prep.threshold {
		return nil, nil, fmt.Errorf("%w: local files cover %d/%d episodes, below coverage threshold", errCoverageDrifted, len(episodes), prep.totalEpisodes)
	}

	if prep.manual {
		for id, file := range localFiles {
			info, statErr := backend.Stat(ctx, file.sourcePath)
			if statErr != nil || !info.Mode.IsRegular() || info.Size != file.size {
				prep.rejected[normalizeHash(episodes[id].torrentHash)] = "local_file_unavailable"
				delete(episodes, id)
				delete(localFiles, id)
			}
		}
	}

	selectedBaseDir, retryDirs, err := selectSeasonPackBaseDir(ctx, backend, inst.HardlinkBaseDir, localFiles)
	if err != nil {
		return nil, nil, err
	}

	buildForDir := func(baseDir string) (*seasonPackPlanBuild, error) {
		destDir := s.seasonPackDestDir(ctx, d, inst, presetName, baseDir, prep.torrentBytes, indexer)
		build, err := s.matcher().buildSeasonPackPlan(d, prep.meta.Files, prep.meta.Name, destDir, localFiles)
		if err != nil {
			return nil, err
		}
		build.backend, build.baseDir = backend, baseDir
		return build, nil
	}
	planBuild, err := buildForDir(selectedBaseDir)
	if err != nil {
		return nil, episodes, err
	}
	sourcePaths := seasonPackSourcePaths(localFiles)
	planBuild.nextBaseDir = func() (*seasonPackPlanBuild, error) {
		for len(retryDirs) > 0 {
			dir := retryDirs[0]
			retryDirs = retryDirs[1:]
			if ok, err := seasonPackBaseDirMatchesAllSources(ctx, backend, dir, sourcePaths); err == nil && ok {
				return buildForDir(dir)
			}
		}
		return nil, nil
	}
	facts := seasonPackFacts(prep, planBuild)
	if facts.Verdict().Refused {
		return nil, nil, fmt.Errorf("%w: incomplete season pack requires recheck, but Skip Recheck is enabled", errSkippedRecheck)
	}

	var pieceSafety PieceBoundarySafetyResult
	facts.UnsafePieces, pieceSafety = hasUnsafeSeasonPackPendingFiles(prep.meta.Info, planBuild.materializedPaths)
	facts.ReflinkAdd = linkMode == "reflink"
	facts.SkipPieceBoundaryCheck = prep.settings.SkipPieceBoundarySafetyCheck
	if facts.Verdict().Refused {
		return planBuild, episodes, fmt.Errorf("%w: %s", errUnsafePieceBoundary, pieceSafety.Reason)
	}

	return planBuild, episodes, nil
}

func (s *Service) createSeasonPackTree(ctx context.Context, planBuild *seasonPackPlanBuild, linkMode string) error {
	backend := planBuild.backend
	createFn := s.seasonPackLinkCreator
	if createFn == nil {
		createFn = backend.HardlinkTree
		if linkMode == "reflink" {
			createFn = backend.ReflinkTree
		}
	}
	for {
		created, err := createFn(ctx, planBuild.plan)
		if err == nil {
			planBuild.created = created
			return nil
		}
		if rollbackErr := rollbackSeasonPackTree(ctx, backend, created, planBuild.packDir); rollbackErr != nil {
			return fmt.Errorf("link_failed: %w", errors.Join(err, fmt.Errorf("rollback failed: %w", rollbackErr)))
		}
		if !isCrossDeviceLinkError(err) || errors.Is(err, hardlinktree.ErrRollbackFailed) || planBuild.nextBaseDir == nil {
			return fmt.Errorf("link_failed: %w", err)
		}
		next, buildErr := planBuild.nextBaseDir()
		if buildErr != nil {
			return fmt.Errorf("link_failed: %w", errors.Join(err, buildErr))
		}
		if next == nil {
			return fmt.Errorf("link_failed: %w", err)
		}
		log.Warn().Err(err).Str("rootDir", planBuild.plan.RootDir).Msg("cross-device season pack link, trying the next base dir")
		next.nextBaseDir = planBuild.nextBaseDir
		*planBuild = *next
	}
}

func (s *Service) seasonPackDestDir(ctx context.Context, d fsops.PathDialect, inst *models.Instance, instanceName, baseDir string, torrentBytes []byte, indexer string) string {
	switch inst.HardlinkDirPreset {
	case "by-tracker":
		incomingTrackerDomain := ParseTorrentAnnounceDomain(torrentBytes)
		trackerDisplayName := s.resolveTrackerDisplayName(ctx, incomingTrackerDomain, &CrossSeedRequest{IndexerName: indexer})
		return d.Join(baseDir, pathutil.SanitizePathSegment(trackerDisplayName))
	case "by-instance":
		return d.Join(baseDir, pathutil.SanitizePathSegment(instanceName))
	default:
		return baseDir
	}
}

func selectSeasonPackBaseDir(ctx context.Context, backend fsops.Backend, configuredDirs string, localFiles map[episodeIdentity]seasonPackLocalFile) (string, []string, error) {
	dirs := parseSeasonPackBaseDirs(configuredDirs)
	if len(dirs) == 0 {
		return "", nil, fmt.Errorf("%w: hardlink base dir not configured", errLayoutMismatch)
	}

	sourcePaths := seasonPackSourcePaths(localFiles)
	if len(sourcePaths) == 0 {
		return "", nil, fmt.Errorf("%w: no resolved episode files for base dir selection", errLayoutMismatch)
	}

	var lastErr error
	ranked := rankBaseDirs(backend.Paths(), dirs, sourcePaths[0])
	for i, dir := range ranked {
		matchesAllSources, err := seasonPackBaseDirMatchesAllSources(ctx, backend, dir, sourcePaths)
		if err != nil {
			lastErr = err
			continue
		}
		if matchesAllSources {
			return dir, ranked[i+1:], nil
		}
	}

	if lastErr != nil {
		return "", nil, fmt.Errorf("%w: no base directory on same filesystem as season pack sources (last error: %w)", errLayoutMismatch, lastErr)
	}
	return "", nil, fmt.Errorf("%w: no base directory on same filesystem as season pack sources", errLayoutMismatch)
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

func seasonPackBaseDirMatchesAllSources(ctx context.Context, backend fsops.Backend, dir string, sourcePaths []string) (bool, error) {
	filesystemDir := dir
	info, err := backend.Stat(ctx, filesystemDir)
	if errors.Is(err, fs.ErrNotExist) {
		filesystemDir = nearestExistingParent(ctx, backend, dir)
		info, err = backend.Stat(ctx, filesystemDir)
	}
	if err != nil {
		return false, fmt.Errorf("failed to inspect directory %s: %w", dir, err)
	}
	if !info.IsDir {
		return false, fmt.Errorf("base path %s is not a directory", dir)
	}

	for _, sourcePath := range sourcePaths {
		sameFS, err := backend.SameFilesystem(ctx, sourcePath, filesystemDir)
		if err != nil && errors.Is(err, fs.ErrNotExist) {
			existingParent := nearestExistingParent(ctx, backend, sourcePath)
			if existingParent != "" {
				sameFS, err = backend.SameFilesystem(ctx, existingParent, filesystemDir)
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

func nearestExistingParent(ctx context.Context, backend fsops.Backend, path string) string {
	d := backend.Paths()
	for dir := d.Dir(path); dir != "." && dir != ""; dir = d.Dir(dir) {
		if _, err := backend.Stat(ctx, dir); err == nil {
			return dir
		}
		next := d.Dir(dir)
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

func (s *Service) failApply(
	ctx context.Context,
	torrentName string,
	err error,
	prep *seasonPackPrep,
	winner *seasonPackCoverage,
	addedInstanceID int,
) (*seasonPackApplyResult, error) {
	reason := "link_failed"
	if errors.Is(err, errLayoutMismatch) {
		reason = "layout_mismatch"
	} else if errors.Is(err, errSkippedRecheck) {
		reason = "skipped_recheck"
	} else if errors.Is(err, errCoverageDrifted) {
		reason = "drifted"
	}
	s.recordApplyRun(ctx, torrentName, reason, err.Error(), winner.instanceID, addedInstanceID, winner.matched, prep.totalEpisodes, winner.coverage, "")
	return &seasonPackApplyResult{reason: reason, message: err.Error(), instanceID: winner.instanceID}, nil
}

func findInstance(instances []*models.Instance, id int) *models.Instance {
	for _, inst := range instances {
		if inst.ID == id {
			return inst
		}
	}
	return nil
}

func (m matcher) parseSeasonPackEpisodePayload(
	fileName string,
	torrentRelease *rls.Release,
) (release *rls.Release, seasonlessOrigin, ok bool) {
	ext := strings.ToLower(path.Ext(fileName))
	if _, known := videoExtensions[ext]; !known {
		return nil, false, false
	}
	if shouldIgnoreFile(fileName, m.normalizer()) {
		return nil, false, false
	}

	parsed := m.parseFileRelease(path.Base(fileName))
	if releases.IsEpisodeRange(parsed) {
		first := *parsed
		id := rangeEpisodeIdentity(parsed)
		first.Type, first.Series, first.Episode = rls.Episode, id.series, id.episode
		parsed = &first
	}
	if !isTVEpisode(parsed) {
		return nil, false, false
	}
	seasonlessOrigin = parsed.Series == 0

	enriched := enrichReleaseFromTorrent(parsed, torrentRelease)
	if torrentRelease != nil && torrentRelease.Series > 0 && enriched.Series != torrentRelease.Series {
		return nil, false, false
	}

	return enriched, seasonlessOrigin, true
}

const notInPackReason = "episode not in pack"

func packEpisodeRejectReason(inPack bool) string {
	if inPack {
		return "episode numbering mismatch"
	}
	return notInPackReason
}

type packEpisodeOrigin struct {
	absolute bool
	seasoned bool
	file     qbt.TorrentFile
}

func seasonlessFilesPerSeason(pack *rls.Release, lowestEpisode int) bool {
	return pack != nil && cmp.Or(pack.Series, inferredSeasonNumeral(pack)) >= 2 && lowestEpisode <= 1
}

func (m matcher) parseSeasonPackName(name string) *rls.Release {
	release := m.parseReleaseName(name)
	season := inferredSeasonNumeral(release)
	if season == 0 {
		return release
	}
	pack := *release
	pack.Type, pack.Series = rls.Series, season
	return &pack
}

func (m matcher) extractPackEpisodes(files qbt.TorrentFiles, packRelease *rls.Release) map[episodeIdentity]packEpisodeOrigin {
	episodes := make(map[episodeIdentity]packEpisodeOrigin)

	minSeasonlessEpisode := -1
	for _, f := range files {
		parsed, seasonlessOrigin, ok := m.parseSeasonPackEpisodePayload(f.Name, packRelease)
		if !ok {
			continue
		}
		if seasonlessOrigin && (minSeasonlessEpisode < 0 || parsed.Episode < minSeasonlessEpisode) {
			minSeasonlessEpisode = parsed.Episode
		}

		id := episodeIdentity{series: parsed.Series, episode: parsed.Episode}
		origin := episodes[id]
		origin.file = f
		if seasonlessOrigin {
			origin.absolute = true
		} else {
			origin.seasoned = true
		}
		episodes[id] = origin
	}

	if minSeasonlessEpisode >= 0 && seasonlessFilesPerSeason(packRelease, minSeasonlessEpisode) {
		for id, origin := range episodes {
			if origin.absolute {
				episodes[id] = packEpisodeOrigin{seasoned: true, file: origin.file}
			}
		}
	}

	return episodes
}

func filterLinkEligible(instances []*models.Instance) []*models.Instance {
	var eligible []*models.Instance
	for _, inst := range instances {
		if !models.FilesystemCapabilitiesOf(inst).Write {
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

func seasonPackPolicy(settings *models.CrossSeedAutomationSettings) matchPolicy {
	return matchPolicy{
		SizeTolerancePercent:       defaultSizeMismatchTolerancePercent,
		AllowMissingCandidateGroup: true,
		SeasonPack:                 seasonPackMatchOptionsFromSettings(settings),
	}
}

func asEpisode(release *rls.Release, episode int) *rls.Release {
	copied := *release
	copied.Type, copied.Episode = rls.Episode, episode
	return &copied
}

func (opts seasonPackMatchOptions) apply(side namedRelease) namedRelease {
	if opts == (seasonPackMatchOptions{}) || side.release == nil {
		return side
	}
	release := *side.release
	if opts.skipRepackCompare {
		release.Other = stripSeasonPackRepackTags(release.Other)
	}
	if opts.simplifyHDRCompare {
		release.HDR = simplifySeasonPackHDRTags(release.HDR)
	}
	if opts.simplifyWEBCompare {
		simplifySeasonPackWEBSource(&release)
	}
	if opts.skipYearCompare {
		release.Year, release.Month, release.Day = 0, 0, 0
	}
	side.release = &release
	return side
}

func stripSeasonPackRepackTags(values []string) []string {
	if len(values) == 0 {
		return nil
	}

	filtered := make([]string, 0, len(values))
	for _, value := range values {
		normalized := normalizeVariant(value)
		if normalized == "" || slices.ContainsFunc(variantTokens(normalized), func(token string) bool {
			return slices.Contains(nonPackVariantOverrides.other, token)
		}) {
			continue
		}
		filtered = append(filtered, value)
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

type seasonLookup struct {
	total      int
	titles     []string
	fromSonarr bool
}

func (s *Service) seasonPackSeasonLookup(ctx context.Context, torrentName string, packRelease *rls.Release) seasonLookup {
	if lookup := s.seasonPackEpisodeTotalLookup; lookup != nil {
		total, titles, ok := lookup(ctx, torrentName, packRelease)
		if !ok {
			total = 0
		}
		return seasonLookup{total: total, titles: titles}
	}
	return s.lookupSeasonPackEpisodeTotal(ctx, torrentName, packRelease)
}

func (s *Service) lookupSeasonPackEpisodeTotal(ctx context.Context, torrentName string, packRelease *rls.Release) seasonLookup {
	if s == nil || packRelease == nil || packRelease.Series <= 0 || torrentName == "" {
		return seasonLookup{}
	}

	var season seasonLookup
	if !isNilARRLookupService(s.arrService) {
		result, err := s.arrService.LookupSeasonEpisodeTotal(ctx, torrentName, packRelease.Series)
		if err != nil {
			log.Debug().Err(err).Str("torrentName", torrentName).Int("season", packRelease.Series).
				Msg("season pack: failed to resolve Sonarr season total")
		} else if result != nil {
			season = seasonLookup{total: result.TotalEpisodes, titles: result.Titles, fromSonarr: true}
			if season.total > 0 {
				return season
			}
		}
	}

	metaSvc := s.getMetadataService(ctx)
	if metaSvc != nil {
		for _, showTitle := range seasonPackMetadataTitles(packRelease) {
			total, err := metaSvc.LookupEpisodeTotal(ctx, showTitle, packRelease.Series)
			if err != nil {
				log.Debug().Err(err).Str("torrentName", torrentName).Str("title", showTitle).Int("season", packRelease.Series).
					Msg("season pack: metadata provider lookup failed")
			} else if total > 0 {
				season.total = total
				return season
			}
		}
	}

	return season
}

func seasonPackMetadataTitles(packRelease *rls.Release) []string {
	titles := []string{strings.ReplaceAll(packRelease.Title, ".", " ")}
	if base := titleWithoutSeasonNumeral(packRelease); base != "" {
		titles = append(titles, strings.ReplaceAll(base, ".", " "))
	}
	return titles
}

func (s *Service) computeCoverage(ctx context.Context, prep *seasonPackPrep, lookup *heldLookup) (matches []seasonPackCoverage, matchedTitles map[int]map[string][]string, err error) {
	matchedTitles = map[int]map[string][]string{}
	torrentsOf := s.seasonPackTorrents(ctx, prep, lookup, nil)
	for _, inst := range prep.eligible {
		torrents, err := torrentsOf(inst.ID)
		if err != nil {
			return nil, nil, fmt.Errorf("load cached torrents for instance %d: %w", inst.ID, err)
		}

		matched := s.matchEpisodeTorrents(torrents, prep.packName, prep.packRelease, prep.packEpisodes, prep.settings, prep.aliasTitles)
		if len(matched) == 0 {
			continue
		}

		match := seasonPackCoverage{instanceID: inst.ID, matched: len(matched)}
		if prep.totalEpisodes > 0 {
			match.total = prep.totalEpisodes
			match.coverage = float64(len(matched)) / float64(prep.totalEpisodes)
		}
		matches = append(matches, match)
		matchedTitles[inst.ID] = titlesByHash(matched)
	}

	return matches, matchedTitles, nil
}

func titlesByHash(matched map[episodeIdentity][]episodeMatch) map[string][]string {
	titles := map[string][]string{}
	for _, episodes := range matched {
		for _, episode := range episodes {
			if len(episode.titles) > 0 {
				titles[episode.torrentHash] = episode.titles
			}
		}
	}
	return titles
}

func (s *Service) matchEpisodeTorrents(
	torrents iter.Seq2[*qbt.Torrent, []string],
	packName string,
	packRelease *rls.Release,
	packEpisodes map[episodeIdentity]packEpisodeOrigin,
	settings *models.CrossSeedAutomationSettings,
	aliasTitles []string,
) map[episodeIdentity][]episodeMatch {
	candidates := make(map[episodeIdentity][]episodeMatch)
	m := s.matcher()

	logged := make(map[string]struct{})
	logFiltered := func(candidateName, reason string) *zerolog.Event {
		if reason == titleMismatchReason || reason == notInPackReason {
			return nil
		}
		key := candidateName + "\x00" + reason
		if _, seen := logged[key]; seen {
			return nil
		}
		logged[key] = struct{}{}
		return log.Trace().
			Str("pack", packRelease.Title).
			Int("season", packRelease.Series).
			Str("candidate", candidateName).
			Str("reason", reason)
	}

	policy := seasonPackPolicy(settings)
	policy.SkipSizeCheck = true
	packEpisodeReleases := make(map[int]*rls.Release)
	gate := s.sourceGate()
	webhookFilter := defaultsFor(triggerWebhook, settings).sourceFilter

	for torrent, alternateTitles := range torrents {
		answer := gate.check(torrent, webhookFilter)
		if answer.lasting() {
			continue
		}

		parsed := s.releaseCache.Parse(torrent.Name)
		isRange := releases.IsEpisodeRange(parsed)
		if !isTVEpisode(parsed) && !isRange {
			continue
		}

		resolved := parsed
		var origin packEpisodeOrigin
		id := episodeIdentity{series: parsed.Series, episode: parsed.Episode}
		if isRange {
			id = rangeEpisodeIdentity(parsed)
		}
		if packEpisodes != nil {
			localSeasonless := parsed.Series == 0
			targetID := id
			if localSeasonless && packRelease != nil && packRelease.Series > 0 {
				targetID.series = packRelease.Series
			}
			var inPack bool
			origin, inPack = packEpisodes[targetID]
			schemeOK := (localSeasonless && origin.absolute) || (!localSeasonless && origin.seasoned)
			if !inPack || !schemeOK {
				reason := packEpisodeRejectReason(inPack)
				pack := namedRelease{release: packRelease, rawName: packName}
				local := namedRelease{release: parsed, rawName: torrent.Name}
				if inPack && !m.titlesMatchInSourceSeason(pack, local, matchEvidence{SourceTitles: aliasTitles, CandidateTitles: alternateTitles}) {
					reason = titleMismatchReason
				}
				logFiltered(torrent.Name, reason).Msg("[CROSSSEED-MATCH] Release filtered")
				continue
			}
			id = targetID
			if localSeasonless && targetID.series != 0 {
				clone := *parsed
				clone.Series = targetID.series
				resolved = &clone
			}
		}

		local := resolved
		if isRange {
			local = asEpisode(resolved, id.episode)
		}
		packEpisode, ok := packEpisodeReleases[id.episode]
		if !ok {
			packEpisode = asEpisode(packRelease, id.episode)
			packEpisodeReleases[id.episode] = packEpisode
		}
		evidence := matchEvidence{SourceTitles: alternateTitles, CandidateTitles: aliasTitles}
		if origin.file.Size > 0 {
			evidence.SourceSize, evidence.CandidateSize = torrent.Size, origin.file.Size
		}
		verdict := m.matchVerdict(namedRelease{release: local, rawName: torrent.Name}, namedRelease{release: packEpisode, rawName: packName}, evidence, policy)
		if !verdict.Matched {
			logFiltered(torrent.Name, verdict.RejectReason).Msg("[CROSSSEED-MATCH] Release filtered")
			continue
		}

		if answer != sourcePass {
			logFiltered(torrent.Name, "source not settled").
				Float64("progress", torrent.Progress).
				Str("state", string(torrent.State)).
				Msg("[CROSSSEED-MATCH] Release filtered")
			continue
		}

		candidates[id] = append(candidates[id], episodeMatch{
			torrentHash: torrent.Hash,
			contentPath: torrent.ContentPath,
			category:    torrent.Category,
			name:        torrent.Name,
			release:     resolved,
			titles:      alternateTitles,
		})
	}

	return candidates
}

func (s *Service) resolveSeasonPackLocalFilesForCandidates(
	ctx context.Context,
	d fsops.PathDialect,
	instanceID int,
	candidates map[episodeIdentity][]episodeMatch,
	packFiles qbt.TorrentFiles,
	packRelease *rls.Release,
	packName string,
	settings *models.CrossSeedAutomationSettings,
	aliasTitles []string,
	rejected map[string]string,
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

	m := s.matcher()
	expected := m.seasonPackExpectedFiles(packFiles, packRelease)
	selected := make(map[episodeIdentity]episodeMatch, len(candidates))
	localFiles := make(map[episodeIdentity]seasonPackLocalFile, len(candidates))
	ids := sortedEpisodeCandidateIDs(candidates)

	for _, id := range ids {
		expectedFile, ok := expected[id]
		if !ok {
			continue
		}

		var lastErr error
		reject := func(candidate episodeMatch, reason string) {
			if rejected != nil {
				rejected[normalizeHash(candidate.torrentHash)] = reason
			}
		}
		for _, candidate := range candidates[id] {
			files, ok := filesByHash[normalizeHash(candidate.torrentHash)]
			if !ok || len(files) == 0 {
				reject(candidate, "file_list_unavailable")
				lastErr = fmt.Errorf("%w: no file list for torrent %s", errLayoutMismatch, candidate.torrentHash)
				continue
			}

			localFile, err := m.resolveSeasonPackLocalFileCandidate(d, id, candidate, files)
			if err != nil {
				reject(candidate, "episode_file_invalid")
				lastErr = err
				continue
			}
			if localFile.size != expectedFile.file.Size {
				reject(candidate, "size_mismatch")
				lastErr = fmt.Errorf("%w: file size mismatch for %s: pack declares %d bytes, local file is %d bytes", errLayoutMismatch, expectedFile.file.Name, expectedFile.file.Size, localFile.size)
				continue
			}
			if !candidate.manual {
				verdict := m.matchVerdict(
					namedRelease{release: localFile.release, rawName: candidate.name},
					namedRelease{release: expectedFile.release, rawName: packName},
					matchEvidence{
						SourceFiles:     qbt.TorrentFiles{{Name: localFile.sourcePath, Size: localFile.size}},
						CandidateFiles:  qbt.TorrentFiles{expectedFile.file},
						SourceTitles:    candidate.titles,
						CandidateTitles: aliasTitles,
					},
					seasonPackPolicy(settings),
				)
				if !verdict.Matched {
					reject(candidate, "release_mismatch")
					lastErr = fmt.Errorf("%w: release mismatch for %s: %s", errLayoutMismatch, expectedFile.file.Name, verdict.RejectReason)
					continue
				}
			}

			localFile.packFile = expectedFile.file.Name
			delete(rejected, normalizeHash(candidate.torrentHash))
			selected[id] = candidate
			localFiles[id] = localFile
			break
		}

		if _, ok := selected[id]; !ok {
			log.Debug().
				Err(lastErr).
				Int("season", id.series).
				Int("episode", id.episode).
				Msg("[SEASONPACK] no local file validated; episode demoted to missing")
		}
	}

	return selected, localFiles, nil
}

type seasonPackExpectedFile struct {
	file    qbt.TorrentFile
	release *rls.Release
}

func (m matcher) seasonPackExpectedFiles(
	packFiles qbt.TorrentFiles,
	packRelease *rls.Release,
) map[episodeIdentity]seasonPackExpectedFile {
	expected := make(map[episodeIdentity]seasonPackExpectedFile)
	for _, file := range packFiles {
		release, _, ok := m.parseSeasonPackEpisodePayload(file.Name, packRelease)
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

func (m matcher) resolveSeasonPackLocalFileCandidate(
	d fsops.PathDialect,
	id episodeIdentity,
	episode episodeMatch,
	files qbt.TorrentFiles,
) (seasonPackLocalFile, error) {
	var matchedFileSize int64
	var matchedRelease *rls.Release
	matchedSourcePath := ""
	matchCount := 0
	playableCount := 0

	for i := range files {
		file := &files[i]
		if _, playable := videoExtensions[strings.ToLower(path.Ext(file.Name))]; playable && !shouldIgnoreFile(file.Name, m.normalizer()) {
			playableCount++
		}
		parsed, _, ok := m.parseSeasonPackEpisodePayload(file.Name, episode.release)
		if !ok {
			continue
		}
		if parsed.Series != id.series || parsed.Episode != id.episode {
			continue
		}

		matchCount++
		matchedFileSize = file.Size
		matchedRelease = parsed
		matchedSourcePath = resolveSeasonPackSourcePath(d, episode.contentPath, files, file.Name)
	}

	if matchCount != 1 || matchedRelease == nil || matchedSourcePath == "" || (episode.manual && playableCount != 1) {
		return seasonPackLocalFile{}, fmt.Errorf("%w: expected exactly one playable episode file in torrent %s", errLayoutMismatch, episode.torrentHash)
	}

	return seasonPackLocalFile{
		sourcePath: matchedSourcePath,
		size:       matchedFileSize,
		release:    matchedRelease,
	}, nil
}

func resolveSeasonPackSourcePath(d fsops.PathDialect, contentPath string, files qbt.TorrentFiles, fileName string) string {
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

	candidatePath, ok := safeSeasonPackJoin(d, rootDir, relativePath)
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

func (m matcher) buildSeasonPackPlan(
	d fsops.PathDialect,
	packFiles qbt.TorrentFiles,
	packName string,
	destDir string,
	localFiles map[episodeIdentity]seasonPackLocalFile,
) (*seasonPackPlanBuild, error) {
	packDir, ok := safeSeasonPackJoin(d, destDir, packName)
	if !ok {
		return nil, fmt.Errorf("%w: invalid pack root path %q", errLayoutMismatch, packName)
	}
	plan := &hardlinktree.TreePlan{
		RootDir: destDir,
		Files:   make([]hardlinktree.FilePlan, 0, len(packFiles)),
	}
	build := &seasonPackPlanBuild{
		plan:              plan,
		packDir:           packDir,
		materializedPaths: make(map[string]struct{}, len(packFiles)),
		totalFiles:        len(packFiles),
	}

	verified := make(map[string]seasonPackLocalFile, len(localFiles))
	for _, localFile := range localFiles {
		verified[localFile.packFile] = localFile
	}
	for _, pf := range packFiles {
		build.totalBytes += pf.Size

		localFile, ok := verified[pf.Name]
		if !ok {
			continue
		}

		filePlan, err := hardlinktree.BuildSingleFilePlan(d, plan.RootDir, pf.Name, localFile.sourcePath)
		if err != nil {
			return nil, fmt.Errorf("%w: invalid pack target path %q: %w", errLayoutMismatch, pf.Name, err)
		}
		plan.Files = append(plan.Files, filePlan.Files...)
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

func safeSeasonPackJoin(d fsops.PathDialect, rootDir, relativePath string) (string, bool) {
	slashPath := strings.ReplaceAll(relativePath, "\\", "/")
	if strings.HasPrefix(slashPath, "/") {
		return "", false
	}

	cleanedPath := d.Clean(d.FromSlash(slashPath))
	if cleanedPath == "." ||
		d.IsAbs(cleanedPath) ||
		isWindowsDriveAbs(d.ToSlash(cleanedPath)) ||
		cleanedPath == ".." ||
		strings.HasPrefix(cleanedPath, ".."+d.Separator()) {
		return "", false
	}

	candidatePath := d.Join(rootDir, cleanedPath)
	relativeToRoot, err := d.Rel(rootDir, candidatePath)
	if err != nil {
		return "", false
	}
	if relativeToRoot == ".." ||
		strings.HasPrefix(relativeToRoot, ".."+d.Separator()) ||
		d.IsAbs(relativeToRoot) ||
		isWindowsDriveAbs(d.ToSlash(relativeToRoot)) {
		return "", false
	}

	return candidatePath, true
}

func rollbackSeasonPackTree(ctx context.Context, backend fsops.Backend, created *fsops.TreeCreateResult, packDir string) error {
	ctx = context.WithoutCancel(ctx)
	var errs []error
	if err := backend.RemoveTree(ctx, created); err != nil {
		errs = append(errs, err)
	}

	if packDir != "" {
		if err := backend.Remove(ctx, packDir, fsops.RemoveOptions{}); err != nil &&
			!errors.Is(err, fs.ErrNotExist) &&
			!fsops.IsDirNotEmpty(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func selectWinner(matches []seasonPackCoverage, threshold float64) *seasonPackCoverage {
	var best *seasonPackCoverage

	for i := range matches {
		m := &matches[i]
		if m.coverage < threshold {
			continue
		}
		if best == nil || isBetterMatch(m, best) {
			best = m
		}
	}

	return best
}

func isBetterMatch(a, b *seasonPackCoverage) bool {
	if a.coverage != b.coverage {
		return a.coverage > b.coverage
	}
	if a.matched != b.matched {
		return a.matched > b.matched
	}
	return a.instanceID < b.instanceID
}

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

func buildCheckResponse(
	passing []seasonPackCoverage,
	allMatches []seasonPackCoverage,
	totalEpisodes int,
	threshold float64,
) *seasonPackCheckResult {
	if len(passing) > 0 {
		return &seasonPackCheckResult{
			ready:   true,
			message: fmt.Sprintf("%d instance(s) meet %.0f%% coverage threshold", len(passing), threshold*100),
			matches: allMatches,
			best:    *selectWinner(passing, threshold),
		}
	}

	if len(allMatches) > 0 {
		best := allMatches[0]
		for i := range allMatches[1:] {
			if isBetterMatch(&allMatches[i+1], &best) {
				best = allMatches[i+1]
			}
		}
		return &seasonPackCheckResult{
			reason:  "below_threshold",
			message: fmt.Sprintf("best coverage %.0f%% on instance %d (%d/%d episodes)", best.coverage*100, best.instanceID, best.matched, totalEpisodes),
			matches: allMatches,
		}
	}

	return &seasonPackCheckResult{
		reason:  "no_matches",
		message: "no matching episodes found on any instance",
	}
}

func (s *Service) recordCheckRun(
	ctx context.Context,
	torrentName string,
	resp *seasonPackCheckResult,
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

	if resp.ready {
		run.Status = "ready"
		run.MatchedEpisodes = resp.best.matched
		run.Coverage = resp.best.coverage
		run.InstanceID = new(resp.best.instanceID)
	} else {
		run.Status = "skipped"
		run.Reason = resp.reason
		run.Message = resp.message
	}

	if _, err := s.seasonPackRunStore.Create(ctx, run); err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).
			Msg("failed to record season pack check run")
	}
}

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

func (s *Service) recordApplyRun(
	ctx context.Context,
	torrentName, reason, message string,
	instanceID, addedInstanceID, matchedEpisodes, totalEpisodes int,
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
	case "already_exists", "blocked", "skipped_recheck":
		run.Status = "skipped"
	default:
		run.Status = "failed"
	}

	if instanceID > 0 {
		run.InstanceID = &instanceID
	}
	if addedInstanceID > 0 {
		run.AddedInstanceID = &addedInstanceID
	}

	if _, err := s.seasonPackRunStore.Create(ctx, run); err != nil {
		log.Warn().Err(err).Str("torrentName", torrentName).
			Msg("failed to record season pack apply run")
	}
}
