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
	// settleTimeout is how long the settle worker watches one torrent before it
	// leaves the torrent paused. It allows time for large recheck queues.
	settleTimeout       = 60 * time.Minute
	recheckPollInterval = 3 * time.Second // Batch API calls per instance
	// recheckResumeStablePolls is how many polls a state must hold before the
	// worker trusts it, when no checking state was seen.
	recheckResumeStablePolls      = 2
	maxMissingFilesResumeAttempts = 3
	// recheckFastCompleteMinElapsed is how long after queuing a verification-required
	// entry may resume on a settled 100% that never showed a checking state: long enough
	// that the recheck qui issued is treated as having run (#2554), well short of
	// settleTimeout.
	recheckFastCompleteMinElapsed = 30 * time.Second
)

// settleSync is what the settle module reads from and sends to qBittorrent.
type settleSync interface {
	GetTorrents(ctx context.Context, instanceID int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	GetTorrentPieceStates(ctx context.Context, instanceID int, hash string) ([]qbt.PieceState, error)
}

var _ settleSync = (*qbittorrent.SyncManager)(nil)

// Settler settles every torrent that qui adds with a recheck: it sends the
// recheck, waits for it, decides whether the torrent may resume, runs the
// linked-file check (ADR 0004), resumes the torrent, and confirms the resume.
// One worker polls every queued torrent.
type Settler struct {
	sync  settleSync
	runs  seasonPackRunCreator
	queue chan *pendingResume
}

// NewSettler builds the settle module. runs receives the season pack history
// row when the linked-file check blocks a season pack resume. Start runs the worker.
func NewSettler(sync settleSync, runs seasonPackRunCreator) *Settler {
	return &Settler{
		sync:  sync,
		runs:  runs,
		queue: make(chan *pendingResume, 100),
	}
}

// Start runs the settle worker until ctx ends.
func (s *Settler) Start(ctx context.Context) {
	go s.run(ctx)
}

// SettleGate is the rule that lets a settled torrent resume.
type SettleGate int

const (
	// GateBudget resumes when the bytes left fit AddedTorrent.Budget, or when
	// only irrelevant sidecar files exceed it. A budget of 0 needs a complete torrent.
	GateBudget SettleGate = iota
	// GateThreshold resumes when the verified progress reaches AddedTorrent.Threshold.
	GateThreshold
	// GateVerify resumes only after an observed full recheck to 100%.
	GateVerify
	// GateMonitor never resumes. It watches the full recheck and logs the result.
	GateMonitor
	// GateAfterRecheck resumes when the recheck is done, with no byte rule.
	GateAfterRecheck
	// GateComplete resumes only when nothing is left.
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

// AddedTorrent is a torrent that qui just added paused and that needs a recheck.
type AddedTorrent struct {
	InstanceID int
	// Hashes are the v1 and v2 hashes to recheck. The worker watches the first.
	Hashes []string
	// LinkedPaths holds the torrent paths that qui hardlinked before the add.
	// It is nil for reflink and regular adds, which skip the linked-file check.
	LinkedPaths map[string]struct{}
	Gate        SettleGate
	Budget      int64
	Threshold   float64
	// SkipAutoResume rechecks the torrent and leaves it paused.
	SkipAutoResume bool
	// RecoverMissingFiles resumes a budget-gated torrent once when qBittorrent
	// reports missing files. A reflink add with extras can need this to start.
	RecoverMissingFiles bool
	// SeasonPackRun is the season pack history row to write when the
	// linked-file check blocks the resume.
	SeasonPackRun *models.SeasonPackRun
}

// SettleOutcome is what the caller learns at once from Settle.
type SettleOutcome struct {
	// Skipped is true when SkipAutoResume leaves the torrent paused.
	Skipped bool
	// QueueFull is true when the worker could not take the torrent, so it
	// stays paused with no automatic resume.
	QueueFull bool
	// RecheckFailed is true when the first recheck call failed. A queued
	// torrent gets the recheck again on each poll.
	RecheckFailed bool
}

// StatusSuffix is the text a cross-seed result appends for this outcome.
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

// Settle sends the recheck and queues the torrent for the worker. It returns
// at once. The worker resumes the torrent when its gate and the linked-file
// check pass, and in all other cases leaves it paused and logs why.
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
		// Nothing else sends the failed recheck: watch it without a resume.
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
	// The worker owns req once it is sent, so the log reads it first.
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

// pendingResume tracks a torrent waiting for recheck to complete before resuming.
type pendingResume struct {
	instanceID int
	hash       string
	gate       SettleGate
	// monitorOnly observes a full recheck without ever resuming the torrent.
	monitorOnly bool
	// recheckPending marks a post-add recheck that failed; the worker sends it
	// once qBittorrent answers again.
	recheckPending bool
	// verificationRequired marks an ambiguous search match that must not trust
	// qBittorrent's optimistic pre-check completion state. The WebUI API exposes
	// no recheck generation, so the worker observes checking, then a 100% result.
	// A checking transition missed between polls falls back to a settled 100% once
	// recheckFastCompleteMinElapsed has passed since queuing; below that it stays paused.
	verificationRequired bool
	// afterRecheck resumes once the recheck is done, with no byte rule.
	afterRecheck bool
	// threshold is the verified-progress fraction required to resume.
	// Used by the season-pack flow, whose gate is "linked bytes verified".
	threshold float64
	// budgetBytes switches the entry to byte-budget mode (new cross-seed
	// additions): resume when missing data fits the budget, or when only
	// irrelevant sidecar files are missing (forgiveness). nil for the threshold
	// and after-recheck gates.
	budgetBytes        *int64
	forgivenessGranted bool
	// forgivenessEvalFailed marks that the LAST forgiveness or linked-file check
	// evaluation could not load its qBittorrent evidence; terminal branches keep
	// the entry and retry instead of dropping it on a transient error.
	forgivenessEvalFailed         bool
	addedAt                       time.Time
	recoverMissingFilesWithResume bool
	missingFilesResumeAttempts    int
	missingFilesResumeSucceeded   bool
	// qBittorrent may accept resume before its files-checked stop settles.
	// Keep watching until running state is stable, and retry if it stops again.
	resumeAttempts             int
	awaitingResumeConfirmation bool
	sawChecking                bool
	// recheckInterrupted disables the timed fallback after resume-data validation.
	recheckInterrupted   bool
	readyPolls           int
	resumeConfirmedPolls int
	// linkedPaths holds the torrent paths qui hardlinked before the add; nil for
	// reflink and regular adds. A linked file that rechecks below 100% blocks the
	// resume unless every failed piece straddles a pending file: downloading it
	// would write into the source through the shared inode. Paths, not indexes:
	// qBittorrent drops pad files from its file list and renumbers.
	linkedPaths map[string]struct{}
	// blockedLinkedFile names the linked file that tripped the check on the last
	// evaluation, with its missing bytes.
	blockedLinkedFile  string
	blockedLinkedBytes int64
	// blockedRun is the season pack history row to append when the check blocks,
	// since the apply row was written before the recheck ran. nil for cross-seed
	// adds, whose result already went back to the caller.
	blockedRun *models.SeasonPackRun
}

// leftPausedMsg names the linked file that tripped the linked-file check, else fallback.
func (req *pendingResume) leftPausedMsg(fallback string) string {
	if req.blockedLinkedFile == "" {
		return fallback
	}
	return fmt.Sprintf("Linked file %s does not match the torrent, left paused to protect the source", req.blockedLinkedFile)
}

func recheckResumeKey(instanceID int, hash string) string {
	return fmt.Sprintf("%d:%s", instanceID, normalizeHash(hash))
}

// pendingResumeBudgetForLog returns the budget for logging, -1 when the gate has no budget.
func pendingResumeBudgetForLog(req *pendingResume) int64 {
	if req.budgetBytes == nil {
		return -1
	}
	return *req.budgetBytes
}

// pendingResumeSatisfied reports whether the torrent's recheck outcome allows auto-resume.
// Threshold mode compares verified progress. Budget mode compares missing bytes against the
// budget, with a forgiveness pass when the shortfall beyond the budget sits in irrelevant
// sidecar files. Hardlink entries then pass the linked-file check.
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

// hardlinkResumeAllowed refuses the resume when a linked file has a failed piece
// that no pending file shares. A failed or empty read keeps the entry for a
// retry and does not resume: empty piece states would make every incomplete
// linked file look mismatched, which blocks the boundary packs the check must
// let through. Decision record: docs/adr/0004-hardlink-resume-never-writes-into-a-linked-file.md.
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

// mismatchedLinkedFile returns the first linked file whose failed pieces cannot all
// be explained by a piece it shares with a pending file, with its missing bytes.
// A piece range past the end of the piece list counts as mismatched.
func mismatchedLinkedFile(files qbt.TorrentFiles, pieces []qbt.PieceState, linked map[string]struct{}) (string, int64) {
	// Only a pending file's first and last piece can reach into a neighbour.
	// An empty file occupies no piece: qBittorrent reports it as [start, start-1].
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

// thresholdOrBudgetSatisfied applies the gate's own rule alone.
func (s *Settler) thresholdOrBudgetSatisfied(ctx context.Context, instanceID int, req *pendingResume, torrent qbt.Torrent) bool {
	if req.afterRecheck {
		// A torrent added paused reports nothing completed until its recheck runs.
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
		// The rescue monitor's 100% verdict also requires full reported progress;
		// other zero-budget resumes keep the pre-rescue missing-bytes rule.
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
	// Cache only positive verdicts: a negative one taken before the recheck
	// finished would otherwise pin the torrent paused forever.
	if req.forgivenessGranted {
		return true
	}
	granted, evalOK := s.missingRelevantBytesWithinBudget(ctx, instanceID, req.hash, budget)
	if !evalOK {
		req.forgivenessEvalFailed = true
		return false
	}
	if granted {
		// Cache only post-check verdicts: a recheck can start and finish between
		// polls, so a grant earned before one was observed must not pin later polls.
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

// missingRelevantBytesWithinBudget reports whether the missing bytes inside relevant wanted
// files fit the budget. Irrelevant sidecars (sample, nfo, subtitle, ...) are excluded from the
// sum: per-file progress is piece-based, so a fully present file next to a missing sidecar
// reports slightly under 1 and only its boundary-piece bytes count against the budget.
// The second return value is false when the file list could not be loaded; the caller keeps
// the queue entry and retries on the next poll instead of treating the error as a verdict.
func (s *Settler) missingRelevantBytesWithinBudget(ctx context.Context, instanceID int, hash string, budget int64) (bool, bool) {
	ctx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	defer cancel()

	// The files cache can hold a pre-recheck snapshot (alignment warms it);
	// the verdict must come from post-recheck per-file progress.
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

	// Lazy so the file-list fetch behind forgiveness only happens at a decision point.
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
		// A fast (100%-overlap) recheck can finish between polls, so checking is never
		// observed and this would wait out settleTimeout. After the heuristic
		// delay, treat a settled 100% as verified and fall through to the normal resume
		// path (still gated by recheckResumeStablePolls); monitorOnly never resumes. Once
		// resume is issued, awaitingResumeConfirmation takes over so the entry is
		// confirmed and dropped rather than waiting out the timeout while it runs.
		fastRecheckComplete := !req.monitorOnly && !req.recheckInterrupted && isPausedOrStopped(state) &&
			satisfied() && time.Since(req.addedAt) >= recheckFastCompleteMinElapsed
		if !fastRecheckComplete {
			return true
		}
	}
	if req.monitorOnly {
		return monitorVerdict(instanceID, hash, req, torrent, isChecking, satisfied)
	}

	// qBittorrent can leave a newly added reflink-with-extras torrent in
	// missingFiles until it is started. Nudge after transient failures, then keep watching.
	if state == qbt.TorrentStateMissingFiles && req.recoverMissingFilesWithResume {
		return s.nudgeMissingFiles(ctx, instanceID, hash, req, progress)
	}

	if req.awaitingResumeConfirmation {
		return s.confirmResume(ctx, instanceID, hash, req, torrent, isChecking, satisfied)
	}

	// A missingFiles recovery nudge can leave an over-budget torrent downloading.
	// The check is cheap arithmetic on purpose: fetching the file list for a
	// forgiveness verdict can stall for recheckAPITimeout while the torrent keeps
	// downloading, so pause first. The paused flow then evaluates forgiveness and
	// resumes when it passes.
	if req.budgetBytes != nil && req.missingFilesResumeSucceeded &&
		isDownloadingOrQueued(state) &&
		torrent.AmountLeft > *req.budgetBytes && !req.forgivenessGranted {
		s.pauseOverBudget(ctx, instanceID, hash, req, torrent)
		return true
	}

	// Resume if the recheck outcome allows it and the torrent is not checking
	if !isChecking && satisfied() {
		req.readyPolls++
		if !req.sawChecking && req.readyPolls < recheckResumeStablePolls {
			return true
		}
		return s.resumePendingRecheck(ctx, instanceID, hash, req, progress, state)
	}

	// Below-threshold torrents that are still queued/downloading can improve.
	// Keep watching until they reach threshold or timeout.
	if isDownloadingOrQueued(state) {
		return true
	}

	// If recheck completed (not checking) with some progress but the outcome does not
	// allow resuming, the torrent won't improve - remove it from queue.
	// Note: We can't do this for 0% progress since we can't distinguish
	// "queued for recheck" from "recheck completed with 0 matches".
	if !isChecking && progress > 0 && !satisfied() {
		if req.forgivenessEvalFailed {
			// Could not load the file list - retry on the next poll instead of
			// dropping the entry on a transient qBittorrent error.
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

	// Torrent not ready yet - either still checking or queued for recheck (0% progress).
	// Keep in queue until absolute timeout.
	return true
}

// observeChecking records a checking state on the entry and reports whether the
// torrent is checking.
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
		// checkingResumeData validates qBittorrent's saved state during startup.
		// Seeing it after we already observed a piece hash check means qBittorrent
		// restarted and re-validated: that breaks continuity with the check we saw,
		// so the earlier progress no longer proves the requested recheck ran. Block
		// the fast resume and re-earn confirmation from a fresh piece check.
		//
		// A checkingResumeData poll with no prior piece check is just normal
		// forced-recheck startup, which every recheck passes through before hashing.
		// A fast (100%-overlap) recheck can still finish between polls, so this must
		// not set recheckInterrupted or the fast path in processPendingRecheckResume
		// could never fire.
		req.sawChecking = false
		req.recheckInterrupted = true
		req.awaitingResumeConfirmation = false
	}
	req.readyPolls = 0
	req.resumeConfirmedPolls = 0
	// A recheck can reveal missing data a pre-check forgiveness pass never
	// saw; the verdict must be re-earned from post-check file progress.
	req.forgivenessGranted = false
	return true
}

// monitorVerdict logs the result of a monitored recheck. It never resumes.
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

// nudgeMissingFiles resumes a torrent that qBittorrent left in missingFiles.
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

// confirmResume watches a resumed torrent until it runs for
// recheckResumeStablePolls polls, and resumes it again if it stops.
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
			// Could not load the file list - retry on the next poll instead
			// of dropping the entry on a transient qBittorrent error.
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

// pauseOverBudget pauses a torrent that a missingFiles nudge left downloading
// over its budget.
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

// sendPendingRecheck retries a post-add recheck that failed. Until it lands the
// torrent's progress is a skip_checking guess, so the entry is not evaluated.
func (s *Settler) sendPendingRecheck(ctx context.Context, instanceID int, hash string, req *pendingResume, state qbt.TorrentState) bool {
	switch state { //nolint:exhaustive // only checking states change the send
	case qbt.TorrentStateCheckingResumeData:
		// Same order as the post-add wait: recheck only after resume data is validated.
		return true
	case qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingDl:
		// The failed call reached qBittorrent after all; a second recheck would restart it.
		// Count it as a seen piece check so a later restart trips the interruption guard.
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
	// Verification timing counts from the recheck, not from the failed add.
	req.addedAt = time.Now()
	log.Debug().
		Int("instanceID", instanceID).
		Str("hash", hash).
		Msg("Sent deferred post-add recheck")
	return true
}

// recordBlockedResume appends the linked-file check verdict to the season pack history.
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

// run is a single goroutine that processes all pending recheck resumes.
// Instead of spawning a goroutine per torrent, this worker manages all pending torrents
// in a map and checks them periodically, avoiding goroutine accumulation.
func (s *Settler) run(ctx context.Context) {
	pending := make(map[string]*pendingResume)
	ticker := time.NewTicker(recheckPollInterval)
	defer ticker.Stop()

	for {
		select {
		case req := <-s.queue:
			pending[recheckResumeKey(req.instanceID, req.hash)] = req

		case <-ticker.C:
			// One API call per instance with all pending hashes
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

// expirePending drops the entries past settleTimeout and groups the others by
// instance for batched API calls.
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

// pollInstance runs one step for each pending torrent of an instance.
func (s *Settler) pollInstance(ctx context.Context, pending map[string]*pendingResume, instanceID int, hashes []string) {
	torrentByHash, ok := s.torrentLookup(ctx, instanceID, hashes)
	if !ok {
		return
	}

	for _, hash := range hashes {
		key := recheckResumeKey(instanceID, hash)
		req := pending[key]
		if req == nil {
			// The step for another hash of the same torrent already took this entry.
			continue
		}
		torrent, found := torrentByHash[normalizeHash(hash)]
		if !found && req.recheckPending {
			// A stalled qBittorrent can hide a fresh add from the cache;
			// dropping it here would leave it stopped for good.
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

		canonicalHash, canonicalKey := rekeyPendingRecheckResume(pending, instanceID, hash, req, torrent)

		if !s.processPendingRecheckResume(ctx, instanceID, canonicalHash, req, torrent) {
			delete(pending, key)
			delete(pending, canonicalKey)
		}
	}
}

// torrentLookup gets the torrents for hashes, keyed by each of their hashes. When
// a hash is missing, it falls back to the full torrent list of the instance. It
// returns false when a call fails, so that no entry is dropped as gone.
func (s *Settler) torrentLookup(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.Torrent, bool) {
	apiCtx, cancel := context.WithTimeout(ctx, recheckAPITimeout)
	torrents, err := s.sync.GetTorrents(apiCtx, instanceID, qbt.TorrentFilterOptions{Hashes: hashes})
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
	allTorrents, allErr := s.sync.GetTorrents(allCtx, instanceID, qbt.TorrentFilterOptions{})
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

func buildTorrentVariantLookup(torrents []qbt.Torrent) map[string]qbt.Torrent {
	torrentByHash := make(map[string]qbt.Torrent, len(torrents))
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

func missingVariantLookupHash(torrentByHash map[string]qbt.Torrent, hashes []string) bool {
	for _, hash := range hashes {
		if _, found := torrentByHash[normalizeHash(hash)]; !found {
			return true
		}
	}
	return false
}
