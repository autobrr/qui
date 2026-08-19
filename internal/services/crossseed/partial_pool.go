// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/hardlink"
	"github.com/autobrr/qui/pkg/hardlinktree"
	"github.com/autobrr/qui/pkg/reflinktree"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	partialPoolSweepInterval = 10 * time.Second
	partialPoolStallWindow   = 15 * time.Minute
	partialPoolCooldown      = 30 * time.Minute
	partialPoolRecheckGrace  = 2 * partialPoolSweepInterval

	partialPoolRecheckPending    = "partial pool recheck pending"
	partialPoolRecheckRequested  = "partial pool recheck requested"
	partialPoolPropagationPause  = "partial pool propagation pause pending"
	partialPoolBudgetPause       = "partial pool budget pause pending"
	partialPoolModePause         = "partial pool mode pause pending"
	partialPoolSafetyPause       = "partial pool safety pause pending"
	partialPoolResumeAttempt     = "partial pool resume attempt "
	partialPoolResumeExhausted   = "partial pool resume attempts exhausted"
	partialPoolRecoveryAttempt   = "partial pool error recovery attempt "
	partialPoolRecoveryExhausted = "partial pool error recovery attempts exhausted"
	partialPoolRecoveryLimit     = 3
)

type partialPoolWake struct {
	poolID     int64
	instanceID int
	hash       string
}

func (s *Service) signalPartialPoolWake(wake partialPoolWake) {
	if s == nil || s.partialPoolWake == nil {
		return
	}
	select {
	case s.partialPoolWake <- wake:
	default:
	}
}

// partialPoolAdmissionRequiresComplete preserves the zero-budget hardlink gate
// while allowing disc-layout reflinks to repair missing data within budget.
func partialPoolAdmissionRequiresComplete(mode string, verifyBeforeSeed, discLayout bool) bool {
	return verifyBeforeSeed || (mode == models.CrossSeedPartialPoolModeHardlink && discLayout)
}

func (s *Service) partialPoolAdmissionEnabled(ctx context.Context, instance *models.Instance, hasExtras bool, req *CrossSeedRequest, requireComplete bool) bool {
	if s == nil || s.automationStore == nil || instance == nil || req == nil || !hasExtras || requireComplete ||
		req.SkipRecheck || req.SkipAutoResume || !instance.HasLocalFilesystemAccess {
		return false
	}
	settings, err := s.GetAutomationSettings(ctx)
	return err == nil && settings != nil && settings.PooledPartialCompletionEnabled
}

func partialPoolCanonicalTorrentKey(torrent *qbt.Torrent) string {
	if torrent == nil {
		return ""
	}
	for _, hash := range []string{torrent.InfohashV1, torrent.InfohashV2, torrent.Hash} {
		if normalized := normalizeHash(hash); normalized != "" {
			return normalized
		}
	}
	return ""
}

func partialPoolTorrentAliases(torrent *qbt.Torrent) []string {
	if torrent == nil {
		return nil
	}
	return normalizedHashes(torrent.InfohashV1, torrent.InfohashV2, torrent.Hash)
}

func partialPoolParsedIdentity(torrentBytes []byte) (key, infohashV1, infohashV2 string, descriptors []partialPoolFileDescriptor, err error) {
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return "", "", "", nil, err
	}
	descriptors, err = buildPartialPoolFileDescriptors(meta.Info)
	if err != nil {
		return "", "", "", nil, err
	}
	if meta.Info != nil && meta.Info.HasV1() {
		infohashV1 = normalizeHash(meta.HashV1)
	}
	if meta.Info != nil && meta.Info.HasV2() {
		infohashV2 = normalizeHash(meta.HashV2)
	}
	key = infohashV1
	if key == "" {
		key = infohashV2
	}
	if key == "" {
		return "", "", "", nil, errors.New("partial pool torrent has no usable identity")
	}
	return key, infohashV1, infohashV2, descriptors, nil
}

func (s *Service) registerPartialPoolAdmission(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	mode, rootPath string,
	materializedFiles []hardlinktree.TorrentFile,
	descriptors []partialPoolFileDescriptor,
) (*models.CrossSeedPartialPool, *models.CrossSeedPartialPoolMember, error) {
	if s == nil || s.automationStore == nil || s.syncManager == nil {
		return nil, nil, errors.New("partial pool storage or qBittorrent service unavailable")
	}
	memberKey, infohashV1, infohashV2, parsedDescriptors, err := partialPoolParsedIdentity(torrentBytes)
	if err != nil {
		return nil, nil, err
	}
	if len(descriptors) == 0 {
		descriptors = parsedDescriptors
	}

	memberAliases := normalizedHashes(memberKey, infohashV1, infohashV2)
	addedTorrent, found, err := s.syncManager.HasTorrentByAnyHash(ctx, candidate.InstanceID, memberAliases)
	if err != nil {
		return nil, nil, fmt.Errorf("resolve added partial pool torrent: %w", err)
	}
	fetchHash := memberKey
	if found && addedTorrent != nil && normalizeHash(addedTorrent.Hash) != "" {
		fetchHash = addedTorrent.Hash
	}
	refreshCtx := qbittorrent.WithForceFilesRefresh(ctx)
	filesByHash, err := s.syncManager.GetTorrentFilesBatch(refreshCtx, candidate.InstanceID, []string{fetchHash})
	if err != nil {
		return nil, nil, fmt.Errorf("refresh added partial pool files: %w", err)
	}
	files := filesByHash[normalizeHash(fetchHash)]
	if len(files) == 0 {
		for _, alias := range normalizedHashes(fetchHash, memberKey, infohashV1, infohashV2) {
			if len(filesByHash[alias]) > 0 {
				files = filesByHash[alias]
				break
			}
		}
	}
	if len(files) == 0 {
		return nil, nil, errors.New("added partial pool torrent returned no files")
	}

	materializedPaths := make(map[string]struct{}, len(materializedFiles))
	for _, file := range materializedFiles {
		materializedPaths[file.Path] = struct{}{}
	}
	fileRows, missingBytes, err := buildPartialPoolAdmissionFiles(descriptors, files, materializedPaths)
	if err != nil {
		return nil, nil, err
	}

	matchedKey := partialPoolCanonicalTorrentKey(matchedTorrent)
	matchedAliases := partialPoolTorrentAliases(matchedTorrent)
	if matchedKey == "" {
		return nil, nil, errors.New("matched partial pool source has no identity")
	}

	sourceInstanceID := 0
	sourceKey := ""
	var sourceAliases []string
	if req.SearchDecision.SourceInstanceID > 0 && normalizeHash(req.SearchDecision.SourceHash) != "" {
		sourceInstanceID = req.SearchDecision.SourceInstanceID
		sourceKey = normalizeHash(req.SearchDecision.SourceHash)
		sourceAliases = []string{sourceKey}
		if sourceTorrent, sourceFound, sourceErr := s.syncManager.HasTorrentByAnyHash(ctx, sourceInstanceID, sourceAliases); sourceErr == nil && sourceFound {
			sourceKey = partialPoolCanonicalTorrentKey(sourceTorrent)
			sourceAliases = partialPoolTorrentAliases(sourceTorrent)
		}
	}

	return s.automationStore.RegisterPartialPoolMember(ctx, models.CrossSeedPartialPoolRegistration{
		SourceInstanceID:  sourceInstanceID,
		SourceTorrentKey:  sourceKey,
		SourceAliases:     sourceAliases,
		MatchedInstanceID: candidate.InstanceID,
		MatchedTorrentKey: matchedKey,
		MatchedAliases:    matchedAliases,
		Member: models.CrossSeedPartialPoolMember{
			InstanceID:      candidate.InstanceID,
			TorrentKey:      memberKey,
			InfoHashV1:      infohashV1,
			InfoHashV2:      infohashV2,
			Mode:            mode,
			RootPath:        rootPath,
			ReportedSeeders: max(req.ReportedSeeders, 0),
			Status:          models.CrossSeedPartialPoolMemberStatusVerifying,
			MissingBytes:    missingBytes,
			LastError:       partialPoolRecheckPending,
		},
		Files: fileRows,
	})
}

func (s *Service) recordPartialPoolRecheckRequested(ctx context.Context, member *models.CrossSeedPartialPoolMember) {
	if s == nil || s.automationStore == nil || member == nil {
		return
	}
	reason := partialPoolRecheckRequested
	_, _ = s.automationStore.TransitionPartialPoolMember(
		ctx,
		member.ID,
		[]string{member.Status},
		member.Status,
		models.PartialPoolMemberMutation{LastError: &reason},
	)
}

