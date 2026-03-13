// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	internalqb "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
)

const (
	partialPoolPollInterval          = 10 * time.Second
	partialPoolMarkerTTL             = 24 * time.Hour
	partialPoolSelectionLimit        = 6 * time.Hour
	partialPoolMissingGrace          = 30 * time.Second
	partialPoolFileCompleteThreshold = 0.999999
)

const (
	partialPoolFileComplete    = "complete"
	partialPoolFileWholeMiss   = "whole_file_missing"
	partialPoolFilePartialMiss = "partial_file_missing"
)

type partialPoolState struct {
	member           *models.CrossSeedPartialPoolMember
	torrent          qbt.Torrent
	files            qbt.TorrentFiles
	classByName      map[string]string
	classByLiveName  map[string]string
	keyByName        map[string]partialPoolFileKey
	liveNameByName   map[string]string
	byName           map[string]partialPoolLiveFile
	missingBytes     int64
	incompleteNames  []string
	incompleteKeys   []partialPoolFileKey
	completeNames    []string
	allWholeMissing  bool
	pieceSafe        bool
	eligibleDownload bool
	manualReview     bool
	manualReason     string
	complete         bool
	checking         bool
	awaitingRecheck  bool
}

type partialPoolForceRefreshContextKey struct{}

type partialPoolFileOwner struct {
	state *partialPoolState
	name  string
}

type partialPoolFileKey struct {
	key  string
	size int64
}

type partialPoolLiveFile struct {
	Index    int
	Name     string
	Progress float64
	Size     int64
}

type partialPoolSelection struct {
	MemberKey  string
	SelectedAt time.Time
}

type partialPoolFileCacheInvalidator interface {
	InvalidateFileCache(ctx context.Context, instanceID int, hash string) error
}

func partialPoolLookupKey(instanceID int, hash string) string {
	if instanceID <= 0 {
		return ""
	}
	hash = normalizeHash(hash)
	if hash == "" {
		return ""
	}
	return fmt.Sprintf("%d|%s", instanceID, hash)
}

func partialPoolSourceKey(member *models.CrossSeedPartialPoolMember) string {
	return fmt.Sprintf("%d|%s", member.SourceInstanceID, normalizeHash(member.SourceHash))
}

func (s *Service) RestoreActivePartialPools(ctx context.Context) error {
	if s == nil || s.partialPoolStore == nil {
		return nil
	}

	members, err := s.partialPoolStore.ListActive(ctx, time.Now().UTC())
	if err != nil {
		return err
	}

	s.partialPoolMu.Lock()
	defer s.partialPoolMu.Unlock()
	s.partialPoolByHash = make(map[string]*models.CrossSeedPartialPoolMember, len(members)*2)
	s.partialPoolBySource = make(map[string]partialPoolSelection)
	for _, member := range members {
		s.storePartialPoolMemberLocked(member)
	}
	s.signalPartialPoolWake()
	return nil
}

func (s *Service) partialPoolWorker() {
	ticker := time.NewTicker(partialPoolPollInterval)
	defer ticker.Stop()

	runCtx, runCancel := context.WithCancel(context.Background())
	defer runCancel()

	if s.partialPoolStop != nil {
		go func(stop <-chan struct{}) {
			<-stop
			runCancel()
		}(s.partialPoolStop)
	}

	for {
		select {
		case <-runCtx.Done():
			return
		case <-s.partialPoolWake:
		case <-ticker.C:
		}

		s.triggerPartialPoolRun(runCtx, s.processPartialPools)
	}
}

func (s *Service) triggerPartialPoolRun(ctx context.Context, process func(context.Context)) {
	if s == nil || process == nil {
		return
	}

	s.partialPoolRunPending.Store(true)
	if !s.partialPoolRunActive.CompareAndSwap(false, true) {
		return
	}

	go s.partialPoolRunLoop(ctx, process)
}

func (s *Service) partialPoolRunLoop(ctx context.Context, process func(context.Context)) {
	for {
		if ctx != nil {
			select {
			case <-ctx.Done():
				s.partialPoolRunActive.Store(false)
				return
			default:
			}
		}

		s.partialPoolRunPending.Store(false)
		process(ctx)

		if s.partialPoolRunPending.Swap(false) {
			continue
		}

		s.partialPoolRunActive.Store(false)
		if !s.partialPoolRunPending.Load() || !s.partialPoolRunActive.CompareAndSwap(false, true) {
			return
		}
	}
}

func (s *Service) processPartialPools(ctx context.Context) {
	if s == nil {
		return
	}

	now := time.Now().UTC()
	if s.partialPoolStore != nil {
		if _, err := s.partialPoolStore.DeleteExpired(ctx, now); err != nil {
			log.Debug().Err(err).Msg("[CROSSSEED-POOL] Failed to prune expired pooled members")
		}
	}
	s.pruneExpiredPartialPoolMembers(now)

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("[CROSSSEED-POOL] Failed to load automation settings")
		return
	}
	if settings == nil || !settings.EnablePooledPartialCompletion {
		if err := s.drainPartialPoolMembers(ctx); err != nil {
			log.Debug().Err(err).Msg("[CROSSSEED-POOL] Failed to clear pooled members while automation is disabled")
		}
		return
	}

	members := s.listPartialPoolMembers()
	if len(members) == 0 {
		return
	}

	pools := make(map[string][]*models.CrossSeedPartialPoolMember)
	for _, member := range members {
		pools[partialPoolSourceKey(member)] = append(pools[partialPoolSourceKey(member)], member)
	}

	for _, poolMembers := range pools {
		s.processPartialPool(ctx, settings, poolMembers)
	}
}

