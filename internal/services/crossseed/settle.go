// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"fmt"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	settleTimeout                 = 60 * time.Minute
	recheckPollInterval           = 3 * time.Second
	recheckResumeStablePolls      = 2
	maxMissingFilesResumeAttempts = 3
	recheckFastCompleteMinElapsed = 30 * time.Second
)

type settleSync interface {
	GetTorrentRefs(ctx context.Context, instanceID int, filter qbittorrent.TorrentRefFilter) ([]*qbt.Torrent, error)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	GetTorrentPieceStates(ctx context.Context, instanceID int, hash string) ([]qbt.PieceState, error)
}

var _ settleSync = (*qbittorrent.SyncManager)(nil)

type Settler struct {
	sync  settleSync
	runs  seasonPackRunCreator
	queue chan *pendingResume
}

func NewSettler(sync settleSync, runs seasonPackRunCreator) *Settler {
	return &Settler{
		sync:  sync,
		runs:  runs,
		queue: make(chan *pendingResume, 100),
	}
}

func (s *Settler) Start(ctx context.Context) {
	go s.run(ctx)
}

type SettleGate int

const (
	GateBudget SettleGate = iota
	GateThreshold
	GateVerify
	GateMonitor
	GateAfterRecheck
	GateComplete
)

func (g SettleGate) String() string {
	switch g {
	case GateThreshold:
		return "threshold"
	case GateVerify:
		return "verify"
	case GateMonitor:
		return "monitor"
	case GateAfterRecheck:
		return "after_recheck"
	case GateComplete:
		return "complete"
	default:
		return "budget"
	}
}

type AddedTorrent struct {
	InstanceID          int
	Hashes              []string
	LinkedPaths         map[string]struct{}
	Gate                SettleGate
	Budget              int64
	Threshold           float64
	SkipAutoResume      bool
	RecoverMissingFiles bool
	SeasonPackRun       *models.SeasonPackRun
}

type SettleOutcome struct {
	Skipped       bool
	QueueFull     bool
	RecheckFailed bool
}

func (o SettleOutcome) StatusSuffix() string {
	var suffix string
	switch {
	case o.QueueFull && o.Skipped && o.RecheckFailed:
		suffix = " - recheck failed and monitor queue full, manual intervention required"
	case o.QueueFull && o.Skipped:
		suffix = " - verification monitor queue full, manual review required"
	case o.QueueFull && o.RecheckFailed:
		suffix = " - recheck failed and auto-resume queue full, manual intervention required"
	case o.QueueFull:
		suffix = " - auto-resume queue full, manual resume required"
	}
	if o.Skipped {
		suffix += " - auto-resume skipped per settings"
	}
	return suffix
}

func (s *Settler) Settle(ctx context.Context, added AddedTorrent) SettleOutcome {
	recheckErr := s.sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), added.InstanceID, added.Hashes, "recheck")
	out := SettleOutcome{Skipped: added.SkipAutoResume, RecheckFailed: recheckErr != nil}
	if recheckErr != nil {
		log.Warn().
			Err(recheckErr).
			Int("instanceID", added.InstanceID).
			Strs("hashes", added.Hashes).
			Bool("skipAutoResume", added.SkipAutoResume).
			Msg("Failed to trigger recheck after add")
	}

	req := added.pendingResume(out.RecheckFailed)
	if added.SkipAutoResume && added.Gate != GateMonitor {
		if !out.RecheckFailed {
			log.Debug().
				Int("instanceID", added.InstanceID).
				Str("hash", req.hash).
				Stringer("gate", req.gate).
				Msg("Skipping auto-resume per user settings (recheck triggered)")
			return out
		}
		req = &pendingResume{
			instanceID:     added.InstanceID,
			hash:           req.hash,
			gate:           GateMonitor,
			monitorOnly:    true,
			budgetBytes:    new(int64),
			recheckPending: true,
		}
	}
	out.QueueFull = !s.enqueue(req)
	return out
}

