// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"syscall"
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
	partialPoolActiveRecoveryInterval = 10 * time.Second
	partialPoolSettledAuditInterval   = 10 * time.Minute
	partialPoolRecheckPollInterval    = 100 * time.Millisecond
	partialPoolRecheckPollSyncTimeout = 5 * time.Second
	partialPoolRecheckObserveTimeout  = 30 * time.Second
	partialPoolStallWindow            = 15 * time.Minute
	partialPoolCooldown               = 30 * time.Minute
	partialPoolRecheckGrace           = 20 * time.Second
	partialPoolAdmissionHold          = 2 * time.Second

	partialPoolRecheckPending    = models.CrossSeedPartialPoolRecheckPending
	partialPoolRecheckRequested  = models.CrossSeedPartialPoolRecheckRequested
	partialPoolRecheckObserved   = "partial pool recheck observed"
	partialPoolRecheckUnobserved = "piece-check start was not observed after the recheck request"
	partialPoolPropagationPause  = "partial pool propagation pause pending"
	partialPoolBudgetPause       = "partial pool budget pause pending"
	partialPoolModePause         = "partial pool mode pause pending"
	partialPoolSafetyPause       = "partial pool safety pause pending"
	partialPoolResumeExhausted   = "partial pool resume attempts exhausted"
	partialPoolRecoveryExhausted = "partial pool error recovery attempts exhausted"
	partialPoolRecoveryLimit     = 3

	partialPoolReflinkStagingPrefix    = ".qui-partial-pool-reflink-"
	partialPoolReflinkStagingOwner     = ".owner-v1"
	partialPoolReflinkStagingOwnerData = "qui partial pool reflink staging v1\n"
	partialPoolReflinkStagingClone     = "clone"
	partialPoolReflinkProbeSource      = ".reflink_probe_src_"
	partialPoolReflinkProbeDestination = ".reflink_probe_dst_"
)

type partialPoolWake struct {
	poolID     int64
	instanceID int
	hash       string
}

// partialPoolPropagationPair identifies one source/target file pairing for
// the current root paths. Including the roots lets a later path change retry
// the same durable file IDs.
type partialPoolPropagationPair struct {
	sourceFileID int64
	targetFileID int64
	sourceRoot   string
	targetRoot   string
}

// partialPoolPropagationPairKey returns the process-local rejection key for a
// claimed source and target.
func partialPoolPropagationPairKey(
	sourceMember *models.CrossSeedPartialPoolMember,
	sourceFile *models.CrossSeedPartialPoolMemberFile,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
) partialPoolPropagationPair {
	return partialPoolPropagationPair{
		sourceFileID: sourceFile.ID,
		targetFileID: targetFile.ID,
		sourceRoot:   sourceMember.RootPath,
		targetRoot:   targetMember.RootPath,
	}
}

// rejectPartialPoolPropagationPair prevents a known cross-filesystem pairing
// from being selected again during this service process.
func (s *Service) rejectPartialPoolPropagationPair(
	sourceMember *models.CrossSeedPartialPoolMember,
	sourceFile *models.CrossSeedPartialPoolMemberFile,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
) {
	s.partialPoolRejectedPairs.Store(partialPoolPropagationPairKey(sourceMember, sourceFile, targetMember, targetFile), struct{}{})
}

// partialPoolPropagationPairRejected reports whether this process already
// observed a cross-filesystem failure for the current file roots.
func (s *Service) partialPoolPropagationPairRejected(
	sourceMember *models.CrossSeedPartialPoolMember,
	sourceFile *models.CrossSeedPartialPoolMemberFile,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
) bool {
	_, rejected := s.partialPoolRejectedPairs.Load(partialPoolPropagationPairKey(sourceMember, sourceFile, targetMember, targetFile))
	return rejected
}

// partialPoolPropagationPairIncompatible reports errors that prove only this
// source and target cannot be linked across their filesystems.
func partialPoolPropagationPairIncompatible(err error) bool {
	return errors.Is(err, syscall.EXDEV) || partialPoolPlatformCrossDeviceError(err)
}

func (s *Service) signalPartialPoolWake(wake partialPoolWake) {
	if s == nil || s.partialPoolWake == nil {
		return
	}
	select {
	case s.partialPoolWake <- wake:
	default:
		// A queued wake will release the coordinator. Preserve the dropped
		// scope as one coalesced full scan so dormant pools cannot be missed.
		s.partialPoolFullScanPending.Store(true)
	}
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

// registerPartialPoolAdmission loads qBittorrent's authoritative file list,
// joins it with the parsed and materialized file evidence, and atomically
// persists the pool member before any recheck is requested. Added torrent IDs
// are lookup hints only; metainfo hashes remain the durable member identity.
func (s *Service) registerPartialPoolAdmission(
	ctx context.Context,
	candidate CrossSeedCandidate,
	torrentBytes []byte,
	addedTorrentIDs []string,
	req *CrossSeedRequest,
	matchedTorrent *qbt.Torrent,
	mode, rootPath string,
	materializedFiles []hardlinktree.TorrentFile,
	replaceablePaths map[string]struct{},
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
	fetchHash := ""
	if responseIDs := normalizedHashes(addedTorrentIDs...); len(responseIDs) > 0 {
		fetchHash = responseIDs[0]
	} else {
		addedTorrent, found, resolveErr := s.syncManager.HasTorrentByAnyHash(ctx, candidate.InstanceID, memberAliases)
		if resolveErr != nil {
			return nil, nil, fmt.Errorf("resolve added partial pool torrent: %w", resolveErr)
		}
		if found && addedTorrent != nil {
			fetchHash = normalizeHash(addedTorrent.Hash)
			if fetchHash == "" {
				fetchHash = partialPoolCanonicalTorrentKey(addedTorrent)
			}
		}
	}
	if fetchHash == "" {
		fetchHash = memberKey
	}
	refreshCtx := qbittorrent.WithPostAddFileFetchRetry(qbittorrent.WithForceFilesRefresh(ctx))
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
	for i := range fileRows {
		_, fileRows[i].ReplaceableAtAdd = replaceablePaths[fileRows[i].RelativePath]
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

	s.partialPoolAdmissionMaterializationMu.Lock()
	defer s.partialPoolAdmissionMaterializationMu.Unlock()
	return s.automationStore.RegisterPartialPoolMember(ctx, models.CrossSeedPartialPoolRegistration{
		SourceInstanceID:  sourceInstanceID,
		SourceTorrentKey:  sourceKey,
		SourceAliases:     sourceAliases,
		MatchedInstanceID: candidate.InstanceID,
		MatchedTorrentKey: matchedKey,
		MatchedAliases:    matchedAliases,
		Member: models.CrossSeedPartialPoolMember{
			InstanceID:   candidate.InstanceID,
			TorrentKey:   memberKey,
			InfoHashV1:   infohashV1,
			InfoHashV2:   infohashV2,
			Mode:         mode,
			RootPath:     rootPath,
			Status:       models.CrossSeedPartialPoolMemberStatusVerifying,
			MissingBytes: missingBytes,
			LastError:    partialPoolRecheckPending,
		},
		Files: fileRows,
	})
}

// recordPartialPoolRecheckRequested durably claims a member's recheck before the
// qBittorrent side effect. It returns false when the claim was not persisted.
func (s *Service) recordPartialPoolRecheckRequested(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember) bool {
	if s == nil || s.automationStore == nil || member == nil {
		return false
	}
	reason := partialPoolRecheckRequested
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
		return false
	}
	member.UpdatedAt = now
	log.Debug().
		Int64("poolID", member.PoolID).
		Int64("memberID", member.ID).
		Str("memberStatus", member.Status).
		Msg("Partial pool recheck observed")
	return true
}

// recordPartialPoolRecheckObserved persists that qBittorrent reported an
// actual piece-checking state for this verification-owned member.
func (s *Service) recordPartialPoolRecheckObserved(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember) bool {
	if s == nil || s.automationStore == nil || member == nil {
		return false
	}
	reason := partialPoolRecheckObserved
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
		RecoveryAttempts: models.NullableInt64Update{Set: true},
		LastError:        &reason,
	}) {
		return false
	}
	member.UpdatedAt = now
	return true
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

// markPartialPoolMemberManual reports whether the manual transition was
// durably persisted and updates the in-memory member only after that success.
func (s *Service) markPartialPoolMemberManual(ctx context.Context, member *models.CrossSeedPartialPoolMember, reason string) bool {
	return s.markPartialPoolMemberManualWithPause(ctx, member, reason, false)
}

func (s *Service) markPartialPoolMemberManualWithPause(ctx context.Context, member *models.CrossSeedPartialPoolMember, reason string, pending bool) bool {
	if s == nil || s.automationStore == nil || member == nil {
		return false
	}
	reason = strings.TrimSpace(reason)
	return s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{
		ReviewPausePending: &pending,
		ResumeAttempts:     models.NullableInt64Update{Set: true},
		RecoveryAttempts:   models.NullableInt64Update{Set: true},
		LastError:          &reason,
	})
}

// pausePartialPoolMemberForReview persists manual review immediately but only
// reports settlement after a later inventory observes the torrent stopped.
func (s *Service) pausePartialPoolMemberForReview(ctx context.Context, member *models.CrossSeedPartialPoolMember, torrent qbt.Torrent, reason string) bool {
	if s == nil || member == nil || ctx.Err() != nil {
		return false
	}
	if pendingReason, pending := partialPoolReviewPauseReason(member); pending {
		reason = pendingReason
	}
	settled := isPausedOrStopped(torrent.State) || torrent.State == qbt.TorrentStateError || torrent.State == qbt.TorrentStateMissingFiles
	if !settled {
		_ = s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause")
	}
	reason = strings.TrimSpace(reason)
	pending := !settled
	if member.Status == models.CrossSeedPartialPoolMemberStatusManual && member.LastError == reason && member.ReviewPausePending == pending {
		return settled
	}
	persisted := s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{
		ReviewPausePending: &pending,
		LastError:          &reason,
	})
	return persisted && settled
}

// partialPoolReviewPauseReason returns the final manual-review reason while a
// durable pause request still awaits a non-transferring torrent observation.
func partialPoolReviewPauseReason(member *models.CrossSeedPartialPoolMember) (string, bool) {
	if member == nil || member.Status != models.CrossSeedPartialPoolMemberStatusManual {
		return "", false
	}
	reason := strings.TrimSpace(member.LastError)
	return reason, member.ReviewPausePending && reason != ""
}

