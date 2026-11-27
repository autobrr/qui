// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"errors"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/automations/verdict"
	"github.com/autobrr/qui/pkg/infohash"
)

var ErrUnregisteredSinceNotLoaded = errors.New("qui has not loaded the unregistered times of this instance yet")

type trackerHealthSource interface {
	GetTrackerHealthSnapshot(instanceID int) *qbittorrent.TrackerHealthSnapshot
	StillUnregistered(ctx context.Context, instanceID int, hashes []string) (*infohash.Set, error)
}

func (s *Service) trackerHealthSource() trackerHealthSource {
	if s.trackerHealth != nil {
		return s.trackerHealth
	}
	if s.syncManager != nil {
		return s.syncManager
	}
	return nil
}

type waitingDelete struct {
	hash  string
	rule  ruleRef
	until time.Time
}

type waitingDetails struct {
	Count int `json:"count"`
}

type ruleWait struct {
	waited verdict.Waited
	wait   time.Duration
}

func deleteWaitOf(rule *models.Automation, waited verdict.Waited) ruleWait {
	w := ruleWait{waited: waited}
	if rule.Conditions != nil && rule.Conditions.Delete != nil && ruleReadsVerdict(rule) {
		w.wait = verdict.DeleteWait(rule.Conditions.Delete)
	}
	return w
}

func (w ruleWait) blocked() bool {
	return w.wait > 0 && !w.waited.Loaded()
}

func (w ruleWait) read(evalCtx *EvalContext) {
	evalCtx.health = w.waited.Due(w.wait)
}

func (w ruleWait) match(evalCtx *EvalContext, hash string, matches func() bool) (due, marked bool) {
	w.read(evalCtx)
	due = matches()
	if w.wait <= 0 || !w.waited.Marks(hash) {
		return due, due
	}
	evalCtx.health = w.waited.Marked()
	marked = matches()
	w.read(evalCtx)
	return due, marked
}

func ruleReadsVerdict(rule *models.Automation) bool {
	if rule.Conditions != nil && rule.Conditions.Delete != nil && conditionReadsVerdict(rule.Conditions.Delete.Condition) {
		return true
	}
	config := rule.SortingConfig
	if config == nil || config.Type != models.SortingTypeScore {
		return false
	}
	return slices.ContainsFunc(config.ScoreRules, func(score models.ScoreRule) bool {
		return score.Conditional != nil && conditionReadsVerdict(score.Conditional.Condition)
	})
}

func conditionReadsVerdict(cond *RuleCondition) bool {
	if cond == nil {
		return false
	}
	if cond.Field == FieldIsUnregistered {
		return true
	}
	if cond.Field == FieldState && strings.EqualFold(strings.TrimSpace(cond.Value), string(qbittorrent.TrackerHealthUnregistered)) {
		return true
	}
	return slices.ContainsFunc(cond.Conditions, conditionReadsVerdict)
}

func allGroupMembersMatchWaited(members []string, torrentByHash map[string]*qbt.Torrent, cond *RuleCondition, evalCtx *EvalContext, w ruleWait) bool {
	if len(members) == 0 {
		return false
	}
	for _, memberHash := range members {
		member, ok := torrentByHash[memberHash]
		if !ok {
			return false
		}
		if cond == nil {
			continue
		}
		due, marked := w.match(evalCtx, memberHash, func() bool { return EvaluateConditionWithContext(cond, *member, evalCtx, 0) })
		if !due || !marked {
			return false
		}
	}
	return true
}

