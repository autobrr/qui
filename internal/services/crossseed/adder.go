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
	"github.com/autobrr/qui/internal/services/crossseedroute"
)

type adderSync interface {
	AddTorrent(ctx context.Context, instanceID int, fileContent []byte, options map[string]string) (*qbt.TorrentAddResponse, error)
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
	BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error
}

var _ adderSync = (*qbittorrent.SyncManager)(nil)

type Adder struct {
	sync               adderSync
	blocklist          *models.CrossSeedBlocklistStore
	settler            *Settler
	resumeBudget       func(context.Context) int64
	runExternalProgram func(ctx context.Context, instanceID int, hashes []string)
	routes             *crossseedroute.Service
}

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

func (a *Adder) SetCrossSeedRoutes(routes *crossseedroute.Service) {
	a.routes = routes
}

type VerifyReason int

const (
	VerifyNone VerifyReason = iota
	VerifyTitleRescue
	VerifyRelaxed
	VerifyManual
)

type AddFacts struct {
	MappedFiles          bool
	PendingFiles         bool
	Align                bool
	RenameOnly           bool
	DiscMarker           string
	Verify               VerifyReason
	ExactMatch           bool
	UnvalidatedManual    bool
	SizeMismatch         bool
	UnsafePieces         bool
	UnsafeFallbackPieces bool
	FolderToRootless     bool
	ReflinkAdd           bool
	LinkFallback         bool
	LinkFallbackRecheck  bool
	LinkTree             bool
	PoolMember           bool
	LinkedPaths          map[string]struct{}
	RecoverMissingFiles  bool
	ManualAssembly       bool
	SeasonPackLinked     float64
	SeasonPackRun        *models.SeasonPackRun

	SkipRecheck            bool
	SkipAutoResume         bool
	StartRunning           bool
	SkipPieceBoundaryCheck bool
	DirScan                bool
}

const (
	sizeMismatchMessage         = "Content file sizes do not match - possible corruption or different release"
	unsafePiecesMessage         = "Skipped: extra files share pieces with content. Disable 'Piece boundary safety check' in Cross-Seed settings to allow"
	unsafeFallbackPiecesMessage = "Skipped: link-mode fallback files share pieces with unmaterialized files"
	folderToRootlessMessage     = "Skipped: cross-seed with extra files and rootless content requires hardlink or reflink mode to avoid scattering files in base directory"
)

func refuseAdd(status, message string) AddVerdict {
	return AddVerdict{Refused: true, Status: status, Message: message}
}

type AddVerdict struct {
	Refused bool
	Status  string
	Message string

	paused       bool
	skipChecking bool
	settle       bool
	gate         SettleGate
	linkRecheck  bool
}

func (f AddFacts) exactInPlace() bool {
	return f.ExactMatch && !f.Align && !f.PendingFiles
}

func (f AddFacts) alignmentPauses() bool {
	return (f.Align && (f.MappedFiles || f.DirScan)) || (f.SkipRecheck && f.RenameOnly)
}

func (f AddFacts) Verdict() AddVerdict {
	disc := f.DiscMarker != ""
	exactInPlace := f.exactInPlace()
	linkRecheck := f.LinkFallbackRecheck || (f.LinkFallback && !exactInPlace)
	if f.SkipRecheck && (f.RenameOnly || exactInPlace) {
		linkRecheck = false
	}
	alignRecheck := (f.Align || f.PendingFiles) && (!f.SkipRecheck || !f.RenameOnly)
	verify := f.Verify != VerifyNone
	dirScanPendingTree := f.DirScan && f.LinkTree && f.PendingFiles

	switch {
	case f.SizeMismatch && !f.ReflinkAdd && !f.UnvalidatedManual:
		return refuseAdd("rejected", sizeMismatchMessage)
	case f.UnsafePieces && !f.ReflinkAdd && !f.SkipPieceBoundaryCheck:
		return refuseAdd("skipped_unsafe_pieces", unsafePiecesMessage)
	case f.UnsafeFallbackPieces && f.LinkFallback && !exactInPlace:
		return refuseAdd("skipped_unsafe_pieces", unsafeFallbackPiecesMessage)
	case f.SkipRecheck && !f.ManualAssembly && (verify || alignRecheck || disc || linkRecheck):
		return refuseAdd("skipped_recheck", skippedRecheckMessage)
	case f.FolderToRootless && !f.UnvalidatedManual:
		return refuseAdd("requires_hardlink_reflink", folderToRootlessMessage)
	}

	v := AddVerdict{
		paused:       !f.StartRunning || verify || disc || linkRecheck || f.alignmentPauses(),
		skipChecking: !f.PendingFiles || f.Align,
		settle:       verify || disc || linkRecheck || alignRecheck || f.ManualAssembly,
		linkRecheck:  linkRecheck,
	}
	if f.LinkTree {
		v.skipChecking = !f.ManualAssembly && !dirScanPendingTree
		v.paused = f.SkipAutoResume || v.settle
	}
	if f.PoolMember {
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
		v.gate = GateAfterRecheck
	case disc || linkRecheck || f.DirScan || (f.MappedFiles && !f.LinkTree && !f.ReflinkAdd):
		v.gate = GateComplete
	default:
		v.gate = GateBudget
	}
	return v
}

type PreAddCheck struct {
	instanceID int
	hashes     []string
}

type AddOutcome int

const (
	AddRefused AddOutcome = iota
	AddAdded
	AddSettling
	AddPooled
	AddLeftPaused
)

const existsInThisInstance = "Torrent already exists in this instance"

type AddResult struct {
	Outcome  AddOutcome
	Status   string
	Message  string
	Existing *qbt.Torrent
	PauseErr error
	Err      error
	Settled  SettleOutcome
	Paused   bool
}

const blocklistCheckFailed = "Failed to check cross-seed blocklist"

func (a *Adder) Check(ctx context.Context, instanceID int, facts AddFacts, hashes ...string) (PreAddCheck, *AddResult) {
	check := PreAddCheck{instanceID: instanceID, hashes: dedupeHashes(hashes...)}

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
		return PreAddCheck{}, &AddResult{Status: StatusExists, Message: existsInThisInstance, Existing: existing}
	}
	if a.routes != nil {
		linked, where, err := a.routes.FindLinked(ctx, a.sync, instanceID, check.hashes)
		if err != nil {
			return PreAddCheck{}, &AddResult{Status: "error", Message: err.Error(), Err: err}
		}
		if linked != nil {
			log.Trace().
				Int("instanceID", instanceID).
				Strs("torrentHashes", check.hashes).
				Str("existingHash", linked.Hash).
				Str("existingName", linked.Name).
				Str("linkedInstance", where).
				Msg("Cross-seed apply skipped: torrent already exists in a linked instance")
			return PreAddCheck{}, &AddResult{Status: StatusExists, Message: "Torrent already exists in " + where, Existing: linked}
		}
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
			return PreAddCheck{}, &AddResult{Status: StatusBlocked, Message: "Blocked by cross-seed blocklist"}
		}
	}
	return check, nil
}

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

type AddRequest struct {
	Check     PreAddCheck
	Torrent   []byte
	Facts     AddFacts
	Placement AddPlacement
	AfterAdd  func(ctx context.Context, addedIDs []string) (hash string, ok bool)
}

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