func normalizedHashes(values ...string) []string {
	seen := make(map[string]struct{}, len(values))
	hashes := make([]string, 0, len(values))
	for _, value := range values {
		value = normalizeHash(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		hashes = append(hashes, value)
	}
	return hashes
}

func (s *Service) markPartialPoolMemberManual(ctx context.Context, memberID int64, expected []string, reason string) {
	if s == nil || s.automationStore == nil || memberID == 0 {
		return
	}
	reason = strings.TrimSpace(reason)
	_, _ = s.automationStore.TransitionPartialPoolMember(ctx, memberID, expected, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason})
}

func (s *Service) pausePartialPoolMemberForReview(ctx context.Context, member *models.CrossSeedPartialPoolMember, torrent qbt.Torrent, reason string) {
	if s == nil || member == nil || ctx.Err() != nil {
		return
	}
	if !isPausedOrStopped(torrent.State) {
		_ = s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause")
	}
	reason = strings.TrimSpace(reason)
	if member.Status == models.CrossSeedPartialPoolMemberStatusManual && member.LastError == reason {
		return
	}
	s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason})
}

type partialPoolTorrentInventory struct {
	loaded  bool
	byAlias map[string]qbt.Torrent
}

type partialPoolMemberSnapshot struct {
	torrent     qbt.Torrent
	files       qbt.TorrentFiles
	fileByIndex map[int]qbt.TorrentFile
}

// RunPartialPoolCoordinator reconciles durable partial completion work until
// ctx is canceled. It owns no child goroutines.
func (s *Service) RunPartialPoolCoordinator(ctx context.Context) {
	if s == nil || s.automationStore == nil || s.syncManager == nil {
		return
	}

	ticker := time.NewTicker(partialPoolSweepInterval)
	defer ticker.Stop()

	s.reconcilePartialPools(ctx, time.Now(), partialPoolWake{})
	for {
		select {
		case <-ctx.Done():
			return
		case wake := <-s.partialPoolWake:
			s.reconcilePartialPools(ctx, time.Now(), wake)
		case now := <-ticker.C:
			s.reconcilePartialPools(ctx, now, partialPoolWake{})
		}
	}
}

func (s *Service) reconcilePartialPools(ctx context.Context, now time.Time, wake partialPoolWake) {
	if ctx.Err() != nil {
		return
	}
	if wake.instanceID > 0 && wake.hash != "" {
		if _, _, err := s.automationStore.ResolvePartialPoolMember(ctx, wake.instanceID, wake.hash); err != nil && ctx.Err() == nil {
			log.Debug().Err(err).Int("instanceID", wake.instanceID).Msg("Partial pool completion wake did not resolve a member")
		}
	} else if wake.poolID > 0 {
		if err := s.automationStore.SetPartialPoolStatus(ctx, wake.poolID, models.CrossSeedPartialPoolStatusActive); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Int64("poolID", wake.poolID).Msg("Failed to activate partial completion pool")
		}
	}
	if ctx.Err() != nil {
		return
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil || settings == nil {
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("Failed to load partial completion settings")
		}
		return
	}
	pools, err := s.automationStore.ListPartialPoolsForReconciliation(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Msg("Failed to list partial completion pools")
		}
		return
	}
	if len(pools) == 0 {
		_ = s.automationStore.PruneEmptyPartialPools(ctx)
		return
	}

	inventories := s.loadPartialPoolTorrentInventories(ctx, pools)
	for _, pool := range pools {
		if ctx.Err() != nil {
			return
		}
		observed := s.observePartialPoolMembers(ctx, pool, inventories)
		if len(observed) == 0 {
			continue
		}
		pool, err = s.automationStore.GetPartialPool(ctx, pool.ID)
		if err != nil {
			continue
		}
		snapshots := make(map[int64]*partialPoolMemberSnapshot, len(pool.Members))
		for _, member := range pool.Members {
			if torrent, ok := partialPoolInventoryTorrent(inventories[member.InstanceID], member); ok {
				snapshots[member.ID] = &partialPoolMemberSnapshot{torrent: torrent}
			}
		}

		if !settings.PooledPartialCompletionEnabled {
			s.reconcileDisabledPartialPool(ctx, pool, snapshots)
			_ = s.automationStore.SetPartialPoolStatus(ctx, pool.ID, models.CrossSeedPartialPoolStatusDormant)
			continue
		}

		s.refreshPartialPoolFiles(ctx, pool, snapshots)
		s.reconcilePartialPool(ctx, now, pool, snapshots, int64(max(settings.AutoResumeMaxDownloadMB, 0))<<20)
	}
	_ = s.automationStore.PruneEmptyPartialPools(ctx)
}

func (s *Service) loadPartialPoolTorrentInventories(ctx context.Context, pools []*models.CrossSeedPartialPool) map[int]partialPoolTorrentInventory {
	instanceIDs := make(map[int]struct{})
	for _, pool := range pools {
		for _, member := range pool.Members {
			if member.Status != models.CrossSeedPartialPoolMemberStatusRemoved {
				instanceIDs[member.InstanceID] = struct{}{}
			}
		}
	}
	ids := make([]int, 0, len(instanceIDs))
	for id := range instanceIDs {
		ids = append(ids, id)
	}
	sort.Ints(ids)

	inventories := make(map[int]partialPoolTorrentInventory, len(ids))
	for _, instanceID := range ids {
		if ctx.Err() != nil {
			break
		}
		torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{})
		if err != nil {
			continue
		}
		inventory := partialPoolTorrentInventory{loaded: true, byAlias: make(map[string]qbt.Torrent, len(torrents)*2)}
		for _, torrent := range torrents {
			for _, alias := range normalizedHashes(torrent.Hash, torrent.InfohashV1, torrent.InfohashV2) {
				inventory.byAlias[alias] = torrent
			}
		}
		inventories[instanceID] = inventory
	}
	return inventories
}

func partialPoolInventoryTorrent(inventory partialPoolTorrentInventory, member *models.CrossSeedPartialPoolMember) (qbt.Torrent, bool) {
	if !inventory.loaded || member == nil {
		return qbt.Torrent{}, false
	}
	for _, alias := range normalizedHashes(member.TorrentKey, member.InfoHashV1, member.InfoHashV2) {
		if torrent, ok := inventory.byAlias[alias]; ok {
			return torrent, true
		}
	}
	return qbt.Torrent{}, false
}

func (s *Service) observePartialPoolMembers(
	ctx context.Context,
	pool *models.CrossSeedPartialPool,
	inventories map[int]partialPoolTorrentInventory,
) map[int64]qbt.Torrent {
	observed := make(map[int64]qbt.Torrent, len(pool.Members))
	for _, member := range pool.Members {
		inventory := inventories[member.InstanceID]
		if !inventory.loaded {
			continue
		}
		torrent, found := partialPoolInventoryTorrent(inventory, member)
		if !found {
			for _, file := range member.Files {
				delete(s.partialPoolCreated, file.ID)
			}
			_ = s.automationStore.MarkPartialPoolMemberRemoved(ctx, member.ID, "torrent no longer exists in qBittorrent")
			continue
		}
		observed[member.ID] = torrent
	}
	return observed
}