// reconcilePartialPoolReviewPauses retries durable manual-review pauses before
// file evidence can short-circuit the pass.
func (s *Service) reconcilePartialPoolReviewPauses(ctx context.Context, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot) {
	for _, member := range pool.Members {
		reason, pending := partialPoolReviewPauseReason(member)
		if !pending {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil {
			continue
		}
		snapshot.reviewPauseHandled = true
		snapshot.stateRetryPending = !s.pausePartialPoolMemberForReview(ctx, member, snapshot.torrent, reason)
	}
}

type partialPoolTorrentInventory struct {
	loaded        bool
	authoritative bool
	byAlias       map[string]qbt.Torrent
}

type partialPoolMemberSnapshot struct {
	torrent            qbt.Torrent
	files              qbt.TorrentFiles
	fileByIndex        map[int]qbt.TorrentFile
	stateRetryPending  bool
	reviewPauseHandled bool
}

// RunPartialPoolCoordinator reconciles durable partial completion work until
// ctx is canceled. It owns no child goroutines.
func (s *Service) RunPartialPoolCoordinator(ctx context.Context) {
	if s == nil || s.automationStore == nil || s.syncManager == nil {
		return
	}

	startedAt := time.Now()
	pendingObservation, nextAdmission, retryFullScan := s.reconcilePartialPoolsScheduled(ctx, startedAt, partialPoolWake{}, true)
	nextActiveRecovery := startedAt.Add(partialPoolActiveRecoveryInterval)
	nextSettledAudit := startedAt.Add(partialPoolSettledAuditInterval)
	reconcileTimer := time.NewTimer(partialPoolNextCoordinatorDelay(time.Now(), nextActiveRecovery, nextAdmission, nextSettledAudit))
	defer reconcileTimer.Stop()
	observationTimer := time.NewTimer(partialPoolRecheckPollInterval)
	if !observationTimer.Stop() {
		<-observationTimer.C
	}
	defer observationTimer.Stop()
	var observationC <-chan time.Time
	scheduleObservation := func(pending bool) {
		if !observationTimer.Stop() {
			select {
			case <-observationTimer.C:
			default:
			}
		}
		observationC = nil
		if pending {
			observationTimer.Reset(partialPoolRecheckPollInterval)
			observationC = observationTimer.C
		}
	}

	scheduleObservation(pendingObservation)
	for {
		select {
		case <-ctx.Done():
			return
		case wake := <-s.partialPoolWake:
			now := time.Now()
			globalWake := wake.poolID <= 0 && (wake.instanceID <= 0 || wake.hash == "")
			scanAll := globalWake || retryFullScan || s.partialPoolFullScanPending.Swap(false)
			pending, wakeAdmission, retry := s.reconcilePartialPoolsScheduled(ctx, now, wake, scanAll)
			retryFullScan = retry
			if pending {
				scheduleObservation(true)
			}
			if !wakeAdmission.IsZero() && (nextAdmission.IsZero() || wakeAdmission.Before(nextAdmission)) {
				nextAdmission = wakeAdmission
			}
			resetPartialPoolCoordinatorTimer(reconcileTimer, time.Now(), nextActiveRecovery, nextAdmission, nextSettledAudit)
		case now := <-reconcileTimer.C:
			auditDue := !now.Before(nextSettledAudit)
			recoveryDue := !now.Before(nextActiveRecovery)
			scanAll := auditDue || retryFullScan || s.partialPoolFullScanPending.Swap(false)
			pending, admission, retry := s.reconcilePartialPoolsScheduled(ctx, now, partialPoolWake{}, scanAll)
			retryFullScan = retry
			scheduleObservation(pending)
			nextAdmission = admission
			if recoveryDue {
				nextActiveRecovery = now.Add(partialPoolActiveRecoveryInterval)
			}
			if auditDue {
				nextSettledAudit = now.Add(partialPoolSettledAuditInterval)
			}
			resetPartialPoolCoordinatorTimer(reconcileTimer, time.Now(), nextActiveRecovery, nextAdmission, nextSettledAudit)
		case now := <-observationC:
			observationC = nil
			scheduleObservation(s.observeRequestedPartialPoolRechecks(ctx, now))
		}
	}
}

func (s *Service) reconcilePartialPools(ctx context.Context, now time.Time, wake partialPoolWake) bool {
	globalWake := wake.poolID <= 0 && (wake.instanceID <= 0 || wake.hash == "")
	pending, _, _ := s.reconcilePartialPoolsScheduled(ctx, now, wake, globalWake)
	return pending
}

// reconcilePartialPoolsScheduled returns whether recheck observation is
// pending, the next persisted admission deadline, and whether all pools need
// another recovery pass.
func (s *Service) reconcilePartialPoolsScheduled(ctx context.Context, now time.Time, wake partialPoolWake, scanAll bool) (bool, time.Time, bool) {
	if ctx.Err() != nil {
		return false, time.Time{}, false
	}
	var pools []*models.CrossSeedPartialPool
	retryFullScan := false
	if !scanAll && wake.instanceID > 0 && wake.hash != "" {
		pool, _, err := s.automationStore.ResolvePartialPoolMember(ctx, wake.instanceID, wake.hash)
		if err != nil && ctx.Err() == nil {
			log.Debug().Err(err).Int("instanceID", wake.instanceID).Msg("Partial pool completion wake did not resolve a member")
			retryFullScan = true
		} else if pool != nil {
			pools = []*models.CrossSeedPartialPool{pool}
		}
	} else if !scanAll && wake.poolID > 0 {
		if err := s.automationStore.SetPartialPoolStatus(ctx, wake.poolID, models.CrossSeedPartialPoolStatusActive); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Int64("poolID", wake.poolID).Msg("Failed to activate partial completion pool")
			retryFullScan = true
		} else {
			pool, err := s.automationStore.GetPartialPool(ctx, wake.poolID)
			if err != nil {
				if ctx.Err() == nil {
					log.Warn().Err(err).Int64("poolID", wake.poolID).Msg("Failed to load targeted partial completion pool")
					retryFullScan = true
				}
			} else {
				pools = []*models.CrossSeedPartialPool{pool}
			}
		}
	}
	if ctx.Err() != nil {
		return false, time.Time{}, false
	}

	settings, err := s.GetAutomationSettings(ctx)
	if err != nil || settings == nil {
		if err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Msg("Failed to load partial completion settings")
		}
		return false, time.Time{}, retryFullScan || scanAll
	}
	if scanAll || wake.instanceID <= 0 && wake.poolID <= 0 {
		if scanAll {
			pools, err = s.automationStore.ListPartialPoolsForReconciliation(ctx)
		} else {
			pools, err = s.automationStore.ListActivePartialPoolsForReconciliation(ctx)
		}
		if err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Msg("Failed to list partial completion pools")
			}
			return false, time.Time{}, retryFullScan || scanAll
		}
	}
	nextAdmission := partialPoolNextAdmissionDeadline(pools, now)
	if len(pools) == 0 {
		_ = s.automationStore.PruneEmptyPartialPools(ctx)
		return false, nextAdmission, retryFullScan
	}

	inventories := s.loadPartialPoolTorrentInventories(ctx, pools)
	pendingObservation := false
	keepPoolActive := func(pool *models.CrossSeedPartialPool) {
		if scanAll {
			retryFullScan = true
		}
		if pool.Status == models.CrossSeedPartialPoolStatusActive {
			return
		}
		err := s.automationStore.SetPartialPoolStatus(ctx, pool.ID, models.CrossSeedPartialPoolStatusActive)
		if err != nil {
			if ctx.Err() == nil {
				log.Warn().Err(err).Int64("poolID", pool.ID).Msg("Failed to retain partial completion pool recovery schedule")
				retryFullScan = true
			}
			return
		}
		pool.Status = models.CrossSeedPartialPoolStatusActive
	}
	for _, pool := range pools {
		if ctx.Err() != nil {
			return false, nextAdmission, false
		}
		if pool.Status != models.CrossSeedPartialPoolStatusActive {
			if err := s.automationStore.SetPartialPoolStatus(ctx, pool.ID, models.CrossSeedPartialPoolStatusActive); err != nil {
				if ctx.Err() == nil {
					log.Warn().Err(err).Int64("poolID", pool.ID).Msg("Failed to schedule partial completion pool before reconciliation")
					retryFullScan = true
				}
				continue
			}
			pool.Status = models.CrossSeedPartialPoolStatusActive
		}
		evidenceComplete := partialPoolInventoriesComplete(pool, inventories)
		observed := s.observePartialPoolMembers(ctx, now, pool, inventories)
		if len(observed) == 0 {
			if !evidenceComplete {
				keepPoolActive(pool)
			}
			pendingObservation = pendingObservation || partialPoolRecheckObservationPending(pool)
			continue
		}
		pool, err = s.automationStore.GetPartialPool(ctx, pool.ID)
		if err != nil {
			if scanAll && ctx.Err() == nil {
				retryFullScan = true
			}
			pendingObservation = pendingObservation || partialPoolRecheckObservationPending(pool)
			continue
		}
		evidenceComplete = partialPoolInventoriesComplete(pool, inventories)
		snapshots := make(map[int64]*partialPoolMemberSnapshot, len(pool.Members))
		for _, member := range pool.Members {
			if torrent, ok := partialPoolInventoryTorrent(inventories[member.InstanceID], member); ok {
				snapshots[member.ID] = &partialPoolMemberSnapshot{torrent: torrent}
			}
		}

		if !settings.PooledPartialCompletionEnabled {
			pending, disableErr := s.reconcileDisabledPartialPool(ctx, pool, snapshots)
			pendingObservation = pendingObservation || pending
			if disableErr != nil && ctx.Err() == nil {
				log.Warn().Err(disableErr).Int64("poolID", pool.ID).Msg("Failed to stop disabled partial completion pool")
				retryFullScan = true
			}
			if pending || !evidenceComplete {
				keepPoolActive(pool)
			} else {
				err := s.automationStore.SetPartialPoolStatus(ctx, pool.ID, models.CrossSeedPartialPoolStatusDormant)
				if err != nil {
					if ctx.Err() == nil {
						log.Warn().Err(err).Int64("poolID", pool.ID).Msg("Failed to persist disabled partial completion pool schedule")
						retryFullScan = true
					}
				} else {
					pool.Status = models.CrossSeedPartialPoolStatusDormant
				}
			}
			continue
		}

		s.reconcilePartialPoolReviewPauses(ctx, pool, snapshots)
		if !evidenceComplete {
			s.pauseUnownedPartialPoolTransfers(ctx, pool, snapshots)
			keepPoolActive(pool)
			pendingObservation = pendingObservation || partialPoolRecheckObservationPending(pool)
			continue
		}
		if !s.refreshPartialPoolFiles(ctx, pool, snapshots) {
			s.pauseUnownedPartialPoolTransfers(ctx, pool, snapshots)
			keepPoolActive(pool)
			pendingObservation = pendingObservation || partialPoolRecheckObservationPending(pool)
			continue
		}
		if err := s.reconcilePartialPool(ctx, now, pool, snapshots, int64(max(settings.AutoResumeMaxDownloadMB, 0))<<20); err != nil && ctx.Err() == nil {
			log.Warn().Err(err).Int64("poolID", pool.ID).Msg("Failed to persist partial completion pool schedule")
			retryFullScan = true
		}
		pendingObservation = pendingObservation || partialPoolRecheckObservationPending(pool)
	}
	_ = s.automationStore.PruneEmptyPartialPools(ctx)
	return pendingObservation, nextAdmission, retryFullScan
}

// partialPoolInventoriesComplete reports whether every durable member resolved
// in the current qBittorrent inventory, not merely whether its instance loaded.
func partialPoolInventoriesComplete(pool *models.CrossSeedPartialPool, inventories map[int]partialPoolTorrentInventory) bool {
	if pool == nil {
		return false
	}
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		if _, found := partialPoolInventoryTorrent(inventories[member.InstanceID], member); !found {
			return false
		}
	}
	return true
}

func partialPoolNextAdmissionDeadline(pools []*models.CrossSeedPartialPool, now time.Time) time.Time {
	var next time.Time
	for _, pool := range pools {
		var poolDeadline time.Time
		for _, member := range pool.Members {
			if member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
				continue
			}
			deadline := member.CreatedAt.Add(partialPoolAdmissionHold)
			if deadline.After(poolDeadline) {
				poolDeadline = deadline
			}
		}
		if poolDeadline.After(now) && (next.IsZero() || poolDeadline.Before(next)) {
			next = poolDeadline
		}
	}
	return next
}

func partialPoolNextCoordinatorDelay(now, activeRecovery, admission, settledAudit time.Time) time.Duration {
	next := activeRecovery
	if !admission.IsZero() && admission.Before(next) {
		next = admission
	}
	if settledAudit.Before(next) {
		next = settledAudit
	}
	return max(next.Sub(now), 0)
}

func resetPartialPoolCoordinatorTimer(timer *time.Timer, now, activeRecovery, admission, settledAudit time.Time) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(partialPoolNextCoordinatorDelay(now, activeRecovery, admission, settledAudit))
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
		inventories[instanceID] = newPartialPoolTorrentInventory(torrents, false)
	}
	return inventories
}

// observeRequestedPartialPoolRechecks polls every requested member in batches
// by qBittorrent instance and persists the first observed piece-check state.
func (s *Service) observeRequestedPartialPoolRechecks(ctx context.Context, now time.Time) bool {
	if ctx.Err() != nil {
		return false
	}
	members, err := s.automationStore.ListPartialPoolMembersAwaitingRecheckObservation(ctx)
	if err != nil {
		return false
	}
	targetsByInstance := make(map[int][]*models.CrossSeedPartialPoolMember)
	for _, member := range members {
		targetsByInstance[member.InstanceID] = append(targetsByInstance[member.InstanceID], member)
	}

	pending := false
	for instanceID, targets := range targetsByInstance {
		if ctx.Err() != nil {
			return false
		}
		hashes := make([]string, 0, len(targets)*3)
		for _, member := range targets {
			hashes = append(hashes, partialPoolMemberHashes(member)...)
		}
		hashes = normalizedHashes(hashes...)
		torrents, fresh := s.forcePartialPoolTorrentSnapshot(ctx, instanceID, hashes)
		if !fresh {
			var loadErr error
			torrents, loadErr = s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Hashes: hashes})
			if loadErr != nil {
				for _, member := range targets {
					if !s.expirePartialPoolRecheckObservation(ctx, now, member) {
						pending = true
					}
				}
				continue
			}
		}
		inventory := newPartialPoolTorrentInventory(torrents, fresh)
		missing := false
		for _, member := range targets {
			if _, found := partialPoolInventoryTorrent(inventory, member); !found {
				missing = true
				break
			}
		}
		if missing {
			if all, allErr := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{}); allErr == nil {
				inventory = newPartialPoolTorrentInventory(all, false)
			}
		}

		for _, member := range targets {
			torrent, found := partialPoolInventoryTorrent(inventory, member)
			if !found {
				if !s.expirePartialPoolRecheckObservation(ctx, now, member) {
					pending = true
				}
				continue
			}
			if partialPoolDataChecking(torrent.State) {
				if !s.recordPartialPoolRecheckObserved(ctx, now, member) {
					pending = true
				}
				continue
			}
			if !s.expirePartialPoolRecheckObservation(ctx, now, member) {
				pending = true
			}
		}
	}
	return pending
}

// forcePartialPoolTorrentSnapshot refreshes and immediately reads raw state so
// a later sync cannot overwrite a brief checking transition before inspection.
func (s *Service) forcePartialPoolTorrentSnapshot(ctx context.Context, instanceID int, hashes []string) ([]qbt.Torrent, bool) {
	if s == nil || s.syncManager == nil {
		return nil, false
	}
	syncManager, err := s.syncManager.GetQBittorrentSyncManager(ctx, instanceID)
	if err != nil || syncManager == nil {
		return nil, false
	}
	syncCtx, cancel := context.WithTimeout(ctx, partialPoolRecheckPollSyncTimeout)
	err = syncManager.Sync(syncCtx)
	cancel()
	if err != nil {
		return nil, false
	}
	return syncManager.GetTorrents(qbt.TorrentFilterOptions{Hashes: hashes}), true
}

