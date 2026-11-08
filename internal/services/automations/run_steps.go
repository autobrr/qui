// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// stepRun is one pass of the batch steps over a Run plan. The live form sends the writes to
// qBittorrent and records the torrents that qBittorrent accepted. The dry-run form sends
// nothing and records the planned torrents.
type stepRun struct {
	s             *Service
	instanceID    int
	torrentByHash map[string]qbt.Torrent
	dryRun        bool
	// summary feeds the notification of a live run. A dry run has none.
	summary *automationSummary
	// resetTags are the managed tags that a live run deletes from qBittorrent before the tags step (ADR 0028).
	resetTags []string
	// activities are the rows that a dry run wrote.
	activities []*models.AutomationActivity
}

// planStep carries out the part of the Run plan in fields.
type planStep struct {
	fields []string // the runPlan fields that the step reads
	run    func(r *stepRun, ctx context.Context, plan *runPlan)
}

// planSteps lists the batch steps in the order that both forms of the run call them.
// External programs, exports and deletions keep their own code.
var planSteps = []planStep{
	{fields: []string{"uploadLimits", "downloadLimits"}, run: (*stepRun).speedLimits},
	{fields: []string{"shareLimits"}, run: (*stepRun).shareLimits},
	{fields: []string{"pause"}, run: func(r *stepRun, ctx context.Context, plan *runPlan) {
		r.bulk(ctx, "pause", "paused", models.ActivityActionPaused, plan.pause)
	}},
	{fields: []string{"resume"}, run: func(r *stepRun, ctx context.Context, plan *runPlan) {
		r.bulk(ctx, "resume", "resumed", models.ActivityActionResumed, plan.resume)
	}},
	{fields: []string{"recheck"}, run: func(r *stepRun, ctx context.Context, plan *runPlan) {
		r.bulk(ctx, "recheck", "rechecked", models.ActivityActionRechecked, plan.recheck)
	}},
	{fields: []string{"reannounce"}, run: func(r *stepRun, ctx context.Context, plan *runPlan) {
		r.bulk(ctx, "reannounce", "reannounced", models.ActivityActionReannounced, plan.reannounce)
	}},
	{fields: []string{"autoManage"}, run: (*stepRun).autoManage},
	{fields: []string{"tags"}, run: (*stepRun).tags},
	{fields: []string{"category"}, run: (*stepRun).category},
	{fields: []string{"move"}, run: (*stepRun).move},
}

func (r *stepRun) runSteps(ctx context.Context, plan *runPlan) {
	for _, step := range planSteps {
		step.run(r, ctx, plan)
	}
}

// write sends the torrents to qBittorrent in batches and returns the torrents of the batches
// that qBittorrent accepted. A dry run sends nothing and returns all the torrents.
func (r *stepRun) write(hashes []string, send func(batch []string) error) []string {
	if r.dryRun || len(hashes) == 0 {
		return hashes
	}
	var accepted []string
	for _, batch := range limitHashBatch(hashes, r.s.cfg.MaxBatchHashes) {
		if send(batch) == nil {
			accepted = append(accepted, batch...)
		}
	}
	return accepted
}

// stepActivity is one Activity row that a step writes.
type stepActivity struct {
	action  string
	outcome string // empty: success in a live run, dry run in a dry run
	hash    string
	reason  string
	details any
	count   int // the torrents that the run summary counts
	rules   map[ruleRef]int
	hashes  []string // the torrents of the sample names
	items   func() []ActivityRunTorrent
}

// record writes the Activity. A live run also adds it to the run summary with the rule counts
// and the sample names.
func (r *stepRun) record(ctx context.Context, a stepActivity) {
	detailsJSON, err := json.Marshal(a.details)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", r.instanceID).Str("action", a.action).Msg("automations: failed to marshal activity details")
		return
	}
	outcome := a.outcome
	switch {
	case outcome != "":
	case r.dryRun:
		outcome = models.ActivityOutcomeDryRun
	default:
		outcome = models.ActivityOutcomeSuccess
	}
	activity := &models.AutomationActivity{
		InstanceID: r.instanceID,
		Hash:       a.hash,
		Action:     a.action,
		Outcome:    outcome,
		Reason:     a.reason,
		Details:    detailsJSON,
		CreatedAt:  time.Now().UTC(),
	}
	if !r.dryRun {
		r.summary.recordActivity(activity, a.count)
		r.summary.recordRuleCounts(a.action, outcome, a.rules)
		r.summary.addTorrentSamples(collectTorrentNamesForHashes(a.hashes, r.torrentByHash), 3)
	}
	if r.s.activityStore == nil {
		return
	}
	activityID, err := r.s.activityStore.CreateWithID(ctx, activity)
	if err != nil {
		log.Warn().Err(err).Int("instanceID", r.instanceID).Str("action", a.action).Msg("automations: failed to record activity")
		return
	}
	if r.dryRun {
		r.activities = append(r.activities, activity)
	}
	if r.s.activityRuns == nil || a.items == nil {
		return
	}
	if items := a.items(); len(items) > 0 {
		r.s.activityRuns.Put(activityID, r.instanceID, items)
	}
}