func (added AddedTorrent) pendingResume(recheckFailed bool) *pendingResume {
	req := &pendingResume{
		instanceID:     added.InstanceID,
		hash:           added.Hashes[0],
		gate:           added.Gate,
		recheckPending: recheckFailed,
		linkedPaths:    added.LinkedPaths,
		blockedRun:     added.SeasonPackRun,
	}
	switch added.Gate {
	case GateBudget:
		req.budgetBytes = new(added.Budget)
		req.recoverMissingFilesWithResume = added.RecoverMissingFiles
	case GateThreshold:
		req.threshold = added.Threshold
	case GateVerify:
		req.budgetBytes = new(int64)
		req.verificationRequired = true
	case GateMonitor:
		req.budgetBytes = new(int64)
		req.monitorOnly = true
		req.verificationRequired = true
	case GateAfterRecheck:
		req.afterRecheck = true
	case GateComplete:
		req.budgetBytes = new(int64)
	}
	return req
}

func (s *Settler) enqueue(req *pendingResume) bool {
	req.addedAt = time.Now()
	logged := log.With().
		Int("instanceID", req.instanceID).
		Str("hash", req.hash).
		Stringer("gate", req.gate).
		Logger()
	budget := pendingResumeBudgetForLog(req)
	threshold, recoverMissingFiles := req.threshold, req.recoverMissingFilesWithResume
	select {
	case s.queue <- req:
		logged.Debug().
			Float64("threshold", threshold).
			Int64("budgetBytes", budget).
			Bool("recoverMissingFilesWithResume", recoverMissingFiles).
			Msg("Added torrent to recheck resume queue")
		return true
	default:
		logged.Warn().Msg("Recheck resume channel full, skipping queue")
		return false
	}
}

type pendingResume struct {
	instanceID                    int
	hash                          string
	gate                          SettleGate
	monitorOnly                   bool
	recheckPending                bool
	verificationRequired          bool
	afterRecheck                  bool
	threshold                     float64
	budgetBytes                   *int64
	forgivenessGranted            bool
	forgivenessEvalFailed         bool
	addedAt                       time.Time
	recoverMissingFilesWithResume bool
	missingFilesResumeAttempts    int
	missingFilesResumeSucceeded   bool
	resumeAttempts                int
	awaitingResumeConfirmation    bool
	sawChecking                   bool
	recheckInterrupted            bool
	readyPolls                    int
	resumeConfirmedPolls          int
	linkedPaths                   map[string]struct{}
	blockedLinkedFile             string
	blockedLinkedBytes            int64
	blockedRun                    *models.SeasonPackRun
}

func (req *pendingResume) leftPausedMsg(fallback string) string {
	if req.blockedLinkedFile == "" {
		return fallback
	}
	return fmt.Sprintf("Linked file %s does not match the torrent, left paused to protect the source", req.blockedLinkedFile)
}

func recheckResumeKey(instanceID int, hash string) string {
	return fmt.Sprintf("%d:%s", instanceID, normalizeHash(hash))
}

func pendingResumeBudgetForLog(req *pendingResume) int64 {
	if req.budgetBytes == nil {
		return -1
	}
	return *req.budgetBytes
}

func (s *Settler) pendingResumeSatisfied(ctx context.Context, instanceID int, req *pendingResume, torrent qbt.Torrent) bool {
	req.forgivenessEvalFailed = false
	req.blockedLinkedFile, req.blockedLinkedBytes = "", 0
	if !s.thresholdOrBudgetSatisfied(ctx, instanceID, req, torrent) {
		return false
	}
	if req.linkedPaths == nil || torrent.AmountLeft <= 0 {
		return true
	}
	return s.hardlinkResumeAllowed(ctx, instanceID, req)
}

