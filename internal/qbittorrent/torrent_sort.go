// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"cmp"
	"reflect"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/stringutils"
)

// torrentSortFields maps the JSON name of each scalar qbt.Torrent field to its
// field index. A field that go-qbittorrent adds later sorts without an edit.
var torrentSortFields = func() map[string]int {
	typ := reflect.TypeFor[qbt.Torrent]()
	fields := make(map[string]int, typ.NumField())
	for i := range typ.NumField() {
		field := typ.Field(i)
		name, _, _ := strings.Cut(field.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		// Pointer and slice fields stay out and sort by name, like an unknown column.
		if k := field.Type.Kind(); k == reflect.Int64 || k == reflect.Float64 || k == reflect.Bool || k == reflect.String {
			fields[name] = i
		}
	}
	return fields
}()

// sortTorrents sorts the rows of one instance in place. cachedHealth fills in
// the tracker health of rows whose trackers were not fetched.
func (sm *SyncManager) sortTorrents(torrents []qbt.Torrent, column string, desc bool, trackerHealthSupported bool, cachedHealth *TrackerHealthCounts) {
	rows := make([]*qbt.Torrent, len(torrents))
	for i := range torrents {
		rows[i] = &torrents[i]
	}
	health := func(i int) TrackerHealth {
		if !trackerHealthSupported {
			return ""
		}
		return sm.resolveTrackerHealth(rows[i], cachedHealth)
	}
	sortByIndex(torrents, sm.torrentOrder(rows, health, column, desc, func(a, b int) int { return a - b }))
}

// sortCrossInstanceTorrents sorts Unified rows in place. Rows that compare
// equal sort by instance, ascending in each direction.
func (sm *SyncManager) sortCrossInstanceTorrents(torrents []CrossInstanceTorrentView, column string, desc bool, skipTrackerHydration bool) {
	rows := make([]*qbt.Torrent, len(torrents))
	for i := range torrents {
		rows[i] = torrents[i].Torrent
	}
	// Each instance's own state sort reads the fetched trackers, and reads the
	// cached health only when the request skipped the fetch. The shown health
	// always falls back to the cache, so it can differ.
	var cachedByInstance map[int]*TrackerHealthCounts
	health := func(i int) TrackerHealth {
		var cached *TrackerHealthCounts
		if skipTrackerHydration {
			id := torrents[i].InstanceID
			var ok bool
			if cached, ok = cachedByInstance[id]; !ok {
				if cachedByInstance == nil {
					cachedByInstance = make(map[int]*TrackerHealthCounts)
				}
				cached = sm.GetTrackerHealthCounts(id)
				cachedByInstance[id] = cached
			}
		}
		return sm.resolveTrackerHealth(rows[i], cached)
	}
	compareInstance := func(a, b int) int {
		return cmp.Or(
			stringutils.CompareFold(torrents[a].InstanceName, torrents[b].InstanceName),
			cmp.Compare(torrents[a].InstanceID, torrents[b].InstanceID),
		)
	}
	order := sm.torrentOrder(rows, health, column, desc, func(a, b int) int {
		return cmp.Or(compareInstance(a, b), a-b)
	})

	if column != "instance" {
		sortByIndex(torrents, order)
		return
	}
	sortByIndex(torrents, func(a, b int) int {
		if result := compareInstance(a, b); result != 0 {
			return flipIf(desc, result)
		}
		return order(a, b)
	})
}

// torrentOrder returns the order of one sort column as a comparator over row
// indexes. It resolves the sort keys once per row, and calls health only for
// the state column. tie decides between two rows with the same hash.
//
// A plain field sorts by its value, then by hash, and descending flips both.
// The columns below with their own rule keep the order qui gave them on top of
// the go-qbittorrent library sort, which qui no longer uses. Both views sort
// through here, so a column sorts the same way in each. See ADR 0014.
func (sm *SyncManager) torrentOrder(rows []*qbt.Torrent, health func(int) TrackerHealth, column string, desc bool, tie func(a, b int) int) func(a, b int) int {
	switch column {
	case "name":
		// qBittorrent sorts names case-sensitive, which puts lowercase after
		// uppercase. The case only breaks ties here.
		lowered := make([]string, len(rows))
		for i, t := range rows {
			lowered[i] = strings.ToLower(t.Name)
		}
		return func(a, b int) int {
			result := strings.Compare(lowered[a], lowered[b])
			if result == 0 {
				result = strings.Compare(rows[a].Name, rows[b].Name)
			}
			if result == 0 {
				result = strings.Compare(rows[a].Hash, rows[b].Hash)
			}
			if result == 0 {
				return tie(a, b)
			}
			return flipIf(desc, result)
		}
	case "state":
		return stateOrder(rows, health, desc, tie)
	case "tracker":
		return sm.trackerOrder(rows, desc, tie)
	case "priority":
		// Priority is the queue position, and 0 means not queued. Queued
		// torrents come first in each direction.
		return func(a, b int) int {
			pa, pb := rows[a].Priority, rows[b].Priority
			switch {
			case pa == 0 && pb == 0:
			case pa == 0:
				return 1
			case pb == 0:
				return -1
			case pa != pb:
				return flipIf(desc, cmp.Compare(pb, pa))
			}
			return compareHashThenTie(rows, a, b, tie)
		}
	case "eta":
		// An infinite ETA (stalled) comes last in each direction, so it does
		// not split the active torrents into two groups.
		const infinityETA int64 = 8640000
		return func(a, b int) int {
			ea, eb := rows[a].ETA, rows[b].ETA
			switch {
			case ea == infinityETA && eb == infinityETA:
			case ea == infinityETA:
				return 1
			case eb == infinityETA:
				return -1
			case ea != eb:
				return flipIf(desc, cmp.Compare(ea, eb))
			}
			return compareHashThenTie(rows, a, b, tie)
		}
	case "last_activity":
		// LastActivity does not move each tick for an active torrent, so whole
		// minutes keep the order still.
		return timestampOrder(rows, desc, tie, func(t *qbt.Torrent) int64 { return t.LastActivity / 60 })
	case "added_on":
		return timestampOrder(rows, desc, tie, func(t *qbt.Torrent) int64 { return t.AddedOn })
	case "completion_on":
		return timestampOrder(rows, desc, tie, func(t *qbt.Torrent) int64 { return NormalizeCompletionTimestamp(t.CompletionOn) })
	case "seen_complete":
		return timestampOrder(rows, desc, tie, func(t *qbt.Torrent) int64 { return NormalizeCompletionTimestamp(t.SeenComplete) })
	}

	index, ok := torrentSortFields[column]
	if !ok {
		index = torrentSortFields["name"]
	}
	field := func(i int) reflect.Value { return reflect.ValueOf(rows[i]).Elem().Field(index) }
	switch reflect.TypeFor[qbt.Torrent]().Field(index).Type.Kind() {
	case reflect.String:
		return fieldOrder(rows, desc, tie, func(i int) string { return field(i).String() })
	case reflect.Float64:
		return fieldOrder(rows, desc, tie, func(i int) float64 { return field(i).Float() })
	case reflect.Bool:
		// false sorts before true.
		return fieldOrder(rows, desc, tie, func(i int) int {
			if field(i).Bool() {
				return 1
			}
			return 0
		})
	default:
		return fieldOrder(rows, desc, tie, func(i int) int64 { return field(i).Int() })
	}
}

// fieldOrder resolves one key per row, then compares by key and hash.
// Descending flips both.
func fieldOrder[K cmp.Ordered](rows []*qbt.Torrent, desc bool, tie func(a, b int) int, key func(int) K) func(a, b int) int {
	keys := make([]K, len(rows))
	for i := range keys {
		keys[i] = key(i)
	}
	return func(a, b int) int {
		result := cmp.Compare(keys[a], keys[b])
		if result == 0 {
			result = strings.Compare(rows[a].Hash, rows[b].Hash)
		}
		if result == 0 {
			return tie(a, b)
		}
		return flipIf(desc, result)
	}
}

func flipIf(desc bool, result int) int {
	if desc {
		return -result
	}
	return result
}

// compareHashThenTie is the tiebreak that ascending and descending share.
func compareHashThenTie(rows []*qbt.Torrent, a, b int, tie func(a, b int) int) int {
	if result := strings.Compare(rows[a].Hash, rows[b].Hash); result != 0 {
		return result
	}
	return tie(a, b)
}

// stateOrder sorts by tracker health first, then by state. Ties show the newest
// torrent first.
func stateOrder(rows []*qbt.Torrent, health func(int) TrackerHealth, desc bool, tie func(a, b int) int) func(a, b int) int {
	type stateSortKey struct {
		trackerPriority int
		statePriority   int
		label           string
	}

	// A library holds thousands of torrents in about fifteen states, so each
	// state's label is lowered once.
	loweredStates := make(map[qbt.TorrentState]string, 16)
	keys := make([]stateSortKey, len(rows))
	for i, t := range rows {
		label, ok := loweredStates[t.State]
		if !ok {
			label = strings.ToLower(string(t.State))
			loweredStates[t.State] = label
		}
		priority := 10
		switch health(i) {
		case TrackerHealthUnregistered:
			label, priority = "unregistered", 0
		case TrackerHealthDown:
			label, priority = "tracker_down", 1
		case TrackerHealthError:
			label, priority = "tracker_error", 2
		}
		keys[i] = stateSortKey{
			trackerPriority: priority,
			statePriority:   stateSortPriority(t.State),
			label:           label,
		}
	}

	return func(a, b int) int {
		ka, kb := &keys[a], &keys[b]
		result := 0
		switch {
		case ka.trackerPriority != kb.trackerPriority:
			result = ka.trackerPriority - kb.trackerPriority
		case ka.statePriority != kb.statePriority:
			result = ka.statePriority - kb.statePriority
		case ka.label != kb.label:
			result = strings.Compare(ka.label, kb.label)
		case rows[a].AddedOn != rows[b].AddedOn:
			// Newest first in ascending order.
			result = cmp.Compare(rows[b].AddedOn, rows[a].AddedOn)
		default:
			// Folded on demand: this tie is too rare to justify lower-casing every name.
			result = stringutils.CompareFold(rows[a].Name, rows[b].Name)
		}
		if result == 0 {
			return compareHashThenTie(rows, a, b, tie)
		}
		return flipIf(desc, result)
	}
}

// trackerOrder compares by display name first, then domain, then full URL.
// Display names come from tracker customizations, so merged trackers sort
// together, and one host never splits on the letter case of its URLs.
// Torrents without a tracker come last in each direction.
func (sm *SyncManager) trackerOrder(rows []*qbt.Torrent, desc bool, tie func(a, b int) int) func(a, b int) int {
	displayNameMap := sm.getTrackerDisplayNameMap()

	type trackerSortKey struct {
		hasDomain   bool
		displayName string // custom name if configured, otherwise domain
		domain      string
		normalized  string
		hash        string
	}

	keys := make([]trackerSortKey, len(rows))
	for i, torrent := range rows {
		key := &keys[i]
		key.hash = strings.ToLower(strings.TrimSpace(torrent.Hash))

		addCandidate := func(candidate string) {
			candidate = strings.TrimSpace(candidate)
			if candidate == "" {
				return
			}

			lowerCandidate := strings.ToLower(candidate)
			if key.normalized == "" {
				key.normalized = lowerCandidate
			}

			domain := strings.ToLower(sm.ExtractDomainFromURL(candidate))
			if domain == "" || domain == "unknown" {
				return
			}

			key.hasDomain = true
			key.domain = domain
			if customName, ok := displayNameMap[domain]; ok {
				key.displayName = strings.ToLower(customName)
			} else {
				key.displayName = domain
			}
		}

		addCandidate(torrent.Tracker)
		if !key.hasDomain {
			for _, tracker := range torrent.Trackers {
				addCandidate(tracker.Url)
				if key.hasDomain {
					break
				}
			}
		}

		if key.normalized == "" {
			key.normalized = key.hash
		}
	}

	return func(a, b int) int {
		ka, kb := &keys[a], &keys[b]
		if ka.hasDomain != kb.hasDomain {
			if ka.hasDomain {
				return -1
			}
			return 1
		}

		result := strings.Compare(ka.displayName, kb.displayName)
		if result == 0 {
			result = strings.Compare(ka.domain, kb.domain)
		}
		if result == 0 {
			result = strings.Compare(ka.normalized, kb.normalized)
		}
		if result == 0 {
			result = strings.Compare(ka.hash, kb.hash)
		}
		if result == 0 {
			// A magnet still fetching metadata has neither tracker nor hash.
			return tie(a, b)
		}
		return flipIf(desc, result)
	}
}

// timestampOrder sorts by a timestamp, then by state, name, and hash. A
// timestamp of 0 or -1 means "never" and sorts as the oldest.
func timestampOrder(rows []*qbt.Torrent, desc bool, tie func(a, b int) int, timestamp func(*qbt.Torrent) int64) func(a, b int) int {
	// The name is not resolved here: the tie it breaks is rare enough that
	// folding on demand beats lower-casing the whole library.
	type timestampSortKey struct {
		timestamp     int64
		statePriority int
	}

	keys := make([]timestampSortKey, len(rows))
	for i, t := range rows {
		keys[i] = timestampSortKey{
			timestamp:     timestamp(t),
			statePriority: stateSortPriority(t.State),
		}
	}

	return func(a, b int) int {
		ka, kb := &keys[a], &keys[b]
		if ka.timestamp != kb.timestamp {
			if desc {
				return cmp.Compare(kb.timestamp, ka.timestamp)
			}
			return cmp.Compare(ka.timestamp, kb.timestamp)
		}
		if ka.statePriority != kb.statePriority {
			return cmp.Compare(ka.statePriority, kb.statePriority)
		}
		if result := stringutils.CompareFold(rows[a].Name, rows[b].Name); result != 0 {
			return result
		}
		return compareHashThenTie(rows, a, b, tie)
	}
}

// sortByIndex sorts items in place by comparing indices instead of the items
// themselves, so sort keys can be resolved once per item up front instead of on
// every comparison. It also keeps large elements (qbt.Torrent is ~600 bytes)
// still while the sort runs. compare must be a total order over indices (add
// the index itself as the last tiebreak to keep a stable result).
func sortByIndex[T any](torrents []T, compare func(aIdx, bIdx int) int) {
	indices := make([]int, len(torrents))
	for idx := range indices {
		indices[idx] = idx
	}

	slices.SortFunc(indices, compare)

	// indices currently maps newPos -> oldPos; invert it to get elementPos -> targetPos,
	// then apply in-place cycle permutation.
	targets := make([]int, len(indices))
	for newPos, oldPos := range indices {
		targets[oldPos] = newPos
	}

	for i := range targets {
		for targets[i] != i {
			j := targets[i]
			torrents[i], torrents[j] = torrents[j], torrents[i]
			targets[i], targets[j] = targets[j], targets[i]
		}
	}
}