// recordCount writes the Activity of an Action that only counts its torrents.
func (r *stepRun) recordCount(ctx context.Context, action string, hashes []string, rules map[string]ruleRef) {
	if len(hashes) == 0 {
		return
	}
	r.record(ctx, stepActivity{
		action:  action,
		details: map[string]int{"count": len(hashes)},
		count:   len(hashes),
		rules:   buildRuleCountsFromHashes(hashes, rules),
		hashes:  hashes,
		items:   func() []ActivityRunTorrent { return buildRunItemsFromHashes(hashes, r.torrentByHash, r.s.syncManager) },
	})
}

// speedLimits sets the upload and download limits and writes one Activity for both.
func (r *stepRun) speedLimits(ctx context.Context, plan *runPlan) {
	upload := r.speedLimit(ctx, "upload", plan.uploadLimits, r.s.syncManager.SetTorrentUploadLimit)
	download := r.speedLimit(ctx, "download", plan.downloadLimits, r.s.syncManager.SetTorrentDownloadLimit)
	if len(upload) == 0 && len(download) == 0 {
		return
	}
	limits := make(map[string]int) // "upload:1024" -> count, "download:2048" -> count
	for limit, hashes := range upload {
		limits[fmt.Sprintf("upload:%d", limit)] = len(hashes)
	}
	for limit, hashes := range download {
		limits[fmt.Sprintf("download:%d", limit)] = len(hashes)
	}
	uploadHashes, uploadWrites := flattenHashes(upload)
	downloadHashes, downloadWrites := flattenHashes(download)
	rules := make(map[ruleRef]int)
	for _, counts := range []map[ruleRef]int{
		buildRuleCountsFromHashes(uploadHashes, plan.uploadLimits.rules),
		buildRuleCountsFromHashes(downloadHashes, plan.downloadLimits.rules),
	} {
		for ref, n := range counts {
			rules[ref] += n
		}
	}
	r.record(ctx, stepActivity{
		action:  models.ActivityActionSpeedLimitsChanged,
		details: map[string]map[string]int{"limits": limits},
		count:   uploadWrites + downloadWrites,
		rules:   rules,
		hashes:  slices.Concat(uploadHashes, downloadHashes),
		items: func() []ActivityRunTorrent {
			return buildSpeedLimitRunItems(upload, download, r.torrentByHash, r.s.syncManager)
		},
	})
}

// speedLimit sets one direction of the speed limits. Each refused batch writes a failed row.
func (r *stepRun) speedLimit(
	ctx context.Context,
	limitType string,
	planned plannedAction[int64],
	setLimit func(ctx context.Context, instanceID int, hashes []string, limit int64) error,
) map[int64][]string {
	accepted := make(map[int64][]string)
	for limit, hashes := range planned.byTarget {
		ok := r.write(hashes, func(batch []string) error {
			err := setLimit(ctx, r.instanceID, batch, limit)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", r.instanceID).Int64("limitKiB", limit).Int("count", len(batch)).Str("limitType", limitType).Msg("automations: speed limit failed")
				r.record(ctx, stepActivity{
					action:  models.ActivityActionLimitFailed,
					outcome: models.ActivityOutcomeFailed,
					hash:    strings.Join(batch, ","),
					reason:  limitType + " limit failed: " + err.Error(),
					details: map[string]any{"limitKiB": limit, "count": len(batch), "type": limitType},
					count:   len(batch),
					rules:   buildRuleCountsFromHashes(batch, planned.rules),
					hashes:  batch,
				})
			}
			return err
		})
		if len(ok) > 0 {
			accepted[limit] = ok
		}
	}
	return accepted
}