// refreshPartialPoolTorrentStates force-refreshes raw qBittorrent state for
// every supplied member. Members on the same instance share one sync so a
// propagation decision compares a coherent source and target snapshot.
func (s *Service) refreshPartialPoolTorrentStates(
	ctx context.Context,
	snapshots map[int64]*partialPoolMemberSnapshot,
	members ...*models.CrossSeedPartialPoolMember,
) bool {
	if ctx.Err() != nil || len(members) == 0 {
		return false
	}
	if s.partialPoolTorrentRefresher != nil {
		return s.partialPoolTorrentRefresher(ctx, snapshots, members...)
	}
	refreshedInstances := make(map[int]struct{}, len(members))
	for _, member := range members {
		if member == nil || snapshots[member.ID] == nil {
			return false
		}
		if _, refreshed := refreshedInstances[member.InstanceID]; refreshed {
			continue
		}
		var instanceMembers []*models.CrossSeedPartialPoolMember
		var hashes []string
		for _, candidate := range members {
			if candidate != nil && candidate.InstanceID == member.InstanceID {
				instanceMembers = append(instanceMembers, candidate)
				hashes = append(hashes, partialPoolMemberHashes(candidate)...)
			}
		}
		torrents, fresh := s.forcePartialPoolTorrentSnapshot(ctx, member.InstanceID, normalizedHashes(hashes...))
		if !fresh {
			return false
		}
		inventory := newPartialPoolTorrentInventory(torrents, true)
		for _, candidate := range instanceMembers {
			torrent, found := partialPoolInventoryTorrent(inventory, candidate)
			if !found {
				return false
			}
			snapshots[candidate.ID].torrent = torrent
		}
		refreshedInstances[member.InstanceID] = struct{}{}
	}
	return true
}

// newPartialPoolTorrentInventory indexes canonical and hybrid hash aliases.
func newPartialPoolTorrentInventory(torrents []qbt.Torrent, authoritative bool) partialPoolTorrentInventory {
	inventory := partialPoolTorrentInventory{loaded: true, authoritative: authoritative, byAlias: make(map[string]qbt.Torrent, len(torrents)*2)}
	for _, torrent := range torrents {
		for _, alias := range normalizedHashes(torrent.Hash, torrent.InfohashV1, torrent.InfohashV2) {
			inventory.byAlias[alias] = torrent
		}
	}
	return inventory
}

// partialPoolRecheckObservationPending reports whether any pool member needs
// short-interval raw qBittorrent polling to witness a piece-check transition.
func partialPoolRecheckObservationPending(pool *models.CrossSeedPartialPool) bool {
	if pool == nil {
		return false
	}
	return slices.ContainsFunc(pool.Members, partialPoolRecheckObservationOwned)
}

// partialPoolRecheckObservationOwned identifies verification state waiting for
// an affirmative piece-check observation.
func partialPoolRecheckObservationOwned(member *models.CrossSeedPartialPoolMember) bool {
	return member != nil &&
		(member.Status == models.CrossSeedPartialPoolMemberStatusVerifying || member.Status == models.CrossSeedPartialPoolMemberStatusRechecking) &&
		member.LastError == partialPoolRecheckRequested
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

// observePartialPoolMembers resolves durable members against the current
// qBittorrent inventory and removes confirmed absences. A newly admitted
// verifying member remains durable through the recheck grace so a stale
// post-add inventory cannot remove it prematurely.
func (s *Service) observePartialPoolMembers(
	ctx context.Context,
	now time.Time,
	pool *models.CrossSeedPartialPool,
	inventories map[int]partialPoolTorrentInventory,
) map[int64]qbt.Torrent {
	observed := make(map[int64]qbt.Torrent, len(pool.Members))
	authoritativeLookupAttempted := make(map[int]bool)
	for _, member := range pool.Members {
		inventory := inventories[member.InstanceID]
		if !inventory.loaded {
			continue
		}
		torrent, found := partialPoolInventoryTorrent(inventory, member)
		if !found {
			if member.Status == models.CrossSeedPartialPoolMemberStatusVerifying &&
				member.LastError == partialPoolRecheckPending &&
				now.Before(member.CreatedAt.Add(partialPoolRecheckGrace)) {
				continue
			}
			if !inventory.authoritative {
				if !authoritativeLookupAttempted[member.InstanceID] {
					authoritativeLookupAttempted[member.InstanceID] = true
					if torrents, fresh := s.forcePartialPoolTorrentSnapshot(ctx, member.InstanceID, nil); fresh {
						inventory = newPartialPoolTorrentInventory(torrents, true)
						inventories[member.InstanceID] = inventory
					}
				}
				if !inventory.authoritative {
					continue
				}
				if torrent, found = partialPoolInventoryTorrent(inventory, member); found {
					observed[member.ID] = torrent
					continue
				}
			}
			if err := cleanupPartialPoolMemberReflinkStaging(pool, member); err != nil {
				log.Warn().Err(err).Int64("memberID", member.ID).Msg("Failed to clean partial pool reflink staging before member removal")
				continue
			}
			for _, file := range member.Files {
				s.deletePartialPoolCreated(file.ID)
			}
			if err := s.automationStore.MarkPartialPoolMemberRemoved(ctx, member.PoolID, member.ID, member.CreatedAt, "torrent no longer exists in qBittorrent"); err != nil && ctx.Err() == nil {
				log.Warn().Err(err).Int64("memberID", member.ID).Msg("Failed to mark partial pool member removed")
			}
			continue
		}
		observed[member.ID] = torrent
	}
	return observed
}

// refreshPartialPoolFiles populates current file evidence and reports whether
// every observed member returned a usable file list.
func (s *Service) refreshPartialPoolFiles(
	ctx context.Context,
	pool *models.CrossSeedPartialPool,
	snapshots map[int64]*partialPoolMemberSnapshot,
) bool {
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
		if !partialPoolRootsEqual(snapshot.torrent.SavePath, member.RootPath) {
			if !snapshot.reviewPauseHandled {
				snapshot.reviewPauseHandled = true
				snapshot.stateRetryPending = !s.pausePartialPoolMemberForReview(ctx, member, snapshot.torrent, "qBittorrent save path no longer matches admitted root")
			}
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
	complete := true
	for _, instanceID := range instanceIDs {
		if ctx.Err() != nil {
			return false
		}
		requests := requestsByInstance[instanceID]
		hashes := make([]string, 0, len(requests))
		for _, request := range requests {
			hashes = append(hashes, request.hash)
		}
		filesByHash, err := s.syncManager.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), instanceID, hashes)
		if err != nil {
			complete = false
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
					snapshot := snapshots[request.member.ID]
					if !snapshot.reviewPauseHandled {
						snapshot.reviewPauseHandled = true
						snapshot.stateRetryPending = !s.pausePartialPoolMemberForReview(ctx, request.member, snapshot.torrent, "qBittorrent files or priorities no longer match admission")
					}
				} else {
					complete = false
				}
				continue
			}
			snapshot := snapshots[request.member.ID]
			snapshot.files = files
			snapshot.fileByIndex = fileByIndex
		}
	}
	return complete
}

// partialPoolRootsEqual compares normalized roots using local host path case
// semantics. Unix roots remain case-sensitive; Windows roots use case folding.
func partialPoolRootsEqual(left, right string) bool {
	left = normalizePath(left)
	right = normalizePath(right)
	if os.PathSeparator == '\\' {
		return strings.EqualFold(left, right)
	}
	return left == right
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

func (s *Service) refreshPartialPoolPropagationMembers(
	ctx context.Context,
	snapshots map[int64]*partialPoolMemberSnapshot,
	refreshed map[int64]bool,
	members ...*models.CrossSeedPartialPoolMember,
) bool {
	var pending []*models.CrossSeedPartialPoolMember
	complete := true
	for _, member := range members {
		if member == nil {
			return false
		}
		if succeeded, seen := refreshed[member.ID]; seen {
			complete = complete && succeeded
			continue
		}
		refreshed[member.ID] = false
		snapshot := snapshots[member.ID]
		if !s.refreshPartialPoolMemberSnapshot(ctx, member, snapshot) {
			complete = false
			continue
		}
		pending = append(pending, member)
	}
	if len(pending) == 0 {
		return complete
	}
	if !s.refreshPartialPoolTorrentStates(ctx, snapshots, pending...) {
		return false
	}
	for _, member := range pending {
		refreshed[member.ID] = true
	}
	return complete
}

func (s *Service) partialPoolCoordinatorEnabled(ctx context.Context) bool {
	settings, err := s.GetAutomationSettings(ctx)
	return err == nil && settings != nil && settings.PooledPartialCompletionEnabled
}

func partialPoolTorrentComplete(torrent qbt.Torrent) bool {
	return torrent.State != qbt.TorrentStateError && torrent.State != qbt.TorrentStateMissingFiles && torrent.Progress >= 1 && torrent.AmountLeft <= 0
}

// partialPoolCompletionPublishable prevents verification-owned or failed
// manual members from turning optimistic qBittorrent progress into pool data.
func partialPoolCompletionPublishable(member *models.CrossSeedPartialPoolMember) bool {
	if member == nil {
		return false
	}
	switch member.Status {
	case models.CrossSeedPartialPoolMemberStatusVerifying, models.CrossSeedPartialPoolMemberStatusRechecking:
		return false
	case models.CrossSeedPartialPoolMemberStatusManual:
		return member.LastError == ""
	default:
		return true
	}
}

// partialPoolInitialVerificationDeferred reports an admitted member whose
// first piece check can wait until it is selected or receives propagated data.
func partialPoolInitialVerificationDeferred(member *models.CrossSeedPartialPoolMember) bool {
	return partialPoolInitialVerificationPending(member) &&
		!partialPoolMemberHasVerificationWork(member)
}

// partialPoolInitialVerificationPending reports a first-check member whose
// recheck has not been requested, including members with files already staged
// for that check.
func partialPoolInitialVerificationPending(member *models.CrossSeedPartialPoolMember) bool {
	return member != nil &&
		member.Status == models.CrossSeedPartialPoolMemberStatusVerifying &&
		member.LastError == partialPoolRecheckPending
}

// partialPoolChecking reports any qBittorrent verification state, including
// resume-data validation.
func partialPoolChecking(state qbt.TorrentState) bool {
	return partialPoolDataChecking(state) || state == qbt.TorrentStateCheckingResumeData
}

// partialPoolDataChecking reports only piece verification, which can satisfy a
// pending pool recheck. Resume-data validation does not verify file contents.
func partialPoolDataChecking(state qbt.TorrentState) bool {
	return state == qbt.TorrentStateCheckingUp || state == qbt.TorrentStateCheckingDl
}

func partialPoolTransferCapable(state qbt.TorrentState) bool {
	return isDownloadingOrQueued(state)
}

// reconcileDisabledPartialPool stops pool-owned transfers and reports whether
// another observation is required before the pool can become dormant.
func (s *Service) reconcileDisabledPartialPool(ctx context.Context, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot) (bool, error) {
	pending := false
	var firstErr error
	for _, member := range pool.Members {
		if partialPoolRecheckObservationOwned(member) {
			pending = true
		}
		if reason, reviewPausePending := partialPoolReviewPauseReason(member); reviewPausePending {
			snapshot := snapshots[member.ID]
			if snapshot == nil || !s.pausePartialPoolMemberForReview(ctx, member, snapshot.torrent, reason) {
				pending = true
			}
			continue
		}
		if member.Status != models.CrossSeedPartialPoolMemberStatusAcquiring || !member.StartedByPool {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil {
			pending = true
			continue
		}
		if partialPoolChecking(snapshot.torrent.State) {
			pending = true
			continue
		}
		if partialPoolTorrentComplete(snapshot.torrent) {
			missing := int64(0)
			changed, err := s.automationStore.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, models.CrossSeedPartialPoolMemberStatusComplete, models.PartialPoolMemberMutation{MissingBytes: &missing})
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("complete disabled partial pool member: %w", err)
				}
				pending = true
				continue
			}
			if !changed {
				pending = true
				continue
			}
			member.Status = models.CrossSeedPartialPoolMemberStatusComplete
			member.StartedByPool = false
			member.MissingBytes = 0
			continue
		}
		if isPausedOrStopped(snapshot.torrent.State) || snapshot.torrent.State == qbt.TorrentStateError || snapshot.torrent.State == qbt.TorrentStateMissingFiles {
			stopped := false
			changed, err := s.automationStore.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{
				StartedByPool:       &stopped,
				LastDownloadedBytes: models.NullableInt64Update{Set: true},
				LastProgressAt:      models.NullableTimeUpdate{Set: true},
			})
			if err != nil {
				if firstErr == nil {
					firstErr = fmt.Errorf("release disabled partial pool downloader: %w", err)
				}
				pending = true
				continue
			}
			if !changed {
				pending = true
				continue
			}
			member.Status = models.CrossSeedPartialPoolMemberStatusWaiting
			member.StartedByPool = false
			member.LastDownloadedBytes = nil
			member.LastProgressAt = nil
			s.resetPartialPoolAcquiringFiles(ctx, member)
			continue
		}
		if ctx.Err() != nil {
			return pending, ctx.Err()
		}
		pending = true
		if err := s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause"); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("pause disabled partial pool downloader: %w", err)
		}
	}
	return pending, firstErr
}

// pauseUnownedPartialPoolTransfers stops live transfers that are not the
// durable acquiring member so only one pool member can download at a time.
func (s *Service) pauseUnownedPartialPoolTransfers(ctx context.Context, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot) bool {
	pending := false
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusAcquiring ||
			member.Status == models.CrossSeedPartialPoolMemberStatusManual ||
			member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil || snapshot.reviewPauseHandled || !partialPoolTransferCapable(snapshot.torrent.State) {
			continue
		}
		pending = true
		snapshot.stateRetryPending = true
		if ctx.Err() != nil {
			continue
		}
		if err := s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause"); err != nil {
			log.Warn().Err(err).Int64("poolID", pool.ID).Int64("memberID", member.ID).Msg("Failed to pause unowned partial pool transfer")
		}
	}
	return pending
}

