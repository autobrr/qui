// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/expr-lang/expr"
	"github.com/expr-lang/expr/vm"
	"github.com/rs/zerolog/log"
)

func statusFiltersRequireTrackerData(statuses []string) bool {
	for _, status := range statuses {
		switch status {
		case "unregistered", "tracker_down", "tracker_error":
			return true
		}
	}

	return false
}

// helper to make it possible to filter by tracker-derived states in FilterSidebar
func filtersRequireTrackerData(filters FilterOptions) bool {
	return statusFiltersRequireTrackerData(filters.Status) ||
		statusFiltersRequireTrackerData(filters.ExcludeStatus)
}

// statusCounter counts the library per torrent state, which expandInto turns
// into status keys once. A library has thousands of torrents and about fifteen
// states, so that is one string map increment per torrent instead of eight.
type statusCounter struct {
	byState   map[qbt.TorrentState]int
	completed int
}

// countTorrentStatuses counts torrent statuses efficiently in a single pass
func (sm *SyncManager) countTorrentStatuses(torrent *qbt.Torrent, counter *statusCounter) {
	counter.byState[torrent.State]++

	if torrent.Progress == 1 {
		counter.completed++
	}
}

// expandInto adds the accumulated counts to the status map.
func (c *statusCounter) expandInto(counts map[string]int) {
	for state, count := range c.byState {
		counts["all"] += count

		if slices.Contains(torrentStateCategories[qbt.TorrentFilterActive], state) {
			counts["active"] += count
		} else {
			counts["inactive"] += count
		}

		// A torrent is stopped if it is in either the old paused states or the new
		// stopped ones. Running is the inverse.
		isPausedOrStopped := slices.Contains(torrentStateCategories[qbt.TorrentFilterPaused], state) ||
			slices.Contains(torrentStateCategories[qbt.TorrentFilterStopped], state)
		if isPausedOrStopped {
			counts["stopped"] += count
			counts["paused"] += count // For backward compatibility
		} else {
			counts["running"] += count
			counts["resumed"] += count // For backward compatibility
		}

		for _, status := range countableStatusesForState[state] {
			counts[status] += count
		}
	}

	counts["completed"] += c.completed
}

// countableStatusesForState inverts torrentStateCategories so counting a
// torrent is one map lookup instead of a scan of every category. "active",
// "paused" and "stopped" are left out because countTorrentStatuses counts them
// directly, including their inverses.
var countableStatusesForState = func() map[qbt.TorrentState][]string {
	byState := make(map[qbt.TorrentState][]string)
	for status, states := range torrentStateCategories {
		if status == qbt.TorrentFilterActive || status == qbt.TorrentFilterPaused || status == qbt.TorrentFilterStopped {
			continue
		}
		for _, state := range states {
			byState[state] = append(byState[state], string(status))
		}
	}
	return byState
}()

// torrentHasAnyTag reports whether the comma-separated tag string contains any
// of the wanted tags. strings.SplitSeq and TrimSpace both return sub-slices of
// the original string, so this walks the tags without allocating.
func torrentHasAnyTag(tags string, wanted map[string]struct{}) bool {
	if len(wanted) == 0 {
		return false
	}
	for tag := range strings.SplitSeq(tags, ",") {
		if _, ok := wanted[strings.TrimSpace(tag)]; ok {
			return true
		}
	}
	return false
}

// applyManualFilters applies all filters manually when library filtering is insufficient.
// With no tracker health snapshot, the tracker health statuses match no torrent.
func (sm *SyncManager) applyManualFilters(
	client *Client,
	torrents []qbt.Torrent,
	filters FilterOptions,
	mainData *qbt.MainData,
	categories map[string]qbt.Category,
	useSubcategories bool,
) []qbt.Torrent {
	return sm.applyManualFiltersWithTrackerHealth(client, torrents, filters, mainData, categories, useSubcategories, nil)
}