func (s *Service) refreshPartialPoolFiles(
	ctx context.Context,
	pool *models.CrossSeedPartialPool,
	snapshots map[int64]*partialPoolMemberSnapshot,
) {
	type memberRequest struct {
		member *models.CrossSeedPartialPoolMember
		hash   string
	}
	requestsByInstance := make(map[int][]memberRequest)
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		snapshot, ok := snapshots[member.ID]
		if !ok {
			continue
		}
		if normalizePathForComparison(snapshot.torrent.SavePath) != normalizePathForComparison(member.RootPath) {
			s.pausePartialPoolMemberForReview(ctx, member, snapshot.torrent, "qBittorrent save path no longer matches admitted root")
			continue
		}
		hash := normalizeHash(snapshot.torrent.Hash)
		if hash == "" {
			hash = member.TorrentKey
		}
		requestsByInstance[member.InstanceID] = append(requestsByInstance[member.InstanceID], memberRequest{member: member, hash: hash})
	}

	instanceIDs := make([]int, 0, len(requestsByInstance))
	for instanceID := range requestsByInstance {
		instanceIDs = append(instanceIDs, instanceID)
	}
	sort.Ints(instanceIDs)
	for _, instanceID := range instanceIDs {
		if ctx.Err() != nil {
			return
		}
		requests := requestsByInstance[instanceID]
		hashes := make([]string, 0, len(requests))
		for _, request := range requests {
			hashes = append(hashes, request.hash)
		}
		filesByHash, err := s.syncManager.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), instanceID, hashes)
		if err != nil {
			continue
		}
		for _, request := range requests {
			files := filesByHash[normalizeHash(request.hash)]
			if len(files) == 0 {
				for _, alias := range normalizedHashes(request.member.TorrentKey, request.member.InfoHashV1, request.member.InfoHashV2) {
					if len(filesByHash[alias]) > 0 {
						files = filesByHash[alias]
						break
					}
				}
			}
			fileByIndex, valid := partialPoolCurrentFiles(request.member, files)
			if !valid {
				if len(files) > 0 {
					s.pausePartialPoolMemberForReview(ctx, request.member, snapshots[request.member.ID].torrent, "qBittorrent files or priorities no longer match admission")
				}
				continue
			}
			snapshot := snapshots[request.member.ID]
			snapshot.files = files
			snapshot.fileByIndex = fileByIndex
		}
	}
}

func partialPoolCurrentFiles(member *models.CrossSeedPartialPoolMember, files qbt.TorrentFiles) (map[int]qbt.TorrentFile, bool) {
	if member == nil || len(files) != len(member.Files) {
		return nil, false
	}
	current := make(map[int]qbt.TorrentFile, len(files))
	for _, file := range files {
		if _, duplicate := current[file.Index]; duplicate {
			return nil, false
		}
		current[file.Index] = file
	}
	for _, file := range member.Files {
		currentFile, ok := current[file.FileIndex]
		if !ok || currentFile.Name != file.RelativePath || currentFile.Size != file.SizeBytes || (currentFile.Priority > 0) != file.WantedAtAdmission {
			return nil, false
		}
	}
	return current, true
}

func (s *Service) refreshPartialPoolMemberSnapshot(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) bool {
	if ctx.Err() != nil || member == nil || snapshot == nil {
		return false
	}
	hash := normalizeHash(snapshot.torrent.Hash)
	if hash == "" {
		hash = member.TorrentKey
	}
	filesByHash, err := s.syncManager.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), member.InstanceID, []string{hash})
	if err != nil {
		return false
	}
	files := filesByHash[normalizeHash(hash)]
	if len(files) == 0 {
		for _, alias := range partialPoolMemberHashes(member) {
			if len(filesByHash[alias]) > 0 {
				files = filesByHash[alias]
				break
			}
		}
	}
	fileByIndex, valid := partialPoolCurrentFiles(member, files)
	if !valid {
		return false
	}
	snapshot.files = files
	snapshot.fileByIndex = fileByIndex
	return true
}

func (s *Service) partialPoolCoordinatorEnabled(ctx context.Context) bool {
	settings, err := s.GetAutomationSettings(ctx)
	return err == nil && settings != nil && settings.PooledPartialCompletionEnabled
}

func partialPoolTorrentComplete(torrent qbt.Torrent) bool {
	return torrent.State != qbt.TorrentStateError && torrent.State != qbt.TorrentStateMissingFiles && torrent.Progress >= 1 && torrent.AmountLeft <= 0
}

func partialPoolChecking(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateCheckingUp || state == qbt.TorrentStateCheckingDl || state == qbt.TorrentStateCheckingResumeData
}

func partialPoolTransferCapable(state qbt.TorrentState) bool {
	return isDownloadingOrQueued(state)
}

func (s *Service) reconcileDisabledPartialPool(ctx context.Context, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot) {
	for _, member := range pool.Members {
		if member.Status != models.CrossSeedPartialPoolMemberStatusAcquiring || !member.StartedByPool {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil || partialPoolTorrentComplete(snapshot.torrent) {
			continue
		}
		if isPausedOrStopped(snapshot.torrent.State) {
			stopped := false
			_, _ = s.automationStore.TransitionPartialPoolMember(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{
				StartedByPool:       &stopped,
				LastDownloadedBytes: models.NullableInt64Update{Set: true},
				LastProgressAt:      models.NullableTimeUpdate{Set: true},
			})
			s.resetPartialPoolAcquiringFiles(ctx, member)
			continue
		}
		if ctx.Err() != nil {
			return
		}
		_ = s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause")
	}
}

func (s *Service) reconcilePartialPool(
	ctx context.Context,
	now time.Time,
	pool *models.CrossSeedPartialPool,
	snapshots map[int64]*partialPoolMemberSnapshot,
	budget int64,
) {
	for _, member := range pool.Members {
		if snapshot := snapshots[member.ID]; snapshot != nil && len(snapshot.files) > 0 && partialPoolTorrentComplete(snapshot.torrent) {
			s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		}
	}
	s.propagatePartialPoolFiles(ctx, now, pool, snapshots)

	for _, member := range pool.Members {
		if ctx.Err() != nil {
			return
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil {
			continue
		}
		if s.reconcilePartialPoolExceptionalState(ctx, now, member, snapshot) {
			continue
		}
		if len(snapshot.files) == 0 {
			continue
		}
		switch member.Status {
		case models.CrossSeedPartialPoolMemberStatusVerifying:
			s.reconcilePartialPoolVerifying(ctx, now, member, snapshot, budget)
		case models.CrossSeedPartialPoolMemberStatusRechecking:
			s.reconcilePartialPoolRechecking(ctx, now, pool, member, snapshot, budget)
		case models.CrossSeedPartialPoolMemberStatusAcquiring:
			s.reconcilePartialPoolAcquiring(ctx, now, member, snapshot, budget)
		case models.CrossSeedPartialPoolMemberStatusComplete:
			s.reconcilePartialPoolComplete(ctx, member, snapshot)
		case models.CrossSeedPartialPoolMemberStatusManual:
			if partialPoolTorrentComplete(snapshot.torrent) {
				s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusComplete, models.PartialPoolMemberMutation{})
			}
		}
	}

	for _, member := range pool.Members {
		if ctx.Err() != nil {
			return
		}
		if member.Status != models.CrossSeedPartialPoolMemberStatusWaiting && member.Status != models.CrossSeedPartialPoolMemberStatusBlocked {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil || len(snapshot.files) == 0 || partialPoolChecking(snapshot.torrent.State) || partialPoolMemberHasVerificationWork(member) {
			continue
		}
		s.reapplyPartialPoolGate(ctx, member, snapshot, budget)
	}

	s.propagatePartialPoolFiles(ctx, now, pool, snapshots)
	if ctx.Err() == nil {
		s.selectAndResumePartialPoolDownloader(ctx, now, pool, snapshots, budget)
	}

	status := models.CrossSeedPartialPoolStatusDormant
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusVerifying || member.Status == models.CrossSeedPartialPoolMemberStatusAcquiring || member.Status == models.CrossSeedPartialPoolMemberStatusRechecking || partialPoolMemberHasVerificationWork(member) || member.LastError == partialPoolPropagationPause || partialPoolResumePending(member.LastError) || partialPoolRecoveryPending(member.LastError) {
			status = models.CrossSeedPartialPoolStatusActive
			break
		}
	}
	_ = s.automationStore.SetPartialPoolStatus(ctx, pool.ID, status)
}

func (s *Service) reconcilePartialPoolExceptionalState(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) bool {
	if member == nil || snapshot == nil || member.Status == models.CrossSeedPartialPoolMemberStatusManual || member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
		return false
	}
	if snapshot.torrent.State == qbt.TorrentStateMissingFiles && member.Mode == models.CrossSeedPartialPoolModeHardlink {
		reason := "hardlink partial pool member entered missingFiles state"
		if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
			s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		} else {
			s.markPartialPoolMemberManual(ctx, member.ID, []string{member.Status}, reason)
			member.Status = models.CrossSeedPartialPoolMemberStatusManual
		}
		return true
	}
	if snapshot.torrent.State == qbt.TorrentStateMissingFiles &&
		member.Mode == models.CrossSeedPartialPoolModeReflink &&
		member.Status == models.CrossSeedPartialPoolMemberStatusComplete &&
		partialPoolResumePending(member.LastError) {
		s.requestPartialPoolResume(ctx, member)
		return true
	}

	attempts, recovering := partialPoolRecoveryAttemptCount(member.LastError)
	if snapshot.torrent.State != qbt.TorrentStateError {
		if !recovering {
			return false
		}
		if partialPoolChecking(snapshot.torrent.State) {
			return true
		}
		if now.Sub(member.UpdatedAt) < partialPoolRecheckGrace {
			return true
		}
		reason := ""
		if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
			reason = partialPoolResumeAttemptReason(0)
		}
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		return false
	}
	if member.Status == models.CrossSeedPartialPoolMemberStatusComplete && !partialPoolResumePending(member.LastError) && !recovering {
		return false
	}
	if !s.partialPoolCoordinatorEnabled(ctx) {
		return true
	}
	if recovering && now.Sub(member.UpdatedAt) < partialPoolRecheckGrace {
		return true
	}
	if attempts >= partialPoolRecoveryLimit {
		s.finishPartialPoolRecoveryExhausted(ctx, member, partialPoolRecoveryExhausted)
		return true
	}

	reason := partialPoolRecoveryAttemptReason(attempts + 1)
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
		return true
	}
	member.UpdatedAt = now
	_ = s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause")
	if err := s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "recheck"); err != nil && attempts+1 >= partialPoolRecoveryLimit {
		s.finishPartialPoolRecoveryExhausted(ctx, member, partialPoolRecoveryExhausted+": "+err.Error())
	}
	return true
}