// shareLimits sets the share limits. The Activity counts the torrents by ratio, seeding time
// and inactive time, so groups that differ only in the share action or the mode add up.
func (r *stepRun) shareLimits(ctx context.Context, plan *runPlan) {
	planned := plan.shareLimits
	ruleMaps := []map[string]ruleRef{planned.ratioRules, planned.seedingRules, planned.actionRules, planned.modeRules}
	accepted := make(map[shareKey][]string)
	for key, hashes := range planned.byTarget {
		ok := r.write(hashes, func(batch []string) error {
			err := r.s.syncManager.SetTorrentShareLimit(ctx, r.instanceID, batch, key.ratio, key.seed, key.inactive, key.action, key.mode)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", r.instanceID).Float64("ratio", key.ratio).Int64("seedMinutes", key.seed).Int64("inactiveMinutes", key.inactive).Str("action", key.action).Str("mode", key.mode).Int("count", len(batch)).Msg("automations: share limit failed")
				r.record(ctx, stepActivity{
					action:  models.ActivityActionLimitFailed,
					outcome: models.ActivityOutcomeFailed,
					hash:    strings.Join(batch, ","),
					reason:  "share limit failed: " + err.Error(),
					details: map[string]any{"ratio": key.ratio, "seedMinutes": key.seed, "inactiveMinutes": key.inactive, "action": key.action, "mode": key.mode, "count": len(batch), "type": "share"},
					count:   len(batch),
					rules:   buildRuleCountsFromHashMaps(batch, ruleMaps...),
					hashes:  batch,
				})
			}
			return err
		})
		if len(ok) > 0 {
			accepted[key] = ok
		}
	}
	if len(accepted) == 0 {
		return
	}
	limits := make(map[string]int)
	for key, hashes := range accepted {
		limits[fmt.Sprintf("%.2f:%d:%d", key.ratio, key.seed, key.inactive)] += len(hashes)
	}
	hashes, writes := flattenHashes(accepted)
	r.record(ctx, stepActivity{
		action:  models.ActivityActionShareLimitsChanged,
		details: map[string]map[string]int{"limits": limits},
		count:   writes,
		rules:   buildRuleCountsFromHashMaps(hashes, ruleMaps...),
		hashes:  hashes,
		items: func() []ActivityRunTorrent {
			return buildShareLimitRunItems(accepted, r.torrentByHash, r.s.syncManager)
		},
	})
}

// bulk carries out pause, resume, recheck or reannounce. A refused batch only logs.
func (r *stepRun) bulk(ctx context.Context, action, done, activityAction string, planned plannedTorrents) {
	accepted := r.write(planned.hashes, func(batch []string) error {
		err := r.s.syncManager.BulkAction(ctx, r.instanceID, batch, action)
		if err != nil {
			log.Warn().Err(err).Int("instanceID", r.instanceID).Int("count", len(batch)).Msg("automations: " + action + " action failed")
		} else {
			log.Info().Int("instanceID", r.instanceID).Int("count", len(batch)).Msg("automations: " + done + " torrents")
		}
		return err
	})
	r.recordCount(ctx, activityAction, accepted, planned.rules)
}

// autoManage turns automatic torrent management on or off. A refused batch only logs.
func (r *stepRun) autoManage(ctx context.Context, plan *runPlan) {
	accepted := make([]string, 0, len(plan.autoManage.byTarget[true])+len(plan.autoManage.byTarget[false]))
	for _, enable := range []bool{true, false} {
		mode := map[bool]string{true: "enabled", false: "disabled"}[enable]
		accepted = append(accepted, r.write(plan.autoManage.byTarget[enable], func(batch []string) error {
			err := r.s.syncManager.SetAutoTMM(ctx, r.instanceID, batch, enable)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", r.instanceID).Int("count", len(batch)).Msg("automations: auto management action failed")
			} else {
				log.Info().Int("instanceID", r.instanceID).Int("count", len(batch)).Str("mode", mode).Msg("automations: auto management for torrents")
			}
			return err
		})...)
	}
	r.recordCount(ctx, models.ActivityActionAutoManaged, accepted, plan.autoManage.rules)
}

// tags resets the managed tags in a live run, then changes the tags. A refused batch only logs.
func (r *stepRun) tags(ctx context.Context, plan *runPlan) {
	if len(r.resetTags) > 0 {
		if err := r.s.syncManager.DeleteTags(ctx, r.instanceID, r.resetTags); err != nil {
			log.Warn().Err(err).Int("instanceID", r.instanceID).Strs("tags", r.resetTags).Msg("automations: failed to delete managed tags from client before retagging")
		} else {
			log.Info().Int("instanceID", r.instanceID).Strs("tags", r.resetTags).Msg("automations: deleted managed tags from client before retagging")
		}
	}
	if len(plan.tags) == 0 {
		return
	}
	accepted := plan.tags
	if !r.dryRun {
		accepted = r.s.applyTagChanges(ctx, r.instanceID, plan.tags)
	}
	if len(accepted) == 0 {
		return
	}

	added := make(map[string]int)   // tag -> count of torrents
	removed := make(map[string]int) // tag -> count of torrents
	rules := make(map[ruleRef]int)
	for _, change := range accepted {
		for _, tag := range change.toAdd {
			added[tag]++
			if ref, ok := change.ruleByTag[tag]; ok {
				rules[ref]++
			}
		}
		for _, tag := range change.toRemove {
			removed[tag]++
			if ref, ok := change.ruleByTag[tag]; ok {
				rules[ref]++
			}
		}
	}
	hashes := slices.Collect(maps.Keys(accepted))
	r.record(ctx, stepActivity{
		action:  models.ActivityActionTagsChanged,
		details: map[string]map[string]int{"added": added, "removed": removed},
		count:   len(accepted),
		rules:   rules,
		hashes:  hashes,
		items:   func() []ActivityRunTorrent { return buildTagRunItems(accepted, r.torrentByHash, r.s.syncManager) },
	})
	if !r.dryRun {
		r.summary.addTagCounts(added, removed)
		r.summary.addTagSamples(collectTorrentNamesForHashes(hashes, r.torrentByHash), 3)
	}
}