// applyManualFiltersWithTrackerHealth applies manual filters. The tracker
// health statuses match the verdicts in cachedHealth.
func (sm *SyncManager) applyManualFiltersWithTrackerHealth(
	client *Client,
	torrents []qbt.Torrent,
	filters FilterOptions,
	mainData *qbt.MainData,
	categories map[string]qbt.Category,
	useSubcategories bool,
	cachedHealth *TrackerHealthSnapshot,
) []qbt.Torrent {
	var matched []int

	// A bad expression fails identically for every torrent, so record the first
	// failure and report once after the loop instead of per torrent.
	var exprErr error
	exprFailures := 0

	hashFilterSet := make(map[string]struct{}, len(filters.Hashes))
	for _, h := range filters.Hashes {
		if h == "" {
			continue
		}
		hashFilterSet[strings.ToUpper(h)] = struct{}{}
	}

	var categoryNames []string
	if useSubcategories {
		categoryNames = collectCategoryNames(mainData, categories)
	}

	// Category set for O(1) lookups
	categorySet := make(map[string]struct{}, len(filters.Categories))
	for _, c := range filters.Categories {
		categorySet[c] = struct{}{}
		if useSubcategories && c != "" {
			expandCategorySet(categorySet, c, categoryNames)
		}
	}

	excludeCategorySet := make(map[string]struct{}, len(filters.ExcludeCategories))
	for _, c := range filters.ExcludeCategories {
		excludeCategorySet[c] = struct{}{}
		if useSubcategories && c != "" {
			expandCategorySet(excludeCategorySet, c, categoryNames)
		}
	}

	// Prepare tag filter sets so each torrent's tag string is split once,
	// instead of once per filter tag.
	includeUntagged := false
	includeTags := make(map[string]struct{}, len(filters.Tags))
	for _, t := range filters.Tags {
		if t == "" {
			includeUntagged = true
		}
		// The empty tag stays in the include set: matching it against an empty
		// segment of a tag string (e.g. "movies,") is existing behavior.
		includeTags[t] = struct{}{}
	}

	excludeUntagged := false
	excludeTags := make(map[string]struct{}, len(filters.ExcludeTags))
	for _, t := range filters.ExcludeTags {
		if t == "" {
			excludeUntagged = true
			continue
		}
		excludeTags[t] = struct{}{}
	}

	// Precompute tracker filter set for O(1) lookups
	trackerFilterSet := make(map[string]struct{}, len(filters.Trackers))
	for _, t := range filters.Trackers {
		trackerFilterSet[t] = struct{}{}
	}

	excludeTrackerSet := make(map[string]struct{}, len(filters.ExcludeTrackers))
	for _, t := range filters.ExcludeTrackers {
		excludeTrackerSet[t] = struct{}{}
	}

	// Precompute a map from torrent hash -> set of tracker domains
	// Uses cached validated mapping when available, falling back to MainData.Trackers
	torrentHashToDomains := map[string]map[string]struct{}{}
	var trackerExclusions map[string]map[string]struct{}
	if client != nil {
		trackerExclusions = client.getTrackerExclusionsCopy()
	}

	// Only build the mapping if tracker filters are active
	if len(filters.Trackers) != 0 || len(filters.ExcludeTrackers) != 0 {
		// Try to use the pre-validated tracker mapping (built in background refresh)
		var validatedMapping *ValidatedTrackerMapping
		if client != nil {
			validatedMapping = sm.getValidatedTrackerMapping(client.instanceID)
		}

		if validatedMapping != nil {
			// Use cached HashToDomains - already validated, no need for torrentBelongsToTrackerDomain
			for _, torrent := range torrents {
				if domains, ok := validatedMapping.HashToDomains[torrent.Hash]; ok {
					for domain := range domains {
						// If filters are set and this domain isn't in either include or exclude sets, skip it
						if len(trackerFilterSet) > 0 || len(excludeTrackerSet) > 0 {
							if _, inFilter := trackerFilterSet[domain]; !inFilter {
								if _, inExclude := excludeTrackerSet[domain]; !inExclude {
									continue
								}
							}
						}

						// Check exclusions
						if hashesToSkip, hasExclusions := trackerExclusions[domain]; hasExclusions {
							if _, skip := hashesToSkip[torrent.Hash]; skip {
								continue
							}
						}

						if torrentHashToDomains[torrent.Hash] == nil {
							torrentHashToDomains[torrent.Hash] = make(map[string]struct{})
						}
						torrentHashToDomains[torrent.Hash][domain] = struct{}{}
					}
				}
			}
		} else if mainData != nil && mainData.Trackers != nil {
			// Fallback: MainData.Trackers. Permanent below qBittorrent 5.1 and on
			// instances whose tracker hydration keeps failing, not just a cold start.
			// Build torrentMap for O(1) lookups to validate tracker membership
			torrentMap := make(map[string]*qbt.Torrent, len(torrents))
			for i := range torrents {
				torrentMap[torrents[i].Hash] = &torrents[i]
			}

			for trackerURL, hashes := range mainData.Trackers {
				domain := sm.ExtractDomainFromURL(trackerURL)
				if domain == "" {
					continue
				}

				// If filters are set and this domain isn't in either include or exclude sets, skip storing it
				if len(trackerFilterSet) > 0 || len(excludeTrackerSet) > 0 {
					if _, ok := trackerFilterSet[domain]; !ok {
						if _, excludeMatch := excludeTrackerSet[domain]; !excludeMatch {
							continue
						}
					}
				}

				for _, h := range hashes {
					// Validate torrent actually belongs to this tracker domain.
					// MainData.Trackers can be stale when trackers are modified.
					if torrent, exists := torrentMap[h]; exists {
						if !sm.torrentBelongsToTrackerDomain(torrent, domain) {
							continue
						}
					}

					if hashesToSkip, ok := trackerExclusions[domain]; ok {
						if _, skip := hashesToSkip[h]; skip {
							continue
						}
					}

					if torrentHashToDomains[h] == nil {
						torrentHashToDomains[h] = make(map[string]struct{})
					}
					torrentHashToDomains[h][domain] = struct{}{}
				}
			}
		}
	}

	var program *vm.Program
	var compileErr error
	if len(filters.Expr) > 0 {
		if p, ok := sm.exprCache.Get(filters.Expr); ok {
			log.Trace().Str("expr", filters.Expr).Msg("Using cached expression")
			program = p
		} else {
			// User expressions see raw qbt.Torrent fields, including the
			// never-completed sentinels in CompletionOn/SeenComplete
			// (NormalizeCompletionTimestamp); guarding here would silently
			// rewrite user-authored comparisons, so they are excluded.
			program, compileErr = expr.Compile(filters.Expr, expr.Env(qbt.Torrent{}), expr.AsBool())
			if compileErr != nil {
				log.Error().Err(compileErr).Msg("Failed to compile expression")
			} else if ok := sm.exprCache.Set(filters.Expr, program, 5*time.Minute); !ok {
				log.Warn().Str("expr", filters.Expr).Msg("Failed to cache expression")
			}
		}
	}

torrentsLoop:
	for i := range torrents {
		torrent := &torrents[i]

		if len(hashFilterSet) > 0 {
			match := false
			candidates := []string{torrent.Hash, torrent.InfohashV1, torrent.InfohashV2}
			for _, candidate := range candidates {
				if candidate == "" {
					continue
				}
				if _, ok := hashFilterSet[strings.ToUpper(candidate)]; ok {
					match = true
					break
				}
			}
			if !match {
				continue
			}
		}

		// Status filters (OR logic)
		if len(filters.Status) > 0 {
			matched := false
			for _, status := range filters.Status {
				if sm.matchTorrentStatusWithTrackerHealth(torrent, status, cachedHealth) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}

		if len(filters.ExcludeStatus) > 0 {
			for _, status := range filters.ExcludeStatus {
				if sm.matchTorrentStatusWithTrackerHealth(torrent, status, cachedHealth) {
					continue torrentsLoop
				}
			}
		}

		// Category filters (OR logic)
		if len(filters.Categories) > 0 {
			if _, ok := categorySet[torrent.Category]; !ok {
				continue
			}
		}

		if len(excludeCategorySet) > 0 {
			if _, ok := excludeCategorySet[torrent.Category]; ok {
				continue
			}
		}

		// Tag filters (OR logic)
		if len(filters.Tags) > 0 {
			if torrent.Tags == "" {
				if !includeUntagged {
					continue
				}
			} else {
				if !torrentHasAnyTag(torrent.Tags, includeTags) {
					continue
				}
			}
		}

		// Exclude tags (AND logic - any match should exclude the torrent)
		if excludeUntagged || len(excludeTags) > 0 {
			if torrent.Tags == "" {
				if excludeUntagged {
					continue
				}
			} else {
				if torrentHasAnyTag(torrent.Tags, excludeTags) {
					continue
				}
			}
		}

		// Tracker filters (OR logic)
		if len(filters.Trackers) > 0 {
			// If we precomputed MainData domains, use them
			if len(torrentHashToDomains) > 0 {
				if domains, ok := torrentHashToDomains[torrent.Hash]; ok && len(domains) > 0 {
					found := false
					for domain := range domains {
						if _, ok := trackerFilterSet[domain]; ok {
							found = true
							break
						}
					}
					if !found {
						continue
					}
				} else {
					// No trackers known for this torrent
					if _, ok := trackerFilterSet[""]; !ok {
						continue
					}
				}
			} else {
				// Fallback to torrent.Tracker
				if torrent.Tracker == "" {
					if _, ok := trackerFilterSet[""]; !ok {
						continue
					}
				} else {
					trackerDomain := sm.ExtractDomainFromURL(torrent.Tracker)
					if trackerDomain == "" {
						continue
					}
					if _, ok := trackerFilterSet[trackerDomain]; !ok {
						continue
					}
				}
			}
		}

		if len(excludeTrackerSet) > 0 {
			if len(torrentHashToDomains) > 0 {
				if domains, ok := torrentHashToDomains[torrent.Hash]; ok && len(domains) > 0 {
					excluded := false
					for domain := range domains {
						if _, ok := excludeTrackerSet[domain]; ok {
							excluded = true
							break
						}
					}
					if excluded {
						continue
					}
				} else {
					// No trackers known for this torrent
					if _, ok := excludeTrackerSet[""]; ok {
						continue
					}
				}
			} else {
				// Fallback to torrent.Tracker metadata
				if torrent.Tracker == "" {
					if _, ok := excludeTrackerSet[""]; ok {
						continue
					}
				} else {
					trackerDomain := sm.ExtractDomainFromURL(torrent.Tracker)
					if trackerDomain != "" {
						if _, ok := excludeTrackerSet[trackerDomain]; ok {
							continue
						}
					}
				}
			}
		}

		if len(filters.Expr) > 0 && compileErr == nil {
			env := *torrent
			if filters.HideTrackerPasskeys {
				env = torrentWithoutPasskeys(torrent)
				env.MagnetURI = ""
			}
			result, err := expr.Run(program, env)
			if err != nil {
				if exprErr == nil {
					exprErr = err
				}
				exprFailures++
				continue
			}

			expResult, ok := result.(bool)
			if !ok {
				if exprErr == nil {
					exprErr = errors.New("expression result is not a boolean")
				}
				exprFailures++
				continue
			}

			if !expResult {
				continue
			}
		}

		// If we reach here, torrent passed all active filters
		matched = append(matched, i)
	}

	// Collecting indices first keeps the growth cost on an int slice; the
	// result is then materialized once at exactly the right size instead of
	// repeatedly doubling a slice of ~600-byte structs. When every torrent
	// passed, the input already is the result and no copy happens at all.
	filtered := torrents
	if len(matched) != len(torrents) {
		filtered = make([]qbt.Torrent, len(matched))
		for pos, idx := range matched {
			filtered[pos] = torrents[idx]
		}
	}

	if exprFailures > 0 {
		log.Error().
			Err(exprErr).
			Int("failedTorrents", exprFailures).
			Int("totalTorrents", len(torrents)).
			Str("expr", filters.Expr).
			Msg("Failed to evaluate expression")
	}

	log.Trace().
		Int("inputTorrents", len(torrents)).
		Int("filteredTorrents", len(filtered)).
		Int("statusFilters", len(filters.Status)).
		Int("excludeStatusFilters", len(filters.ExcludeStatus)).
		Int("categoryFilters", len(filters.Categories)).
		Int("excludeCategoryFilters", len(filters.ExcludeCategories)).
		Int("tagFilters", len(filters.Tags)).
		Int("excludeTagFilters", len(filters.ExcludeTags)).
		Int("trackerFilters", len(filters.Trackers)).
		Int("excludeTrackerFilters", len(filters.ExcludeTrackers)).
		Msg("Applied manual filtering with multiple selections")

	return filtered
}

func collectCategoryNames(mainData *qbt.MainData, categories map[string]qbt.Category) []string {
	var mainCount int
	if mainData != nil && mainData.Categories != nil {
		mainCount = len(mainData.Categories)
	}
	if len(categories) == 0 && mainCount == 0 {
		return nil
	}

	names := make([]string, 0, len(categories)+mainCount)
	seen := make(map[string]struct{}, len(categories))

	for name := range categories {
		names = append(names, name)
		seen[name] = struct{}{}
	}

	if mainCount > 0 {
		for name := range mainData.Categories {
			if _, exists := seen[name]; exists {
				continue
			}
			names = append(names, name)
			seen[name] = struct{}{}
		}
	}

	return names
}

func expandCategorySet(target map[string]struct{}, parent string, categoryNames []string) {
	if len(categoryNames) == 0 {
		return
	}

	prefix := parent + "/"
	for _, name := range categoryNames {
		if strings.HasPrefix(name, prefix) {
			target[name] = struct{}{}
		}
	}
}

// Torrent state categories for fast lookup
var torrentStateCategories = map[qbt.TorrentFilter][]qbt.TorrentState{
	qbt.TorrentFilterDownloading:        {qbt.TorrentStateDownloading, qbt.TorrentStateStalledDl, qbt.TorrentStateMetaDl, qbt.TorrentStateQueuedDl, qbt.TorrentStateAllocating, qbt.TorrentStateCheckingDl, qbt.TorrentStateForcedDl},
	qbt.TorrentFilterUploading:          {qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp},
	qbt.TorrentFilter("seeding"):        {qbt.TorrentStateUploading, qbt.TorrentStateStalledUp, qbt.TorrentStateQueuedUp, qbt.TorrentStateCheckingUp, qbt.TorrentStateForcedUp},
	qbt.TorrentFilterPaused:             {qbt.TorrentStatePausedDl, qbt.TorrentStatePausedUp, qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp},
	qbt.TorrentFilterActive:             {qbt.TorrentStateDownloading, qbt.TorrentStateUploading, qbt.TorrentStateForcedDl, qbt.TorrentStateForcedUp},
	qbt.TorrentFilterStalled:            {qbt.TorrentStateStalledDl, qbt.TorrentStateStalledUp},
	qbt.TorrentFilterChecking:           {qbt.TorrentStateCheckingDl, qbt.TorrentStateCheckingUp, qbt.TorrentStateCheckingResumeData},
	qbt.TorrentFilterError:              {qbt.TorrentStateError, qbt.TorrentStateMissingFiles},
	qbt.TorrentFilterMoving:             {qbt.TorrentStateMoving},
	qbt.TorrentFilterStalledUploading:   {qbt.TorrentStateStalledUp},
	qbt.TorrentFilterStalledDownloading: {qbt.TorrentStateStalledDl},
	qbt.TorrentFilterStopped:            {qbt.TorrentStateStoppedDl, qbt.TorrentStateStoppedUp},
	// TorrentFilterRunning is handled specially in matchTorrentStatus as inverse of stopped
}

// matchTorrentStatus checks if a torrent matches a specific status filter
// matchTorrentStatusWithTrackerHealth uses cached tracker-health data only for
// tracker-health statuses; all qBittorrent state filters keep their normal
// state-based matching.
func (sm *SyncManager) matchTorrentStatusWithTrackerHealth(torrent *qbt.Torrent, status string, cachedHealth *TrackerHealthSnapshot) bool {
	switch health := TrackerHealth(strings.ToLower(status)); health {
	case TrackerHealthUnregistered, TrackerHealthDown, TrackerHealthError:
		return cachedHealth.Health(torrent.Hash) == health
	}

	// Handle special cases first
	switch qbt.TorrentFilter(status) {
	case qbt.TorrentFilterAll:
		return true
	case qbt.TorrentFilterCompleted:
		return torrent.Progress == 1
	case qbt.TorrentFilterInactive:
		// Inactive is the inverse of active
		return !slices.Contains(torrentStateCategories[qbt.TorrentFilterActive], torrent.State)
	case qbt.TorrentFilterRunning, qbt.TorrentFilterResumed:
		// Running/Resumed means "not paused and not stopped"
		pausedStates := torrentStateCategories[qbt.TorrentFilterPaused]
		stoppedStates := torrentStateCategories[qbt.TorrentFilterStopped]
		return !slices.Contains(pausedStates, torrent.State) && !slices.Contains(stoppedStates, torrent.State)
	case qbt.TorrentFilterStopped, qbt.TorrentFilterPaused:
		// Stopped/Paused includes both paused and stopped states
		pausedStates := torrentStateCategories[qbt.TorrentFilterPaused]
		stoppedStates := torrentStateCategories[qbt.TorrentFilterStopped]
		return slices.Contains(pausedStates, torrent.State) || slices.Contains(stoppedStates, torrent.State)
	default:
		// Grouped categories and direct state names fall through below.
	}

	// For grouped status categories, check if state is in the category
	if category, exists := torrentStateCategories[qbt.TorrentFilter(status)]; exists {
		return slices.Contains(category, torrent.State)
	}

	// For everything else, just do direct equality with the string representation
	return string(torrent.State) == status
}
