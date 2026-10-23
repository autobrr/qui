// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"fmt"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// adderSync is what the Adder sends to qBittorrent.
type adderSync interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
}

var _ adderSync = (*qbittorrent.SyncManager)(nil)

// Adder does every cross-seed add: the Pre-add check, the Add verdict, the add,
// the resume or the hand-off to Settle, and the External Program run. A caller
// gives facts and placement, never a state option or a Settle resume rule.
type Adder struct {
	sync               adderSync
	blocklist          *models.CrossSeedBlocklistStore
	settler            *Settler
	resumeBudget       func(context.Context) int64
	runExternalProgram func(ctx context.Context, instanceID int, hashes []string)
}

// NewAdder builds the Adder. resumeBudget returns the Max auto-start download
// budget, and runExternalProgram sends the Execution Request after an add.
func NewAdder(
	sync adderSync,
	blocklist *models.CrossSeedBlocklistStore,
	settler *Settler,
	resumeBudget func(context.Context) int64,
	runExternalProgram func(ctx context.Context, instanceID int, hashes []string),
) *Adder {
	return &Adder{
		sync:               sync,
		blocklist:          blocklist,
		settler:            settler,
		resumeBudget:       resumeBudget,
		runExternalProgram: runExternalProgram,
	}
}

// VerifyReason is why a match must pass a full recheck before it seeds.
type VerifyReason int

const (
	VerifyNone VerifyReason = iota
	VerifyTitleRescue
	VerifyRelaxed
	VerifyManual
)

// AddFacts are what a caller knows about one cross-seed add. A caller can ask
// for the Add verdict early with the facts it has so far.
type AddFacts struct {
	// PendingFiles is set when some source files are not on disk.
	PendingFiles bool
	// Align is set when the caller renames files to the matched torrent after the add.
	Align bool
	// RenameOnly is set when each aligned file exists at the matched size.
	RenameOnly bool
	// DiscMarker is the disc layout folder (BDMV, VIDEO_TS), or empty.
	DiscMarker string
	Verify     VerifyReason
	// ExactMatch is set when the match type is exact.
	ExactMatch bool
	// LinkFallback is set when hardlink or reflink mode fell back to a regular add.
	LinkFallback bool
	// LinkFallbackRecheck is set when link mode asked for a full recheck after its fallback.
	LinkFallbackRecheck bool
	// LinkTree is set when hardlink or reflink mode built a link tree for the add.
	LinkTree bool
	// PoolMember is set when a partial link tree joins a partial pool, which resumes it.
	PoolMember bool
	// LinkedPaths are the hardlinked files that Settle checks before the resume.
	LinkedPaths map[string]struct{}
	// RecoverMissingFiles lets Settle resume a torrent that reports missing files.
	RecoverMissingFiles bool
	// ManualAssembly is set for a season pack that the user assembles from
	// selected episodes. It skips the blocklist and always waits for a recheck.
	ManualAssembly bool
	// SeasonPackLinked is the byte fraction that a season-pack plan linked.
	// Settle resumes the pack when the recheck reaches it.
	SeasonPackLinked float64
	// SeasonPackRun is the season pack history row that Settle writes when the
	// linked-file check blocks the resume.
	SeasonPackRun *models.SeasonPackRun

	// The settings of the Trigger.
	SkipRecheck    bool
	SkipAutoResume bool
	StartRunning   bool
	// DirScan is set for a Dir Scan add. Dir Scan has no Max auto-start download
	// budget: it adds an alignment paused, resumes a link tree with Pending files
	// after its recheck, and resumes each other add in Settle at 100%.
	DirScan bool
}

// AddVerdict is the decision for one cross-seed add: refuse with a status,
// add running, or add paused for Settle.
type AddVerdict struct {
	Refused bool
	// Status and Message describe a refusal.
	Status  string
	Message string

	paused       bool
	skipChecking bool
	settle       bool
	gate         SettleGate
	linkRecheck  bool
}