// category sets the category. A refused batch only logs.
func (r *stepRun) category(ctx context.Context, plan *runPlan) {
	accepted := make(map[string][]string)
	for _, category := range slices.Sorted(maps.Keys(plan.category.byTarget)) {
		ok := r.write(plan.category.byTarget[category], func(batch []string) error {
			err := r.s.syncManager.SetCategory(ctx, r.instanceID, batch, category)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", r.instanceID).Str("category", category).Int("count", len(batch)).Msg("automations: set category failed")
			} else {
				log.Debug().Int("instanceID", r.instanceID).Str("category", category).Int("count", len(batch)).Msg("automations: set category on torrents")
			}
			return err
		})
		if len(ok) > 0 {
			accepted[category] = ok
		}
	}
	if len(accepted) == 0 {
		return
	}
	hashes, writes := flattenHashes(accepted)
	r.record(ctx, stepActivity{
		action:  models.ActivityActionCategoryChanged,
		details: map[string]map[string]int{"categories": countByTarget(accepted)},
		count:   writes,
		rules:   buildRuleCountsFromHashes(hashes, plan.category.rules),
		hashes:  hashes,
		items:   func() []ActivityRunTorrent { return buildCategoryRunItems(accepted, r.torrentByHash, r.s.syncManager) },
	})
}

// move sets the save path. All refused batches of the run write one failed row.
func (r *stepRun) move(ctx context.Context, plan *runPlan) {
	accepted := make(map[string][]string)
	refused := make(map[string][]string)
	for _, path := range slices.Sorted(maps.Keys(plan.move.byTarget)) {
		ok := r.write(plan.move.byTarget[path], func(batch []string) error {
			err := r.s.syncManager.SetLocation(ctx, r.instanceID, batch, path)
			if err != nil {
				log.Warn().Err(err).Int("instanceID", r.instanceID).Str("path", path).Strs("hashes", batch).Msg("automations: move failed")
				refused[path] = append(refused[path], batch...)
			} else {
				log.Debug().Int("instanceID", r.instanceID).Str("path", path).Strs("hashes", batch).Msg("automations: moved torrent")
			}
			return err
		})
		if len(ok) > 0 {
			accepted[path] = ok
		}
	}
	if len(accepted) > 0 {
		hashes, writes := flattenHashes(accepted)
		r.record(ctx, stepActivity{
			action:  models.ActivityActionMoved,
			details: map[string]map[string]int{"paths": countByTarget(accepted)},
			count:   writes,
			rules:   buildRuleCountsFromHashes(hashes, plan.move.rules),
			hashes:  hashes,
			items:   func() []ActivityRunTorrent { return buildMoveRunItems(accepted, r.torrentByHash, r.s.syncManager) },
		})
	}
	if len(refused) > 0 {
		hashes, writes := flattenHashes(refused)
		r.record(ctx, stepActivity{
			action:  models.ActivityActionMoved,
			outcome: models.ActivityOutcomeFailed,
			details: map[string]map[string]int{"paths": countByTarget(refused)},
			count:   writes,
			rules:   buildRuleCountsFromHashes(hashes, plan.move.rules),
			hashes:  hashes,
		})
	}
}

// flattenHashes returns each torrent of the targets once, and the number of writes. A
// cross-seed can join two category targets in one run: its rule counts it once, and the
// Activity counts both writes.
func flattenHashes[K comparable](byTarget map[K][]string) (unique []string, writes int) {
	var all []string
	for _, hashes := range byTarget {
		all = append(all, hashes...)
	}
	return dedupeHashes(all), len(all)
}

// countByTarget returns the number of torrents of each target.
func countByTarget(byTarget map[string][]string) map[string]int {
	counts := make(map[string]int, len(byTarget))
	for target, hashes := range byTarget {
		counts[target] = len(hashes)
	}
	return counts
}