func (s *Settler) hardlinkResumeAllowed(ctx context.Context, instanceID int, req *pendingResume) bool {
	ctx, cancel := context.WithTimeout(qbittorrent.WithForceFilesRefresh(ctx), recheckAPITimeout)
	defer cancel()

	filesByHash, err := s.sync.GetTorrentFilesBatch(ctx, instanceID, []string{req.hash})
	files := filesByHash[normalizeHash(req.hash)]
	if err != nil || len(files) == 0 {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", req.hash).
			Msg("Linked-file check could not read the file list, will retry")
		req.forgivenessEvalFailed = true
		return false
	}
	pieces, err := s.sync.GetTorrentPieceStates(ctx, instanceID, req.hash)
	if err != nil || len(pieces) == 0 {
		log.Debug().Err(err).Int("instanceID", instanceID).Str("hash", req.hash).
			Msg("Linked-file check could not read the piece states, will retry")
		req.forgivenessEvalFailed = true
		return false
	}

	name, missing := mismatchedLinkedFile(files, pieces, req.linkedPaths)
	if name == "" {
		return true
	}
	req.blockedLinkedFile = name
	req.blockedLinkedBytes = missing
	return false
}

func mismatchedLinkedFile(files qbt.TorrentFiles, pieces []qbt.PieceState, linked map[string]struct{}) (string, int64) {
	shared := make([]bool, len(pieces))
	for _, file := range files {
		if _, ok := linked[file.Name]; ok || file.Size <= 0 || len(file.PieceRange) < 2 {
			continue
		}
		for _, p := range file.PieceRange[:2] {
			if p < len(shared) {
				shared[p] = true
			}
		}
	}
	for _, file := range files {
		if _, ok := linked[file.Name]; !ok || file.Progress >= 1 {
			continue
		}
		missing := int64((1 - float64(file.Progress)) * float64(file.Size))
		if len(file.PieceRange) < 2 || file.PieceRange[1] >= len(pieces) {
			return file.Name, missing
		}
		for p := file.PieceRange[0]; p <= file.PieceRange[1]; p++ {
			if pieces[p] != qbt.PieceStateAlreadyDownloaded && !shared[p] {
				return file.Name, missing
			}
		}
	}
	return "", 0
}

func (s *Settler) thresholdOrBudgetSatisfied(ctx context.Context, instanceID int, req *pendingResume, torrent qbt.Torrent) bool {
	if req.afterRecheck {
		return req.sawChecking || torrent.Completed > 0
	}
	if req.budgetBytes == nil {
		return torrent.Progress >= req.threshold
	}

	budget := *req.budgetBytes
	if req.verificationRequired {
		return torrent.Progress >= 1 && torrent.AmountLeft <= 0
	}
	if budget <= 0 {
		if req.monitorOnly {
			return torrent.Progress >= 1 && torrent.AmountLeft <= 0
		}
		return torrent.AmountLeft <= 0
	}
	if torrent.AmountLeft <= budget {
		return true
	}
	if torrent.AmountLeft > irrelevantResumeForgivenessCapBytes {
		return false
	}
	if req.forgivenessGranted {
		return true
	}
	granted, evalOK := s.missingRelevantBytesWithinBudget(ctx, instanceID, req.hash, budget)
	if !evalOK {
		req.forgivenessEvalFailed = true
		return false
	}
	if granted {
		if req.sawChecking {
			req.forgivenessGranted = true
		}
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", req.hash).
			Int64("amountLeft", torrent.AmountLeft).
			Int64("budgetBytes", budget).
			Msg("Auto-resume budget exceeded but the shortfall beyond it is only irrelevant files, allowing resume")
	}
	return granted
}

func (s *Settler) missingRelevantBytesWithinBudget(ctx context.Context, instanceID int, hash string, budget int64) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	defer cancel()

	ctx = qbittorrent.WithForceFilesRefresh(ctx)

	filesByHash, err := s.sync.GetTorrentFilesBatch(ctx, instanceID, []string{hash})
	if err != nil {
		return false, false
	}
	files := filesByHash[normalizeHash(hash)]
	if len(files) == 0 {
		return false, false
	}

	return relevantMissingFilesWithinBudget(files, budget, stringutils.DefaultNormalizer), true
}