func (s *Service) finishPartialPoolRecoveryExhausted(ctx context.Context, member *models.CrossSeedPartialPoolMember, reason string) {
	if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		return
	}
	s.markPartialPoolMemberManual(ctx, member.ID, []string{member.Status}, reason)
	member.Status = models.CrossSeedPartialPoolMemberStatusManual
}

func (s *Service) reconcilePartialPoolVerifying(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot, budget int64) {
	if partialPoolChecking(snapshot.torrent.State) {
		return
	}
	if member.LastError == partialPoolRecheckPending {
		s.requestPartialPoolRecheck(ctx, now, member)
		return
	}
	if member.LastError == partialPoolRecheckRequested && now.Sub(member.UpdatedAt) < partialPoolRecheckGrace {
		return
	}

	if partialPoolTorrentComplete(snapshot.torrent) {
		s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		s.completeAndResumePartialPoolMember(ctx, member, snapshot)
		return
	}
	status, reason := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	if status == models.CrossSeedPartialPoolMemberStatusManual {
		for _, file := range member.Files {
			if file.MaterializedAtAdd && snapshot.fileByIndex[file.FileIndex].Progress < 1 {
				s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusManual, models.PartialPoolFileMutation{LastError: &reason})
			}
		}
	}
	s.updatePartialPoolObservedFiles(ctx, member, snapshot, false)
	empty := ""
	missing := max(snapshot.torrent.AmountLeft, 0)
	s.transitionPartialPoolMember(ctx, member, status, models.PartialPoolMemberMutation{MissingBytes: &missing, LastError: choosePartialPoolError(status, reason, &empty)})
}

func choosePartialPoolError(status, reason string, empty *string) *string {
	if status == models.CrossSeedPartialPoolMemberStatusManual {
		return &reason
	}
	return empty
}

func partialPoolPostRecheckVerdict(
	member *models.CrossSeedPartialPoolMember,
	snapshot *partialPoolMemberSnapshot,
	budget int64,
	normalizer *stringutils.Normalizer[string, string],
) (string, string) {
	if member == nil || snapshot == nil || len(snapshot.files) == 0 {
		return models.CrossSeedPartialPoolMemberStatusManual, "missing refreshed qBittorrent file evidence"
	}
	if member.Mode == models.CrossSeedPartialPoolModeHardlink {
		for _, file := range member.Files {
			if (file.MaterializedAtAdd || file.SourceFileID != nil) && snapshot.fileByIndex[file.FileIndex].Progress < 1 {
				return models.CrossSeedPartialPoolMemberStatusManual, "a hardlinked file failed verification"
			}
		}
		if PolicyForSourceFiles(snapshot.files).DiscLayout {
			budget = 0
		}
	}
	if !postRecheckBudgetSatisfied(snapshot.torrent, budget, snapshot.files, normalizer) {
		return models.CrossSeedPartialPoolMemberStatusBlocked, "post-recheck missing bytes exceed the auto-start budget"
	}
	return models.CrossSeedPartialPoolMemberStatusWaiting, ""
}

func (s *Service) reapplyPartialPoolGate(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot, budget int64) {
	status, reason := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	if partialPoolTorrentComplete(snapshot.torrent) {
		status = models.CrossSeedPartialPoolMemberStatusComplete
	}
	missing := max(snapshot.torrent.AmountLeft, 0)
	empty := ""
	s.transitionPartialPoolMember(ctx, member, status, models.PartialPoolMemberMutation{MissingBytes: &missing, LastError: choosePartialPoolError(status, reason, &empty)})
}

func (s *Service) requestPartialPoolRecheck(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember) {
	if ctx.Err() != nil || member == nil || !s.partialPoolCoordinatorEnabled(ctx) {
		return
	}
	err := s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), member.InstanceID, partialPoolMemberHashes(member), "recheck")
	if err != nil {
		s.markPartialPoolMemberManual(ctx, member.ID, []string{member.Status}, "recheck request failed: "+err.Error())
		member.Status = models.CrossSeedPartialPoolMemberStatusManual
		return
	}
	reason := partialPoolRecheckRequested
	if s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
		member.UpdatedAt = now
	}
}

func partialPoolResumeAttemptCount(reason string) (int, bool) {
	return partialPoolAttemptCount(reason, partialPoolResumeAttempt)
}

func partialPoolRecoveryAttemptCount(reason string) (int, bool) {
	return partialPoolAttemptCount(reason, partialPoolRecoveryAttempt)
}

func partialPoolRecoveryPending(reason string) bool {
	_, pending := partialPoolRecoveryAttemptCount(reason)
	return pending
}

func partialPoolAttemptCount(reason, prefix string) (int, bool) {
	value, found := strings.CutPrefix(reason, prefix)
	if !found {
		return 0, false
	}
	attempts, err := strconv.Atoi(value)
	return attempts, err == nil && attempts >= 0
}

func partialPoolResumePending(reason string) bool {
	_, pending := partialPoolResumeAttemptCount(reason)
	return pending
}

func partialPoolResumeAttemptReason(attempts int) string {
	return partialPoolResumeAttempt + strconv.Itoa(attempts)
}

func partialPoolRecoveryAttemptReason(attempts int) string {
	return partialPoolRecoveryAttempt + strconv.Itoa(attempts)
}

func (s *Service) requestPartialPoolResume(ctx context.Context, member *models.CrossSeedPartialPoolMember) bool {
	if ctx.Err() != nil || member == nil || !s.partialPoolCoordinatorEnabled(ctx) {
		return false
	}
	attempts, pending := partialPoolResumeAttemptCount(member.LastError)
	if !pending {
		attempts = 0
	}
	if attempts >= maxRecheckResumeAttempts {
		reason := partialPoolResumeExhausted
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		return false
	}

	reason := partialPoolResumeAttemptReason(attempts + 1)
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
		return false
	}
	err := s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), member.InstanceID, partialPoolMemberHashes(member), "resume")
	if err == nil {
		return true
	}
	if attempts+1 >= maxRecheckResumeAttempts {
		reason = partialPoolResumeExhausted + ": " + err.Error()
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
	}
	return false
}

func (s *Service) completeAndResumePartialPoolMember(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) {
	if member == nil || snapshot == nil {
		return
	}
	reason := partialPoolResumeAttemptReason(0)
	if !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusComplete, models.PartialPoolMemberMutation{LastError: &reason}) {
		return
	}
	if isRecheckResumeConfirmed(snapshot.torrent.State) {
		empty := ""
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &empty})
		return
	}
	s.requestPartialPoolResume(ctx, member)
}

func (s *Service) reconcilePartialPoolComplete(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) {
	if !partialPoolResumePending(member.LastError) {
		return
	}
	if !partialPoolTorrentComplete(snapshot.torrent) {
		reason := "completed partial pool member lost verification before resume"
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		return
	}
	if isRecheckResumeConfirmed(snapshot.torrent.State) {
		empty := ""
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &empty})
		return
	}
	if partialPoolChecking(snapshot.torrent.State) {
		return
	}
	if isPausedOrStopped(snapshot.torrent.State) {
		s.requestPartialPoolResume(ctx, member)
	}
}

