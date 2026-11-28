// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"slices"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/automations/verdict"
	"github.com/autobrr/qui/pkg/infohash"
)

type pendingDeleteStore interface {
	ListPendingDeletes(ctx context.Context, instanceID int) ([]models.PendingDelete, error)
	ApplyPendingDeletes(ctx context.Context, instanceID int, change models.PendingDeleteChange) error
}

func (s *Service) SetPendingDeleteStore(store pendingDeleteStore) {
	s.pendingDeletes = store
}

func (s *Service) waitedNow(instanceID int) verdict.Waited {
	if source := s.trackerHealthSource(); source != nil {
		return verdict.ForDelete(source.GetTrackerHealthSnapshot(instanceID), s.now())
	}
	return verdict.Waited{}
}

type pendingKey struct {
	ruleID int
	hash   string
}

type pendingCompare struct {
	rules         []*models.Automation
	selection     *deletionSelection
	undeleted     *infohash.Set
	torrentByHash map[string]*qbt.Torrent
	scope         *ruleScope
	waited        verdict.Waited
	claimed       func(hash string) bool
	summary       *automationSummary
}

type waitRuleCount struct {
	ID    int    `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type waitStartedDetails struct {
	Count int             `json:"count"`
	Rules []waitRuleCount `json:"rules"`
}

type waitEndedCauses struct {
	Stopped      int `json:"stopped"`
	TrackerWorks int `json:"trackerWorks"`
}

type waitEndedDetails struct {
	Count  int             `json:"count"`
	Causes waitEndedCauses `json:"causes"`
	Rules  []waitRuleCount `json:"rules"`
}

type waitLine struct {
	byRule map[int]int
	keys   []pendingKey
}

func (l *waitLine) add(key pendingKey) {
	if l.byRule == nil {
		l.byRule = make(map[int]int)
	}
	l.byRule[key.ruleID]++
	l.keys = append(l.keys, key)
}

func (l *waitLine) hashes() []string {
	hashes := make([]string, 0, len(l.keys))
	for _, key := range l.keys {
		hashes = append(hashes, key.hash)
	}
	return infohash.UniqueStrings(hashes)
}

func (l *waitLine) ruleCounts(rules map[int]*models.Automation) []waitRuleCount {
	counts := make([]waitRuleCount, 0, len(l.byRule))
	for ruleID, count := range l.byRule {
		counts = append(counts, waitRuleCount{ID: ruleID, Name: rules[ruleID].Name, Count: count})
	}
	slices.SortFunc(counts, func(a, b waitRuleCount) int {
		return cmp.Or(b.Count-a.Count, a.ID-b.ID)
	})
	return counts
}

type storedVerdict uint8

const (
	keepRow storedVerdict = iota
	removeRow
	endWait
)

type pendingDiff struct {
	in      pendingCompare
	rules   map[int]*models.Automation
	current map[pendingKey]time.Time
	due     map[pendingKey]struct{}
	change  models.PendingDeleteChange
	started waitLine
	ended   waitLine
	causes  waitEndedCauses
	counted *infohash.Set
}

func (s *Service) recordPendingDeletes(ctx context.Context, instanceID int, in pendingCompare) []*models.AutomationActivity {
	if s.pendingDeletes == nil {
		return nil
	}
	rules := in.comparedRules()
	if len(rules) == 0 {
		return nil
	}
	stored, err := s.pendingDeletes.ListPendingDeletes(ctx, instanceID)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to read the torrents that wait for a delete")
		return nil
	}
	if s.claimed != nil {
		in.claimed = s.claimed.ClaimCheck(instanceID)
	}

	diff := newPendingDiff(in, rules)
	for _, row := range stored {
		diff.compare(row)
	}
	diff.start(s.now())
	lines := diff.lines()
	for _, line := range lines {
		detailsJSON, err := json.Marshal(line.details)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", instanceID).Str("action", line.action).Msg("automations: failed to marshal activity details")
			return nil
		}
		diff.change.Activities = append(diff.change.Activities, &models.AutomationActivity{
			InstanceID: instanceID,
			Action:     line.action,
			Outcome:    models.ActivityOutcomeSuccess,
			Details:    detailsJSON,
			CreatedAt:  time.Now().UTC(),
		})
	}

	if err := s.pendingDeletes.ApplyPendingDeletes(ctx, instanceID, diff.change); errors.Is(err, models.ErrPendingDeleteRuleChanged) {
		log.Debug().Int("instanceID", instanceID).Msg("automations: a rule changed during the run, so the torrents that wait for a delete are compared again on the next run")
		return nil
	} else if err != nil {
		log.Warn().Err(err).Int("instanceID", instanceID).Msg("automations: failed to store the torrents that wait for a delete")
		return nil
	}
	for i, activity := range diff.change.Activities {
		if s.activityRuns != nil {
			s.activityRuns.Put(activity.ID, instanceID, buildRunItemsFromHashes(lines[i].hashes, in.torrentByHash, in.scope))
		}
	}
	in.reportStarted(diff.started.keys, rules)
	return diff.change.Activities
}

func (in pendingCompare) reportStarted(keys []pendingKey, rules map[int]*models.Automation) {
	if in.summary == nil {
		return
	}
	for _, key := range keys {
		rule := rules[key.ruleID]
		torrent := in.torrentByHash[key.hash]
		in.summary.addWaiting(rule, deleteWaitOf(rule, in.waited).wait, key.hash, waitingTorrent{name: torrent.Name, tracker: in.scope.tracker(torrent)})
	}
}

func newPendingDiff(in pendingCompare, rules map[int]*models.Automation) *pendingDiff {
	due := make(map[pendingKey]struct{})
	if in.selection != nil {
		for _, key := range in.selection.due {
			due[key] = struct{}{}
		}
	}
	return &pendingDiff{in: in, rules: rules, current: in.currentPending(rules), due: due, counted: infohash.NewSet()}
}

func (d *pendingDiff) compare(row models.PendingDelete) {
	if _, compared := d.rules[row.RuleID]; !compared {
		return
	}
	key := pendingKey{ruleID: row.RuleID, hash: row.Hash}
	if since, still := d.current[key]; still {
		delete(d.current, key)
		if !since.IsZero() && !withinASecond(since, row.UnregisteredSince) {
			d.change.Put = append(d.change.Put, models.PendingDelete{RuleID: key.ruleID, Hash: key.hash, UnregisteredSince: since})
		}
		return
	}
	_, due := d.due[key]
	switch d.in.storedVerdict(row.Hash, due) {
	case keepRow:
	case removeRow:
		d.change.Remove = append(d.change.Remove, row)
	case endWait:
		d.change.Remove = append(d.change.Remove, row)
		d.endWait(key)
	}
}

func (d *pendingDiff) endWait(key pendingKey) {
	d.ended.add(key)
	hash, err := infohash.Parse(key.hash)
	if err != nil || !d.counted.Add(hash) {
		return
	}
	if torrentStopped(d.in.torrentByHash[key.hash]) {
		d.causes.Stopped++
	} else {
		d.causes.TrackerWorks++
	}
}

func (d *pendingDiff) start(now time.Time) {
	for key, since := range d.current {
		if since.IsZero() {
			since = now
		}
		d.change.Put = append(d.change.Put, models.PendingDelete{RuleID: key.ruleID, Hash: key.hash, UnregisteredSince: since})
		d.started.add(key)
	}
}

func (d *pendingDiff) lines() []waitActivity {
	var lines []waitActivity
	if len(d.started.keys) > 0 {
		hashes := d.started.hashes()
		lines = append(lines, waitActivity{
			action:  models.ActivityActionUnregisteredWaitStarted,
			details: waitStartedDetails{Count: len(hashes), Rules: d.started.ruleCounts(d.rules)},
			hashes:  hashes,
		})
	}
	if len(d.ended.keys) > 0 {
		hashes := d.ended.hashes()
		lines = append(lines, waitActivity{
			action:  models.ActivityActionUnregisteredWaitEnded,
			details: waitEndedDetails{Count: len(hashes), Causes: d.causes, Rules: d.ended.ruleCounts(d.rules)},
			hashes:  hashes,
		})
	}
	return lines
}

type waitActivity struct {
	action  string
	details any
	hashes  []string
}

func (in pendingCompare) comparedRules() map[int]*models.Automation {
	compared := make(map[int]*models.Automation)
	for _, rule := range in.rules {
		if !rule.KeepsPendingDeletes() || deleteWaitOf(rule, in.waited).blocked() {
			continue
		}
		if in.selection != nil {
			if _, failed := in.selection.failedRules[rule.ID]; failed {
				continue
			}
		}
		compared[rule.ID] = rule
	}
	return compared
}

func (in pendingCompare) deleted(hash string) bool {
	if in.selection == nil {
		return false
	}
	if _, selected := in.selection.byHash[hash]; !selected {
		return false
	}
	parsed, err := infohash.Parse(hash)
	return err != nil || in.undeleted == nil || !in.undeleted.Has(parsed)
}

func (in pendingCompare) currentPending(rules map[int]*models.Automation) map[pendingKey]time.Time {
	current := make(map[pendingKey]time.Time)
	if in.selection == nil {
		return current
	}
	for _, waiting := range in.selection.marked {
		if _, compared := rules[waiting.rule.id]; !compared || in.deleted(waiting.hash) {
			continue
		}
		since, timed := in.waited.Since(waiting.hash)
		if !timed && !in.waited.UnregisteredNow(waiting.hash) {
			continue
		}
		current[pendingKey{ruleID: waiting.rule.id, hash: waiting.hash}] = since
	}
	return current
}

func (in pendingCompare) storedVerdict(hash string, due bool) storedVerdict {
	if in.deleted(hash) {
		return removeRow
	}
	if _, exists := in.torrentByHash[hash]; !exists {
		if in.claimed != nil && in.claimed(hash) {
			return keepRow
		}
		return removeRow
	}
	if _, timed := in.waited.Since(hash); !timed {
		return endWait
	}
	if due || (in.selection != nil && in.selection.isClaimed(hash)) {
		return keepRow
	}
	if in.scope != nil {
		if _, skipped := in.scope.skipped[hash]; skipped {
			return keepRow
		}
	}
	return removeRow
}

func torrentStopped(torrent *qbt.Torrent) bool {
	if torrent == nil {
		return false
	}
	switch torrent.State {
	case qbt.TorrentStatePausedUp, qbt.TorrentStatePausedDl, qbt.TorrentStateStoppedUp, qbt.TorrentStateStoppedDl:
		return true
	default:
		return false
	}
}

func withinASecond(a, b time.Time) bool {
	return a.Sub(b).Abs() < time.Second
}