func (s *Settler) processPendingRecheckResume(ctx context.Context, instanceID int, hash string, req *pendingResume, torrent qbt.Torrent) bool {
	if req.recheckPending {
		return s.sendPendingRecheck(ctx, instanceID, hash, req, torrent.State)
	}

	progress := torrent.Progress
	state := torrent.State

	var satisfiedResult *bool
	satisfied := func() bool {
		if satisfiedResult == nil {
			v := s.pendingResumeSatisfied(ctx, instanceID, req, torrent)
			satisfiedResult = &v
		}
		return *satisfiedResult
	}

	isChecking := observeChecking(req, state)
	if req.verificationRequired && !req.sawChecking && !req.awaitingResumeConfirmation {
		fastRecheckComplete := !req.monitorOnly && !req.recheckInterrupted && isPausedOrStopped(state) &&
			satisfied() && time.Since(req.addedAt) >= recheckFastCompleteMinElapsed
		if !fastRecheckComplete {
			return true
		}
	}
	if req.monitorOnly {
		return monitorVerdict(instanceID, hash, req, torrent, isChecking, satisfied)
	}

	if state == qbt.TorrentStateMissingFiles && req.recoverMissingFilesWithResume {
		return s.nudgeMissingFiles(ctx, instanceID, hash, req, progress)
	}

	if req.awaitingResumeConfirmation {
		return s.confirmResume(ctx, instanceID, hash, req, torrent, isChecking, satisfied)
	}

	if req.budgetBytes != nil && req.missingFilesResumeSucceeded &&
		isDownloadingOrQueued(state) &&
		torrent.AmountLeft > *req.budgetBytes && !req.forgivenessGranted {
		s.pauseOverBudget(ctx, instanceID, hash, req, torrent)
		return true
	}

	if !isChecking && satisfied() {
		req.readyPolls++
		if !req.sawChecking && req.readyPolls < recheckResumeStablePolls {
			return true
		}
		return s.resumePendingRecheck(ctx, instanceID, hash, req, progress, state)
	}

	if isDownloadingOrQueued(state) {
		return true
	}

	if !isChecking && progress > 0 && !satisfied() {
		if req.forgivenessEvalFailed {
			return true
		}
		log.Warn().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Stringer("gate", req.gate).
			Float64("progress", progress).
			Float64("threshold", req.threshold).
			Int64("amountLeft", torrent.AmountLeft).
			Int64("budgetBytes", pendingResumeBudgetForLog(req)).
			Str("linkedFile", req.blockedLinkedFile).
			Int64("linkedMissingBytes", req.blockedLinkedBytes).
			Msg(req.leftPausedMsg("Recheck completed below the resume gate, torrent left paused for manual review"))
		s.recordBlockedResume(ctx, req)
		return false
	}

	return true
}

func observeChecking(req *pendingResume, state qbt.TorrentState) bool {
	isPieceChecking := state == qbt.TorrentStateCheckingUp ||
		state == qbt.TorrentStateCheckingDl
	isChecking := isPieceChecking || state == qbt.TorrentStateCheckingResumeData
	if !isChecking {
		return false
	}
	if isPieceChecking {
		req.sawChecking = true
	} else if req.verificationRequired && req.sawChecking {
		req.sawChecking = false
		req.recheckInterrupted = true
		req.awaitingResumeConfirmation = false
	}
	req.readyPolls = 0
	req.resumeConfirmedPolls = 0
	req.forgivenessGranted = false
	return true
}

func monitorVerdict(instanceID int, hash string, req *pendingResume, torrent qbt.Torrent, isChecking bool, satisfied func() bool) bool {
	if isChecking {
		return true
	}
	if satisfied() {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Stringer("gate", req.gate).
			Msg("Monitored recheck completed at 100%; torrent left paused per settings")
		return false
	}
	if torrent.Progress > 0 || req.sawChecking {
		log.Warn().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Stringer("gate", req.gate).
			Float64("progress", torrent.Progress).
			Int64("amountLeft", torrent.AmountLeft).
			Msg("Monitored recheck completed below 100%; torrent left paused for manual review")
		return false
	}
	return true
}