func (s *Service) processPartialPool(ctx context.Context, settings *models.CrossSeedAutomationSettings, members []*models.CrossSeedPartialPoolMember) {
	if len(members) == 0 {
		return
	}

	poolKey := partialPoolSourceKey(members[0])
	log.Debug().
		Str("poolKey", poolKey).
		Int("memberCount", len(members)).
		Msg("[CROSSSEED-POOL] Processing pooled partial members")

	states := s.loadPartialPoolStates(ctx, settings, members)
	if len(states) == 0 {
		log.Debug().
			Str("poolKey", poolKey).
			Msg("[CROSSSEED-POOL] No active pooled states loaded")
		return
	}

	activeStates := make([]*partialPoolState, 0, len(states))
	completedStates := make([]*partialPoolState, 0, len(states))
	for _, state := range states {
		if state.complete {
			completedStates = append(completedStates, state)
			activeStates = append(activeStates, state)
			continue
		}
		if state.manualReview {
			log.Info().
				Str("poolKey", poolKey).
				Int("instanceID", state.member.TargetInstanceID).
				Str("hash", state.member.TargetHash).
				Str("torrentState", string(state.torrent.State)).
				Int64("missingBytes", state.missingBytes).
				Int("incompleteFiles", len(state.incompleteNames)).
				Str("reason", state.manualReason).
				Msg("[CROSSSEED-POOL] Pooled member requires manual review")
			s.dropPartialPoolMember(ctx, state.member, state.manualReason)
			continue
		}
		activeStates = append(activeStates, state)
	}
	if len(activeStates) == 0 {
		log.Debug().
			Str("poolKey", poolKey).
			Msg("[CROSSSEED-POOL] No pooled members remain active")
		s.clearPartialPoolSelection(poolKey)
		return
	}

	s.resumePartialPoolMembers(ctx, completedStates)
	s.propagateCompletedPoolFiles(ctx, activeStates)

	incomplete := make([]*partialPoolState, 0, len(activeStates))
	for _, state := range activeStates {
		if state.complete {
			continue
		}
		incomplete = append(incomplete, state)
	}
	if len(incomplete) == 0 {
		for _, state := range activeStates {
			s.removePartialPoolMember(ctx, state.member)
		}
		s.clearPartialPoolSelection(poolKey)
		return
	}

	// Don't churn member state while any pooled download is already active. Keep the
	// preferred candidate updated in-memory so rotation can happen after the timeout.
	if s.partialPoolHasActiveDownloader(incomplete) {
		selected := s.selectPreferredPartialPoolDownloader(poolKey, incomplete, time.Now().UTC())
		if selected != nil {
			log.Debug().
				Str("poolKey", poolKey).
				Int("instanceID", selected.member.TargetInstanceID).
				Str("selectedHash", selected.member.TargetHash).
				Int64("missingBytes", selected.missingBytes).
				Int("incompleteFiles", len(selected.incompleteNames)).
				Msg("[CROSSSEED-POOL] Active downloader already running; leaving pool unchanged")
		}
		return
	}

	selected := s.selectPreferredPartialPoolDownloader(poolKey, incomplete, time.Now().UTC())
	if selected == nil {
		log.Debug().
			Str("poolKey", poolKey).
			Int("incompleteMembers", len(incomplete)).
			Msg("[CROSSSEED-POOL] No eligible pooled downloader selected")
		return
	}
	log.Info().
		Str("poolKey", poolKey).
		Int("instanceID", selected.member.TargetInstanceID).
		Str("selectedHash", selected.member.TargetHash).
		Str("mode", selected.member.Mode).
		Int64("missingBytes", selected.missingBytes).
		Int("incompleteFiles", len(selected.incompleteNames)).
		Msg("[CROSSSEED-POOL] Selected pooled downloader")
	s.resumePartialPoolMembers(ctx, []*partialPoolState{selected})
}

func (s *Service) loadPartialPoolStates(ctx context.Context, settings *models.CrossSeedAutomationSettings, members []*models.CrossSeedPartialPoolMember) []*partialPoolState {
	if len(members) == 0 {
		return nil
	}

	byInstance := make(map[int][]*models.CrossSeedPartialPoolMember)
	for _, member := range members {
		byInstance[member.TargetInstanceID] = append(byInstance[member.TargetInstanceID], member)
	}

	var states []*partialPoolState
	for instanceID, instanceMembers := range byInstance {
		hashes := make([]string, 0, len(instanceMembers)*2)
		for _, member := range instanceMembers {
			hashes = append(hashes, member.TargetHash)
			hashes = append(hashes, member.TargetHashV2)
			s.invalidatePartialPoolFileCache(ctx, member.TargetInstanceID, member.TargetHash)
			s.invalidatePartialPoolFileCache(ctx, member.TargetInstanceID, member.TargetHashV2)
		}
		hashes = uniqueStrings(hashes)

		torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: hashes})
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("[CROSSSEED-POOL] Failed to load pool torrents")
			continue
		}

		torrentByHash := make(map[string]qbt.Torrent, len(torrents))
		for _, torrent := range torrents {
			torrentByHash[normalizeHash(torrent.Hash)] = torrent
		}

		filesByHash, err := s.getPartialPoolTorrentFilesBatch(ctx, instanceID, hashes)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("[CROSSSEED-POOL] Failed to load pool files")
			continue
		}

		for _, member := range instanceMembers {
			torrent, ok := torrentByHash[normalizeHash(member.TargetHash)]
			if !ok && member.TargetHashV2 != "" {
				torrent, ok = torrentByHash[normalizeHash(member.TargetHashV2)]
			}
			if !ok {
				resolvedTorrent, found, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, []string{member.TargetHash, member.TargetHashV2})
				if err != nil {
					log.Debug().
						Err(err).
						Int("instanceID", member.TargetInstanceID).
						Str("hash", member.TargetHash).
						Str("hashV2", member.TargetHashV2).
						Msg("[CROSSSEED-POOL] Failed to resolve pooled member via variant-aware lookup")
				} else if found && resolvedTorrent != nil {
					torrent = *resolvedTorrent
					ok = true
					torrentByHash[normalizeHash(torrent.Hash)] = torrent
					if torrent.InfohashV1 != "" {
						torrentByHash[normalizeHash(torrent.InfohashV1)] = torrent
					}
					if torrent.InfohashV2 != "" {
						torrentByHash[normalizeHash(torrent.InfohashV2)] = torrent
					}
					log.Debug().
						Int("instanceID", member.TargetInstanceID).
						Str("hash", member.TargetHash).
						Str("hashV2", member.TargetHashV2).
						Str("resolvedHash", torrent.Hash).
						Str("torrentState", string(torrent.State)).
						Msg("[CROSSSEED-POOL] Resolved pooled member via variant-aware lookup fallback")
				}
			}
			if !ok {
				if partialPoolMemberMissingGraceActive(member, time.Now().UTC()) {
					log.Debug().
						Int("instanceID", member.TargetInstanceID).
						Str("hash", member.TargetHash).
						Str("hashV2", member.TargetHashV2).
						Msg("[CROSSSEED-POOL] Deferring pooled member removal until torrent appears in sync state")
					continue
				}
				log.Debug().
					Int("instanceID", member.TargetInstanceID).
					Str("hash", member.TargetHash).
					Str("hashV2", member.TargetHashV2).
					Msg("[CROSSSEED-POOL] Removing pooled member because torrent is still missing after grace period")
				s.removePartialPoolMember(ctx, member)
				continue
			}
			if !s.partialPoolMemberMatchesTorrent(member, torrent) {
				log.Info().
					Int("instanceID", member.TargetInstanceID).
					Str("hash", member.TargetHash).
					Int64("storedAddedOn", member.TargetAddedOn).
					Int64("liveAddedOn", torrent.AddedOn).
					Msg("[CROSSSEED-POOL] Removing stale pooled member after torrent re-add")
				s.removePartialPoolMember(ctx, member)
				continue
			}

			files, ok := filesByHash[normalizeHash(member.TargetHash)]
			if !ok && member.TargetHashV2 != "" {
				files, ok = filesByHash[normalizeHash(member.TargetHashV2)]
			}
			if !ok {
				log.Debug().
					Int("instanceID", member.TargetInstanceID).
					Str("hash", member.TargetHash).
					Str("hashV2", member.TargetHashV2).
					Msg("[CROSSSEED-POOL] Skipping pooled member until torrent files are available")
				continue
			}

			state := s.buildPartialPoolState(member, torrent, files)
			state = s.applyPartialPoolSettings(state, settings)
			limit := models.DefaultCrossSeedAutomationSettings().MaxMissingBytesAfterRecheck
			if state.member != nil && state.member.MaxMissingBytesAfterRecheck > 0 {
				limit = state.member.MaxMissingBytesAfterRecheck
			} else if settings != nil {
				limit = settings.MaxMissingBytesAfterRecheck
			}
			log.Debug().
				Int("instanceID", member.TargetInstanceID).
				Str("hash", member.TargetHash).
				Str("mode", member.Mode).
				Str("torrentState", string(torrent.State)).
				Bool("checking", state.checking).
				Bool("awaitingRecheckCompletion", state.awaitingRecheck).
				Bool("complete", state.complete).
				Bool("eligibleDownload", state.eligibleDownload).
				Bool("manualReview", state.manualReview).
				Int64("missingBytes", state.missingBytes).
				Int64("missingLimit", limit).
				Int("sourceFiles", len(member.SourceFiles)).
				Int("incompleteFiles", len(state.incompleteNames)).
				Str("manualReason", state.manualReason).
				Msg("[CROSSSEED-POOL] Loaded pooled member state")
			states = append(states, state)
		}
	}

	return states
}