// Verdict makes the Add verdict from the facts. A later fact only adds a
// reason for a recheck, except for the link-fallback rule and pool membership.
// A pool member skips Settle, because its partial pool resumes it.
func (f AddFacts) Verdict() AddVerdict {
	disc := f.DiscMarker != ""
	exactInPlace := f.ExactMatch && !f.Align && !f.PendingFiles
	linkRecheck := f.LinkFallbackRecheck || (f.LinkFallback && !exactInPlace)
	// A byte-complete pair needs no recheck after a link-mode fallback: every file
	// exists at the matched size, and the renames of a rename-only pair are
	// verified. Link mode itself would add such a pair without a recheck (#2272, #3028).
	if f.SkipRecheck && (f.RenameOnly || exactInPlace) {
		linkRecheck = false
	}
	alignRecheck := (f.Align || f.PendingFiles) && (!f.SkipRecheck || !f.RenameOnly)
	verify := f.Verify != VerifyNone
	dirScanPendingTree := f.DirScan && f.LinkTree && f.PendingFiles

	if f.SkipRecheck && !f.ManualAssembly && (verify || alignRecheck || disc || linkRecheck) {
		return AddVerdict{Refused: true, Status: "skipped_recheck", Message: skippedRecheckMessage}
	}

	v := AddVerdict{
		// A rename-only add with Skip recheck on must not run before its renames land.
		// Dir Scan renames the paths of an alignment before qBittorrent uses them.
		paused: !f.StartRunning || verify || disc || linkRecheck || (f.SkipRecheck && f.RenameOnly) || (f.DirScan && f.Align),
		// qBittorrent blocks renames while it checks a torrent, so an add that aligns
		// skips the check and Settle rechecks after the renames.
		skipChecking: !f.PendingFiles || f.Align,
		settle:       verify || disc || linkRecheck || alignRecheck || f.ManualAssembly,
		linkRecheck:  linkRecheck,
	}
	if f.LinkTree {
		// Every linked file is in place, so qBittorrent skips its check and Settle
		// rechecks for the Pending files. A link-mode add ignores Start paused.
		// qBittorrent checks a manual assembly on add.
		// The after-recheck gate of Dir Scan reads the completed bytes, which
		// skip_checking reports as done before the recheck runs.
		v.skipChecking = !f.ManualAssembly && !dirScanPendingTree
		v.paused = f.SkipAutoResume || v.settle
	}
	if f.PoolMember {
		// The partial pool resumes its members, so a member skips Settle.
		v.paused, v.settle = true, false
	}
	switch {
	case f.SeasonPackLinked > 0:
		v.gate = GateThreshold
	case f.SkipAutoResume && f.Verify == VerifyTitleRescue:
		v.gate = GateMonitor
	case verify:
		v.gate = GateVerify
	case dirScanPendingTree:
		// Download missing files lets the tree fetch its Pending files, also for a disc layout.
		v.gate = GateAfterRecheck
	case disc || linkRecheck || f.DirScan:
		v.gate = GateComplete
	default:
		v.gate = GateBudget
	}
	return v
}

// PreAddCheck is the result of the blocklist check and the check for an
// existing torrent. Add takes it, so no add skips the check.
type PreAddCheck struct {
	instanceID int
	hashes     []string
}

// AddOutcome is what happened to one cross-seed add.
type AddOutcome int

const (
	AddRefused AddOutcome = iota
	AddAdded
	AddSettling
	// AddPooled means the torrent waits paused for its partial pool.
	AddPooled
	// AddLeftPaused means AfterAdd failed, so the torrent stays paused.
	AddLeftPaused
)

// AddResult is the result of a Pre-add check refusal or of an add.
type AddResult struct {
	Outcome AddOutcome
	// Status names a refusal: blocked, exists, skipped_recheck, or error.
	Status string
	// Message is the text of a refusal. For an added torrent, it is the text
	// that the caller appends to its own message.
	Message string
	// Existing is the torrent that an exists refusal found.
	Existing *qbt.Torrent
	// PauseErr is set when AddLeftPaused could not pause a running torrent.
	PauseErr error
	// Err is the cause of a refusal with the status error.
	Err error
	// Settled is what Settle reported for an AddSettling torrent.
	Settled SettleOutcome
	// Paused is set when qui added the torrent paused.
	Paused bool
}

// blocklistCheckFailed starts the message of a failed blocklist read.
const blocklistCheckFailed = "Failed to check cross-seed blocklist"

// Check runs the Pre-add check for the v1 and v2 hashes of a torrent. Only
// facts.ManualAssembly applies here: it skips the blocklist. A non-nil result is
// a refusal, and the check it returns then is empty.
func (a *Adder) Check(ctx context.Context, instanceID int, facts AddFacts, hashes ...string) (PreAddCheck, *AddResult) {
	check := PreAddCheck{instanceID: instanceID, hashes: dedupeHashes(hashes...)}

	// The existing torrents come first, so a torrent that is in qBittorrent is
	// refused as exists also when it is on the blocklist.
	existing, exists, err := a.sync.HasTorrentByAnyHash(ctx, instanceID, check.hashes)
	if err != nil {
		return PreAddCheck{}, &AddResult{Status: "error", Message: fmt.Sprintf("Failed to check existing torrents: %v", err), Err: err}
	}
	if exists && existing != nil {
		log.Debug().
			Int("instanceID", instanceID).
			Strs("torrentHashes", check.hashes).
			Str("existingHash", existing.Hash).
			Str("existingName", existing.Name).
			Msg("Cross-seed apply skipped: torrent already exists in instance")
		return PreAddCheck{}, &AddResult{Status: "exists", Message: "Torrent already exists in this instance", Existing: existing}
	}

	if a.blocklist != nil && !facts.ManualAssembly {
		blockedHash, blocked, err := a.blocklist.FindBlocked(ctx, instanceID, check.hashes)
		if err != nil {
			return PreAddCheck{}, &AddResult{Status: "error", Message: fmt.Sprintf("%s: %v", blocklistCheckFailed, err), Err: err}
		}
		if blocked {
			log.Info().
				Int("instanceID", instanceID).
				Strs("torrentHashes", check.hashes).
				Str("blockedHash", blockedHash).
				Msg("Cross-seed apply skipped: infohash is blocked")
			return PreAddCheck{}, &AddResult{Status: "blocked", Message: "Blocked by cross-seed blocklist"}
		}
	}
	return check, nil
}

