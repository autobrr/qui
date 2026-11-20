// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
)

// transferRunner starts the Transfers of Transfer to Instance.
type transferRunner interface {
	Start(ctx context.Context, req transfer.Request) (int64, error)
	Preview(ctx context.Context, req transfer.Request) (*transfer.Preview, error)
}

// transferHold is how long a rule waits before it asks again for a Transfer
// that the Transfer module refused.
const transferHold = time.Hour

// SetTransferRunner lets Transfer to Instance start Transfers. Call it once
// at start-up.
func (s *Service) SetTransferRunner(runner transferRunner) {
	s.transferRunner = runner
	s.transferHolds = make(map[string]time.Time)
}

// pendingTransfer is one Transfer to Instance in the Run plan. failureReason
// is set when the save path does not render.
type pendingTransfer struct {
	hash          string
	torrent       qbt.Torrent
	trackerDomain string
	action        *models.TransferToInstanceAction
	savePath      string
	rule          ruleRef
	failureReason string
	// exportsToDo holds, by hash, the target instance of each Export of the
	// run that still has to bring its torrent there.
	exportsToDo map[string]int
}

func (s *Service) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return time.Now()
}

func transferHoldKey(instanceID int, hash string) string {
	return fmt.Sprintf("%d:%s", instanceID, hash)
}

// planTransfer renders the save path of a Transfer. It leaves out a torrent
// that the target has, and a torrent whose Transfer the module refused less
// than an hour ago. It adds a target whose check fails to failedTargets, and
// does not check that target again in the run.
func (s *Service) planTransfer(ctx context.Context, instanceID int, hash string, torrent qbt.Torrent, state *torrentDesiredState, evalCtx *EvalContext, failedTargets map[int]bool) (pendingTransfer, bool) {
	action := state.transferToInstance
	s.mu.RLock()
	until, held := s.transferHolds[transferHoldKey(instanceID, hash)]
	s.mu.RUnlock()
	if held && s.now().Before(until) {
		return pendingTransfer{}, false
	}
	if !failedTargets[action.TargetInstanceID] {
		// A bounded timeout keeps a slow or unreachable target from stalling the whole run.
		checkCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		_, exists, err := s.targetLookup.HasTorrentByAnyHash(checkCtx, action.TargetInstanceID, []string{hash})
		cancel()
		if err != nil {
			failedTargets[action.TargetInstanceID] = true
		} else if exists {
			return pendingTransfer{}, false
		}
	}

	pending := pendingTransfer{hash: hash, torrent: torrent, action: action, rule: state.transferToInstanceRule}
	if action.SavePath != "" {
		resolved, ok := renderExportSavePath(action.SavePath, torrent, state, evalCtx)
		if !ok {
			pending.failureReason = "Save path template resolution failed"
		}
		pending.savePath = resolved
	}
	return pending, true
}

// transfersAfterMoves holds back the Transfers of torrents that move in this
// run or that qBittorrent moves now. A later run starts them.
func transfersAfterMoves(transfers []pendingTransfer, moved map[string]ruleRef) []pendingTransfer {
	ready := transfers[:0]
	for _, pending := range transfers {
		if _, movesNow := moved[pending.hash]; movesNow || pending.torrent.State == qbt.TorrentStateMoving {
			continue
		}
		ready = append(ready, pending)
	}
	return ready
}

// transfersAfterExports holds back the Transfer of a torrent while an Export
// still has to bring that torrent to another instance. The Transfer can remove
// the torrent from the source before the Export reads it. A later run starts it.
func transfersAfterExports(transfers []pendingTransfer, exports []pendingExportToInstance) []pendingTransfer {
	toDo := make(map[string]int, len(exports))
	for _, export := range exports {
		if export.mark != exportAlreadyOnTarget {
			toDo[export.hash] = export.action.TargetInstanceID
		}
	}
	ready := transfers[:0]
	for _, pending := range transfers {
		pending.exportsToDo = toDo
		if !pending.takesExport([]transfer.Member{{Hash: pending.hash}}) {
			ready = append(ready, pending)
		}
	}
	return ready
}

// takesExport reports whether one of the members is a torrent that an Export
// still has to bring to an instance other than the target of the Transfer.
func (p pendingTransfer) takesExport(members []transfer.Member) bool {
	return slices.ContainsFunc(members, func(member transfer.Member) bool {
		target, waits := p.exportsToDo[strings.ToLower(member.Hash)]
		return waits && target != p.action.TargetInstanceID
	})
}

// start starts the Transfer. It waits when the Transfer takes a cross-seed
// along that an Export still has to bring to another instance. Only the
// module knows which torrents a Transfer takes along, so the run asks it
// first, and a refusal of that question is the refusal of the start.
func (s *Service) start(ctx context.Context, instanceID int, pending pendingTransfer) (waits bool, err error) {
	if len(pending.exportsToDo) > 0 {
		preview, err := s.transferRunner.Preview(ctx, pending.request(instanceID))
		if err != nil {
			return false, err
		}
		if pending.takesExport(preview.Members) {
			return true, nil
		}
	}
	_, err = s.transferRunner.Start(ctx, pending.request(instanceID))
	return false, err
}

func (p pendingTransfer) request(instanceID int) transfer.Request {
	return transfer.Request{
		SourceInstanceID: instanceID,
		Hash:             p.hash,
		TargetInstanceID: p.action.TargetInstanceID,
		Placement:        transfer.Placement{SavePath: p.savePath, Category: p.action.Category, Tags: p.action.Tags},
		Options: transfer.Options{After: transfer.After(p.action.After), LeaveStopped: p.action.LeaveStopped,
			ForcedWay: transfer.ForcedWays[p.action.Way]},
		StartedBy: transfer.StartedByRule,
		RuleID:    new(p.rule.id),
	}
}