func (s *Service) getPartialPoolTorrentFilesBatch(
	ctx context.Context,
	instanceID int,
	hashes []string,
) (map[string]qbt.TorrentFiles, error) {
	if s == nil || s.syncManager == nil {
		return map[string]qbt.TorrentFiles{}, nil
	}

	ctx = context.WithValue(ctx, partialPoolForceRefreshContextKey{}, true)
	ctx = internalqb.WithForceFilesRefresh(ctx)

	return s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

func partialPoolMemberMissingGraceActive(member *models.CrossSeedPartialPoolMember, now time.Time) bool {
	if member == nil {
		return false
	}

	reference := member.CreatedAt
	if reference.IsZero() || member.UpdatedAt.After(reference) {
		reference = member.UpdatedAt
	}
	if reference.IsZero() {
		return false
	}

	return now.Sub(reference) < partialPoolMissingGrace
}

func (s *Service) buildPartialPoolState(member *models.CrossSeedPartialPoolMember, torrent qbt.Torrent, files qbt.TorrentFiles) *partialPoolState {
	state := &partialPoolState{
		member:          member,
		torrent:         torrent,
		files:           files,
		classByName:     make(map[string]string, len(member.SourceFiles)),
		classByLiveName: make(map[string]string, len(member.SourceFiles)),
		keyByName:       make(map[string]partialPoolFileKey, len(member.SourceFiles)),
		liveNameByName:  make(map[string]string, len(member.SourceFiles)),
		byName:          make(map[string]partialPoolLiveFile, len(files)),
		allWholeMissing: true,
	}
	liveByKey := make(map[partialPoolFileKey][]partialPoolLiveFile, len(files))

	for _, file := range files {
		live := partialPoolLiveFile{
			Index:    file.Index,
			Name:     file.Name,
			Progress: float64(file.Progress),
			Size:     file.Size,
		}
		state.byName[file.Name] = live
		key := partialPoolFileKey{
			key:  normalizeFileKey(file.Name),
			size: file.Size,
		}
		liveByKey[key] = append(liveByKey[key], live)
	}

	for _, sourceFile := range member.SourceFiles {
		fileKey := partialPoolStoredFileKey(sourceFile)
		state.keyByName[sourceFile.Name] = fileKey

		live, ok := state.byName[sourceFile.Name]
		if ok && live.Size == sourceFile.Size {
			liveByKey = partialPoolConsumeLiveFile(liveByKey, live)
		} else {
			live, ok = partialPoolTakeLiveFile(liveByKey, fileKey)
		}
		if !ok {
			state.classByName[sourceFile.Name] = partialPoolFileWholeMiss
			state.missingBytes += sourceFile.Size
			state.incompleteNames = append(state.incompleteNames, sourceFile.Name)
			state.incompleteKeys = append(state.incompleteKeys, fileKey)
			continue
		}
		state.liveNameByName[sourceFile.Name] = live.Name

		progress := live.Progress
		switch {
		case progress >= partialPoolFileCompleteThreshold:
			state.classByName[sourceFile.Name] = partialPoolFileComplete
			state.classByLiveName[live.Name] = partialPoolFileComplete
			state.completeNames = append(state.completeNames, sourceFile.Name)
		case progress <= 0:
			state.classByName[sourceFile.Name] = partialPoolFileWholeMiss
			state.classByLiveName[live.Name] = partialPoolFileWholeMiss
			state.missingBytes += sourceFile.Size
			state.incompleteNames = append(state.incompleteNames, sourceFile.Name)
			state.incompleteKeys = append(state.incompleteKeys, fileKey)
		default:
			state.classByName[sourceFile.Name] = partialPoolFilePartialMiss
			state.classByLiveName[live.Name] = partialPoolFilePartialMiss
			state.allWholeMissing = false
			state.incompleteNames = append(state.incompleteNames, sourceFile.Name)
			state.incompleteKeys = append(state.incompleteKeys, fileKey)
			state.missingBytes += int64(math.Ceil(float64(sourceFile.Size) * (1 - progress)))
		}
	}

	state.checking = isTorrentCheckingState(torrent.State)
	state.awaitingRecheck = state.checking
	state.complete = len(state.incompleteNames) == 0 && !state.checking
	return state
}

func partialPoolStoredFileKey(file models.CrossSeedPartialFile) partialPoolFileKey {
	key := strings.TrimSpace(file.Key)
	if key == "" {
		key = normalizeFileKey(file.Name)
	}
	return partialPoolFileKey{
		key:  key,
		size: file.Size,
	}
}

func partialPoolTakeLiveFile(
	liveByKey map[partialPoolFileKey][]partialPoolLiveFile,
	key partialPoolFileKey,
) (partialPoolLiveFile, bool) {
	liveFiles := liveByKey[key]
	if len(liveFiles) == 0 {
		return partialPoolLiveFile{}, false
	}
	live := liveFiles[0]
	if len(liveFiles) == 1 {
		delete(liveByKey, key)
	} else {
		liveByKey[key] = liveFiles[1:]
	}
	return live, true
}

func partialPoolConsumeLiveFile(
	liveByKey map[partialPoolFileKey][]partialPoolLiveFile,
	live partialPoolLiveFile,
) map[partialPoolFileKey][]partialPoolLiveFile {
	key := partialPoolFileKey{
		key:  normalizeFileKey(live.Name),
		size: live.Size,
	}
	liveFiles := liveByKey[key]
	for i, candidate := range liveFiles {
		if candidate.Index != live.Index || candidate.Name != live.Name {
			continue
		}
		if len(liveFiles) == 1 {
			delete(liveByKey, key)
			return liveByKey
		}
		last := len(liveFiles) - 1
		liveFiles[i] = liveFiles[last]
		liveFiles[last] = partialPoolLiveFile{}
		liveByKey[key] = liveFiles[:last]
		return liveByKey
	}
	return liveByKey
}

func (s *Service) applyPartialPoolSettings(state *partialPoolState, settings *models.CrossSeedAutomationSettings) *partialPoolState {
	if state == nil {
		return nil
	}
	if state.awaitingRecheck {
		return state
	}
	if state.member.Mode == models.CrossSeedPartialMemberModeHardlink {
		if !state.allWholeMissing {
			state.manualReview = true
			state.manualReason = "post-recheck missing bytes exist inside linked files"
			return state
		}

		filesForBoundary := make([]TorrentFileForBoundaryCheck, 0, len(state.member.SourceFiles))
		for _, sourceFile := range state.member.SourceFiles {
			filesForBoundary = append(filesForBoundary, TorrentFileForBoundaryCheck{
				Path:      sourceFile.Name,
				Size:      sourceFile.Size,
				IsContent: state.classByName[sourceFile.Name] == partialPoolFileComplete,
			})
		}
		result := CheckPieceBoundarySafety(filesForBoundary, state.member.SourcePieceLength)
		state.pieceSafe = result.Safe
		switch {
		case state.complete:
			state.eligibleDownload = false
		case !result.Safe:
			state.manualReview = true
			state.manualReason = "missing whole files share pieces with linked content"
		default:
			state.eligibleDownload = true
		}
		return state
	}

	state.pieceSafe = true
	if state.complete {
		return state
	}

	limit := models.DefaultCrossSeedAutomationSettings().MaxMissingBytesAfterRecheck
	if state.member != nil && state.member.MaxMissingBytesAfterRecheck > 0 {
		limit = state.member.MaxMissingBytesAfterRecheck
	} else if settings != nil {
		limit = settings.MaxMissingBytesAfterRecheck
	}
	if state.missingBytes > limit {
		state.manualReview = true
		state.manualReason = "post-recheck missing bytes exceed pooled reflink limit"
		return state
	}

	state.eligibleDownload = true
	return state
}

func (s *Service) propagateCompletedPoolFiles(ctx context.Context, states []*partialPoolState) {
	if len(states) < 2 {
		return
	}

	rechecksByInstance := make(map[int][]string)
	ownersByKey := make(map[partialPoolFileKey][]partialPoolFileOwner)
	for _, state := range states {
		for _, name := range state.completeNames {
			key, ok := state.keyByName[name]
			if !ok || key.size <= 0 {
				continue
			}
			ownersByKey[key] = append(ownersByKey[key], partialPoolFileOwner{state: state, name: name})
		}
	}
	if len(ownersByKey) == 0 {
		return
	}

	for _, recipient := range states {
		if recipient.complete || recipient.checking {
			continue
		}

		recipientPaused := false
		propagatedFiles := 0
		for _, name := range recipient.incompleteNames {
			if recipient.classByName[name] != partialPoolFileWholeMiss {
				continue
			}
			key, ok := recipient.keyByName[name]
			if !ok {
				continue
			}
			filePropagated := false
			for _, owner := range ownersByKey[key] {
				if owner.state.member.TargetInstanceID == recipient.member.TargetInstanceID &&
					normalizeHash(owner.state.member.TargetHash) == normalizeHash(recipient.member.TargetHash) {
					continue
				}
				if !recipientPaused {
					if !s.pausePartialPoolRecipientForPropagation(ctx, recipient) {
						break
					}
					recipientPaused = true
				}
				if err := s.propagatePartialPoolFile(ctx, owner.state, owner.name, recipient, name); err != nil {
					log.Debug().
						Err(err).
						Str("sourceFile", owner.name).
						Str("targetFile", name).
						Str("targetHash", recipient.member.TargetHash).
						Msg("[CROSSSEED-POOL] Failed to propagate completed file")
					continue
				}
				filePropagated = true
				propagatedFiles++
				break
			}
			if !filePropagated {
				continue
			}
		}

		if propagatedFiles > 0 {
			s.invalidatePartialPoolFileCache(ctx, recipient.member.TargetInstanceID, recipient.member.TargetHash)
			// Keep propagated recipients out of downloader selection until the next poll
			// refreshes qBittorrent state after the relinked files are rechecked.
			recipient.checking = true
			schedulePartialPoolBulkHash(rechecksByInstance, recipient.member.TargetInstanceID, recipient.member.TargetHash)
			schedulePartialPoolBulkHash(rechecksByInstance, recipient.member.TargetInstanceID, recipient.member.TargetHashV2)
		}
	}

	s.runPartialPoolBulkAction(ctx, rechecksByInstance, "recheck", "[CROSSSEED-POOL] Failed to trigger recheck after propagation")
}

func (s *Service) propagatePartialPoolFile(
	_ context.Context,
	owner *partialPoolState,
	ownerName string,
	recipient *partialPoolState,
	recipientName string,
) error {
	size := s.partialPoolFileSize(owner.member, ownerName)
	if size <= 0 {
		return fmt.Errorf("file %s not found in marker", ownerName)
	}

	srcName := ownerName
	if liveName := strings.TrimSpace(owner.liveNameByName[ownerName]); liveName != "" {
		srcName = liveName
	}
	if !treeModeFileAvailable(owner.member.ManagedRoot, srcName, size, 1.0) {
		return fmt.Errorf("source file unavailable on disk: %s", srcName)
	}
	dstName := recipientName
	if liveName := strings.TrimSpace(recipient.liveNameByName[recipientName]); liveName != "" {
		dstName = liveName
	}

	srcPath := filepath.Join(owner.member.ManagedRoot, filepath.FromSlash(srcName))
	dstPath := filepath.Join(recipient.member.ManagedRoot, filepath.FromSlash(dstName))

	if err := os.MkdirAll(filepath.Dir(dstPath), 0o755); err != nil {
		return err
	}
	if _, err := os.Lstat(dstPath); err == nil {
		if removeErr := os.Remove(dstPath); removeErr != nil {
			return removeErr
		}
	} else if err != nil && !os.IsNotExist(err) {
		return err
	}

	plan := &hardlinktree.TreePlan{
		RootDir: recipient.member.ManagedRoot,
		Files: []hardlinktree.FilePlan{{
			SourcePath: srcPath,
			TargetPath: dstPath,
		}},
	}

	if recipient.member.Mode == models.CrossSeedPartialMemberModeReflink {
		return reflinktree.Create(plan)
	}
	return hardlinktree.Create(plan)
}

func (s *Service) selectPartialPoolDownloader(states []*partialPoolState) *partialPoolState {
	if len(states) == 0 {
		return nil
	}

	needCounts := make(map[partialPoolFileKey]int, len(states)*4)
	for _, state := range states {
		if !state.eligibleDownload || state.checking {
			continue
		}
		for _, key := range state.incompleteKeys {
			needCounts[key]++
		}
	}

	sort.Slice(states, func(i, j int) bool {
		left := partialPoolCandidateScore(states[i], needCounts)
		right := partialPoolCandidateScore(states[j], needCounts)
		if left != right {
			return left > right
		}
		if states[i].member.Mode != states[j].member.Mode {
			return states[i].member.Mode == models.CrossSeedPartialMemberModeReflink
		}
		return states[i].missingBytes < states[j].missingBytes
	})

	for _, state := range states {
		if state.eligibleDownload && !state.checking {
			return state
		}
	}
	return nil
}

func (s *Service) selectPreferredPartialPoolDownloader(poolKey string, states []*partialPoolState, now time.Time) *partialPoolState {
	selected := s.selectPartialPoolDownloader(states)
	if selected == nil {
		s.clearPartialPoolSelection(poolKey)
		return nil
	}

	candidates := s.rankPartialPoolDownloaders(states)
	if len(candidates) == 0 {
		s.clearPartialPoolSelection(poolKey)
		return nil
	}

	selectedKey := partialPoolStateMemberKey(selected)

	s.partialPoolMu.Lock()
	defer s.partialPoolMu.Unlock()

	if s.partialPoolBySource == nil {
		s.partialPoolBySource = make(map[string]partialPoolSelection)
	}

	selection, ok := s.partialPoolBySource[poolKey]
	if !ok || selection.MemberKey == "" {
		s.partialPoolBySource[poolKey] = partialPoolSelection{
			MemberKey:  selectedKey,
			SelectedAt: now,
		}
		return selected
	}

	for _, candidate := range candidates {
		if partialPoolStateMemberKey(candidate) != selection.MemberKey {
			continue
		}
		if now.Sub(selection.SelectedAt) < partialPoolSelectionLimit || len(candidates) == 1 {
			return candidate
		}
		break
	}

	for _, candidate := range candidates {
		candidateKey := partialPoolStateMemberKey(candidate)
		if candidateKey == selection.MemberKey && len(candidates) > 1 {
			continue
		}
		s.partialPoolBySource[poolKey] = partialPoolSelection{
			MemberKey:  candidateKey,
			SelectedAt: now,
		}
		return candidate
	}

	s.partialPoolBySource[poolKey] = partialPoolSelection{
		MemberKey:  selectedKey,
		SelectedAt: now,
	}
	return selected
}

func (s *Service) rankPartialPoolDownloaders(states []*partialPoolState) []*partialPoolState {
	if len(states) == 0 {
		return nil
	}

	needCounts := make(map[partialPoolFileKey]int, len(states)*4)
	candidates := make([]*partialPoolState, 0, len(states))
	for _, state := range states {
		if !state.eligibleDownload || state.checking {
			continue
		}
		candidates = append(candidates, state)
		for _, key := range state.incompleteKeys {
			needCounts[key]++
		}
	}

	sort.Slice(candidates, func(i, j int) bool {
		left := partialPoolCandidateScore(candidates[i], needCounts)
		right := partialPoolCandidateScore(candidates[j], needCounts)
		if left != right {
			return left > right
		}
		if candidates[i].member.Mode != candidates[j].member.Mode {
			return candidates[i].member.Mode == models.CrossSeedPartialMemberModeReflink
		}
		return candidates[i].missingBytes < candidates[j].missingBytes
	})

	return candidates
}

func partialPoolCandidateScore(state *partialPoolState, needCounts map[partialPoolFileKey]int) int64 {
	if state == nil || !state.eligibleDownload || state.checking {
		return -1
	}

	var score int64
	for _, key := range state.incompleteKeys {
		if needCounts[key] > 1 {
			score += key.size * 10
			continue
		}
		score += key.size
	}
	if state.member.Mode == models.CrossSeedPartialMemberModeReflink {
		score++
	}
	return score
}

func (s *Service) pausePartialPoolRecipientForPropagation(ctx context.Context, state *partialPoolState) bool {
	if state == nil || state.complete || state.checking {
		return false
	}
	if s.partialPoolTorrentPaused(state.torrent.State) {
		return true
	}
	if err := s.pausePartialPoolHash(ctx, state.member.TargetInstanceID, state.member.TargetHash); err != nil {
		log.Debug().Err(err).Str("hash", state.member.TargetHash).Msg("[CROSSSEED-POOL] Failed to pause pooled recipient before propagation")
		return false
	}
	state.torrent.State = qbt.TorrentStatePausedDl
	return true
}

func (s *Service) resumePartialPoolMembers(ctx context.Context, states []*partialPoolState) {
	byInstance := make(map[int][]string)
	for _, state := range states {
		if state == nil || state.checking || s.partialPoolTorrentRunning(state.torrent.State) {
			continue
		}
		schedulePartialPoolBulkHash(byInstance, state.member.TargetInstanceID, state.member.TargetHash)
	}

	s.runPartialPoolBulkAction(ctx, byInstance, "resume", "[CROSSSEED-POOL] Failed to resume pooled members")
}

func (s *Service) registerPartialPoolMember(
	ctx context.Context,
	sourceInstanceID int,
	sourceHash string,
	targetInstanceID int,
	targetHash string,
	targetHashV2 string,
	targetName string,
	mode string,
	managedRoot string,
	sourcePieceLength int64,
	maxMissingBytesAfterRecheck int64,
	sourceFiles qbt.TorrentFiles,
) error {
	if s == nil {
		return nil
	}
	if s.partialPoolStore == nil {
		log.Warn().
			Int("sourceInstanceID", sourceInstanceID).
			Str("sourceHash", sourceHash).
			Int("targetInstanceID", targetInstanceID).
			Str("targetHash", targetHash).
			Str("targetHashV2", targetHashV2).
			Str("targetName", targetName).
			Str("mode", mode).
			Msg("[CROSSSEED-POOL] Partial pool store unavailable; skipping pooled registration")
		return nil
	}

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID:            sourceInstanceID,
		SourceHash:                  sourceHash,
		TargetInstanceID:            targetInstanceID,
		TargetHash:                  targetHash,
		TargetHashV2:                targetHashV2,
		TargetAddedOn:               s.partialPoolRegistrationTargetAddedOn(ctx, targetInstanceID, targetHash, targetHashV2),
		TargetName:                  targetName,
		Mode:                        mode,
		ManagedRoot:                 managedRoot,
		SourcePieceLength:           sourcePieceLength,
		MaxMissingBytesAfterRecheck: maxMissingBytesAfterRecheck,
		SourceFiles:                 s.partialPoolRegistrationFiles(ctx, targetInstanceID, targetHash, targetHashV2, sourceFiles),
		ExpiresAt:                   time.Now().UTC().Add(partialPoolMarkerTTL),
	}

	stored, err := s.partialPoolStore.Upsert(ctx, member)
	if err != nil {
		log.Warn().
			Err(err).
			Int("sourceInstanceID", sourceInstanceID).
			Str("sourceHash", sourceHash).
			Int("targetInstanceID", targetInstanceID).
			Str("targetHash", targetHash).
			Str("targetHashV2", targetHashV2).
			Str("targetName", targetName).
			Str("mode", mode).
			Int("sourceFiles", len(member.SourceFiles)).
			Int64("maxMissingBytesAfterRecheck", maxMissingBytesAfterRecheck).
			Msg("[CROSSSEED-POOL] Failed to register pooled member")
		return err
	}
	log.Info().
		Int("sourceInstanceID", stored.SourceInstanceID).
		Str("sourceHash", stored.SourceHash).
		Int("targetInstanceID", stored.TargetInstanceID).
		Str("targetHash", stored.TargetHash).
		Str("targetHashV2", stored.TargetHashV2).
		Int64("targetAddedOn", stored.TargetAddedOn).
		Str("targetName", stored.TargetName).
		Str("mode", stored.Mode).
		Int("sourceFiles", len(stored.SourceFiles)).
		Int64("missingBytesLimit", stored.MaxMissingBytesAfterRecheck).
		Str("poolKey", partialPoolSourceKey(stored)).
		Msg("[CROSSSEED-POOL] Registered pooled member")

	// Persist first, then update in-memory indexes. This is intentionally not
	// atomic: a crash in between leaves restoreable state in the DB, and
	// RestoreActivePartialPools rebuilds memory from the persisted members.
	s.partialPoolMu.Lock()
	s.storePartialPoolMemberLocked(stored)
	s.partialPoolMu.Unlock()
	s.signalPartialPoolWake()
	return nil
}