func (s *Service) reconcilePartialPoolRechecking(
	ctx context.Context,
	now time.Time,
	pool *models.CrossSeedPartialPool,
	member *models.CrossSeedPartialPoolMember,
	snapshot *partialPoolMemberSnapshot,
	budget int64,
) {
	if member.LastError == partialPoolRecheckPending {
		s.requestPartialPoolRecheck(ctx, now, member)
		return
	}
	if partialPoolChecking(snapshot.torrent.State) {
		return
	}
	if member.LastError == partialPoolRecheckRequested && now.Sub(member.UpdatedAt) < partialPoolRecheckGrace {
		return
	}
	if partialPoolTorrentComplete(snapshot.torrent) {
		s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		s.completeAndResumePartialPoolMember(ctx, member, snapshot)
		return
	}

	for _, file := range member.Files {
		if file.Status != models.CrossSeedPartialPoolFileStatusVerifying {
			continue
		}
		current := snapshot.fileByIndex[file.FileIndex]
		if current.Progress >= 1 {
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{})
			delete(s.partialPoolCreated, file.ID)
			continue
		}
		if member.Mode == models.CrossSeedPartialPoolModeHardlink {
			if s.rollbackLivePartialPoolHardlink(ctx, file, pool) {
				reason := partialPoolRecheckPending
				s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusRechecking, models.PartialPoolMemberMutation{LastError: &reason})
				s.requestPartialPoolRecheck(ctx, now, member)
				return
			}
			reason := "propagated hardlink failed verification; target ownership is not provable"
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusManual, models.PartialPoolFileMutation{LastError: &reason})
			s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusRechecking}, reason)
			member.Status = models.CrossSeedPartialPoolMemberStatusManual
			return
		}
		reason := "propagated reflink failed verification; retained clone requires download repair"
		s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{
			SourceFileID: models.NullableInt64Update{Set: true},
			LastError:    &reason,
		})
	}

	status, reason := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	missing := max(snapshot.torrent.AmountLeft, 0)
	empty := ""
	s.transitionPartialPoolMember(ctx, member, status, models.PartialPoolMemberMutation{MissingBytes: &missing, LastError: choosePartialPoolError(status, reason, &empty)})
}

func (s *Service) rollbackLivePartialPoolHardlink(ctx context.Context, file *models.CrossSeedPartialPoolMemberFile, pool *models.CrossSeedPartialPool) bool {
	created := s.partialPoolCreated[file.ID]
	if created == nil || file.SourceFileID == nil {
		return false
	}
	sourceMember, sourceFile := partialPoolFileByID(pool, *file.SourceFileID)
	targetMember := partialPoolMemberForFile(pool, file)
	if sourceMember == nil || sourceFile == nil || targetMember == nil {
		return false
	}
	sourcePath, err := partialPoolLocalPath(sourceMember, sourceFile)
	if err != nil {
		return false
	}
	targetPath, err := partialPoolLocalPath(targetMember, file)
	if err != nil || !partialPoolCreatedContains(created, targetPath) || !partialPoolSameFile(sourcePath, targetPath) || ctx.Err() != nil {
		return false
	}
	if err := created.Rollback(); err != nil {
		return false
	}
	delete(s.partialPoolCreated, file.ID)
	s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{SourceFileID: models.NullableInt64Update{Set: true}})
	return true
}

func partialPoolCreatedContains(created *hardlinktree.Created, target string) bool {
	if created == nil {
		return false
	}
	return slices.Contains(created.Files, target)
}

func partialPoolSameFile(sourcePath, targetPath string) bool {
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil || !sourceInfo.Mode().IsRegular() {
		return false
	}
	targetInfo, err := os.Lstat(targetPath)
	if err != nil || !targetInfo.Mode().IsRegular() {
		return false
	}
	sourceID, _, err := hardlink.GetFileID(sourceInfo, sourcePath)
	if err != nil {
		return false
	}
	targetID, _, err := hardlink.GetFileID(targetInfo, targetPath)
	return err == nil && sourceID == targetID
}

func (s *Service) reconcilePartialPoolAcquiring(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot, budget int64) {
	s.updatePartialPoolObservedFiles(ctx, member, snapshot, true)
	missing := max(snapshot.torrent.AmountLeft, 0)
	if partialPoolChecking(snapshot.torrent.State) {
		s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{
			MissingBytes:   &missing,
			LastProgressAt: models.NullableTimeUpdate{Set: true},
		})
		return
	}
	if partialPoolTorrentComplete(snapshot.torrent) {
		s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		s.completeAndResumePartialPoolMember(ctx, member, snapshot)
		return
	}
	if snapshot.torrent.State == qbt.TorrentStateMissingFiles {
		if member.Mode != models.CrossSeedPartialPoolModeReflink {
			s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, "hardlink downloader entered missingFiles state")
			member.Status = models.CrossSeedPartialPoolMemberStatusManual
			return
		}
		s.requestPartialPoolResume(ctx, member)
		if strings.HasPrefix(member.LastError, partialPoolResumeExhausted) {
			s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, member.LastError)
			member.Status = models.CrossSeedPartialPoolMemberStatusManual
		}
		return
	}
	if isPausedOrStopped(snapshot.torrent.State) {
		if member.LastError == partialPoolBudgetPause {
			stopped := false
			empty := ""
			s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{StartedByPool: &stopped, LastError: &empty})
			s.resetPartialPoolAcquiringFiles(ctx, member)
			return
		}
		if member.LastError == partialPoolModePause || member.LastError == partialPoolSafetyPause {
			reason := "link mode or local filesystem access was disabled"
			if member.LastError == partialPoolSafetyPause {
				reason = "a hardlinked file failed verification"
			}
			s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, reason)
			member.Status = models.CrossSeedPartialPoolMemberStatusManual
			return
		}
		if partialPoolResumePending(member.LastError) {
			s.requestPartialPoolResume(ctx, member)
			if strings.HasPrefix(member.LastError, partialPoolResumeExhausted) {
				s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, member.LastError)
				member.Status = models.CrossSeedPartialPoolMemberStatusManual
			}
			return
		}
		status, _ := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
		modeEnabled := s.partialPoolMemberModeEnabled(ctx, member)
		if status != models.CrossSeedPartialPoolMemberStatusWaiting || !modeEnabled {
			reason := partialPoolBudgetPause
			if status == models.CrossSeedPartialPoolMemberStatusManual {
				reason = partialPoolSafetyPause
			} else if !modeEnabled {
				reason = partialPoolModePause
			}
			s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{LastError: &reason})
			return
		}
		if member.LastDownloadedBytes == nil || snapshot.torrent.Downloaded != *member.LastDownloadedBytes {
			downloaded := snapshot.torrent.Downloaded
			s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{
				MissingBytes:        &missing,
				LastDownloadedBytes: models.NullableInt64Update{Set: true, Value: &downloaded},
				LastProgressAt:      models.NullableTimeUpdate{Set: true, Value: &now},
			})
		}
		if partialPoolStalled(now, member.LastProgressAt) {
			retryAfter := now.Add(partialPoolCooldown)
			stopped := false
			if s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{
				MissingBytes:  &missing,
				StartedByPool: &stopped,
				RetryAfter:    models.NullableTimeUpdate{Set: true, Value: &retryAfter},
			}) {
				member.RetryAfter = &retryAfter
			}
			s.resetPartialPoolAcquiringFiles(ctx, member)
			return
		}
		if ctx.Err() != nil {
			return
		}
		s.requestPartialPoolResume(ctx, member)
		return
	}
	if partialPoolTransferCapable(snapshot.torrent.State) && partialPoolResumePending(member.LastError) {
		empty := ""
		s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{LastError: &empty})
	}
	status, _ := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	modeEnabled := s.partialPoolMemberModeEnabled(ctx, member)
	if status != models.CrossSeedPartialPoolMemberStatusWaiting || !modeEnabled {
		reason := partialPoolBudgetPause
		if status == models.CrossSeedPartialPoolMemberStatusManual {
			reason = partialPoolSafetyPause
		} else if !modeEnabled {
			reason = partialPoolModePause
		}
		s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{LastError: &reason})
		if ctx.Err() == nil {
			_ = s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause")
		}
		return
	}

	downloaded, progressedAt, update, stalled := partialPoolProgressDecision(now, snapshot.torrent.Downloaded, member.LastDownloadedBytes, member.LastProgressAt, partialPoolTransferCapable(snapshot.torrent.State))
	if update {
		s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{
			MissingBytes:        &missing,
			LastDownloadedBytes: models.NullableInt64Update{Set: true, Value: &downloaded},
			LastProgressAt:      models.NullableTimeUpdate{Set: true, Value: &progressedAt},
		})
	}
	if !stalled || ctx.Err() != nil {
		return
	}
	if err := s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause"); err != nil {
		s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, "stalled downloader could not be paused: "+err.Error())
		member.Status = models.CrossSeedPartialPoolMemberStatusManual
	}
}

