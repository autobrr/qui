// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"encoding/json/v2"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// listRows returns the rows that a list request with filters gives from
// torrents, with the tracker inputs that the sync manager gathers.
func listRows(sm *SyncManager, client *Client, torrents []qbt.Torrent, filters FilterOptions, mainData *qbt.MainData, useSubcategories bool, health *TrackerHealthSnapshot) []qbt.Torrent {
	rule := sm.torrentListRuleFor(client, filters, mainData, health, useSubcategories)
	var rows []qbt.Torrent
	for i := range torrents {
		if rule.match(&torrents[i]) {
			rows = append(rows, torrents[i])
		}
	}
	if filters.Expr != "" {
		rows = sm.filterTorrentsByExpr(rows, filters.Expr, filters.HideTrackerPasskeys)
	}
	return rows
}

// sidebarLibrary holds each qBittorrent state, a parent category with a
// subcategory and a look-alike, tags, blank tags, tracker health, and each way
// that a torrent gets its Trackers.
func sidebarLibrary() ([]qbt.Torrent, map[string]map[string]struct{}, trackerSources, *TrackerHealthSnapshot) {
	categories := []string{"", "movies", "movies/extra", "movies-extra", "tv"}
	tags := []string{"", "keep", "keep, other", " , ", "other"}
	mapping := map[string]map[string]struct{}{}
	mainData := &qbt.MainData{Trackers: map[string][]string{}}
	states := qbt.KnownTorrentStates()
	torrents := make([]qbt.Torrent, 0, len(states))
	for i, state := range states {
		t := qbt.Torrent{
			Hash:     fmt.Sprintf("%040x", i),
			State:    state,
			Category: categories[i%len(categories)],
			Tags:     tags[(i/2)%len(tags)],
			Size:     int64(i + 1),
		}
		switch i % 6 {
		case 0: // in the mapping
			t.Tracker, t.TrackersCount = "https://a.example.invalid/announce", 1
			mapping[t.Hash] = map[string]struct{}{"a.example.invalid": {}}
		case 1: // in the mapping with two domains
			t.Tracker, t.TrackersCount = "https://b.example.invalid/announce", 2
			mapping[t.Hash] = map[string]struct{}{"b.example.invalid": {}, "c.example.invalid": {}}
		case 2: // added after the last tracker health pass
			t.Tracker, t.TrackersCount = "https://a.example.invalid/announce", 1
		case 3: // no successful announce yet, so qBittorrent reports a blank primary tracker
			t.TrackersCount = 1
			mainData.Trackers["https://d.example.invalid/announce"] = append(mainData.Trackers["https://d.example.invalid/announce"], t.Hash)
		case 4: // no tracker
		case 5: // only a pseudo tracker
			t.Tracker = "** [DHT] **"
		}
		torrents = append(torrents, t)
	}
	health := NewTrackerHealthSnapshot(map[string]TrackerHealth{
		torrents[0].Hash: TrackerHealthDown,
		torrents[1].Hash: TrackerHealthUnregistered,
		torrents[2].Hash: TrackerHealthError,
	})
	// A tracker edit moved torrent 6 away from a.example.invalid.
	exclusions := map[string]map[string]struct{}{"a.example.invalid": {torrents[6].Hash: {}}}
	return torrents, mapping, trackerSources{exclusions: exclusions, mainData: mainData, extractDomain: ExtractDomainFromURL}, health
}

func byDomain(mapping map[string]map[string]struct{}) map[string]map[string]struct{} {
	out := map[string]map[string]struct{}{}
	for hash, domains := range mapping {
		for domain := range domains {
			if out[domain] == nil {
				out[domain] = map[string]struct{}{}
			}
			out[domain][hash] = struct{}{}
		}
	}
	return out
}