// reconcilePartialPool applies current pool evidence and returns any failure
// to persist the pool's next scheduling state.
func (s *Service) reconcilePartialPool(
	ctx context.Context,
	now time.Time,
	pool *models.CrossSeedPartialPool,
	snapshots map[int64]*partialPoolMemberSnapshot,
	budget int64,
) error {
	admissionWindowClosed := partialPoolAdmissionWindowClosed(pool, now)
	stateRetryPending := s.reconcilePartialPoolManualFiles(ctx, pool)
	stateRetryPending = s.pauseUnownedPartialPoolTransfers(ctx, pool, snapshots) || stateRetryPending
	for _, member := range pool.Members {
		// qBittorrent can optimistically report a newly added torrent as
		// complete before its first recheck. Verification-owned members publish
		// through their status handler after that recheck settles.
		if !partialPoolCompletionPublishable(member) {
			continue
		}
		if snapshot := snapshots[member.ID]; snapshot != nil && len(snapshot.files) > 0 && partialPoolTorrentComplete(snapshot.torrent) {
			s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		}
	}
	propagationRefresh := make(map[int64]bool)
	s.propagatePartialPoolFiles(ctx, now, pool, snapshots, admissionWindowClosed, propagationRefresh)

	for _, member := range pool.Members {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil {
			continue
		}
		handled, retry := s.reconcilePartialPoolExceptionalState(ctx, now, member, snapshot)
		stateRetryPending = stateRetryPending || retry
		if handled {
			continue
		}
		if len(snapshot.files) == 0 {
			continue
		}
		switch member.Status {
		case models.CrossSeedPartialPoolMemberStatusVerifying:
			if partialPoolMemberHasPropagationWork(member) ||
				(admissionWindowClosed && s.partialPoolMemberHasPendingPropagationSource(ctx, pool, member, snapshot, snapshots, true)) {
				continue
			}
			if partialPoolInitialVerificationDeferred(member) && !partialPoolDataChecking(snapshot.torrent.State) {
				continue
			}
			if !admissionWindowClosed && member.LastError == partialPoolRecheckPending && !partialPoolDataChecking(snapshot.torrent.State) {
				continue
			}
			s.reconcilePartialPoolVerifying(ctx, now, pool, member, snapshot, budget)
		case models.CrossSeedPartialPoolMemberStatusRechecking:
			if partialPoolMemberHasPropagationWork(member) {
				continue
			}
			if !admissionWindowClosed && member.LastError == partialPoolRecheckPending && !partialPoolDataChecking(snapshot.torrent.State) {
				continue
			}
			s.reconcilePartialPoolRechecking(ctx, now, pool, member, snapshot, budget)
		case models.CrossSeedPartialPoolMemberStatusAcquiring:
			s.reconcilePartialPoolAcquiring(ctx, now, member, snapshot, budget)
		case models.CrossSeedPartialPoolMemberStatusComplete:
			s.reconcilePartialPoolComplete(ctx, member, snapshot)
		case models.CrossSeedPartialPoolMemberStatusManual:
			if partialPoolCompletionPublishable(member) && partialPoolTorrentComplete(snapshot.torrent) {
				s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusComplete, models.PartialPoolMemberMutation{})
			}
		}
	}

	for _, member := range pool.Members {
		if ctx.Err() != nil {
			return ctx.Err()
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

	s.propagatePartialPoolFiles(ctx, now, pool, snapshots, admissionWindowClosed, propagationRefresh)
	for _, snapshot := range snapshots {
		if snapshot != nil && snapshot.stateRetryPending {
			stateRetryPending = true
			break
		}
	}
	selectionPending := false
	if ctx.Err() == nil && !stateRetryPending {
		selectionPending = s.selectAndResumePartialPoolDownloader(ctx, now, pool, snapshots, budget)
	}

	status := models.CrossSeedPartialPoolStatusDormant
	if !partialPoolAdmissionReady(pool, now) || stateRetryPending || selectionPending {
		status = models.CrossSeedPartialPoolStatusActive
	} else {
		for _, member := range pool.Members {
			cooldownPending := member.Status == models.CrossSeedPartialPoolMemberStatusWaiting && member.RetryAfter != nil
			snapshot := snapshots[member.ID]
			checking := snapshot != nil && partialPoolChecking(snapshot.torrent.State)
			snapshotRetryPending := snapshot != nil && snapshot.stateRetryPending
			if member.Status == models.CrossSeedPartialPoolMemberStatusAcquiring || partialPoolInitialVerificationDeferred(member) || partialPoolMemberHasVerificationWork(member) || cooldownPending || checking || snapshotRetryPending || member.ReviewPausePending || member.LastError == partialPoolPropagationPause || partialPoolResumePending(member) || partialPoolRecoveryPending(member) {
				status = models.CrossSeedPartialPoolStatusActive
				break
			}
		}
	}
	changed, err := s.automationStore.SetPartialPoolStatusIfUnchanged(ctx, pool.ID, pool.UpdatedAt, status)
	if err != nil {
		return fmt.Errorf("persist partial pool scheduling state: %w", err)
	}
	if !changed {
		return nil
	}
	pool.Status = status
	return nil
}

// reconcilePartialPoolManualFiles repairs a file-first manual transition after
// a failed member CAS and reports whether the durable retry is still pending.
func (s *Service) reconcilePartialPoolManualFiles(ctx context.Context, pool *models.CrossSeedPartialPool) bool {
	retryPending := false
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusManual || member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		for _, file := range member.Files {
			if file.Status != models.CrossSeedPartialPoolFileStatusManual {
				continue
			}
			reason := strings.TrimSpace(file.LastError)
			if reason == "" {
				reason = "partial pool file requires manual review"
			}
			if !s.markPartialPoolMemberManual(ctx, member, reason) {
				retryPending = true
			}
			break
		}
	}
	return retryPending
}

func (s *Service) reconcilePartialPoolExceptionalState(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) (bool, bool) {
	if member == nil || snapshot == nil || member.Status == models.CrossSeedPartialPoolMemberStatusManual || member.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
		return false, false
	}
	if snapshot.torrent.State == qbt.TorrentStateMissingFiles && member.Mode == models.CrossSeedPartialPoolModeHardlink {
		if member.Status == models.CrossSeedPartialPoolMemberStatusVerifying || member.Status == models.CrossSeedPartialPoolMemberStatusRechecking {
			return false, false
		}
		reason := "hardlink partial pool member entered missingFiles state"
		if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
			return true, !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		}
		return true, !s.markPartialPoolMemberManual(ctx, member, reason)
	}
	if snapshot.torrent.State == qbt.TorrentStateMissingFiles &&
		member.Mode == models.CrossSeedPartialPoolModeReflink &&
		member.Status == models.CrossSeedPartialPoolMemberStatusComplete &&
		partialPoolResumePending(member) {
		s.requestPartialPoolResume(ctx, member)
		return true, false
	}

	attempts := int64(0)
	recovering := partialPoolRecoveryPending(member)
	if recovering {
		attempts = *member.RecoveryAttempts
	}
	if snapshot.torrent.State != qbt.TorrentStateError {
		if !recovering {
			return false, false
		}
		if partialPoolDataChecking(snapshot.torrent.State) {
			if member.Status != models.CrossSeedPartialPoolMemberStatusComplete {
				s.recordPartialPoolRecheckObserved(ctx, now, member)
			}
			return true, false
		}
		if snapshot.torrent.State == qbt.TorrentStateCheckingResumeData {
			return true, false
		}
		if now.Sub(member.UpdatedAt) < partialPoolRecheckGrace {
			return true, false
		}
		empty := ""
		mutation := models.PartialPoolMemberMutation{
			RecoveryAttempts: models.NullableInt64Update{Set: true},
			LastError:        &empty,
		}
		if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
			zero := int64(0)
			mutation.ResumeAttempts = models.NullableInt64Update{Set: true, Value: &zero}
		}
		s.transitionPartialPoolMember(ctx, member, member.Status, mutation)
		return false, false
	}
	if member.Status == models.CrossSeedPartialPoolMemberStatusComplete && !partialPoolResumePending(member) && !recovering {
		return false, false
	}
	if !s.partialPoolCoordinatorEnabled(ctx) {
		return true, false
	}
	if recovering && now.Sub(member.UpdatedAt) < partialPoolRecheckGrace {
		return true, false
	}
	if attempts >= int64(partialPoolRecoveryLimit) {
		s.finishPartialPoolRecoveryExhausted(ctx, member, partialPoolRecoveryExhausted)
		return true, false
	}

	nextAttempt := attempts + 1
	empty := ""
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
		RecoveryAttempts: models.NullableInt64Update{Set: true, Value: &nextAttempt},
		LastError:        &empty,
	}) {
		return true, true
	}
	member.UpdatedAt = now
	_ = s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "pause")
	if err := s.syncManager.BulkAction(ctx, member.InstanceID, partialPoolMemberHashes(member), "recheck"); err != nil && nextAttempt >= int64(partialPoolRecoveryLimit) {
		s.finishPartialPoolRecoveryExhausted(ctx, member, partialPoolRecoveryExhausted+": "+err.Error())
	}
	return true, false
}

func (s *Service) finishPartialPoolRecoveryExhausted(ctx context.Context, member *models.CrossSeedPartialPoolMember, reason string) {
	if member.Status == models.CrossSeedPartialPoolMemberStatusComplete {
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
			ResumeAttempts:   models.NullableInt64Update{Set: true},
			RecoveryAttempts: models.NullableInt64Update{Set: true},
			LastError:        &reason,
		})
		return
	}
	s.markPartialPoolMemberManual(ctx, member, reason)
}

func (s *Service) reconcilePartialPoolVerifying(ctx context.Context, now time.Time, pool *models.CrossSeedPartialPool, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot, budget int64) {
	if !s.partialPoolRecheckSettled(ctx, now, member, snapshot) {
		return
	}

	if partialPoolTorrentComplete(snapshot.torrent) {
		s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		s.completeAndResumePartialPoolMember(ctx, member, snapshot)
		return
	}
	if !s.settlePartialPoolRecheckFiles(ctx, now, pool, member, snapshot) {
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
	}
	if PolicyForSourceFiles(snapshot.files).DiscLayout {
		budget = 0
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

// requestPartialPoolRecheck persists ownership before issuing the qBittorrent
// action and moves the member to manual intervention if that action fails.
func (s *Service) requestPartialPoolRecheck(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember) {
	if ctx.Err() != nil || member == nil || !s.partialPoolCoordinatorEnabled(ctx) {
		return
	}
	if !s.recordPartialPoolRecheckRequested(ctx, now, member) {
		return
	}
	err := s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), member.InstanceID, partialPoolMemberHashes(member), "recheck")
	if err != nil {
		s.markPartialPoolMemberManual(ctx, member, "recheck request failed: "+err.Error())
		return
	}
	log.Debug().
		Int64("poolID", member.PoolID).
		Int64("memberID", member.ID).
		Str("memberStatus", member.Status).
		Msg("Partial pool recheck requested")
}

// expirePartialPoolRecheckObservation fails closed when a successful request
// never produces an observable piece-check state within the polling window.
func (s *Service) expirePartialPoolRecheckObservation(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember) bool {
	if member == nil || now.Before(member.UpdatedAt.Add(partialPoolRecheckObserveTimeout)) {
		return false
	}
	reason := partialPoolRecheckUnobserved
	return s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason})
}

// partialPoolRecheckSettled requires a durably observed piece-check transition
// before verification-owned state can publish or consume qBittorrent progress.
func (s *Service) partialPoolRecheckSettled(ctx context.Context, now time.Time, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) bool {
	if member == nil || snapshot == nil {
		return false
	}
	if partialPoolDataChecking(snapshot.torrent.State) {
		if member.LastError != partialPoolRecheckObserved {
			s.recordPartialPoolRecheckObserved(ctx, now, member)
		}
		return false
	}
	if snapshot.torrent.State == qbt.TorrentStateCheckingResumeData {
		if member.LastError == partialPoolRecheckObserved {
			reason := partialPoolRecheckPending
			s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
		}
		return false
	}
	if member.LastError == partialPoolRecheckPending {
		s.requestPartialPoolRecheck(ctx, now, member)
		return false
	}
	if member.LastError == partialPoolRecheckObserved {
		return true
	}
	if member.LastError == partialPoolRecheckRequested {
		s.expirePartialPoolRecheckObservation(ctx, now, member)
		return false
	}
	reason := partialPoolRecheckPending
	s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
	return false
}

// partialPoolRecoveryPending reports whether a durable error-recovery attempt
// sequence is active.
func partialPoolRecoveryPending(member *models.CrossSeedPartialPoolMember) bool {
	return member != nil && member.RecoveryAttempts != nil
}

// partialPoolResumePending reports whether a durable resume attempt sequence is
// active.
func partialPoolResumePending(member *models.CrossSeedPartialPoolMember) bool {
	return member != nil && member.ResumeAttempts != nil
}