func partialPoolProgressDecision(
	now time.Time,
	currentDownloaded int64,
	lastDownloaded *int64,
	lastProgressAt *time.Time,
	transferCapable bool,
) (downloaded int64, progressedAt time.Time, update, stalled bool) {
	if !transferCapable {
		return 0, time.Time{}, false, false
	}
	if lastDownloaded == nil || lastProgressAt == nil || currentDownloaded != *lastDownloaded {
		return currentDownloaded, now, true, false
	}
	return currentDownloaded, *lastProgressAt, false, !now.Before(lastProgressAt.Add(partialPoolStallWindow))
}

func partialPoolStalled(now time.Time, lastProgressAt *time.Time) bool {
	return lastProgressAt != nil && !now.Before(lastProgressAt.Add(partialPoolStallWindow))
}

func (s *Service) updatePartialPoolObservedFiles(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot, acquiring bool) {
	for _, file := range member.Files {
		current := snapshot.fileByIndex[file.FileIndex]
		if current.Priority == 0 {
			continue
		}
		if current.Progress >= 1 {
			switch file.Status {
			case models.CrossSeedPartialPoolFileStatusPresent:
				s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{})
			case models.CrossSeedPartialPoolFileStatusMissing, models.CrossSeedPartialPoolFileStatusAcquiring:
				s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusAvailable, models.PartialPoolFileMutation{})
			}
		} else if acquiring && file.Status == models.CrossSeedPartialPoolFileStatusMissing {
			empty := ""
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusAcquiring, models.PartialPoolFileMutation{LastError: &empty})
		}
	}
}

func (s *Service) resetPartialPoolAcquiringFiles(ctx context.Context, member *models.CrossSeedPartialPoolMember) {
	for _, file := range member.Files {
		if file.Status == models.CrossSeedPartialPoolFileStatusAcquiring {
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{})
		}
	}
}

func (s *Service) publishPartialPoolCompletedFiles(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) {
	for _, file := range member.Files {
		current := snapshot.fileByIndex[file.FileIndex]
		if current.Priority == 0 || current.Progress < 1 {
			continue
		}
		switch file.Status {
		case models.CrossSeedPartialPoolFileStatusPresent,
			models.CrossSeedPartialPoolFileStatusVerifying:
			if s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{}) {
				delete(s.partialPoolCreated, file.ID)
			}
		case models.CrossSeedPartialPoolFileStatusPropagating:
			if s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{}) {
				if s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{}) {
					delete(s.partialPoolCreated, file.ID)
				}
			}
		case models.CrossSeedPartialPoolFileStatusMissing, models.CrossSeedPartialPoolFileStatusAcquiring:
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusAvailable, models.PartialPoolFileMutation{})
		}
	}
}

func (s *Service) propagatePartialPoolFiles(ctx context.Context, now time.Time, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot) {
	if !s.partialPoolCoordinatorEnabled(ctx) {
		return
	}
	for _, targetMember := range pool.Members {
		if targetMember.Status != models.CrossSeedPartialPoolMemberStatusWaiting && targetMember.Status != models.CrossSeedPartialPoolMemberStatusBlocked && targetMember.Status != models.CrossSeedPartialPoolMemberStatusRechecking {
			continue
		}
		targetSnapshot := snapshots[targetMember.ID]
		if targetSnapshot == nil || len(targetSnapshot.files) == 0 || partialPoolChecking(targetSnapshot.torrent.State) {
			continue
		}
		if targetSnapshot.torrent.State == qbt.TorrentStateMissingFiles || targetSnapshot.torrent.State == qbt.TorrentStateError || partialPoolRecoveryPending(targetMember.LastError) {
			continue
		}
		if !isPausedOrStopped(targetSnapshot.torrent.State) {
			if targetMember.LastError != partialPoolPropagationPause {
				reason := partialPoolPropagationPause
				if !s.transitionPartialPoolMember(ctx, targetMember, targetMember.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
					continue
				}
			}
			if ctx.Err() == nil {
				if err := s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), targetMember.InstanceID, partialPoolMemberHashes(targetMember), "pause"); err != nil {
					s.markPartialPoolMemberManual(ctx, targetMember.ID, []string{targetMember.Status}, "propagation target could not be paused: "+err.Error())
					targetMember.Status = models.CrossSeedPartialPoolMemberStatusManual
				}
			}
			continue
		}
		if targetMember.LastError == partialPoolPropagationPause {
			empty := ""
			if !s.transitionPartialPoolMember(ctx, targetMember, targetMember.Status, models.PartialPoolMemberMutation{LastError: &empty}) {
				continue
			}
		}

		hasVerifying := false
		for _, targetFile := range targetMember.Files {
			if targetFile.Status == models.CrossSeedPartialPoolFileStatusPropagating {
				if s.finishPartialPoolPropagation(ctx, pool, targetMember, targetFile, snapshots) {
					hasVerifying = true
				}
				if targetMember.Status == models.CrossSeedPartialPoolMemberStatusManual {
					break
				}
			}
			if targetFile.Status == models.CrossSeedPartialPoolFileStatusVerifying {
				hasVerifying = true
			}
		}
		if targetMember.Status == models.CrossSeedPartialPoolMemberStatusManual {
			continue
		}
		if targetMember.Status == models.CrossSeedPartialPoolMemberStatusRechecking {
			if hasVerifying && targetMember.LastError != partialPoolRecheckPending && targetMember.LastError != partialPoolRecheckRequested {
				s.claimPartialPoolRecheck(ctx, now, targetMember)
			}
			continue
		}

		for _, targetFile := range targetMember.Files {
			if targetFile.Status != models.CrossSeedPartialPoolFileStatusMissing || targetFile.LastError != "" {
				continue
			}
			targetCurrent := targetSnapshot.fileByIndex[targetFile.FileIndex]
			if targetCurrent.Priority == 0 || targetCurrent.Progress > 0 || targetFile.SizeBytes <= 0 {
				continue
			}
			sourceFile := selectPartialPoolSourceFile(pool, targetMember, targetFile, snapshots)
			if sourceFile == nil {
				continue
			}
			if !s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
				SourceFileID: models.NullableInt64Update{Set: true, Value: &sourceFile.ID},
			}) {
				continue
			}
			if s.finishPartialPoolPropagation(ctx, pool, targetMember, targetFile, snapshots) {
				hasVerifying = true
			}
			if targetMember.Status == models.CrossSeedPartialPoolMemberStatusManual {
				break
			}
		}
		if targetMember.Status == models.CrossSeedPartialPoolMemberStatusManual {
			continue
		}
		if hasVerifying {
			s.claimPartialPoolRecheck(ctx, now, targetMember)
		}
	}
}

func selectPartialPoolSourceFile(
	pool *models.CrossSeedPartialPool,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
	snapshots map[int64]*partialPoolMemberSnapshot,
) *models.CrossSeedPartialPoolMemberFile {
	for _, sourceMember := range pool.Members {
		if sourceMember.ID == targetMember.ID || sourceMember.Status == models.CrossSeedPartialPoolMemberStatusManual || sourceMember.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		sourceSnapshot := snapshots[sourceMember.ID]
		if sourceSnapshot == nil || len(sourceSnapshot.files) == 0 ||
			partialPoolChecking(sourceSnapshot.torrent.State) ||
			sourceSnapshot.torrent.State == qbt.TorrentStateError ||
			sourceSnapshot.torrent.State == qbt.TorrentStateMissingFiles {
			continue
		}
		for _, sourceFile := range sourceMember.Files {
			if sourceFile.Status != models.CrossSeedPartialPoolFileStatusAvailable && sourceFile.Status != models.CrossSeedPartialPoolFileStatusVerified {
				continue
			}
			current := sourceSnapshot.fileByIndex[sourceFile.FileIndex]
			if current.Priority > 0 && current.Progress >= 1 && partialPoolFilesPair(sourceMember, targetMember, sourceFile, targetFile) {
				return sourceFile
			}
		}
	}
	return nil
}