// TestSidebarCountsEqualRows holds the Sidebar count: for each sidebar entry,
// the count equals the rows of that filter, and a second filter that excludes
// nothing changes neither.
func TestSidebarCountsEqualRows(t *testing.T) {
	t.Parallel()

	torrents, mapping, sources, health := sidebarLibrary()
	for _, useSubcategories := range []bool{false, true} {
		t.Run(fmt.Sprintf("subcategories %t", useSubcategories), func(t *testing.T) {
			counts := countTorrents(torrentCountsInput{
				torrents:         torrents,
				useSubcategories: useSubcategories,
				health:           health,
				domainToHashes:   byDomain(mapping),
				trackers:         sources,
			}).counts
			rowSources := sources
			rowSources.mapping = mapping
			rows := func(filters FilterOptions) int {
				rule := newTorrentListRule(filters, health, useSubcategories, rowSources)
				n := 0
				for i := range torrents {
					if rule.match(&torrents[i]) {
						n++
					}
				}
				return n
			}

			entries := map[string]FilterOptions{}
			require.Len(t, counts.Status, len(countedStatuses))
			for status := range counts.Status {
				entries["status "+status] = FilterOptions{Status: []string{status}}
			}
			for category := range counts.Categories {
				entries["category "+category] = FilterOptions{Categories: []string{category}}
			}
			for tag := range counts.Tags {
				entries["tag "+tag] = FilterOptions{Tags: []string{tag}}
			}
			for tracker := range counts.Trackers {
				entries["tracker "+tracker] = FilterOptions{Trackers: []string{tracker}}
			}
			require.Contains(t, entries, "tracker ", "the library has torrents with no tracker")
			require.Contains(t, entries, "tag ", "the library has torrents with no tag")

			for name, filters := range entries {
				var want int
				switch kind, key, _ := strings.Cut(name, " "); kind {
				case "status":
					want = counts.Status[key]
				case "category":
					want = counts.Categories[key]
				case "tag":
					want = counts.Tags[key]
				case "tracker":
					want = counts.Trackers[key]
				}
				require.Equal(t, want, rows(filters), "the rows of %s", name)

				second := filters
				second.ExcludeCategories = []string{"no-such-category"}
				second.ExcludeTags = []string{"no-such-tag"}
				second.ExcludeTrackers = []string{"no-such-tracker.example.invalid"}
				require.Equal(t, want, rows(second), "the rows of %s with a second filter that excludes nothing", name)
			}

			// The tracker that is down keeps the torrent under its tracker.
			require.Positive(t, counts.Status["tracker_down"])
			require.Equal(t, 1, rows(FilterOptions{Status: []string{"tracker_down"}, Trackers: []string{"a.example.invalid"}}))
		})
	}
}

// newRuleTestClient starts a fake qBittorrent 5.1 whose sync data is mainData.
func newRuleTestClient(t *testing.T, instanceID int, mainData map[string]any) *Client {
	t.Helper()
	mainData["rid"], mainData["full_update"] = 1, true
	body, err := json.Marshal(mainData)
	require.NoError(t, err)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = io.WriteString(w, "Ok.")
		case "/api/v2/app/webapiVersion":
			_, _ = io.WriteString(w, "2.11.4")
		case "/api/v2/sync/maindata":
			_, _ = w.Write(body)
		case "/api/v2/torrents/categories":
			_ = json.MarshalWrite(w, mainData["categories"])
		case "/api/v2/torrents/tags":
			_, _ = io.WriteString(w, "[]")
		case "/api/v2/torrents/info":
			_, _ = io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	client, err := NewClientWithTimeout(instanceID, srv.URL, "", "", "", nil, nil, false, time.Second, time.Second)
	require.NoError(t, err)
	t.Cleanup(client.optimisticUpdates.Close)
	require.NoError(t, client.GetSyncManager().Sync(t.Context()))
	return client
}

func ruleTestLibrary() map[string]any {
	return map[string]any{
		"server_state": map[string]any{"use_subcategories": true},
		"categories": map[string]any{
			"movies":       map[string]any{"name": "movies"},
			"movies/extra": map[string]any{"name": "movies/extra"},
		},
		"trackers": map[string]any{"https://new.example.invalid/announce": []string{"cc"}},
		"torrents": map[string]any{
			"aa": map[string]any{"name": "A", "state": "stalledUP", "category": "movies/extra", "tags": "keep", "progress": 1,
				"tracker": "https://a.example.invalid/announce", "trackers_count": 1},
			"bb": map[string]any{"name": "B", "state": "downloading", "category": "movies/extra", "tags": "", "progress": 0.5},
			"cc": map[string]any{"name": "C", "state": "checkingUP", "category": "", "tags": " , ", "progress": 0.4,
				"tracker": "", "trackers_count": 1},
			"dd": map[string]any{"name": "D", "state": "pausedUP", "category": "tv", "tags": "keep", "progress": 1,
				"tracker": "https://a.example.invalid/announce", "trackers_count": 1},
		},
	}
}

func rowHashes(resp *TorrentResponse) []string {
	hashes := make([]string, 0, len(resp.Torrents))
	for _, row := range resp.Torrents {
		hashes = append(hashes, row.Hash)
	}
	slices.Sort(hashes)
	return hashes
}