func (s *Service) requestPartialPoolResume(ctx context.Context, member *models.CrossSeedPartialPoolMember) bool {
	if ctx.Err() != nil || member == nil || !s.partialPoolCoordinatorEnabled(ctx) {
		return false
	}
	attempts := int64(0)
	if partialPoolResumePending(member) {
		attempts = *member.ResumeAttempts
	}
	if attempts >= int64(maxRecheckResumeAttempts) {
		reason := partialPoolResumeExhausted
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
			ResumeAttempts: models.NullableInt64Update{Set: true},
			LastError:      &reason,
		})
		return false
	}

	nextAttempt := attempts + 1
	empty := ""
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
		ResumeAttempts: models.NullableInt64Update{Set: true, Value: &nextAttempt},
		LastError:      &empty,
	}) {
		return false
	}
	err := s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), member.InstanceID, partialPoolMemberHashes(member), "resume")
	if err == nil {
		return true
	}
	if nextAttempt >= int64(maxRecheckResumeAttempts) {
		reason := partialPoolResumeExhausted + ": " + err.Error()
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
			ResumeAttempts: models.NullableInt64Update{Set: true},
			LastError:      &reason,
		})
	}
	return false
}

func (s *Service) completeAndResumePartialPoolMember(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) {
	if member == nil || snapshot == nil {
		return
	}
	zero := int64(0)
	empty := ""
	if !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusComplete, models.PartialPoolMemberMutation{
		ResumeAttempts:   models.NullableInt64Update{Set: true, Value: &zero},
		RecoveryAttempts: models.NullableInt64Update{Set: true},
		LastError:        &empty,
	}) {
		return
	}
	if isRecheckResumeConfirmed(snapshot.torrent.State) {
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
			ResumeAttempts: models.NullableInt64Update{Set: true},
			LastError:      &empty,
		})
		return
	}
	s.requestPartialPoolResume(ctx, member)
}

func (s *Service) reconcilePartialPoolComplete(ctx context.Context, member *models.CrossSeedPartialPoolMember, snapshot *partialPoolMemberSnapshot) {
	if !partialPoolResumePending(member) {
		return
	}
	if !partialPoolTorrentComplete(snapshot.torrent) {
		reason := "completed partial pool member lost verification before resume"
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
			ResumeAttempts: models.NullableInt64Update{Set: true},
			LastError:      &reason,
		})
		return
	}
	if isRecheckResumeConfirmed(snapshot.torrent.State) {
		empty := ""
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{
			ResumeAttempts: models.NullableInt64Update{Set: true},
			LastError:      &empty,
		})
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
	if !s.partialPoolRecheckSettled(ctx, now, member, snapshot) {
		return
	}
	if partialPoolTorrentComplete(snapshot.torrent) {
		s.publishPartialPoolCompletedFiles(ctx, member, snapshot)
		s.completeAndResumePartialPoolMember(ctx, member, snapshot)
		return
	}

	if !s.settlePartialPoolRecheckFiles(ctx, now, pool, member, snapshot) {
		return
	}

	status, reason := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	missing := max(snapshot.torrent.AmountLeft, 0)
	empty := ""
	s.transitionPartialPoolMember(ctx, member, status, models.PartialPoolMemberMutation{MissingBytes: &missing, LastError: choosePartialPoolError(status, reason, &empty)})
}

// settlePartialPoolRecheckFiles durably resolves propagated files after a
// settled piece check. It returns true only when every verifying file is
// settled and no hardlink rollback requires another check.
func (s *Service) settlePartialPoolRecheckFiles(
	ctx context.Context,
	now time.Time,
	pool *models.CrossSeedPartialPool,
	member *models.CrossSeedPartialPoolMember,
	snapshot *partialPoolMemberSnapshot,
) bool {
	for _, file := range member.Files {
		if file.Status != models.CrossSeedPartialPoolFileStatusVerifying {
			continue
		}
		current := snapshot.fileByIndex[file.FileIndex]
		if current.Progress >= 1 {
			if !s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{}) {
				return false
			}
			s.deletePartialPoolCreated(file.ID)
			continue
		}
		if member.Mode == models.CrossSeedPartialPoolModeHardlink {
			var sourceMember *models.CrossSeedPartialPoolMember
			var sourceFile *models.CrossSeedPartialPoolMemberFile
			if file.SourceFileID != nil {
				sourceMember, sourceFile = partialPoolFileByID(pool, *file.SourceFileID)
			}
			rolledBack, retry := s.rollbackLivePartialPoolHardlink(ctx, file, pool)
			if retry {
				return false
			}
			if rolledBack {
				if sourceMember != nil && sourceFile != nil {
					s.rejectPartialPoolPropagationPair(sourceMember, sourceFile, member, file)
				}
				s.requestPartialPoolRecheck(ctx, now, member)
				return false
			}
			reason := "propagated hardlink failed verification; target ownership is not provable"
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusManual, models.PartialPoolFileMutation{LastError: &reason})
			s.markPartialPoolMemberManual(ctx, member, reason)
			return false
		}
		reason := "propagated reflink failed verification; retained clone requires download repair"
		if !s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{
			SourceFileID: models.NullableInt64Update{Set: true},
			LastError:    &reason,
		}) {
			return false
		}
	}
	return true
}

// rollbackLivePartialPoolHardlink removes an owned target, or adopts an
// already-absent target, before atomically recording the missing file and its
// pending follow-up check. rolledBack is true only after that state commits;
// retry asks the caller to retain verification ownership, while two false
// results mean the live target cannot be removed safely.
func (s *Service) rollbackLivePartialPoolHardlink(ctx context.Context, file *models.CrossSeedPartialPoolMemberFile, pool *models.CrossSeedPartialPool) (rolledBack, retry bool) {
	targetMember := partialPoolMemberForFile(pool, file)
	if targetMember == nil {
		return false, false
	}
	targetPath, err := partialPoolLocalPath(targetMember, file)
	if err != nil {
		return false, false
	}
	if ctx.Err() != nil {
		return false, true
	}
	s.partialPoolAdmissionMaterializationMu.Lock()
	defer s.partialPoolAdmissionMaterializationMu.Unlock()
	currentPool, err := s.automationStore.GetPartialPool(ctx, pool.ID)
	if err != nil || !partialPoolMemberFileAdmissionCurrent(currentPool, targetMember, file) {
		return false, true
	}
	targetMissing := false
	if _, err := os.Lstat(targetPath); os.IsNotExist(err) {
		targetMissing = true
	} else if err != nil {
		return false, false
	}

	if !targetMissing {
		created := s.loadPartialPoolCreated(file.ID)
		if created == nil || file.SourceFileID == nil {
			return false, false
		}
		sourceMember, sourceFile := partialPoolFileByID(pool, *file.SourceFileID)
		if sourceMember == nil || sourceFile == nil {
			return false, false
		}
		if !partialPoolMemberFileAdmissionCurrent(currentPool, sourceMember, sourceFile) {
			return false, true
		}
		sourcePath, err := partialPoolLocalPath(sourceMember, sourceFile)
		if err != nil || !partialPoolCreatedContains(created, targetPath) || !partialPoolSameFile(sourcePath, targetPath) {
			return false, false
		}
		if err := created.Rollback(); err != nil {
			if _, statErr := os.Lstat(targetPath); os.IsNotExist(statErr) {
				return false, true
			}
			return false, false
		}
	}
	changed, err := s.automationStore.TransitionPartialPoolHardlinkRollback(ctx, targetMember.ID, file.ID, targetMember.CreatedAt, file.CreatedAt, targetMember.Status)
	if err != nil || !changed {
		return false, true
	}
	file.Status = models.CrossSeedPartialPoolFileStatusMissing
	file.SourceFileID = nil
	targetMember.LastError = partialPoolRecheckPending
	s.deletePartialPoolCreated(file.ID)
	return true, false
}

func (s *Service) loadPartialPoolCreated(fileID int64) *hardlinktree.Created {
	s.partialPoolCreatedMu.Lock()
	defer s.partialPoolCreatedMu.Unlock()
	return s.partialPoolCreated[fileID]
}

func (s *Service) storePartialPoolCreated(fileID int64, created *hardlinktree.Created) {
	s.partialPoolCreatedMu.Lock()
	defer s.partialPoolCreatedMu.Unlock()
	if s.partialPoolCreated == nil {
		s.partialPoolCreated = make(map[int64]*hardlinktree.Created)
	}
	s.partialPoolCreated[fileID] = created
}

func (s *Service) deletePartialPoolCreated(fileID int64) {
	s.partialPoolCreatedMu.Lock()
	defer s.partialPoolCreatedMu.Unlock()
	delete(s.partialPoolCreated, fileID)
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
			s.markPartialPoolMemberManual(ctx, member, "hardlink downloader entered missingFiles state")
			return
		}
		s.requestPartialPoolResume(ctx, member)
		if strings.HasPrefix(member.LastError, partialPoolResumeExhausted) {
			s.markPartialPoolMemberManual(ctx, member, member.LastError)
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
			s.markPartialPoolMemberManual(ctx, member, reason)
			return
		}
		if partialPoolResumePending(member) {
			s.requestPartialPoolResume(ctx, member)
			if strings.HasPrefix(member.LastError, partialPoolResumeExhausted) {
				s.markPartialPoolMemberManual(ctx, member, member.LastError)
			}
			return
		}
		status, _ := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
		modeEnabled, modeErr := s.partialPoolMemberModeEnabled(ctx, member)
		if modeErr != nil {
			snapshot.stateRetryPending = true
			return
		}
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
	if partialPoolTransferCapable(snapshot.torrent.State) && partialPoolResumePending(member) {
		empty := ""
		s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusAcquiring, models.PartialPoolMemberMutation{
			ResumeAttempts: models.NullableInt64Update{Set: true},
			LastError:      &empty,
		})
	}
	status, _ := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	modeEnabled, modeErr := s.partialPoolMemberModeEnabled(ctx, member)
	if modeErr != nil {
		snapshot.stateRetryPending = true
		return
	}
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
		s.markPartialPoolMemberManualWithPause(ctx, member, "stalled downloader could not be paused: "+err.Error(), true)
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
				s.deletePartialPoolCreated(file.ID)
			}
		case models.CrossSeedPartialPoolFileStatusPropagating:
			if s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{}) {
				if s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{}) {
					s.deletePartialPoolCreated(file.ID)
				}
			}
		case models.CrossSeedPartialPoolFileStatusMissing, models.CrossSeedPartialPoolFileStatusAcquiring:
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusAvailable, models.PartialPoolFileMutation{})
		}
	}
}

func (s *Service) propagatePartialPoolFiles(ctx context.Context, now time.Time, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot, admissionWindowClosed bool, refreshed map[int64]bool) {
	if !s.partialPoolCoordinatorEnabled(ctx) {
		return
	}
	for _, targetMember := range pool.Members {
		initialVerificationMember := admissionWindowClosed && targetMember.Status == models.CrossSeedPartialPoolMemberStatusVerifying
		if targetMember.Status != models.CrossSeedPartialPoolMemberStatusWaiting && targetMember.Status != models.CrossSeedPartialPoolMemberStatusBlocked && targetMember.Status != models.CrossSeedPartialPoolMemberStatusRechecking && !initialVerificationMember {
			continue
		}
		if partialPoolMemberHasPropagationWork(targetMember) && !s.preparePartialPoolRecheckForPropagation(ctx, now, targetMember, snapshots) {
			continue
		}
		targetSnapshot := snapshots[targetMember.ID]
		if targetSnapshot == nil || targetSnapshot.stateRetryPending || len(targetSnapshot.files) == 0 {
			continue
		}
		pendingPropagationSource := targetMember.Status != models.CrossSeedPartialPoolMemberStatusRechecking &&
			s.partialPoolMemberHasPendingPropagationSource(ctx, pool, targetMember, targetSnapshot, snapshots, initialVerificationMember)
		if pendingPropagationSource && !s.preparePartialPoolRecheckForPropagation(ctx, now, targetMember, snapshots) {
			continue
		}
		initialVerification := initialVerificationMember && partialPoolInitialVerificationPending(targetMember)
		if targetMember.Status == models.CrossSeedPartialPoolMemberStatusVerifying && !initialVerification {
			continue
		}
		if partialPoolChecking(targetSnapshot.torrent.State) {
			continue
		}
		missingFilesReflinkRecovery := targetMember.Mode == models.CrossSeedPartialPoolModeReflink &&
			targetSnapshot.torrent.State == qbt.TorrentStateMissingFiles &&
			(initialVerification || partialPoolMemberHasVerificationWork(targetMember))
		if targetSnapshot.torrent.State == qbt.TorrentStateError ||
			(targetSnapshot.torrent.State == qbt.TorrentStateMissingFiles && !missingFilesReflinkRecovery) ||
			partialPoolRecoveryPending(targetMember) {
			continue
		}
		if !missingFilesReflinkRecovery && !isPausedOrStopped(targetSnapshot.torrent.State) {
			if targetMember.LastError != partialPoolPropagationPause {
				reason := partialPoolPropagationPause
				if !s.transitionPartialPoolMember(ctx, targetMember, targetMember.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
					continue
				}
			}
			if ctx.Err() == nil {
				if err := s.syncManager.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), targetMember.InstanceID, partialPoolMemberHashes(targetMember), "pause"); err != nil {
					s.markPartialPoolMemberManualWithPause(ctx, targetMember, "propagation target could not be paused: "+err.Error(), true)
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
				if s.finishPartialPoolPropagation(ctx, pool, targetMember, targetFile, snapshots, refreshed) {
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
			if hasVerifying && targetMember.LastError != partialPoolRecheckPending && targetMember.LastError != partialPoolRecheckRequested && targetMember.LastError != partialPoolRecheckObserved {
				s.claimPartialPoolRecheck(ctx, targetMember)
			}
			continue
		}

		for _, targetFile := range targetMember.Files {
			if targetFile.Status != models.CrossSeedPartialPoolFileStatusMissing || targetFile.LastError != "" {
				continue
			}
			targetCurrent := targetSnapshot.fileByIndex[targetFile.FileIndex]
			if targetCurrent.Priority == 0 || targetFile.SizeBytes <= 0 {
				continue
			}
			if initialVerification {
				if !targetFile.WantedAtAdmission || targetFile.MaterializedAtAdd {
					continue
				}
			} else if targetCurrent.Progress > 0 {
				continue
			}
			sourceFile := s.selectPartialPoolSourceFile(ctx, pool, targetMember, targetFile, snapshots)
			if sourceFile == nil {
				continue
			}
			if !s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
				SourceFileID: models.NullableInt64Update{Set: true, Value: &sourceFile.ID},
			}) {
				continue
			}
			log.Debug().
				Int64("poolID", pool.ID).
				Int64("targetMemberID", targetMember.ID).
				Int64("targetFileID", targetFile.ID).
				Int64("sourceMemberID", sourceFile.MemberID).
				Int64("sourceFileID", sourceFile.ID).
				Int64("sizeBytes", targetFile.SizeBytes).
				Str("mode", targetMember.Mode).
				Bool("initialVerification", initialVerification).
				Msg("Partial pool file propagation claimed")
			if s.finishPartialPoolPropagation(ctx, pool, targetMember, targetFile, snapshots, refreshed) {
				hasVerifying = true
			}
			if targetMember.Status == models.CrossSeedPartialPoolMemberStatusManual {
				break
			}
		}
		if targetMember.Status == models.CrossSeedPartialPoolMemberStatusManual {
			continue
		}
		pendingPropagationSource = s.partialPoolMemberHasPendingPropagationSource(ctx, pool, targetMember, targetSnapshot, snapshots, initialVerificationMember)
		if hasVerifying && !pendingPropagationSource {
			s.claimPartialPoolRecheck(ctx, targetMember)
		}
	}
}