func (s *Settler) nudgeMissingFiles(ctx context.Context, instanceID int, hash string, req *pendingResume, progress float64) bool {
	if req.missingFilesResumeSucceeded {
		return true
	}
	if req.missingFilesResumeAttempts >= maxMissingFilesResumeAttempts {
		log.Warn().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Stringer("gate", req.gate).
			Int("attempts", req.missingFilesResumeAttempts).
			Msg("MissingFiles recheck recovery resume attempts exhausted")
		return false
	}

	req.missingFilesResumeAttempts++
	resumeCtx, resumeCancel := context.WithTimeout(ctx, recheckAPITimeout)
	err := s.sync.BulkAction(resumeCtx, instanceID, []string{hash}, "resume")
	resumeCancel()
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Float64("progress", progress).
			Int("attempt", req.missingFilesResumeAttempts).
			Int("maxAttempts", maxMissingFilesResumeAttempts).
			Stringer("gate", req.gate).
			Msg("Failed to resume missingFiles torrent during recheck recovery")
		return req.missingFilesResumeAttempts < maxMissingFilesResumeAttempts
	}

	req.missingFilesResumeSucceeded = true
	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Float64("progress", progress).
		Float64("threshold", req.threshold).
		Int("attempt", req.missingFilesResumeAttempts).
		Msg("Resumed missingFiles torrent to continue post-add recheck recovery")
	return true
}

func (s *Settler) confirmResume(ctx context.Context, instanceID int, hash string, req *pendingResume, torrent qbt.Torrent, isChecking bool, satisfied func() bool) bool {
	progress, state := torrent.Progress, torrent.State
	if isRecheckResumeConfirmed(state) {
		req.resumeConfirmedPolls++
		if req.resumeConfirmedPolls < recheckResumeStablePolls {
			return true
		}
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Float64("progress", progress).
			Float64("threshold", req.threshold).
			Str("state", string(state)).
			Int("attempts", req.resumeAttempts).
			Msg("Confirmed torrent resumed after recheck")
		return false
	}

	req.resumeConfirmedPolls = 0
	if isChecking {
		return true
	}

	if isPausedOrStopped(state) && !satisfied() {
		if req.forgivenessEvalFailed {
			return true
		}
		log.Warn().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Stringer("gate", req.gate).
			Float64("progress", progress).
			Float64("threshold", req.threshold).
			Int64("amountLeft", torrent.AmountLeft).
			Int64("budgetBytes", pendingResumeBudgetForLog(req)).
			Str("state", string(state)).
			Str("linkedFile", req.blockedLinkedFile).
			Int64("linkedMissingBytes", req.blockedLinkedBytes).
			Msg(req.leftPausedMsg("Recheck resume stopped below the resume gate, torrent left paused for manual review"))
		s.recordBlockedResume(ctx, req)
		return false
	}

	if isPausedOrStopped(state) && satisfied() {
		log.Debug().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Float64("progress", progress).
			Float64("threshold", req.threshold).
			Str("state", string(state)).
			Int("attempts", req.resumeAttempts).
			Msg("Torrent still stopped after recheck resume, retrying")
		return s.resumePendingRecheck(ctx, instanceID, hash, req, progress, state)
	}

	return true
}

func (s *Settler) pauseOverBudget(ctx context.Context, instanceID int, hash string, req *pendingResume, torrent qbt.Torrent) {
	pauseCtx, pauseCancel := context.WithTimeout(ctx, recheckAPITimeout)
	err := s.sync.BulkAction(pauseCtx, instanceID, []string{hash}, "pause")
	pauseCancel()
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Int64("amountLeft", torrent.AmountLeft).
			Int64("budgetBytes", pendingResumeBudgetForLog(req)).
			Msg("Failed to pause over-budget torrent after missingFiles recovery")
	}
}