// regression: #3868
// TestGetTorrentsWithFiltersFollowsOneRule sends each filter alone and with a
// second filter that excludes nothing. Both give the same rows, the rows equal
// the sidebar count, and each Unified view instance follows the same rule.
func TestGetTorrentsWithFiltersFollowsOneRule(t *testing.T) {
	t.Parallel()

	store, err := models.NewInstanceStore(testdb.NewMigratedSQLite(t, "qbittorrent-list-rule"), make([]byte, 32))
	require.NoError(t, err)
	clients := map[int]*Client{}
	ids := make([]int, 0, 2)
	for _, name := range []string{"a", "b"} {
		instance, err := store.Create(t.Context(), name, "http://127.0.0.1:8080", "", "", nil, nil, false, nil)
		require.NoError(t, err)
		clients[instance.ID] = newRuleTestClient(t, instance.ID, ruleTestLibrary())
		ids = append(ids, instance.ID)
	}
	sm := NewSyncManager(&ClientPool{instanceStore: store, clients: clients}, nil)
	ctx := WithSkipFreshData(t.Context())
	id := ids[0]

	for _, tc := range []struct {
		name    string
		filters FilterOptions
		want    []string
		count   func(*TorrentCounts) int
	}{
		{"seeding", FilterOptions{Status: []string{"seeding"}}, []string{"aa", "cc"}, func(c *TorrentCounts) int { return c.Status["seeding"] }},
		{"completed during a recheck", FilterOptions{Status: []string{"completed"}}, []string{"aa", "cc", "dd"}, func(c *TorrentCounts) int { return c.Status["completed"] }},
		{"running", FilterOptions{Status: []string{"running"}}, []string{"aa", "bb", "cc"}, func(c *TorrentCounts) int { return c.Status["running"] }},
		{"a state name in any letter case", FilterOptions{Status: []string{"stalledup"}}, []string{"aa"}, nil},
		{"stalled_seeding", FilterOptions{Status: []string{"stalled_seeding"}}, []string{"aa"}, func(c *TorrentCounts) int { return c.Status["stalled_uploading"] }},
		{"a parent category", FilterOptions{Categories: []string{"movies"}}, []string{"aa", "bb"}, func(c *TorrentCounts) int { return c.Categories["movies"] }},
		{"no tag", FilterOptions{Tags: []string{""}}, []string{"bb", "cc"}, func(c *TorrentCounts) int { return c.Tags[""] }},
		{"no tracker", FilterOptions{Trackers: []string{""}}, []string{"bb"}, func(c *TorrentCounts) int { return c.Trackers[""] }},
		{"a tracker before the first tracker health pass", FilterOptions{Trackers: []string{"a.example.invalid"}}, []string{"aa", "dd"}, func(c *TorrentCounts) int { return c.Trackers["a.example.invalid"] }},
		{"a tracker of a torrent with a blank primary tracker", FilterOptions{Trackers: []string{"new.example.invalid"}}, []string{"cc"}, func(c *TorrentCounts) int { return c.Trackers["new.example.invalid"] }},
		{"an unknown status", FilterOptions{Status: []string{"tpyo"}}, []string{}, nil},
		{"an unknown status next to a known one", FilterOptions{Status: []string{"downloading", "tpyo"}}, []string{}, nil},
		{"an unknown excludeStatus value", FilterOptions{ExcludeStatus: []string{"tpyo"}}, []string{}, nil},
		{"an expression that does not compile", FilterOptions{Expr: "Ratio >"}, []string{}, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			second := tc.filters
			second.ExcludeCategories = []string{"no-such-category"}
			for _, filters := range []FilterOptions{tc.filters, second} {
				resp, err := sm.GetTorrentsWithFilters(ctx, id, 0, 0, "", "", "", filters)
				require.NoError(t, err)
				require.Equal(t, tc.want, rowHashes(resp), "filters %+v", filters)
				if tc.count != nil {
					require.Equal(t, len(tc.want), tc.count(resp.Counts), "the sidebar count")
				}
			}

			unified, err := sm.GetCrossInstanceTorrentsWithFilters(ctx, 0, 0, "", "", "", tc.filters, ids)
			require.NoError(t, err)
			for _, instanceID := range ids {
				var got []string
				for _, row := range unified.CrossInstanceTorrents {
					if row.InstanceID == instanceID {
						got = append(got, row.Hash)
					}
				}
				slices.Sort(got)
				require.Equal(t, tc.want, append([]string{}, got...), "instance %d of the Unified view", instanceID)
			}
		})
	}
}

// A parent gets each subcategory size once, in any map order.
func TestAddSubcategoriesToParentsCountsEachSizeOnce(t *testing.T) {
	t.Parallel()

	for range 50 {
		counts := &TorrentCounts{
			Categories:    map[string]int{"tv": 1, "tv/anime": 1, "tv/anime/season": 1},
			CategorySizes: map[string]int64{"tv": 1, "tv/anime": 10, "tv/anime/season": 100},
		}
		addSubcategoriesToParents(counts)
		require.Equal(t, map[string]int{"tv": 3, "tv/anime": 2, "tv/anime/season": 1}, counts.Categories)
		require.Equal(t, map[string]int64{"tv": 111, "tv/anime": 110, "tv/anime/season": 100}, counts.CategorySizes)
	}
}