// selectPartialPoolSourceFile returns the first live compatible source that
// has not already failed against this target across filesystems.
func (s *Service) selectPartialPoolSourceFile(
	ctx context.Context,
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
		modeEnabled, modeErr := s.partialPoolMemberModeEnabled(ctx, sourceMember)
		if modeErr != nil {
			if targetSnapshot := snapshots[targetMember.ID]; targetSnapshot != nil {
				targetSnapshot.stateRetryPending = true
			}
			continue
		}
		if !modeEnabled {
			continue
		}
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
			if current.Priority > 0 && current.Progress >= 1 &&
				partialPoolFilesPair(sourceMember, targetMember, sourceFile, targetFile) &&
				!s.partialPoolPropagationPairRejected(sourceMember, sourceFile, targetMember, targetFile) {
				return sourceFile
			}
		}
	}
	return nil
}

// partialPoolReflinkStagingPaths returns deterministic staging paths adjacent
// to the target so retries can find crash leftovers and replacement stays on
// the same filesystem.
func partialPoolReflinkStagingPaths(targetPath string) (string, string) {
	digest := sha256.Sum256([]byte(filepath.Clean(targetPath)))
	root := filepath.Join(filepath.Dir(targetPath), fmt.Sprintf("%s%x", partialPoolReflinkStagingPrefix, digest[:16]))
	return root, filepath.Join(root, partialPoolReflinkStagingClone)
}

// partialPoolReflinkStagingOwnerPath returns the ownership marker beside a
// staging root, where it remains discoverable if the root is missing.
func partialPoolReflinkStagingOwnerPath(root string) string {
	return root + partialPoolReflinkStagingOwner
}

// partialPoolReflinkStagingRootOwned reports whether root has the exact regular
// ownership marker written by qui. A missing marker means unowned; an invalid
// or unreadable marker returns an error.
func partialPoolReflinkStagingRootOwned(root string) (bool, error) {
	ownerPath := partialPoolReflinkStagingOwnerPath(root)
	info, err := os.Lstat(ownerPath)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if !info.Mode().IsRegular() {
		return false, errors.New("reflink staging ownership marker is invalid")
	}
	data, err := os.ReadFile(ownerPath)
	if err != nil {
		return false, err
	}
	if string(data) != partialPoolReflinkStagingOwnerData {
		return false, errors.New("reflink staging ownership marker is invalid")
	}
	return true, nil
}

// ensurePartialPoolReflinkStagingRoot prepares the target parent and an owned
// staging directory. It refuses pre-existing unowned paths and syncs a new
// ownership marker before creating the directory so interrupted creation can
// be recovered safely.
func ensurePartialPoolReflinkStagingRoot(root string) error {
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return err
	}
	info, err := os.Lstat(root)
	rootMissing := os.IsNotExist(err)
	if err != nil && !rootMissing {
		return err
	}
	if !rootMissing && !info.IsDir() {
		return errors.New("reflink staging path is not a directory")
	}

	owned, err := partialPoolReflinkStagingRootOwned(root)
	if err != nil {
		return err
	}
	if !rootMissing {
		if !owned {
			return errors.New("pre-existing reflink staging directory is not owned by qui")
		}
		return nil
	}

	createdOwner := false
	if !owned {
		ownerPath := partialPoolReflinkStagingOwnerPath(root)
		marker, openErr := os.OpenFile(ownerPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if openErr != nil {
			return openErr
		}
		if _, writeErr := marker.WriteString(partialPoolReflinkStagingOwnerData); writeErr != nil {
			_ = marker.Close()
			_ = os.Remove(ownerPath)
			return writeErr
		}
		if syncErr := marker.Sync(); syncErr != nil {
			_ = marker.Close()
			_ = os.Remove(ownerPath)
			return syncErr
		}
		if closeErr := marker.Close(); closeErr != nil {
			_ = os.Remove(ownerPath)
			return closeErr
		}
		createdOwner = true
	}
	if err := os.Mkdir(root, 0o700); err != nil {
		if createdOwner {
			_ = os.Remove(partialPoolReflinkStagingOwnerPath(root))
		}
		return err
	}
	return nil
}