// quiet reports whether the module turned the Transfer down for a reason
// that needs no activity: another Transfer holds the torrent, the last one
// failed for good and waits for the user, a cross-seed with the same files on
// hardlinks passes or passed them, or qBittorrent moves a torrent of the group, which
// the next run asks about again.
func quiet(err error) bool {
	return errors.Is(err, transfer.ErrClaimed) || errors.Is(err, transfer.ErrWaitsForUser) ||
		errors.Is(err, transfer.ErrStays) || errors.Is(err, transfer.ErrMoving)
}

// startTransfers starts the planned Transfers and records each start and each
// refusal. After a refusal, the rule asks again no sooner than an hour later.
// When the run ends first, the Transfers that did not start wait for the next
// run.
func (s *Service) startTransfers(ctx context.Context, instanceID int, transfers []pendingTransfer, summary *automationSummary) {
	for i, pending := range transfers {
		reason := pending.failureReason
		if reason == "" {
			waits, err := s.start(ctx, instanceID, pending)
			if waits || quiet(err) {
				continue
			}
			if err != nil && ctx.Err() != nil {
				log.Warn().Err(err).Int("instanceID", instanceID).Int("waiting", len(transfers)-i).
					Msg("automations: the run ended before each Transfer to Instance started")
				return
			}
			if err != nil {
				reason = err.Error()
			}
		}
		outcome := models.ActivityOutcomeSuccess
		if reason != "" {
			outcome = models.ActivityOutcomeFailed
			s.mu.Lock()
			s.transferHolds[transferHoldKey(instanceID, pending.hash)] = s.now().Add(transferHold)
			s.mu.Unlock()
			log.Warn().Int("instanceID", instanceID).Str("hash", pending.hash).Str("reason", reason).
				Msg("automations: Transfer to Instance did not start")
		}
		activity := &models.AutomationActivity{
			InstanceID:    instanceID,
			Hash:          pending.hash,
			TorrentName:   pending.torrent.Name,
			TrackerDomain: pending.trackerDomain,
			Action:        models.ActivityActionTransferredToInstance,
			RuleID:        new(pending.rule.id),
			RuleName:      pending.rule.name,
			Outcome:       outcome,
			Reason:        reason,
		}
		activity.Details, _ = json.Marshal(transferDetails{TargetInstanceID: pending.action.TargetInstanceID, Count: 1}) //nolint:errchkjson // int fields cannot fail to encode
		summary.recordActivity(activity, 1)
		if s.activityStore != nil {
			if err := s.activityStore.Create(ctx, activity); err != nil {
				log.Warn().Err(err).Str("hash", pending.hash).Int("instanceID", instanceID).Msg("automations: failed to record activity")
			}
		}
	}
}

// transferDetails are the details of a Transfer to Instance activity. Way is
// empty in a live run, and Refused marks a dry run that the module refused.
type transferDetails struct {
	TargetInstanceID int          `json:"targetInstanceId"`
	Way              transfer.Way `json:"way,omitempty"`
	Count            int          `json:"count"`
	Refused          bool         `json:"refused,omitzero"`
}

// recordDryRunTransfers asks the module for each planned Transfer, as Start
// would, and records the Transfers that a live run starts, by target and way,
// each torrent with the size of its Transfer. A torrent that an earlier
// Transfer takes along shows once, and a cross-seed with the same files on
// hardlinks that an earlier Transfer leaves on the source does not show.
func (s *Service) recordDryRunTransfers(ctx context.Context, r *stepRun, transfers []pendingTransfer) {
	type group struct {
		target int
		way    transfer.Way
	}
	var order []group
	items := map[group][]ActivityRunTorrent{}
	taken := map[string]bool{}
	for _, pending := range transfers {
		if taken[pending.hash] {
			continue
		}
		reason := pending.failureReason
		var preview *transfer.Preview
		if reason == "" {
			var err error
			preview, err = s.transferRunner.Preview(ctx, pending.request(r.instanceID))
			if quiet(err) {
				continue
			}
			if err != nil {
				reason = err.Error()
			}
		}
		if preview != nil && pending.takesExport(preview.Members) {
			continue
		}
		item := buildRunItemFromHash(pending.hash, r.torrentByHash, r.scope)
		if reason != "" {
			r.record(ctx, stepActivity{
				action:  models.ActivityActionTransferredToInstance,
				hash:    pending.hash,
				reason:  reason,
				details: transferDetails{TargetInstanceID: pending.action.TargetInstanceID, Count: 1, Refused: true},
				items:   func() []ActivityRunTorrent { return []ActivityRunTorrent{item} },
			})
			continue
		}
		for _, member := range preview.Members {
			taken[strings.ToLower(member.Hash)] = true
		}
		for _, hash := range preview.Versions {
			taken[hash] = true
		}
		item.Size = new(preview.Bytes)
		key := group{pending.action.TargetInstanceID, preview.Way}
		if _, ok := items[key]; !ok {
			order = append(order, key)
		}
		items[key] = append(items[key], item)
	}
	for _, key := range order {
		groupItems := items[key]
		sortActivityRunItems(groupItems)
		r.record(ctx, stepActivity{
			action:  models.ActivityActionTransferredToInstance,
			details: transferDetails{TargetInstanceID: key.target, Way: key.way, Count: len(groupItems)},
			items:   func() []ActivityRunTorrent { return groupItems },
		})
	}
}