func (s *Service) partialPoolRegistrationTargetAddedOn(
	ctx context.Context,
	instanceID int,
	targetHash string,
	targetHashV2 string,
) int64 {
	if s == nil || s.syncManager == nil || instanceID <= 0 {
		return 0
	}

	torrent, found, err := s.syncManager.HasTorrentByAnyHash(ctx, instanceID, []string{targetHash, targetHashV2})
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Str("targetHash", targetHash).
			Str("targetHashV2", targetHashV2).
			Msg("[CROSSSEED-POOL] Failed to resolve target torrent AddedOn for pooled registration")
		return 0
	}
	if !found || torrent == nil {
		return 0
	}

	return torrent.AddedOn
}

func (s *Service) partialPoolRegistrationFiles(
	ctx context.Context,
	instanceID int,
	targetHash string,
	targetHashV2 string,
	fallback qbt.TorrentFiles,
) []models.CrossSeedPartialFile {
	hashes := uniqueStrings([]string{targetHash, targetHashV2})
	if s != nil && s.syncManager != nil && instanceID > 0 && len(hashes) > 0 {
		filesByHash, err := s.getPartialPoolTorrentFilesBatch(ctx, instanceID, hashes)
		if err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Str("targetHash", targetHash).
				Msg("[CROSSSEED-POOL] Failed to load target torrent files for pooled registration")
		} else {
			for _, hash := range hashes {
				files, ok := filesByHash[normalizeHash(hash)]
				if !ok || len(files) == 0 {
					continue
				}
				return partialPoolStoredFilesFromTorrentFiles(files)
			}
		}
	}

	return partialPoolStoredFilesFromTorrentFiles(fallback)
}