// cleanPartialPoolReflinkStagingRoot removes recognized regular artifacts from
// an owned staging root. Missing or unowned roots are preserved; unknown,
// non-regular, or protected-file aliases are rejected.
func cleanPartialPoolReflinkStagingRoot(root string, protectedPaths ...string) error {
	owned, err := partialPoolReflinkStagingRootOwned(root)
	if err != nil {
		return err
	}
	rootInfo, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !rootInfo.IsDir() {
		if owned {
			return errors.New("owned reflink staging path is not a directory")
		}
		return nil
	}
	if !owned {
		return nil
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	removable := make([]string, 0, len(entries))
	for _, entry := range entries {
		name := entry.Name()
		if name != partialPoolReflinkStagingClone &&
			!strings.HasPrefix(name, partialPoolReflinkProbeSource) &&
			!strings.HasPrefix(name, partialPoolReflinkProbeDestination) {
			return errors.New("reflink staging directory contains an unknown entry")
		}
		entryPath := filepath.Join(root, name)
		entryInfo, statErr := os.Lstat(entryPath)
		if statErr != nil {
			return statErr
		}
		if !entryInfo.Mode().IsRegular() {
			return errors.New("reflink staging entry is not a regular file")
		}
		for _, protectedPath := range protectedPaths {
			protectedInfo, protectedErr := os.Lstat(protectedPath)
			if os.IsNotExist(protectedErr) {
				continue
			}
			if protectedErr != nil {
				return protectedErr
			}
			if os.SameFile(entryInfo, protectedInfo) {
				return errors.New("reflink staging entry aliases a protected file")
			}
		}
		removable = append(removable, entryPath)
	}
	for _, entryPath := range removable {
		if err := os.Remove(entryPath); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	return nil
}

// releasePartialPoolReflinkStagingRoot removes an empty owned root before its
// ownership marker. Unowned roots are preserved, and unsafe or failed removal
// is returned for a later retry.
func releasePartialPoolReflinkStagingRoot(root string) error {
	owned, err := partialPoolReflinkStagingRootOwned(root)
	if err != nil {
		return err
	}
	if !owned {
		return nil
	}
	ownerPath := partialPoolReflinkStagingOwnerPath(root)
	info, err := os.Lstat(root)
	if os.IsNotExist(err) {
		return os.Remove(ownerPath)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("owned reflink staging path is not a directory")
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return err
	}
	if len(entries) != 0 {
		return errors.New("owned reflink staging directory is not empty")
	}
	if err := os.Remove(root); err != nil {
		return err
	}
	return os.Remove(ownerPath)
}

// cleanupPartialPoolMemberReflinkStaging removes owned crash artifacts for a
// reflink member. Errors are returned so durable member removal can wait and
// retry without abandoning full-size staging files.
func cleanupPartialPoolMemberReflinkStaging(pool *models.CrossSeedPartialPool, member *models.CrossSeedPartialPoolMember) error {
	if member == nil || member.Mode != models.CrossSeedPartialPoolModeReflink {
		return nil
	}
	for _, file := range member.Files {
		targetPath, err := partialPoolLocalPath(member, file)
		if err != nil {
			return fmt.Errorf("resolve staging target: %w", err)
		}
		protectedPaths := []string{targetPath}
		if file.SourceFileID != nil {
			if sourceMember, sourceFile := partialPoolFileByID(pool, *file.SourceFileID); sourceMember != nil && sourceFile != nil {
				sourcePath, sourceErr := partialPoolLocalPath(sourceMember, sourceFile)
				if sourceErr != nil {
					return fmt.Errorf("resolve staging source: %w", sourceErr)
				}
				protectedPaths = append(protectedPaths, sourcePath)
			}
		}
		root, _ := partialPoolReflinkStagingPaths(targetPath)
		if err := cleanPartialPoolReflinkStagingRoot(root, protectedPaths...); err != nil {
			return err
		}
		if err := releasePartialPoolReflinkStagingRoot(root); err != nil {
			return err
		}
	}
	return nil
}

// finishPartialPoolPropagation validates and materializes one claimed pool
// file. It reports success only when the target is ready for verification;
// cross-filesystem failures reject only that source/target pair, unsafe inputs
// or other filesystem failures move the file and member to manual handling,
// and unavailable live state remains retryable. A failed durable transition
// marks its snapshot so reconciliation retains the active schedule.
func (s *Service) finishPartialPoolPropagation(
	ctx context.Context,
	pool *models.CrossSeedPartialPool,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
	snapshots map[int64]*partialPoolMemberSnapshot,
	refreshed map[int64]bool,
) bool {
	if ctx.Err() != nil {
		return false
	}
	targetSnapshot := snapshots[targetMember.ID]
	resetRejectedPairClaim := func() {
		empty := ""
		if !s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{
			SourceFileID: models.NullableInt64Update{Set: true},
			LastError:    &empty,
		}) && targetSnapshot != nil {
			targetSnapshot.stateRetryPending = true
		}
	}
	if targetFile.SourceFileID == nil {
		resetRejectedPairClaim()
		return false
	}
	markManual := func(failureCategory, reason string) {
		if !s.markPartialPoolPropagationManual(ctx, targetMember, targetFile, failureCategory, reason) && targetSnapshot != nil {
			targetSnapshot.stateRetryPending = true
		}
	}
	cleanupStagingBeforeManual := func() bool {
		if err := cleanupPartialPoolMemberReflinkStaging(pool, targetMember); err != nil {
			log.Warn().
				Err(err).
				Int64("poolID", targetMember.PoolID).
				Int64("memberID", targetMember.ID).
				Msg("Failed to clean partial pool reflink staging before manual propagation handling")
			if targetSnapshot != nil {
				targetSnapshot.stateRetryPending = true
			}
			return false
		}
		return true
	}
	sourceMember, sourceFile := partialPoolFileByID(pool, *targetFile.SourceFileID)
	if sourceMember == nil || sourceFile == nil {
		if !cleanupStagingBeforeManual() {
			return false
		}
		markManual("source_missing", "propagation source no longer exists")
		return false
	}
	if sourceMember.Status == models.CrossSeedPartialPoolMemberStatusManual ||
		sourceMember.Status == models.CrossSeedPartialPoolMemberStatusRemoved ||
		(sourceFile.Status != models.CrossSeedPartialPoolFileStatusAvailable && sourceFile.Status != models.CrossSeedPartialPoolFileStatusVerified) {
		if !cleanupStagingBeforeManual() {
			return false
		}
		markManual("source_unavailable", "persisted propagation source is no longer available")
		return false
	}
	if s.partialPoolPropagationPairRejected(sourceMember, sourceFile, targetMember, targetFile) {
		resetRejectedPairClaim()
		return false
	}
	sourceSnapshot := snapshots[sourceMember.ID]
	if sourceSnapshot == nil || targetSnapshot == nil || len(sourceSnapshot.files) == 0 || len(targetSnapshot.files) == 0 {
		return false
	}
	if !s.partialPoolCoordinatorEnabled(ctx) ||
		!s.refreshPartialPoolPropagationMembers(ctx, snapshots, refreshed, sourceMember, targetMember) {
		return false
	}
	sourceCurrent := sourceSnapshot.fileByIndex[sourceFile.FileIndex]
	targetCurrent := targetSnapshot.fileByIndex[targetFile.FileIndex]
	if sourceCurrent.Priority == 0 || targetCurrent.Priority == 0 {
		s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{SourceFileID: models.NullableInt64Update{Set: true}})
		return false
	}
	targetState := targetSnapshot.torrent.State
	if partialPoolChecking(targetState) {
		return false
	}
	if targetState == qbt.TorrentStateError ||
		(targetState == qbt.TorrentStateMissingFiles && targetMember.Mode != models.CrossSeedPartialPoolModeReflink) ||
		(targetState != qbt.TorrentStateMissingFiles && !isPausedOrStopped(targetState)) {
		return false
	}
	if partialPoolChecking(sourceSnapshot.torrent.State) {
		return false
	}
	if sourceSnapshot.torrent.State == qbt.TorrentStateError || sourceSnapshot.torrent.State == qbt.TorrentStateMissingFiles || sourceCurrent.Progress < 1 {
		if !cleanupStagingBeforeManual() {
			return false
		}
		markManual("source_incomplete", "propagation source is no longer complete")
		return false
	}
	if !partialPoolFilesPair(sourceMember, targetMember, sourceFile, targetFile) {
		markManual("file_pair_changed", "persisted source and target no longer satisfy file pairing")
		return false
	}
	sourceModeEnabled, sourceModeErr := s.partialPoolMemberModeEnabled(ctx, sourceMember)
	if sourceModeErr != nil {
		targetSnapshot.stateRetryPending = true
		return false
	}
	targetModeEnabled, targetModeErr := s.partialPoolMemberModeEnabled(ctx, targetMember)
	if targetModeErr != nil {
		targetSnapshot.stateRetryPending = true
		return false
	}
	if !sourceModeEnabled || !targetModeEnabled {
		markManual("mode_disabled", "link mode or local filesystem access was disabled")
		return false
	}

	s.partialPoolAdmissionMaterializationMu.Lock()
	defer s.partialPoolAdmissionMaterializationMu.Unlock()
	currentPool, err := s.automationStore.GetPartialPool(ctx, pool.ID)
	if err != nil || !partialPoolPropagationAdmissionsCurrent(currentPool, sourceMember, sourceFile, targetMember, targetFile) {
		targetSnapshot.stateRetryPending = true
		return false
	}
	sourcePath, err := partialPoolLocalPath(sourceMember, sourceFile)
	if err != nil {
		markManual("unsafe_source_path", "unsafe source path: "+err.Error())
		return false
	}
	targetPath, err := partialPoolLocalPath(targetMember, targetFile)
	if err != nil {
		markManual("unsafe_target_path", "unsafe target path: "+err.Error())
		return false
	}
	sourceInfo, err := os.Lstat(sourcePath)
	if err != nil || !sourceInfo.Mode().IsRegular() || sourceInfo.Size() != sourceFile.SizeBytes {
		markManual("source_file_invalid", "propagation source is missing, moved, or has the wrong size")
		return false
	}
	log.Debug().
		Int64("poolID", targetMember.PoolID).
		Int64("targetMemberID", targetMember.ID).
		Int64("targetFileID", targetFile.ID).
		Int64("sourceMemberID", sourceMember.ID).
		Int64("sourceFileID", sourceFile.ID).
		Int64("sizeBytes", targetFile.SizeBytes).
		Str("mode", targetMember.Mode).
		Msg("Partial pool file propagation starting")

	replaceReflinkTarget := false
	if targetInfo, statErr := os.Lstat(targetPath); statErr == nil {
		if !targetInfo.Mode().IsRegular() {
			markManual("target_not_regular", "a non-regular target already exists")
			return false
		}
		if targetMember.Mode == models.CrossSeedPartialPoolModeHardlink {
			if !partialPoolSameFile(sourcePath, targetPath) {
				markManual("target_conflict", "a different target file already exists")
				return false
			}
			if !s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{}) {
				targetSnapshot.stateRetryPending = true
				return false
			}
			return true
		}
		if os.SameFile(sourceInfo, targetInfo) {
			markManual("source_target_alias", "reflink propagation target resolves to the source file")
			return false
		}
		if !targetFile.ReplaceableAtAdd {
			markManual("target_conflict", "a pre-existing target file cannot be replaced safely")
			return false
		}
		replaceReflinkTarget = true
	} else if !os.IsNotExist(statErr) {
		markManual("target_stat_failed", "target path could not be inspected: "+statErr.Error())
		return false
	}

	plan, err := hardlinktree.BuildSingleFilePlan(targetMember.RootPath, targetFile.RelativePath, sourcePath)
	if err != nil {
		markManual("plan_build_failed", err.Error())
		return false
	}
	if targetMember.Mode == models.CrossSeedPartialPoolModeReflink {
		// Reuse an ownership-marked target staging path so retries and later
		// registrations remove crash leftovers. Existing qBittorrent placeholders
		// remain intact until the same-filesystem rename succeeds.
		stagingRoot, stagingPath := partialPoolReflinkStagingPaths(targetPath)
		if stagingErr := ensurePartialPoolReflinkStagingRoot(stagingRoot); stagingErr != nil {
			markManual("staging_prepare_failed", "reflink staging could not be prepared: "+stagingErr.Error())
			return false
		}
		if removeErr := cleanPartialPoolReflinkStagingRoot(stagingRoot, sourcePath, targetPath); removeErr != nil {
			markManual("staging_cleanup_failed", "stale reflink target could not be removed: "+removeErr.Error())
			return false
		}
		defer func() {
			_ = cleanPartialPoolReflinkStagingRoot(stagingRoot, sourcePath, targetPath)
			_ = releasePartialPoolReflinkStagingRoot(stagingRoot)
		}()
		plan.RootDir = stagingRoot
		if replaceReflinkTarget {
			plan.Files[0].TargetPath = stagingPath
		}
	}
	if ctx.Err() != nil {
		return false
	}
	var created *hardlinktree.Created
	if targetMember.Mode == models.CrossSeedPartialPoolModeHardlink {
		created, err = hardlinktree.Create(plan)
	} else if s.reflinkMaterializer != nil {
		materialized, materializeErr := s.reflinkMaterializer(ctx, plan.RootDir, plan)
		if materialized != nil {
			created = &hardlinktree.Created{Files: materialized.Files, Dirs: materialized.Dirs}
		}
		err = materializeErr
	} else {
		created, err = reflinktree.Create(plan)
	}
	if err != nil {
		if partialPoolPropagationPairIncompatible(err) {
			s.rejectPartialPoolPropagationPair(sourceMember, sourceFile, targetMember, targetFile)
			resetRejectedPairClaim()
			log.Debug().
				Int64("poolID", targetMember.PoolID).
				Int64("targetMemberID", targetMember.ID).
				Int64("targetFileID", targetFile.ID).
				Int64("sourceMemberID", sourceMember.ID).
				Int64("sourceFileID", sourceFile.ID).
				Str("mode", targetMember.Mode).
				Msg("Partial pool propagation pair rejected across filesystems")
			return false
		}
		markManual("materialization_failed", "file propagation failed: "+err.Error())
		return false
	}
	if replaceReflinkTarget {
		if err := os.Rename(plan.Files[0].TargetPath, targetPath); err != nil {
			if rollbackErr := created.Rollback(); rollbackErr != nil {
				err = errors.Join(err, fmt.Errorf("rollback temporary reflink: %w", rollbackErr))
			}
			markManual("target_replace_failed", "reflink target could not be replaced: "+err.Error())
			return false
		}
	}
	if targetMember.Mode == models.CrossSeedPartialPoolModeHardlink {
		s.storePartialPoolCreated(targetFile.ID, created)
	}
	if !s.transitionPartialPoolFile(ctx, targetFile, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{}) {
		if targetMember.Mode == models.CrossSeedPartialPoolModeReflink {
			if replaceReflinkTarget && created != nil {
				created.Files = []string{targetPath}
			}
			if rollbackErr := s.rollbackPartialPoolPropagation(created); rollbackErr != nil {
				markManual("materialization_rollback_failed", "reflink propagation rollback failed: "+rollbackErr.Error())
				return false
			}
		}
		targetSnapshot.stateRetryPending = true
		return false
	}
	log.Debug().
		Int64("poolID", targetMember.PoolID).
		Int64("targetMemberID", targetMember.ID).
		Int64("targetFileID", targetFile.ID).
		Int64("sourceMemberID", sourceMember.ID).
		Int64("sourceFileID", sourceFile.ID).
		Str("mode", targetMember.Mode).
		Msg("Partial pool file propagation completed")
	return true
}

func (s *Service) rollbackPartialPoolPropagation(created *hardlinktree.Created) error {
	if s.partialPoolPropagationRollback != nil {
		return s.partialPoolPropagationRollback(created)
	}
	return created.Rollback()
}

func partialPoolPropagationAdmissionsCurrent(
	pool *models.CrossSeedPartialPool,
	sourceMember *models.CrossSeedPartialPoolMember,
	sourceFile *models.CrossSeedPartialPoolMemberFile,
	targetMember *models.CrossSeedPartialPoolMember,
	targetFile *models.CrossSeedPartialPoolMemberFile,
) bool {
	return partialPoolMemberFileAdmissionCurrent(pool, sourceMember, sourceFile) &&
		partialPoolMemberFileAdmissionCurrent(pool, targetMember, targetFile)
}

func partialPoolMemberFileAdmissionCurrent(
	pool *models.CrossSeedPartialPool,
	member *models.CrossSeedPartialPoolMember,
	file *models.CrossSeedPartialPoolMemberFile,
) bool {
	if member == nil || file == nil {
		return false
	}
	currentMember, currentFile := partialPoolFileByID(pool, file.ID)
	return currentMember != nil && currentFile != nil &&
		currentMember.ID == member.ID && currentMember.CreatedAt.Equal(member.CreatedAt) &&
		currentFile.CreatedAt.Equal(file.CreatedAt)
}

// partialPoolMemberModeEnabled returns false without an error only when loaded
// instance settings disable the member's local link mode. Lookup failures are retryable.
func (s *Service) partialPoolMemberModeEnabled(ctx context.Context, member *models.CrossSeedPartialPoolMember) (bool, error) {
	if member == nil {
		return false, errors.New("partial pool member is required")
	}
	if s == nil || s.instanceStore == nil {
		return false, errors.New("partial pool instance store is unavailable")
	}
	instance, err := s.instanceStore.Get(ctx, member.InstanceID)
	if err != nil {
		return false, fmt.Errorf("load partial pool member instance %d: %w", member.InstanceID, err)
	}
	if instance == nil {
		return false, fmt.Errorf("load partial pool member instance %d: empty result", member.InstanceID)
	}
	if !instance.HasLocalFilesystemAccess {
		return false, nil
	}
	if member.Mode == models.CrossSeedPartialPoolModeHardlink {
		return instance.UseHardlinks, nil
	}
	return member.Mode == models.CrossSeedPartialPoolModeReflink && instance.UseReflinks, nil
}

func (s *Service) markPartialPoolPropagationManual(ctx context.Context, member *models.CrossSeedPartialPoolMember, file *models.CrossSeedPartialPoolMemberFile, failureCategory, reason string) bool {
	event := log.Debug().
		Int64("poolID", member.PoolID).
		Int64("memberID", member.ID).
		Int64("fileID", file.ID).
		Str("mode", member.Mode).
		Str("failureCategory", failureCategory)
	if file.SourceFileID != nil {
		event.Int64("sourceFileID", *file.SourceFileID)
	}
	event.Msg("Partial pool file propagation failed")
	s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusManual, models.PartialPoolFileMutation{LastError: &reason})
	s.deletePartialPoolCreated(file.ID)
	return s.markPartialPoolMemberManual(ctx, member, reason)
}

// preparePartialPoolRecheckForPropagation waits for an outstanding check to be
// positively observed and settled before invalidating it. This prevents a
// delayed pre-propagation request from starting during filesystem writes.
func (s *Service) preparePartialPoolRecheckForPropagation(
	ctx context.Context,
	now time.Time,
	member *models.CrossSeedPartialPoolMember,
	snapshots map[int64]*partialPoolMemberSnapshot,
) bool {
	if member == nil || (member.LastError != partialPoolRecheckRequested && member.LastError != partialPoolRecheckObserved) {
		return true
	}
	if !s.refreshPartialPoolTorrentStates(ctx, snapshots, member) {
		return false
	}
	targetState := snapshots[member.ID].torrent.State
	if member.LastError == partialPoolRecheckRequested {
		if partialPoolDataChecking(targetState) {
			s.recordPartialPoolRecheckObserved(ctx, now, member)
			return false
		}
		return false
	}
	if partialPoolChecking(targetState) {
		return false
	}
	return s.invalidatePartialPoolRecheckForPropagation(ctx, member)
}

// invalidatePartialPoolRecheckForPropagation resets a settled, observed check
// before pending filesystem materialization makes its result stale. Persisting
// the reset before the side effect keeps crash recovery safe.
func (s *Service) invalidatePartialPoolRecheckForPropagation(ctx context.Context, member *models.CrossSeedPartialPoolMember) bool {
	if member == nil || member.LastError != partialPoolRecheckObserved {
		return false
	}
	previousState := member.LastError
	reason := partialPoolRecheckPending
	if !s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason}) {
		return false
	}
	log.Debug().
		Int64("poolID", member.PoolID).
		Int64("memberID", member.ID).
		Str("memberStatus", member.Status).
		Str("previousRecheckState", previousState).
		Msg("Partial pool recheck invalidated for pending file propagation")
	return true
}