func (s *Service) finishPartialPoolPropagation(
	ctx context.Context,
	pool *models.CrossSeedPartialPool,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
	snapshots map[int64]*partialPoolMemberSnapshot,
) bool {
	if ctx.Err() != nil || targetFile.SourceFileID == nil {
		return false
	}
	sourceMember, sourceFile := partialPoolFileByID(pool, *targetFile.SourceFileID)
	if sourceMember == nil || sourceFile == nil {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "propagation source no longer exists")
		return false
	}
	sourceSnapshot := snapshots[sourceMember.ID]
	targetSnapshot := snapshots[targetMember.ID]
	if sourceSnapshot == nil || targetSnapshot == nil || len(sourceSnapshot.files) == 0 || len(targetSnapshot.files) == 0 {
		return false
	}
	if !s.partialPoolCoordinatorEnabled(ctx) ||
		!s.refreshPartialPoolMemberSnapshot(ctx, sourceMember, sourceSnapshot) ||
		!s.refreshPartialPoolMemberSnapshot(ctx, targetMember, targetSnapshot) {
		return false
	}
	sourceCurrent := sourceSnapshot.fileByIndex[sourceFile.FileIndex]
	targetCurrent := targetSnapshot.fileByIndex[targetFile.FileIndex]
	if sourceCurrent.Priority == 0 || targetCurrent.Priority == 0 {
		s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{SourceFileID: models.NullableInt64Update{Set: true}})
		return false
	}
	if sourceCurrent.Progress < 1 {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "propagation source is no longer complete")
		return false
	}
	if !partialPoolFilesPair(sourceMember, targetMember, sourceFile, targetFile) {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "persisted source and target no longer satisfy file pairing")
		return false
	}
	if !s.partialPoolMemberModeEnabled(ctx, sourceMember) || !s.partialPoolMemberModeEnabled(ctx, targetMember) {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "link mode or local filesystem access was disabled")
		return false
	}

	sourcePath, err := partialPoolLocalPath(sourceMember, sourceFile)
	if err != nil {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "unsafe source path: "+err.Error())
		return false
	}
	targetPath, err := partialPoolLocalPath(targetMember, targetFile)
	if err != nil {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "unsafe target path: "+err.Error())
		return false
	}
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() != sourceFile.SizeBytes {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "propagation source is missing, moved, or has the wrong size")
		return false
	}

	if targetInfo, statErr := os.Lstat(targetPath); statErr == nil {
		if !targetInfo.Mode().IsRegular() {
			s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "a non-regular target already exists")
			return false
		}
		if targetMember.Mode == models.CrossSeedPartialPoolModeHardlink && !partialPoolSameFile(sourcePath, targetPath) {
			s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "a different target file already exists")
			return false
		}
		s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
		return true
	} else if !os.IsNotExist(statErr) {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "target path could not be inspected: "+statErr.Error())
		return false
	}

	plan, err := hardlinktree.BuildSingleFilePlan(targetMember.RootPath, targetFile.RelativePath, sourcePath)
	if err != nil {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, err.Error())
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	var created *hardlinktree.Created
	if targetMember.Mode == models.CrossSeedPartialPoolModeHardlink {
		created, err = hardlinktree.Create(plan)
	} else {
		created, err = reflinktree.Create(plan)
	}
	if err != nil {
		s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, "file propagation failed: "+err.Error())
		return false
	}
	if targetMember.Mode == models.CrossSeedPartialPoolModeHardlink {
		if s.partialPoolCreated == nil {
			s.partialPoolCreated = make(map[int64]*hardlinktree.Created)
		}
		s.partialPoolCreated[targetFile.ID] = created
	}
	s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	return true
}

func (s *Service) partialPoolMemberModeEnabled(ctx context.Context, member *models.CrossSeedPartialPoolMember) bool {
	if member == nil || s.instanceStore == nil {
		return false
	}
	instance, err := s.instanceStore.Get(ctx, member.InstanceID)
	if err != nil || instance == nil || !instance.HasLocalFilesystemAccess {
		return false
	}
	if member.Mode == models.CrossSeedPartialPoolModeHardlink {
		return instance.UseHardlinks
	}
	return member.Mode == models.CrossSeedPartialPoolModeReflink && instance.UseReflinks
}

func (s *Service) markPartialPoolPropagationManual(ctx context.Context, member *models.CrossSeedPartialPoolMember, file *models.CrossSeedPartialPoolMemberFile, reason string) {
	s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusManual, models.PartialPoolFileMutation{LastError: &reason})
	delete(s.partialPoolCreated, file.ID)
	s.markPartialPoolMemberManual(ctx, member.ID, []string{member.Status}, reason)
	member.Status = models.CrossSeedPartialPoolMemberStatusManual
}

func (s *Service) claimPartialPoolRecheck(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember) {
	reason := partialPoolRecheckPending
	switch member.Status {
	case models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolMemberStatusBlocked:
		if !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusRechecking, models.PartialPoolMemberMutation{LastError: &reason}) {
			return
		}
	case models.CrossSeedPartialPoolMemberStatusRechecking:
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
	default:
		return
	}
	s.requestPartialPoolRecheck(ctx, now, member)
}

type partialPoolSelection struct {
	member        *models.CrossSeedPartialPoolMember
	reusableBytes int64
	unlocked      int
	amountLeft    int64
}

func selectPartialPoolDownloader(pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot, now time.Time) *models.CrossSeedPartialPoolMember {
	if pool == nil {
		return nil
	}
	for _, member := range pool.Members {
		snapshot := snapshots[member.ID]
		if snapshot == nil {
			continue
		}
		if member.Status == models.CrossSeedPartialPoolMemberStatusAcquiring || (member.Status != models.CrossSeedPartialPoolMemberStatusComplete && partialPoolTransferCapable(snapshot.torrent.State)) {
			return nil
		}
	}

	var selections []partialPoolSelection
	for _, member := range pool.Members {
		if member.Status != models.CrossSeedPartialPoolMemberStatusWaiting || !partialPoolCooldownReady(member, now) {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil || len(snapshot.files) == 0 || !partialPoolMemberResumable(member, snapshot.torrent.State) {
			continue
		}
		missing := partialPoolMissingWantedFiles(member, snapshot)
		if len(missing) == 0 {
			continue
		}
		selection := partialPoolSelection{member: member, amountLeft: snapshot.torrent.AmountLeft}
		for _, file := range missing {
			if partialPoolFilePairsAnotherMissing(pool, member, file, snapshots) {
				selection.reusableBytes += file.SizeBytes
			}
		}
		for _, other := range pool.Members {
			if other.ID == member.ID || other.Status == models.CrossSeedPartialPoolMemberStatusManual || other.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
				continue
			}
			otherSnapshot := snapshots[other.ID]
			otherMissing := partialPoolMissingWantedFiles(other, otherSnapshot)
			if len(otherMissing) > 0 && partialPoolFilesUnlockMember(member, missing, other, otherMissing) {
				selection.unlocked++
			}
		}
		selections = append(selections, selection)
	}
	if len(selections) == 0 {
		return nil
	}
	sort.Slice(selections, func(i, j int) bool {
		a, b := selections[i], selections[j]
		if a.reusableBytes != b.reusableBytes {
			return a.reusableBytes > b.reusableBytes
		}
		if a.unlocked != b.unlocked {
			return a.unlocked > b.unlocked
		}
		if a.amountLeft != b.amountLeft {
			return a.amountLeft < b.amountLeft
		}
		if a.member.ReportedSeeders != b.member.ReportedSeeders {
			return a.member.ReportedSeeders > b.member.ReportedSeeders
		}
		if a.member.InstanceID != b.member.InstanceID {
			return a.member.InstanceID < b.member.InstanceID
		}
		return strings.ToLower(a.member.TorrentKey) < strings.ToLower(b.member.TorrentKey)
	})
	return selections[0].member
}

func partialPoolCooldownReady(member *models.CrossSeedPartialPoolMember, now time.Time) bool {
	return member != nil && (member.RetryAfter == nil || !now.Before(*member.RetryAfter))
}

func partialPoolMemberResumable(member *models.CrossSeedPartialPoolMember, state qbt.TorrentState) bool {
	return isPausedOrStopped(state) || member != nil && member.Mode == models.CrossSeedPartialPoolModeReflink && state == qbt.TorrentStateMissingFiles
}