func partialPoolStoredFilesFromTorrentFiles(files qbt.TorrentFiles) []models.CrossSeedPartialFile {
	storedFiles := make([]models.CrossSeedPartialFile, 0, len(files))
	for _, file := range files {
		storedFiles = append(storedFiles, models.CrossSeedPartialFile{
			Name: file.Name,
			Size: file.Size,
			Key:  normalizeFileKey(file.Name),
		})
	}
	return storedFiles
}

func (s *Service) partialPoolOwnsLiveTorrent(ctx context.Context, instanceID int, torrent qbt.Torrent) bool {
	key := partialPoolLookupKey(instanceID, torrent.Hash)
	if key == "" {
		return false
	}

	now := time.Now().UTC()

	s.partialPoolMu.RLock()
	member, ok := s.partialPoolByHash[key]
	expired := ok && partialPoolMemberExpired(member, now)
	s.partialPoolMu.RUnlock()
	if !ok {
		return false
	}
	if expired {
		s.partialPoolMu.Lock()
		member, ok = s.partialPoolByHash[key]
		if ok && partialPoolMemberExpired(member, now) {
			s.removePartialPoolMemberLocked(member)
		}
		s.partialPoolMu.Unlock()
		return false
	}
	if s.partialPoolMemberMatchesTorrent(member, torrent) {
		return true
	}

	log.Info().
		Int("instanceID", instanceID).
		Str("hash", torrent.Hash).
		Int64("storedAddedOn", member.TargetAddedOn).
		Int64("liveAddedOn", torrent.AddedOn).
		Msg("[CROSSSEED-POOL] Clearing stale pooled ownership for re-added torrent")
	s.removePartialPoolMember(ctx, member)
	return false
}

