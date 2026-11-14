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
	"github.com/autobrr/qui/internal/trackergroup"
)

// listRows returns the rows that a list request with filters gives from
// torrents, with the tracker inputs that the sync manager gathers.
func listRows(sm *SyncManager, client *Client, torrents []qbt.Torrent, filters FilterOptions, mainData *qbt.MainData, useSubcategories bool, health *TrackerHealthSnapshot) []qbt.Torrent {
	rule := sm.torrentListRuleFor(client, filters, mainData, health, useSubcategories)
	var refs []*qbt.Torrent
	for i := range torrents {
		if rule.match(&torrents[i]) {
			refs = append(refs, &torrents[i])
		}
	}
	if filters.Expr != "" {
		refs = sm.filterTorrentsByExpr(refs, filters.Expr, false)
	}
	return torrentValues(refs)
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
				torrents:         torrentRefs(torrents),
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
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
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
// TestListTorrentsFollowsOneRule sends each filter alone and with a
// second filter that excludes nothing. Both give the same rows, the rows equal
// the sidebar count, and each Unified view instance follows the same rule.
func TestListTorrentsFollowsOneRule(t *testing.T) {
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
				resp, err := sm.ListTorrents(t.Context(), TorrentListView{InstanceID: id, Filters: filters, SidebarCounts: true, CacheOnly: true})
				require.NoError(t, err)
				require.Equal(t, tc.want, rowHashes(resp), "filters %+v", filters)
				if tc.count != nil {
					require.Equal(t, len(tc.want), tc.count(resp.Counts), "the sidebar count")
				}
			}

			unified, err := sm.ListTorrents(t.Context(), TorrentListView{Unified: true, InstanceIDs: ids, Filters: tc.filters, CacheOnly: true})
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

// TestTrackerGroupsCountEachTorrentOnce holds the Tracker group totals: a
// torrent counts toward one group, so the groups add up to the library, and a
// content path that two torrents of one group share counts once.
func TestTrackerGroupsCountEachTorrentOnce(t *testing.T) {
	t.Parallel()

	withMetadata, noMetadata := true, false
	torrent := func(hash, tracker string, private *bool, size, uploaded int64, contentPath string) qbt.Torrent {
		tr := qbt.Torrent{Hash: hash, Tracker: tracker, Size: size, Uploaded: uploaded, Downloaded: 1, ContentPath: contentPath}
		if tracker != "" {
			tr.TrackersCount = 1
		}
		if private != nil {
			tr.HasMetadata, tr.Private = &withMetadata, *private
		} else {
			tr.HasMetadata = &noMetadata
		}
		return tr
	}
	private, public := true, false
	torrents := []qbt.Torrent{
		torrent("h1", "https://a.example.invalid/announce", &private, 100, 10, "/data/shared"),
		torrent("h2", "https://a.example.invalid/announce", &private, 100, 20, "/data/shared"),
		torrent("h3", "https://c.example.invalid/announce", &public, 300, 30, "/data/three"),
		torrent("h4", "https://d.example.invalid/announce", nil, 400, 40, "/data/four"),
		torrent("h5", "", &private, 500, 50, "/data/five"),
	}
	// h1 announces to two domains of one tracker.
	mapping := map[string]map[string]struct{}{
		"a.example.invalid": {"h1": {}, "h2": {}},
		"b.example.invalid": {"h1": {}},
	}
	in := torrentCountsInput{
		torrents:       torrentRefs(torrents),
		domainToHashes: mapping,
		trackers:       trackerSources{extractDomain: ExtractDomainFromURL},
	}
	counts := countTorrents(in).counts

	// The sidebar counts still count h1 under each of its domains.
	require.Equal(t, 2, counts.Trackers["a.example.invalid"])
	require.Equal(t, 1, counts.Trackers["b.example.invalid"])

	groupOf := func(name string, kind trackergroup.Kind) trackergroup.Group {
		return trackergroup.Group{Kind: kind, Name: name}
	}
	total := func(groups map[trackergroup.Group]TrackerTransferStats) (n int, uploaded int64) {
		for _, stats := range groups {
			n += stats.Count
			uploaded += stats.Uploaded
		}
		return n, uploaded
	}

	t.Run("no customization", func(t *testing.T) {
		groups := groupTrackers(in, trackergroup.New(nil))
		require.Equal(t, map[trackergroup.Group]TrackerTransferStats{
			groupOf("a.example.invalid", trackergroup.Domain): {Count: 2, Uploaded: 30, Downloaded: 2, TotalSize: 100},
			groupOf("Public", trackergroup.Public):            {Count: 1, Uploaded: 30, Downloaded: 1, TotalSize: 300},
			groupOf("d.example.invalid", trackergroup.Domain): {Count: 1, Uploaded: 40, Downloaded: 1, TotalSize: 400},
			groupOf("No tracker", trackergroup.NoTracker):     {Count: 1, Uploaded: 50, Downloaded: 1, TotalSize: 500},
		}, groups)
		n, uploaded := total(groups)
		require.Equal(t, len(torrents), n)
		require.EqualValues(t, 150, uploaded)
	})

	t.Run("a customization merges two groups", func(t *testing.T) {
		mapper := trackergroup.New([]*models.TrackerCustomization{
			{DisplayName: "Example", Domains: []string{"b.example.invalid", "d.example.invalid"}},
		})
		groups := groupTrackers(in, mapper)
		require.Equal(t, TrackerTransferStats{Count: 2, Uploaded: 50, Downloaded: 2, TotalSize: 500},
			groups[groupOf("Example", trackergroup.Customization)])
		require.Equal(t, TrackerTransferStats{Count: 1, Uploaded: 20, Downloaded: 1, TotalSize: 100},
			groups[groupOf("a.example.invalid", trackergroup.Domain)])
		n, uploaded := total(groups)
		require.Equal(t, len(torrents), n)
		require.EqualValues(t, 150, uploaded)
	})

	t.Run("sizes merge across sets of one group", func(t *testing.T) {
		mapper := trackergroup.New([]*models.TrackerCustomization{
			{DisplayName: "Example", Domains: []string{"a.example.invalid", "b.example.invalid"}},
		})
		// h1 has the sets {a, b} and h2 has {a}. Both are in Example and share a path.
		stats := groupTrackers(in, mapper)[groupOf("Example", trackergroup.Customization)]
		require.Equal(t, TrackerTransferStats{Count: 2, Uploaded: 30, Downloaded: 2, TotalSize: 100}, stats)
	})
}

// An empty content path is an unknown identity, not proof of shared data, so
// two torrents of one group with no content path count their sizes apart.
func TestTrackerGroupsDoNotDeduplicateEmptyContentPath(t *testing.T) {
	t.Parallel()

	torrents := []qbt.Torrent{
		{Hash: "h1", Tracker: "https://a.example.invalid/announce", TrackersCount: 1, Size: 30, Uploaded: 10},
		{Hash: "h2", Tracker: "https://a.example.invalid/announce", TrackersCount: 1, Size: 300, Uploaded: 100},
	}
	in := torrentCountsInput{
		torrents:       torrentRefs(torrents),
		domainToHashes: map[string]map[string]struct{}{"a.example.invalid": {"h1": {}, "h2": {}}},
		trackers:       trackerSources{extractDomain: ExtractDomainFromURL},
	}

	groups := groupTrackers(in, trackergroup.New(nil))

	require.Equal(t, map[trackergroup.Group]TrackerTransferStats{
		{Kind: trackergroup.Domain, Name: "a.example.invalid"}: {Count: 2, Uploaded: 110, TotalSize: 330},
	}, groups)
}

// regression: #4100
func TestTrackerFilterFindsATorrentThatTheMappingDoesNotHoldUnderEachDomain(t *testing.T) {
	t.Parallel()

	torrents := []qbt.Torrent{
		{Hash: "hash-mapped", Tracker: "https://alpha.example/announce", TrackersCount: 1},
		{Hash: "hash-new", Tracker: "https://charlie.example/announce", TrackersCount: 2},
		{Hash: "hash-edited", Tracker: "https://charlie.example/announce", TrackersCount: 2},
	}
	mainData := &qbt.MainData{Trackers: map[string][]string{
		"https://alpha.example/announce":   {"hash-mapped", "hash-new", "hash-edited"},
		"https://charlie.example/announce": {"hash-new", "hash-edited"},
	}}
	mapping := func() *ValidatedTrackerMapping {
		return &ValidatedTrackerMapping{
			HashToDomains:  map[string]map[string]struct{}{"hash-mapped": {"alpha.example": {}}, "hash-edited": {"old.example": {}}},
			DomainToHashes: map[string]map[string]struct{}{"alpha.example": {"hash-mapped": {}}, "old.example": {"hash-edited": {}}},
		}
	}
	hashes := func(rows []qbt.Torrent) []string {
		out := make([]string, 0, len(rows))
		for _, row := range rows {
			out = append(out, row.Hash)
		}
		return out
	}
	filter := FilterOptions{Trackers: []string{"alpha.example"}}

	t.Run("no tracker exclusions", func(t *testing.T) {
		t.Parallel()
		sm := &SyncManager{validatedTrackerMapping: map[int]*ValidatedTrackerMapping{7: mapping()}}
		rows := listRows(sm, &Client{instanceID: 7}, torrents, filter, mainData, false, nil)
		require.Equal(t, []string{"hash-mapped", "hash-new"}, hashes(rows))
	})

	t.Run("the mapping holds a torrent only under an excluded domain", func(t *testing.T) {
		t.Parallel()
		sm := &SyncManager{validatedTrackerMapping: map[int]*ValidatedTrackerMapping{7: mapping()}}
		client := &Client{instanceID: 7, trackerExclusions: map[string]map[string]struct{}{"old.example": {"hash-edited": {}}}}
		rows := listRows(sm, client, torrents, filter, mainData, false, nil)
		require.Equal(t, []string{"hash-mapped", "hash-new", "hash-edited"}, hashes(rows))
	})
}