func (s *Settler) sendPendingRecheck(ctx context.Context, instanceID int, hash string, req *pendingResume, state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only checking states change the send
	case qbt.TorrentStateCheckingResumeData:
		return true
	case qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl:
		req.recheckPending = false
		req.sawChecking = true
		return true
	}
	ctx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	err := s.sync.BulkAction(ctx, instanceID, []string{hash}, "recheck")
	cancel()
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Msg("Deferred post-add recheck failed, will retry")
		return true
	}
	req.recheckPending = false
	req.addedAt = time.Now()
	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Msg("Sent deferred post-add recheck")
	return true
}

func (s *Settler) recordBlockedResume(ctx context.Context, req *pendingResume) {
	if req.blockedRun == nil || req.blockedLinkedFile == "" || s.runs == nil {
		return
	}
	run := req.blockedRun
	run.Status, run.Reason, run.Message = "failed", "linked_file_mismatch", req.leftPausedMsg("")
	ctx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	defer cancel()
	if _, err := s.runs.Create(ctx, run); err != nil {
		log.Warn().Err(err).Str("hash", req.hash).Msg("failed to record blocked season pack resume")
	}
}

func (s *Settler) resumePendingRecheck(ctx context.Context, instanceID int, hash string, req *pendingResume, progress float64, state qbt.TorrentState) bool {
	if req.resumeAttempts >= maxRecheckResumeAttempts {
		log.Warn().
			Int("instanceID", instanceID).
			Str("hash", hash).
			Stringer("gate", req.gate).
			Float64("progress", progress).
			Float64("threshold", req.threshold).
			Str("state", string(state)).
			Int("attempts", req.resumeAttempts).
			Msg("Recheck resume attempts exhausted, torrent left for manual review")
		return false
	}

	req.resumeAttempts++
	resumeCtx, resumeCancel := context.WithTimeout(ctx, recheckAPITimeout)
	err := s.sync.BulkAction(resumeCtx, instanceID, []string{hash}, "resume")
	resumeCancel()
	if err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", instanceID).
			Str("hash", hash).
			Float64("progress", progress).
			Float64("threshold", req.threshold).
			Str("state", string(state)).
			Int("attempt", req.resumeAttempts).
			Int("maxAttempts", maxRecheckResumeAttempts).
			Stringer("gate", req.gate).
			Msg("Failed to resume torrent after recheck")
		return req.resumeAttempts < maxRecheckResumeAttempts
	}

	req.awaitingResumeConfirmation = true
	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Float64("progress", progress).
		Float64("threshold", req.threshold).
		Str("state", string(state)).
		Int("attempt", req.resumeAttempts).
		Msg("Resumed torrent after recheck completed, awaiting confirmation")
	return true
}

func (s *Settler) run(ctx context.Context) {
	pending := make(map[string]*pendingResume)
	ticker := time.NewTicker(recheckPollInterval)
	defer ticker.Stop()

	for {
		select {
		case req := <-s.queue:
			pending[recheckResumeKey(req.instanceID, req.hash)] = req

		case <-ticker.C:
			for instanceID, hashes := range expirePending(pending) {
				s.pollInstance(ctx, pending, instanceID, hashes)
			}

		case <-ctx.Done():
			log.Debug().
				Int("pendingCount", len(pending)).
				Msg("Recheck resume worker shutting down")
			return
		}
	}
}

func expirePending(pending map[string]*pendingResume) map[int][]string {
	byInstance := make(map[int][]string)
	for key, req := range pending {
		if time.Since(req.addedAt) > settleTimeout {
			message := "Recheck resume absolute timeout reached, removing from queue"
			switch {
			case req.recheckPending:
				message = "Deferred post-add recheck was never accepted, torrent left paused for manual review"
			case req.verificationRequired && !req.sawChecking:
				message = "Verification recheck transition was not observed, torrent left paused for manual review"
			}
			log.Warn().
				Int("instanceID", req.instanceID).
				Str("hash", req.hash).
				Stringer("gate", req.gate).
				Dur("elapsed", time.Since(req.addedAt)).
				Msg(message)
			delete(pending, key)
			continue
		}
		byInstance[req.instanceID] = append(byInstance[req.instanceID], req.hash)
	}
	return byInstance
}