func (s *Service) partialPoolOwnsTorrent(instanceID int, hash string) bool {
	key := partialPoolLookupKey(instanceID, hash)
	if key == "" {
		return false
	}

	now := time.Now().UTC()

	s.partialPoolMu.RLock()
	member, ok := s.partialPoolByHash[key]
	expired := ok && partialPoolMemberExpired(member, now)
	s.partialPoolMu.RUnlock()
	if !ok {
		return false
	}
	if !expired {
		return true
	}

	s.partialPoolMu.Lock()
	member, ok = s.partialPoolByHash[key]
	if ok && partialPoolMemberExpired(member, now) {
		s.removePartialPoolMemberLocked(member)
	}
	s.partialPoolMu.Unlock()

	return false
}

func (s *Service) partialPoolMemberMatchesTorrent(member *models.CrossSeedPartialPoolMember, torrent qbt.Torrent) bool {
	if member == nil {
		return false
	}
	if !torrentMatchesAnyHash(torrent, []string{member.TargetHash, member.TargetHashV2}) {
		return false
	}
	if member.TargetAddedOn == 0 || torrent.AddedOn == 0 {
		return true
	}
	return member.TargetAddedOn == torrent.AddedOn
}

func (s *Service) storePartialPoolMemberLocked(member *models.CrossSeedPartialPoolMember) {
	if member == nil {
		return
	}
	if s.partialPoolByHash == nil {
		s.partialPoolByHash = make(map[string]*models.CrossSeedPartialPoolMember)
	}
	if s.partialPoolBySource == nil {
		s.partialPoolBySource = make(map[string]partialPoolSelection)
	}
	if key := partialPoolLookupKey(member.TargetInstanceID, member.TargetHash); key != "" {
		s.partialPoolByHash[key] = member
	}
	if key := partialPoolLookupKey(member.TargetInstanceID, member.TargetHashV2); key != "" {
		s.partialPoolByHash[key] = member
	}
}