func partialPoolMissingWantedFiles(member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) []*models.CrossSeedPartialPoolMemberFile {
	if member == nil || snapshot == nil || len(snapshot.files) == 0 {
		return nil
	}
	var missing []*models.CrossSeedPartialPoolMemberFile
	for _, file := range member.Files {
		current, ok := snapshot.fileByIndex[file.FileIndex]
		if !ok || current.Priority == 0 || current.Progress >= 1 || file.Status == models.CrossSeedPartialPoolFileStatusManual {
			continue
		}
		missing = append(missing, file)
	}
	return missing
}

func partialPoolFilePairsAnotherMissing(pool *models.CrossSeedPartialPool, member *models.CrossSeedPartialPoolMember, file *models.CrossSeedPartialPoolMemberFile, snapshots map[int64]*partialPoolMemberSnapshot) bool {
	for _, other := range pool.Members {
		if other.ID == member.ID || other.Status == models.CrossSeedPartialPoolMemberStatusManual || other.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		for _, otherFile := range partialPoolMissingWantedFiles(other, snapshots[other.ID]) {
			if partialPoolFilesPair(member, other, file, otherFile) {
				return true
			}
		}
	}
	return false
}

func partialPoolFilesUnlockMember(sourceMember *models.CrossSeedPartialPoolMember, sourceFiles []*models.CrossSeedPartialPoolMemberFile, targetMember *models.CrossSeedPartialPoolMember, targetFiles []*models.CrossSeedPartialPoolMemberFile) bool {
	for _, targetFile := range targetFiles {
		matched := false
		for _, sourceFile := range sourceFiles {
			if partialPoolFilesPair(sourceMember, targetMember, sourceFile, targetFile) {
				matched = true
				break
			}
		}
		if !matched {
			return false
		}
	}
	return len(targetFiles) > 0
}

func (s *Service) selectAndResumePartialPoolDownloader(ctx context.Context, now time.Time, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot, budget int64) {
	member := selectPartialPoolDownloader(pool, snapshots, now)
	if member == nil || ctx.Err() != nil {
		return
	}
	snapshot := snapshots[member.ID]
	status, reason := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	if status != models.CrossSeedPartialPoolMemberStatusWaiting {
		if status != member.Status {
			empty := ""
			s.transitionPartialPoolMember(ctx, member, status, models.PartialPoolMemberMutation{LastError: choosePartialPoolError(status, reason, &empty)})
		}
		return
	}
	if !s.partialPoolMemberModeEnabled(ctx, member) {
		s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusWaiting}, "link mode or local filesystem access was disabled")
		member.Status = models.CrossSeedPartialPoolMemberStatusManual
		return
	}
	claimed, err := s.automationStore.ClaimPartialPoolDownloader(ctx, member.ID, snapshot.torrent.Downloaded, now)
	if err != nil || !claimed {
		return
	}
	member.Status = models.CrossSeedPartialPoolMemberStatusAcquiring
	member.StartedByPool = true
	member.LastDownloadedBytes = &snapshot.torrent.Downloaded
	member.LastProgressAt = &now
	member.RetryAfter = nil
	member.LastError = ""
	settings, settingsErr := s.GetAutomationSettings(ctx)
	if settingsErr != nil || settings == nil || !settings.PooledPartialCompletionEnabled || !s.refreshPartialPoolMemberSnapshot(ctx, member, snapshot) {
		s.releasePartialPoolDownloaderClaim(ctx, member, "pooled completion setting or current file evidence changed before resume")
		return
	}
	currentBudget := int64(max(settings.AutoResumeMaxDownloadMB, 0)) << 20
	status, reason = partialPoolPostRecheckVerdict(member, snapshot, currentBudget, normalizerForService(s))
	if status != models.CrossSeedPartialPoolMemberStatusWaiting {
		if status == models.CrossSeedPartialPoolMemberStatusManual {
			s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, reason)
			member.Status = models.CrossSeedPartialPoolMemberStatusManual
		} else {
			s.releasePartialPoolDownloaderClaim(ctx, member, reason)
		}
		return
	}
	if !s.partialPoolMemberModeEnabled(ctx, member) {
		s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, "link mode or local filesystem access was disabled")
		member.Status = models.CrossSeedPartialPoolMemberStatusManual
		return
	}
	for _, file := range partialPoolMissingWantedFiles(member, snapshot) {
		if file.Status == models.CrossSeedPartialPoolFileStatusMissing {
			empty := ""
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusAcquiring, models.PartialPoolFileMutation{LastError: &empty})
		}
	}
	if ctx.Err() != nil {
		return
	}
	s.requestPartialPoolResume(ctx, member)
	if strings.HasPrefix(member.LastError, partialPoolResumeExhausted) {
		s.markPartialPoolMemberManual(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, member.LastError)
		member.Status = models.CrossSeedPartialPoolMemberStatusManual
	}
}

func (s *Service) releasePartialPoolDownloaderClaim(ctx context.Context, member *models.CrossSeedPartialPoolMember, reason string) {
	stopped := false
	s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{StartedByPool: &stopped, LastError: &reason})
	s.resetPartialPoolAcquiringFiles(ctx, member)
}

func partialPoolMemberHashes(member *models.CrossSeedPartialPoolMember) []string {
	if member == nil {
		return nil
	}
	return normalizedHashes(member.TorrentKey, member.InfoHashV1, member.InfoHashV2)
}

func partialPoolMemberHasVerificationWork(member *models.CrossSeedPartialPoolMember) bool {
	if member == nil {
		return false
	}
	for _, file := range member.Files {
		if file.Status == models.CrossSeedPartialPoolFileStatusPropagating || file.Status == models.CrossSeedPartialPoolFileStatusVerifying {
			return true
		}
	}
	return false
}

func partialPoolFileByID(pool *models.CrossSeedPartialPool, id int64) (*models.CrossSeedPartialPoolMember, *models.CrossSeedPartialPoolMemberFile) {
	if pool == nil {
		return nil, nil
	}
	for _, member := range pool.Members {
		for _, file := range member.Files {
			if file.ID == id {
				return member, file
			}
		}
	}
	return nil, nil
}

func partialPoolMemberForFile(pool *models.CrossSeedPartialPool, file *models.CrossSeedPartialPoolMemberFile) *models.CrossSeedPartialPoolMember {
	if pool == nil || file == nil {
		return nil
	}
	for _, member := range pool.Members {
		if member.ID == file.MemberID {
			return member
		}
	}
	return nil
}

func (s *Service) transitionPartialPoolMember(ctx context.Context, member *models.CrossSeedPartialPoolMember, status string, mutation models.PartialPoolMemberMutation) bool {
	if member == nil || ctx.Err() != nil {
		return false
	}
	changed, err := s.automationStore.TransitionPartialPoolMember(ctx, member.ID, []string{member.Status}, status, mutation)
	if err != nil || !changed {
		return false
	}
	member.Status = status
	if status == models.CrossSeedPartialPoolMemberStatusManual || status == models.CrossSeedPartialPoolMemberStatusComplete || status == models.CrossSeedPartialPoolMemberStatusRemoved {
		member.StartedByPool = false
	}
	if mutation.MissingBytes != nil {
		member.MissingBytes = *mutation.MissingBytes
	}
	if mutation.StartedByPool != nil {
		member.StartedByPool = *mutation.StartedByPool
	}
	if mutation.LastDownloadedBytes.Set {
		member.LastDownloadedBytes = mutation.LastDownloadedBytes.Value
	}
	if mutation.LastProgressAt.Set {
		member.LastProgressAt = mutation.LastProgressAt.Value
	}
	if mutation.RetryAfter.Set {
		member.RetryAfter = mutation.RetryAfter.Value
	}
	if mutation.LastError != nil {
		member.LastError = *mutation.LastError
	}
	return true
}

func (s *Service) transitionPartialPoolFile(ctx context.Context, file *models.CrossSeedPartialPoolMemberFile, status string, mutation models.PartialPoolFileMutation) bool {
	if file == nil || ctx.Err() != nil {
		return false
	}
	changed, err := s.automationStore.TransitionPartialPoolFile(ctx, file.ID, []string{file.Status}, status, mutation)
	if err != nil || !changed {
		return false
	}
	file.Status = status
	if mutation.SourceFileID.Set {
		file.SourceFileID = mutation.SourceFileID.Value
	}
	if mutation.LastError != nil {
		file.LastError = *mutation.LastError
	}
	return true
}