// AddPlacement is where a cross-seed lands. The caller owns it.
type AddPlacement struct {
	SavePath      string
	Category      string
	Tags          []string
	AutoTMM       bool
	ContentLayout string
}

func (p AddPlacement) options() map[string]string {
	options := map[string]string{"autoTMM": strconv.FormatBool(p.AutoTMM)}
	if p.SavePath != "" {
		options["savepath"] = p.SavePath
	}
	if p.Category != "" {
		options["category"] = p.Category
	}
	if len(p.Tags) > 0 {
		options["tags"] = strings.Join(p.Tags, ",")
	}
	if p.ContentLayout != "" {
		options["contentLayout"] = p.ContentLayout
	}
	KeepAtSavePath(options)
	return options
}

// AddRequest is one cross-seed add.
type AddRequest struct {
	Check     PreAddCheck
	Torrent   []byte
	Facts     AddFacts
	Placement AddPlacement
	// AfterAdd runs when qBittorrent has the torrent, before the resume or Settle.
	// It gets the IDs that qBittorrent returned for the add. It returns the hash
	// that qBittorrent lists the torrent under (empty for the first checked hash),
	// and false when the torrent must stay paused.
	AfterAdd func(ctx context.Context, addedIDs []string) (hash string, ok bool)
}

// Add makes the Add verdict with all the facts, adds the torrent, resumes it,
// hands it to Settle, or leaves it paused, and then runs the External Program.
func (a *Adder) Add(ctx context.Context, req AddRequest) AddResult {
	v := req.Facts.Verdict()
	if v.Refused {
		return AddResult{Status: v.Status, Message: v.Message}
	}
	instanceID := req.Check.instanceID

	options := req.Placement.options()
	paused := strconv.FormatBool(v.paused)
	options["paused"] = paused
	options["stopped"] = paused
	if v.skipChecking {
		options["skip_checking"] = "true"
	}

	response, err := a.sync.AddTorrent(ctx, instanceID, req.Torrent, options)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceID", instanceID).
			Strs("torrentHashes", req.Check.hashes).
			Msg("Failed to add cross-seed torrent")
		return AddResult{Status: "error", Message: fmt.Sprintf("Failed to add torrent: %v", err), Err: err}
	}
	// qBittorrent has the torrent, so the renames, the recheck and the External
	// Program run also when the run stops now.
	ctx = context.WithoutCancel(ctx)

	var message string
	if req.Facts.DiscMarker != "" {
		message += " - disc layout detected (" + req.Facts.DiscMarker + "), full recheck required"
	}
	if v.linkRecheck {
		message += " - link-mode filesystem fallback, full recheck required"
	}

	hash := req.Check.hashes[0]
	if req.AfterAdd != nil {
		var addedIDs []string
		if response != nil {
			addedIDs = response.AddedTorrentIds
		}
		activeHash, ok := req.AfterAdd(ctx, addedIDs)
		hash = cmp.Or(activeHash, hash)
		if !ok {
			result := AddResult{Outcome: AddLeftPaused, Message: message, Paused: v.paused}
			if !v.paused {
				result.PauseErr = a.sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), instanceID, []string{hash}, "pause")
			}
			return result
		}
	}

	outcome := AddAdded
	var settled SettleOutcome
	switch {
	case v.settle:
		// qBittorrent does not recheck a torrent that it adds paused, so Settle
		// sends the recheck and resumes the torrent when its gate passes.
		added := AddedTorrent{
			InstanceID:          instanceID,
			Hashes:              dedupeHashes(append([]string{hash}, req.Check.hashes...)...),
			LinkedPaths:         req.Facts.LinkedPaths,
			Gate:                v.gate,
			SkipAutoResume:      req.Facts.SkipAutoResume,
			RecoverMissingFiles: req.Facts.RecoverMissingFiles,
			Threshold:           req.Facts.SeasonPackLinked * seasonPackResumeSlack,
			SeasonPackRun:       req.Facts.SeasonPackRun,
		}
		if v.gate == GateBudget {
			added.Budget = a.resumeBudget(ctx)
		}
		settled = a.settler.Settle(ctx, added)
		message += settled.StatusSuffix()
		outcome = AddSettling
	case req.Facts.PoolMember:
		// The caller wakes the pool, which resumes the torrent.
		outcome = AddPooled
	case !v.paused:
	case req.Facts.SkipAutoResume:
		message += " - auto-resume skipped per settings"
	default:
		if err := a.sync.BulkAction(qbittorrent.WithPostAddBulkActionRetry(ctx), instanceID, []string{hash}, "resume"); err != nil {
			log.Warn().
				Err(err).
				Int("instanceID", instanceID).
				Str("torrentHash", hash).
				Msg("Failed to resume cross-seed torrent after add")
			message += " - auto-resume failed, manual resume required"
		}
	}

	a.runExternalProgram(ctx, instanceID, req.Check.hashes)
	return AddResult{Outcome: outcome, Message: message, Settled: settled, Paused: v.paused}
}