func (s *Settler) pollInstance(ctx context.Context, pending map[string]*pendingResume, instanceID int, hashes []string) {
	torrentByHash, ok := s.torrentLookup(ctx, instanceID, hashes)
	if !ok {
		return
	}

	for _, hash := range hashes {
		key := recheckResumeKey(instanceID, hash)
		req := pending[key]
		if req == nil {
			continue
		}
		ref, found := torrentByHash[normalizeHash(hash)]
		if !found && req.recheckPending {
			continue
		}
		if !found {
			log.Debug().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Msg("Torrent not found during recheck resume, removing from queue")
			delete(pending, key)
			continue
		}

		torrent := *ref
		canonicalHash, canonicalKey := rekeyPendingRecheckResume(pending, instanceID, hash, req, torrent)

		if !s.processPendingRecheckResume(ctx, instanceID, canonicalHash, req, torrent) {
			delete(pending, key)
			delete(pending, canonicalKey)
		}
	}
}

func (s *Settler) torrentLookup(ctx context.Context, instanceID int, hashes []string) (map[string]*qbt.Torrent, bool) {
	apiCtx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	torrents, err := s.sync.GetTorrentRefs(apiCtx, instanceID, qbittorrent.TorrentRefFilter{Hashes: hashes})
	cancel()
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Int("hashCount", len(hashes)).
			Msg("Failed to get torrent states during recheck resume, will retry")
		return nil, false
	}

	torrentByHash := buildTorrentVariantLookup(torrents)
	if !missingVariantLookupHash(torrentByHash, hashes) {
		return torrentByHash, true
	}
	allCtx, allCancel := context.WithTimeout(ctx, recheckAPITimeout)
	allTorrents, allErr := s.sync.GetTorrentRefs(allCtx, instanceID, qbittorrent.TorrentRefFilter{})
	allCancel()
	if allErr != nil {
		log.Debug().
			Err(allErr).
			Int("instanceID", instanceID).
			Int("hashCount", len(hashes)).
			Msg("Failed to get full torrent state map during recheck resume, will retry")
		return nil, false
	}
	return buildTorrentVariantLookup(allTorrents), true
}

func rekeyPendingRecheckResume(
	pending map[string]*pendingResume,
	instanceID int,
	queuedHash string,
	req *pendingResume,
	torrent qbt.Torrent,
) (string, string) {
	canonicalHash := canonicalRecheckResumeHash(queuedHash, torrent)
	canonicalKey := recheckResumeKey(instanceID, canonicalHash)
	key := recheckResumeKey(instanceID, queuedHash)
	if canonicalKey != key {
		delete(pending, key)
		pending[canonicalKey] = req
	}
	req.hash = canonicalHash
	return canonicalHash, canonicalKey
}

func canonicalRecheckResumeHash(queuedHash string, torrent qbt.Torrent) string {
	if canonicalHash := normalizeHash(torrent.Hash); canonicalHash != "" {
		return canonicalHash
	}
	return normalizeHash(queuedHash)
}

func buildTorrentVariantLookup(torrents []*qbt.Torrent) map[string]*qbt.Torrent {
	torrentByHash := make(map[string]*qbt.Torrent, len(torrents))
	for _, torrent := range torrents {
		for _, hash := range []string{torrent.Hash, torrent.InfohashV1, torrent.InfohashV2} {
			normalized := normalizeHash(hash)
			if normalized == "" {
				continue
			}
			torrentByHash[normalized] = torrent
		}
	}
	return torrentByHash
}

func missingVariantLookupHash(torrentByHash map[string]*qbt.Torrent, hashes []string) bool {
	for _, hash := range hashes {
		if _, found := torrentByHash[normalizeHash(hash)]; !found {
			return true
		}
	}
	return false
}