func (s *Service) confirmVerdictClaims(ctx context.Context, instanceID int, claims []deletionClaim, sel *deletionSelection, in deletionSelectionInput) []deletionClaim {
	waited := in.evalCtx.waited
	var hashes []string
	for _, claim := range claims {
		if conditionReadsVerdict(claim.rule.Conditions.Delete.Condition) && waited.UnregisteredNow(claim.hash) {
			hashes = append(hashes, claim.hash)
		}
	}
	source := s.trackerHealthSource()
	if len(hashes) == 0 || source == nil {
		return claims
	}

	readCtx, cancel := context.WithTimeout(ctx, s.cfg.ApplyTimeout)
	still, err := source.StillUnregistered(readCtx, instanceID, hashes)
	cancel()
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Int("torrents", len(hashes)).
			Msg("automations: skipped the deletes of unregistered torrents, the fresh tracker read failed")
		still = infohash.NewSet()
	}
	cleared := infohash.NewSet()
	for _, hash := range infohash.ParseAll(hashes) {
		if !still.Has(hash) {
			cleared.Add(hash)
		}
	}
	if cleared.Len() == 0 {
		return claims
	}
	in.evalCtx.waited = waited.Without(cleared)
	in.evalCtx.health = in.evalCtx.waited.Raw()
	clearedTorrents := make(map[string]*qbt.Torrent, cleared.Len())
	for _, torrent := range in.torrents {
		if hash, err := infohash.Parse(torrent.Hash); err == nil && cleared.Has(hash) {
			clearedTorrents[torrent.Hash] = torrent
		}
	}

	dropped := 0
	kept := claims[:0]
	for _, claim := range claims {
		hash, _ := infohash.Parse(claim.hash)
		if cleared.Has(hash) && !claimStillMatches(claim, clearedTorrents[claim.hash], in) {
			delete(sel.triggers, claim.hash)
			if stats := getOrCreateRuleStats(in.stats, claim.rule); stats != nil {
				stats.DeleteApplied--
			}
			dropped++
			log.Trace().Int("instanceID", instanceID).Str("hash", claim.hash).Str("ruleName", claim.rule.Name).
				Msg("automations: skipped delete, the torrent is no longer unregistered")
			continue
		}
		kept = append(kept, claim)
	}
	if dropped > 0 && err == nil {
		log.Info().Int("instanceID", instanceID).Int("torrents", dropped).
			Msg("automations: skipped the deletes of torrents that are no longer unregistered")
	}
	return kept
}

func claimStillMatches(claim deletionClaim, torrent *qbt.Torrent, in deletionSelectionInput) bool {
	if torrent == nil {
		return false
	}
	enterRule(in.evalCtx, claim.rule, in.torrents, in.scope)
	due, marked := deleteWaitOf(claim.rule, in.evalCtx.waited).match(in.evalCtx, claim.hash, func() bool {
		return shouldDeleteTorrent(claim.rule, torrent, in.evalCtx)
	})
	return due && marked
}

func (sel *deletionSelection) dropDeletedWaits() {
	sel.waiting = slices.DeleteFunc(sel.waiting, func(w waitingDelete) bool {
		_, deleted := sel.byHash[w.hash]
		return deleted
	})
}

func addWaitingPreviewRows(result *PreviewResult, torrents []*qbt.Torrent, selection *deletionSelection, evalCtx *EvalContext, cfg previewConfig, rule *models.Automation, scoreByHash map[string]float64, scope *ruleScope, rowIndex int) {
	if len(selection.waiting) == 0 {
		return
	}
	until := make(map[string]time.Time, len(selection.waiting))
	for _, waiting := range selection.waiting {
		until[waiting.hash] = waiting.until
	}
	result.WaitingCount = len(until)
	for _, torrent := range torrents {
		at, ok := until[torrent.Hash]
		if !ok {
			continue
		}
		rowIndex++
		if rowIndex <= cfg.offset || len(result.Examples) >= cfg.limit {
			continue
		}
		row := buildPreviewTorrent(torrent, scope.tracker(torrent), evalCtx, false, false, computePreviewScore(torrent, rule, evalCtx, scoreByHash))
		row.WaitsUntil = at.Unix()
		result.Examples = append(result.Examples, row)
	}
}

func (sel *deletionSelection) waitingHashes() []string {
	hashes := make([]string, 0, len(sel.waiting))
	for _, waiting := range sel.waiting {
		hashes = append(hashes, waiting.hash)
	}
	return infohash.UniqueStrings(hashes)
}