func (s *Service) listPartialPoolMembers() []*models.CrossSeedPartialPoolMember {
	s.partialPoolMu.RLock()
	defer s.partialPoolMu.RUnlock()

	now := time.Now().UTC()
	seen := make(map[string]struct{}, len(s.partialPoolByHash))
	members := make([]*models.CrossSeedPartialPoolMember, 0, len(s.partialPoolByHash))
	for _, member := range s.partialPoolByHash {
		if partialPoolMemberExpired(member, now) {
			continue
		}
		key := partialPoolLookupKey(member.TargetInstanceID, member.TargetHash)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		members = append(members, member)
	}
	return members
}

func (s *Service) drainPartialPoolMembers(ctx context.Context) error {
	if s == nil {
		return nil
	}

	membersByKey := make(map[string]*models.CrossSeedPartialPoolMember)

	s.partialPoolMu.Lock()
	for _, member := range s.partialPoolByHash {
		if member == nil {
			continue
		}
		key := partialPoolLookupKey(member.TargetInstanceID, member.TargetHash)
		if key == "" {
			continue
		}
		membersByKey[key] = member
	}
	s.partialPoolByHash = make(map[string]*models.CrossSeedPartialPoolMember)
	s.partialPoolBySource = make(map[string]partialPoolSelection)
	s.partialPoolMu.Unlock()

	if s.partialPoolStore == nil {
		return nil
	}

	storedMembers, err := s.partialPoolStore.ListActive(ctx, time.Now().UTC())
	if err != nil {
		return fmt.Errorf("list active pooled members for drain: %w", err)
	}
	for _, member := range storedMembers {
		key := partialPoolLookupKey(member.TargetInstanceID, member.TargetHash)
		if key == "" {
			continue
		}
		membersByKey[key] = member
	}

	var errs []error
	for _, member := range membersByKey {
		if err := s.partialPoolStore.DeleteByAnyHash(ctx, member.TargetInstanceID, member.TargetHash, member.TargetHashV2); err != nil {
			errs = append(errs, fmt.Errorf("delete pooled member %d/%s: %w", member.TargetInstanceID, normalizeHash(member.TargetHash), err))
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	return nil
}

func partialPoolMemberExpired(member *models.CrossSeedPartialPoolMember, now time.Time) bool {
	if member == nil {
		return true
	}
	if member.ExpiresAt.IsZero() {
		return false
	}
	return !member.ExpiresAt.After(now)
}

func (s *Service) pruneExpiredPartialPoolMembers(now time.Time) {
	if s == nil {
		return
	}

	s.partialPoolMu.Lock()
	defer s.partialPoolMu.Unlock()

	for _, member := range s.partialPoolByHash {
		if !partialPoolMemberExpired(member, now) {
			continue
		}
		s.removePartialPoolMemberLocked(member)
	}
}

func (s *Service) removePartialPoolMemberLocked(member *models.CrossSeedPartialPoolMember) {
	if member == nil {
		return
	}
	delete(s.partialPoolByHash, partialPoolLookupKey(member.TargetInstanceID, member.TargetHash))
	delete(s.partialPoolByHash, partialPoolLookupKey(member.TargetInstanceID, member.TargetHashV2))
	poolKey := partialPoolSourceKey(member)
	if selection, ok := s.partialPoolBySource[poolKey]; ok {
		memberKey := partialPoolLookupKey(member.TargetInstanceID, member.TargetHash)
		if selection.MemberKey == memberKey {
			delete(s.partialPoolBySource, poolKey)
		}
	}
}

func (s *Service) removePartialPoolMember(ctx context.Context, member *models.CrossSeedPartialPoolMember) {
	if member == nil {
		return
	}
	if s.partialPoolStore != nil {
		if err := s.partialPoolStore.DeleteByAnyHash(ctx, member.TargetInstanceID, member.TargetHash, member.TargetHashV2); err != nil {
			log.Debug().Err(err).Str("hash", member.TargetHash).Msg("[CROSSSEED-POOL] Failed to delete pooled member marker")
		}
	}

	s.partialPoolMu.Lock()
	s.removePartialPoolMemberLocked(member)
	s.partialPoolMu.Unlock()
}

func (s *Service) dropPartialPoolMember(ctx context.Context, member *models.CrossSeedPartialPoolMember, reason string) {
	if member == nil {
		return
	}
	if err := s.pausePartialPoolHash(ctx, member.TargetInstanceID, member.TargetHash); err != nil {
		log.Debug().Err(err).Str("hash", member.TargetHash).Msg("[CROSSSEED-POOL] Failed to pause pooled member for manual review")
		return
	}
	log.Info().
		Int("instanceID", member.TargetInstanceID).
		Str("hash", member.TargetHash).
		Str("mode", member.Mode).
		Str("reason", reason).
		Msg("[CROSSSEED-POOL] Leaving pooled member paused for manual review")
	s.removePartialPoolMember(ctx, member)
}

func (s *Service) signalPartialPoolWake() {
	if s == nil || s.partialPoolWake == nil {
		return
	}
	select {
	case s.partialPoolWake <- struct{}{}:
	default:
	}
}

func (s *Service) invalidatePartialPoolFileCache(ctx context.Context, instanceID int, hash string) {
	invalidator, ok := s.syncManager.(partialPoolFileCacheInvalidator)
	if !ok {
		return
	}
	_ = invalidator.InvalidateFileCache(ctx, instanceID, hash)
}

func (s *Service) partialPoolFileSize(member *models.CrossSeedPartialPoolMember, name string) int64 {
	for _, file := range member.SourceFiles {
		if file.Name == name {
			return file.Size
		}
	}
	return 0
}

func (s *Service) pausePartialPoolHash(ctx context.Context, instanceID int, hash string) error {
	if s == nil || s.syncManager == nil || instanceID <= 0 || strings.TrimSpace(hash) == "" {
		return nil
	}
	return s.syncManager.BulkAction(ctx, instanceID, []string{hash}, "pause")
}

func (s *Service) partialPoolTorrentPaused(state qbt.TorrentState) bool {
	return state == qbt.TorrentStatePausedDl ||
		state == qbt.TorrentStatePausedUp ||
		state == qbt.TorrentStateStoppedDl ||
		state == qbt.TorrentStateStoppedUp
}

func (s *Service) partialPoolHasActiveDownloader(states []*partialPoolState) bool {
	for _, state := range states {
		if state == nil || state.complete || state.checking {
			continue
		}
		if s.partialPoolTorrentDownloading(state.torrent.State) {
			return true
		}
	}
	return false
}

func (s *Service) partialPoolTorrentDownloading(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateDownloading ||
		state == qbt.TorrentStateStalledDl ||
		state == qbt.TorrentStateMetaDl ||
		state == qbt.TorrentStateQueuedDl ||
		state == qbt.TorrentStateAllocating ||
		state == qbt.TorrentStateForcedDl
}

func (s *Service) partialPoolTorrentRunning(state qbt.TorrentState) bool {
	return s.partialPoolTorrentDownloading(state) ||
		state == qbt.TorrentStateUploading ||
		state == qbt.TorrentStateStalledUp ||
		state == qbt.TorrentStateQueuedUp ||
		state == qbt.TorrentStateForcedUp
}

func isTorrentCheckingState(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateCheckingUp ||
		state == qbt.TorrentStateCheckingDl ||
		state == qbt.TorrentStateCheckingResumeData
}

func shouldUsePartialPool(settings *models.CrossSeedAutomationSettings, matchType string, hasExtras bool, discLayout bool) bool {
	if settings == nil || !settings.EnablePooledPartialCompletion {
		return false
	}
	if hasExtras || discLayout {
		return true
	}
	return matchType != "exact"
}

func partialPoolShouldKeepPaused(req *CrossSeedRequest, pooled bool) bool {
	if pooled {
		return true
	}
	return req.SkipAutoResume
}

func uniqueStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		out = append(out, value)
	}
	return out
}

func partialPoolStateMemberKey(state *partialPoolState) string {
	if state == nil || state.member == nil {
		return ""
	}
	return partialPoolLookupKey(state.member.TargetInstanceID, state.member.TargetHash)
}

func schedulePartialPoolBulkHash(byInstance map[int][]string, instanceID int, hash string) {
	if byInstance == nil || instanceID <= 0 {
		return
	}
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return
	}
	hashes := byInstance[instanceID]
	for _, existing := range hashes {
		if strings.EqualFold(existing, hash) {
			return
		}
	}
	byInstance[instanceID] = append(hashes, hash)
}

func (s *Service) runPartialPoolBulkAction(ctx context.Context, byInstance map[int][]string, action string, logMessage string) {
	for instanceID, hashes := range byInstance {
		if len(hashes) == 0 {
			continue
		}
		if err := s.syncManager.BulkAction(ctx, instanceID, hashes, action); err != nil {
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Strs("hashes", hashes).
				Msg(logMessage)
		}
	}
}

func (s *Service) clearPartialPoolSelection(poolKey string) {
	if poolKey == "" {
		return
	}
	s.partialPoolMu.Lock()
	delete(s.partialPoolBySource, poolKey)
	s.partialPoolMu.Unlock()
}