func (s *Service) claimPartialPoolRecheck(ctx context.Context, member *models.CrossSeedPartialPoolMember) {
	if partialPoolMemberHasPropagationWork(member) {
		return
	}
	reason := partialPoolRecheckPending
	switch member.Status {
	case models.CrossSeedPartialPoolMemberStatusVerifying:
		if member.LastError != partialPoolRecheckPending {
			return
		}
	case models.CrossSeedPartialPoolMemberStatusWaiting, models.CrossSeedPartialPoolMemberStatusBlocked:
		if !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusRechecking, models.PartialPoolMemberMutation{LastError: &reason}) {
			return
		}
	case models.CrossSeedPartialPoolMemberStatusRechecking:
		s.transitionPartialPoolMember(ctx, member, member.Status, models.PartialPoolMemberMutation{LastError: &reason})
	default:
		return
	}
	s.signalPartialPoolWake(partialPoolWake{poolID: member.PoolID})
}

type partialPoolSelection struct {
	member        *models.CrossSeedPartialPoolMember
	verified      bool
	reusableBytes int64
	unlocked      int
	amountLeft    int64
}

// selectPartialPoolDownloader returns the highest-ranked member eligible to
// own the pool download. It waits for admission and active transfers. During
// lazy initial verification, it defers candidates with a live propagation
// source so pool data can be materialized before the first piece check.
func (s *Service) selectPartialPoolDownloader(ctx context.Context, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot, now time.Time) *models.CrossSeedPartialPoolMember {
	if !partialPoolAdmissionReady(pool, now) {
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
		if partialPoolMemberHasVerificationWork(member) {
			continue
		}
		deferredVerification := partialPoolInitialVerificationDeferred(member)
		if (member.Status != models.CrossSeedPartialPoolMemberStatusWaiting && !deferredVerification) || !partialPoolCooldownReady(member, now) {
			continue
		}
		snapshot := snapshots[member.ID]
		if snapshot == nil || len(snapshot.files) == 0 || !partialPoolMemberResumable(member, snapshot.torrent.State) {
			continue
		}
		missing := partialPoolMissingWantedFiles(member, snapshot)
		if len(missing) == 0 && !deferredVerification {
			continue
		}
		if deferredVerification {
			waitForPropagation := false
			for _, file := range missing {
				if s.partialPoolFileHasAvailableSource(ctx, pool, member, file, snapshots) {
					waitForPropagation = true
					break
				}
			}
			if waitForPropagation {
				continue
			}
		}
		selection := partialPoolSelection{member: member, verified: !deferredVerification, amountLeft: snapshot.torrent.AmountLeft}
		if deferredVerification {
			selection.amountLeft = member.MissingBytes
		}
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
		if a.verified != b.verified {
			return a.verified
		}
		if a.reusableBytes != b.reusableBytes {
			return a.reusableBytes > b.reusableBytes
		}
		if a.unlocked != b.unlocked {
			return a.unlocked > b.unlocked
		}
		if a.amountLeft != b.amountLeft {
			return a.amountLeft < b.amountLeft
		}
		if a.member.InstanceID != b.member.InstanceID {
			return a.member.InstanceID < b.member.InstanceID
		}
		return strings.ToLower(a.member.TorrentKey) < strings.ToLower(b.member.TorrentKey)
	})
	return selections[0].member
}

// partialPoolAdmissionWindowClosed reports whether every known admission has
// remained in the pool for the full batching window.
func partialPoolAdmissionWindowClosed(pool *models.CrossSeedPartialPool, now time.Time) bool {
	if pool == nil {
		return false
	}
	for _, member := range pool.Members {
		if member.Status != models.CrossSeedPartialPoolMemberStatusRemoved && now.Before(member.CreatedAt.Add(partialPoolAdmissionHold)) {
			return false
		}
	}
	return true
}

// partialPoolAdmissionReady holds downloader selection until the newest
// admission window has closed and every requested check has settled. Initial
// checks that have not been selected yet do not hold the pool.
func partialPoolAdmissionReady(pool *models.CrossSeedPartialPool, now time.Time) bool {
	if !partialPoolAdmissionWindowClosed(pool, now) {
		return false
	}
	for _, member := range pool.Members {
		if member.Status == models.CrossSeedPartialPoolMemberStatusRechecking ||
			(member.Status == models.CrossSeedPartialPoolMemberStatusVerifying && !partialPoolInitialVerificationDeferred(member)) {
			return false
		}
	}
	return true
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
	deferredVerification := partialPoolInitialVerificationDeferred(member)
	for _, file := range member.Files {
		current, ok := snapshot.fileByIndex[file.FileIndex]
		if !ok || current.Priority == 0 || file.Status == models.CrossSeedPartialPoolFileStatusManual {
			continue
		}
		if deferredVerification {
			if !file.WantedAtAdmission || file.MaterializedAtAdd || file.Status != models.CrossSeedPartialPoolFileStatusMissing {
				continue
			}
		} else if current.Progress >= 1 {
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

// partialPoolMemberHasPendingPropagationSource reports an eligible missing file
// that should wait for pool data. Checking sources count because their durable
// availability can be used after their current check settles.
func (s *Service) partialPoolMemberHasPendingPropagationSource(
	ctx context.Context,
	pool *models.CrossSeedPartialPool,
	targetMember *models.CrossSeedPartialPoolMember,
	targetSnapshot *partialPoolMemberSnapshot,
	snapshots map[int64]*partialPoolMemberSnapshot,
	initialVerification bool,
) bool {
	if targetMember == nil || targetSnapshot == nil || len(targetSnapshot.files) == 0 {
		return false
	}
	for _, targetFile := range targetMember.Files {
		if targetFile.Status != models.CrossSeedPartialPoolFileStatusMissing || targetFile.LastError != "" || targetFile.SizeBytes <= 0 {
			continue
		}
		targetCurrent, ok := targetSnapshot.fileByIndex[targetFile.FileIndex]
		if !ok || targetCurrent.Priority == 0 {
			continue
		}
		if initialVerification {
			if !targetFile.WantedAtAdmission || targetFile.MaterializedAtAdd {
				continue
			}
		} else if targetCurrent.Progress > 0 {
			continue
		}
		if s.partialPoolFileHasAvailableSource(ctx, pool, targetMember, targetFile, snapshots) {
			return true
		}
	}
	return false
}

// partialPoolFileHasAvailableSource reports whether a live peer has a paired,
// wanted file durably marked available or verified, currently complete or
// checking, and not rejected for this target in the current process.
func (s *Service) partialPoolFileHasAvailableSource(ctx context.Context, pool *models.CrossSeedPartialPool, targetMember *models.CrossSeedPartialPoolMember, targetFile *models.CrossSeedPartialPoolMemberFile, snapshots map[int64]*partialPoolMemberSnapshot) bool {
	for _, sourceMember := range pool.Members {
		if sourceMember.ID == targetMember.ID || sourceMember.Status == models.CrossSeedPartialPoolMemberStatusManual || sourceMember.Status == models.CrossSeedPartialPoolMemberStatusRemoved {
			continue
		}
		sourceSnapshot := snapshots[sourceMember.ID]
		modeEnabled, modeErr := s.partialPoolMemberModeEnabled(ctx, sourceMember)
		if modeErr != nil {
			if targetSnapshot := snapshots[targetMember.ID]; targetSnapshot != nil {
				targetSnapshot.stateRetryPending = true
			}
			continue
		}
		if !modeEnabled {
			continue
		}
		if sourceSnapshot == nil || len(sourceSnapshot.files) == 0 || sourceSnapshot.torrent.State == qbt.TorrentStateError || sourceSnapshot.torrent.State == qbt.TorrentStateMissingFiles {
			continue
		}
		sourceChecking := partialPoolChecking(sourceSnapshot.torrent.State)
		for _, sourceFile := range sourceMember.Files {
			if sourceFile.Status != models.CrossSeedPartialPoolFileStatusAvailable && sourceFile.Status != models.CrossSeedPartialPoolFileStatusVerified {
				continue
			}
			current, ok := sourceSnapshot.fileByIndex[sourceFile.FileIndex]
			if !ok || current.Priority == 0 || (!sourceChecking && current.Progress < 1) {
				continue
			}
			if partialPoolFilesPair(sourceMember, targetMember, sourceFile, targetFile) &&
				!s.partialPoolPropagationPairRejected(sourceMember, sourceFile, targetMember, targetFile) {
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

// selectAndResumePartialPoolDownloader reports whether an eligible downloader
// could not be durably claimed and needs the active recovery schedule.
func (s *Service) selectAndResumePartialPoolDownloader(ctx context.Context, now time.Time, pool *models.CrossSeedPartialPool, snapshots map[int64]*partialPoolMemberSnapshot, budget int64) bool {
	member := s.selectPartialPoolDownloader(ctx, pool, snapshots, now)
	for _, snapshot := range snapshots {
		if snapshot != nil && snapshot.stateRetryPending {
			return true
		}
	}
	if member == nil || ctx.Err() != nil {
		return false
	}
	snapshot := snapshots[member.ID]
	if partialPoolInitialVerificationDeferred(member) {
		s.requestPartialPoolRecheck(ctx, now, member)
		return false
	}
	status, reason := partialPoolPostRecheckVerdict(member, snapshot, budget, normalizerForService(s))
	if status != models.CrossSeedPartialPoolMemberStatusWaiting {
		if status != member.Status {
			empty := ""
			if !s.transitionPartialPoolMember(ctx, member, status, models.PartialPoolMemberMutation{LastError: choosePartialPoolError(status, reason, &empty)}) {
				return true
			}
		}
		return false
	}
	modeEnabled, modeErr := s.partialPoolMemberModeEnabled(ctx, member)
	if modeErr != nil {
		return true
	}
	if !modeEnabled {
		reason = "link mode or local filesystem access was disabled"
		return !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason})
	}
	claimed, err := s.automationStore.ClaimPartialPoolDownloader(ctx, member.ID, snapshot.torrent.Downloaded, member.CreatedAt, now, now.Add(-partialPoolAdmissionHold))
	if err != nil {
		if ctx.Err() == nil {
			log.Warn().Err(err).Int64("poolID", pool.ID).Int64("memberID", member.ID).Msg("Failed to claim partial pool downloader")
		}
		return true
	}
	if !claimed {
		return true
	}
	log.Debug().
		Int64("poolID", pool.ID).
		Int64("memberID", member.ID).
		Int64("amountLeft", snapshot.torrent.AmountLeft).
		Int64("autoResumeBudgetBytes", budget).
		Msg("Partial pool downloader claimed")
	member.Status = models.CrossSeedPartialPoolMemberStatusAcquiring
	member.StartedByPool = true
	member.LastDownloadedBytes = &snapshot.torrent.Downloaded
	member.LastProgressAt = &now
	member.RetryAfter = nil
	member.LastError = ""
	settings, settingsErr := s.GetAutomationSettings(ctx)
	if settingsErr != nil || settings == nil || !settings.PooledPartialCompletionEnabled || !s.refreshPartialPoolMemberSnapshot(ctx, member, snapshot) {
		s.releasePartialPoolDownloaderClaim(ctx, member, "pooled completion setting or current file evidence changed before resume")
		return true
	}
	currentBudget := int64(max(settings.AutoResumeMaxDownloadMB, 0)) << 20
	status, reason = partialPoolPostRecheckVerdict(member, snapshot, currentBudget, normalizerForService(s))
	if status != models.CrossSeedPartialPoolMemberStatusWaiting {
		if status == models.CrossSeedPartialPoolMemberStatusManual {
			if !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason}) {
				return true
			}
		} else {
			s.releasePartialPoolDownloaderClaim(ctx, member, reason)
		}
		return false
	}
	modeEnabled, modeErr = s.partialPoolMemberModeEnabled(ctx, member)
	if modeErr != nil {
		log.Warn().Err(modeErr).Int64("poolID", pool.ID).Int64("memberID", member.ID).Msg("Failed to reload partial pool member mode before resume")
		s.releasePartialPoolDownloaderClaim(ctx, member, "instance metadata unavailable before resume")
		return true
	}
	if !modeEnabled {
		reason = "link mode or local filesystem access was disabled"
		return !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason})
	}
	for _, file := range partialPoolMissingWantedFiles(member, snapshot) {
		if file.Status == models.CrossSeedPartialPoolFileStatusMissing {
			empty := ""
			s.transitionPartialPoolFile(ctx, file, models.CrossSeedPartialPoolFileStatusAcquiring, models.PartialPoolFileMutation{LastError: &empty})
		}
	}
	if ctx.Err() != nil {
		return false
	}
	s.requestPartialPoolResume(ctx, member)
	if strings.HasPrefix(member.LastError, partialPoolResumeExhausted) {
		reason = member.LastError
		if !s.transitionPartialPoolMember(ctx, member, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{LastError: &reason}) {
			return true
		}
	}
	return false
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

func partialPoolMemberHasPropagationWork(member *models.CrossSeedPartialPoolMember) bool {
	if member == nil {
		return false
	}
	for _, file := range member.Files {
		if file.Status == models.CrossSeedPartialPoolFileStatusPropagating {
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
	changed, err := s.automationStore.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{member.Status}, status, mutation)
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
	if mutation.ReviewPausePending != nil {
		member.ReviewPausePending = *mutation.ReviewPausePending
	}
	if mutation.ResumeAttempts.Set {
		member.ResumeAttempts = mutation.ResumeAttempts.Value
	}
	if mutation.RecoveryAttempts.Set {
		member.RecoveryAttempts = mutation.RecoveryAttempts.Value
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
	changed, err := s.automationStore.TransitionPartialPoolFile(ctx, file.ID, file.CreatedAt, []string{file.Status}, status, mutation)
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
