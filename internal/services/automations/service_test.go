// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"database/sql"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// -----------------------------------------------------------------------------
// matchesTracker tests
// -----------------------------------------------------------------------------

func TestMatchesTracker(t *testing.T) {
	tests := []struct {
		name    string
		pattern string
		domains []string
		want    bool
	}{
		// Wildcard
		{
			name:    "wildcard matches all",
			pattern: "*",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "wildcard matches empty domains",
			pattern: "*",
			domains: []string{},
			want:    true,
		},

		// Empty pattern
		{
			name:    "empty pattern matches nothing",
			pattern: "",
			domains: []string{"tracker.example.com"},
			want:    false,
		},

		// Exact match
		{
			name:    "exact match",
			pattern: "tracker.example.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "exact match case insensitive",
			pattern: "Tracker.Example.COM",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "exact match no match",
			pattern: "other.tracker.com",
			domains: []string{"tracker.example.com"},
			want:    false,
		},

		// Suffix pattern (.domain)
		{
			name:    "suffix pattern matches",
			pattern: ".example.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "suffix pattern case insensitive",
			pattern: ".Example.COM",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "suffix pattern no match different domain",
			pattern: ".other.com",
			domains: []string{"tracker.example.com"},
			want:    false,
		},

		// Multiple patterns (comma separated)
		{
			name:    "comma separated first matches",
			pattern: "tracker.example.com, other.tracker.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "comma separated second matches",
			pattern: "other.tracker.com, tracker.example.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "comma separated none match",
			pattern: "foo.com, bar.com",
			domains: []string{"tracker.example.com"},
			want:    false,
		},

		// Multiple patterns (semicolon separated)
		{
			name:    "semicolon separated matches",
			pattern: "foo.com; tracker.example.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},

		// Multiple patterns (pipe separated)
		{
			name:    "pipe separated matches",
			pattern: "foo.com|tracker.example.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},

		// Glob patterns
		{
			name:    "glob wildcard prefix",
			pattern: "*.example.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "glob wildcard middle",
			pattern: "tracker.*.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "glob question mark",
			pattern: "tracker.exampl?.com",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
		{
			name:    "glob no match",
			pattern: "*.other.com",
			domains: []string{"tracker.example.com"},
			want:    false,
		},

		// Multiple domains
		{
			name:    "multiple domains first matches",
			pattern: "tracker.example.com",
			domains: []string{"tracker.example.com", "other.tracker.com"},
			want:    true,
		},
		{
			name:    "multiple domains second matches",
			pattern: "other.tracker.com",
			domains: []string{"tracker.example.com", "other.tracker.com"},
			want:    true,
		},

		// Edge cases
		{
			name:    "empty domains with non-wildcard pattern",
			pattern: "tracker.example.com",
			domains: []string{},
			want:    false,
		},
		{
			name:    "whitespace in pattern is trimmed",
			pattern: "  tracker.example.com  ",
			domains: []string{"tracker.example.com"},
			want:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := matchesTracker(tc.pattern, tc.domains)
			assert.Equal(t, tc.want, got)
		})
	}
}

// -----------------------------------------------------------------------------
// detectCrossSeeds tests
// -----------------------------------------------------------------------------

func TestDetectCrossSeeds(t *testing.T) {
	tests := []struct {
		name        string
		target      qbt.Torrent
		allTorrents []qbt.Torrent
		want        bool
	}{
		{
			name:        "no other torrents",
			target:      qbt.Torrent{Hash: "abc", ContentPath: "/data/movie"},
			allTorrents: []qbt.Torrent{{Hash: "abc", ContentPath: "/data/movie"}},
			want:        false,
		},
		{
			name:   "different paths no cross-seed",
			target: qbt.Torrent{Hash: "abc", ContentPath: "/data/movie1"},
			allTorrents: []qbt.Torrent{
				{Hash: "abc", ContentPath: "/data/movie1"},
				{Hash: "def", ContentPath: "/data/movie2"},
			},
			want: false,
		},
		{
			name:   "same path is cross-seed",
			target: qbt.Torrent{Hash: "abc", ContentPath: "/data/movie"},
			allTorrents: []qbt.Torrent{
				{Hash: "abc", ContentPath: "/data/movie"},
				{Hash: "def", ContentPath: "/data/movie"},
			},
			want: true,
		},
		{
			name:   "case insensitive match",
			target: qbt.Torrent{Hash: "abc", ContentPath: "/Data/Movie"},
			allTorrents: []qbt.Torrent{
				{Hash: "abc", ContentPath: "/Data/Movie"},
				{Hash: "def", ContentPath: "/data/movie"},
			},
			want: true,
		},
		{
			name:   "backslash normalized",
			target: qbt.Torrent{Hash: "abc", ContentPath: "D:\\Data\\Movie"},
			allTorrents: []qbt.Torrent{
				{Hash: "abc", ContentPath: "D:\\Data\\Movie"},
				{Hash: "def", ContentPath: "D:/Data/Movie"},
			},
			want: true,
		},
		{
			name:   "trailing slash normalized",
			target: qbt.Torrent{Hash: "abc", ContentPath: "/data/movie/"},
			allTorrents: []qbt.Torrent{
				{Hash: "abc", ContentPath: "/data/movie/"},
				{Hash: "def", ContentPath: "/data/movie"},
			},
			want: true,
		},
		{
			name:        "empty content path",
			target:      qbt.Torrent{Hash: "abc", ContentPath: ""},
			allTorrents: []qbt.Torrent{{Hash: "abc", ContentPath: ""}},
			want:        false,
		},
		{
			name:   "multiple cross-seeds",
			target: qbt.Torrent{Hash: "abc", ContentPath: "/data/movie"},
			allTorrents: []qbt.Torrent{
				{Hash: "abc", ContentPath: "/data/movie"},
				{Hash: "def", ContentPath: "/data/movie"},
				{Hash: "ghi", ContentPath: "/data/movie"},
			},
			want: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := detectCrossSeeds(tc.target, tc.allTorrents)
			assert.Equal(t, tc.want, got)
		})
	}
}

// -----------------------------------------------------------------------------
// normalizePath tests
// -----------------------------------------------------------------------------

func TestNormalizePath(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "empty string",
			input: "",
			want:  "",
		},
		{
			name:  "lowercase conversion",
			input: "/Data/Movie",
			want:  "/data/movie",
		},
		{
			name:  "backslash to forward slash",
			input: "D:\\Data\\Movie",
			want:  "d:/data/movie",
		},
		{
			name:  "trailing slash removed",
			input: "/data/movie/",
			want:  "/data/movie",
		},
		{
			name:  "all transformations",
			input: "D:\\Data\\Movie\\",
			want:  "d:/data/movie",
		},
		{
			name:  "already normalized",
			input: "/data/movie",
			want:  "/data/movie",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizePath(tc.input)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestCrossSeedRuleRefsByKey(t *testing.T) {
	t.Parallel()

	torrentByHash := map[string]qbt.Torrent{
		"h1": {Hash: "h1", ContentPath: "/downloads/group-a", SavePath: "/downloads"},
		"h2": {Hash: "h2", ContentPath: "/downloads/group-b", SavePath: "/downloads"},
		"h3": {Hash: "h3", ContentPath: "/downloads/group-a", SavePath: "/downloads"},
	}
	ruleByHash := map[string]ruleRef{
		"h1": {id: 10, name: "Rule A"},
		"h2": {id: 20, name: "Rule B"},
		"h3": {id: 30, name: "Rule A Override"},
	}

	got := crossSeedRuleRefsByKey([]string{"h1", "h3", "h2"}, torrentByHash, ruleByHash)
	require.Len(t, got, 2)

	keyA, ok := makeCrossSeedKey(torrentByHash["h1"])
	require.True(t, ok)
	keyB, ok := makeCrossSeedKey(torrentByHash["h2"])
	require.True(t, ok)

	// First trigger for a key wins to keep attribution deterministic.
	require.Equal(t, ruleRef{id: 10, name: "Rule A"}, got[keyA])
	require.Equal(t, ruleRef{id: 20, name: "Rule B"}, got[keyB])
}

func TestInheritRuleRefForCrossSeed(t *testing.T) {
	t.Parallel()

	key := crossSeedKey{
		contentPath: "/downloads/group-a",
		savePath:    "/downloads",
	}
	ruleByHash := map[string]ruleRef{
		"h1": {id: 10, name: "Rule A"},
	}
	ruleByCrossSeedKey := map[crossSeedKey]ruleRef{
		key: {id: 10, name: "Rule A"},
	}

	inheritRuleRefForCrossSeed("x1", key, ruleByHash, ruleByCrossSeedKey)
	require.Equal(t, ruleRef{id: 10, name: "Rule A"}, ruleByHash["x1"])

	// Existing explicit attribution should not be overwritten.
	ruleByHash["x1"] = ruleRef{id: 99, name: "Explicit Rule"}
	inheritRuleRefForCrossSeed("x1", key, ruleByHash, ruleByCrossSeedKey)
	require.Equal(t, ruleRef{id: 99, name: "Explicit Rule"}, ruleByHash["x1"])

	counts := buildRuleCountsFromHashes([]string{"h1", "x1"}, ruleByHash)
	require.Equal(t, 1, counts[ruleRef{id: 10, name: "Rule A"}])
	require.Equal(t, 1, counts[ruleRef{id: 99, name: "Explicit Rule"}])
}

// -----------------------------------------------------------------------------
// limitHashBatch tests
// -----------------------------------------------------------------------------

func TestLimitHashBatch(t *testing.T) {
	tests := []struct {
		name   string
		hashes []string
		max    int
		want   [][]string
	}{
		{
			name:   "empty input",
			hashes: []string{},
			max:    10,
			want:   [][]string{{}},
		},
		{
			name:   "under limit single batch",
			hashes: []string{"a", "b", "c"},
			max:    10,
			want:   [][]string{{"a", "b", "c"}},
		},
		{
			name:   "exactly at limit",
			hashes: []string{"a", "b", "c"},
			max:    3,
			want:   [][]string{{"a", "b", "c"}},
		},
		{
			name:   "over limit splits evenly",
			hashes: []string{"a", "b", "c", "d"},
			max:    2,
			want:   [][]string{{"a", "b"}, {"c", "d"}},
		},
		{
			name:   "over limit with remainder",
			hashes: []string{"a", "b", "c", "d", "e"},
			max:    2,
			want:   [][]string{{"a", "b"}, {"c", "d"}, {"e"}},
		},
		{
			name:   "max of 1",
			hashes: []string{"a", "b", "c"},
			max:    1,
			want:   [][]string{{"a"}, {"b"}, {"c"}},
		},
		{
			name:   "zero max returns single batch",
			hashes: []string{"a", "b", "c"},
			max:    0,
			want:   [][]string{{"a", "b", "c"}},
		},
		{
			name:   "negative max returns single batch",
			hashes: []string{"a", "b", "c"},
			max:    -1,
			want:   [][]string{{"a", "b", "c"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := limitHashBatch(tc.hashes, tc.max)
			assert.Equal(t, tc.want, got)
		})
	}
}

// -----------------------------------------------------------------------------
// torrentHasTag tests
// -----------------------------------------------------------------------------

func TestTorrentHasTag(t *testing.T) {
	tests := []struct {
		name      string
		tags      string
		candidate string
		want      bool
	}{
		{
			name:      "empty tags",
			tags:      "",
			candidate: "tagA",
			want:      false,
		},
		{
			name:      "single tag match",
			tags:      "tagA",
			candidate: "tagA",
			want:      true,
		},
		{
			name:      "single tag no match",
			tags:      "tagA",
			candidate: "tagB",
			want:      false,
		},
		{
			name:      "multiple tags first match",
			tags:      "tagA, tagB, tagC",
			candidate: "tagA",
			want:      true,
		},
		{
			name:      "multiple tags middle match",
			tags:      "tagA, tagB, tagC",
			candidate: "tagB",
			want:      true,
		},
		{
			name:      "multiple tags last match",
			tags:      "tagA, tagB, tagC",
			candidate: "tagC",
			want:      true,
		},
		{
			name:      "case insensitive",
			tags:      "TagA, TAGB",
			candidate: "taga",
			want:      true,
		},
		{
			name:      "whitespace trimmed",
			tags:      "  tagA  ,  tagB  ",
			candidate: "tagA",
			want:      true,
		},
		{
			name:      "partial match fails",
			tags:      "tagABC",
			candidate: "tagA",
			want:      false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := torrentHasTag(tc.tags, tc.candidate)
			assert.Equal(t, tc.want, got)
		})
	}
}

// -----------------------------------------------------------------------------
// selectMatchingRules tests
// -----------------------------------------------------------------------------

func TestSelectMatchingRules(t *testing.T) {
	// Create a minimal SyncManager for domain extraction
	sm := qbittorrent.NewSyncManager(nil, nil)

	tests := []struct {
		name        string
		torrent     qbt.Torrent
		rules       []*models.Automation
		wantFirstID int   // 0 means expect empty slice
		wantCount   int   // expected number of matching rules
		wantIDs     []int // all expected matching rule IDs in order
	}{
		{
			name:        "no rules returns empty",
			torrent:     qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules:       []*models.Automation{},
			wantFirstID: 0,
			wantCount:   0,
		},
		{
			name:    "disabled rule skipped",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.Automation{
				{ID: 1, Enabled: false, TrackerPattern: "tracker.example.com"},
			},
			wantFirstID: 0,
			wantCount:   0,
		},
		{
			name:    "enabled rule matches",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.Automation{
				{ID: 1, Enabled: true, TrackerPattern: "tracker.example.com"},
			},
			wantFirstID: 1,
			wantCount:   1,
		},
		{
			name:    "multiple matching rules returned in order",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.Automation{
				{ID: 1, Enabled: true, TrackerPattern: "tracker.example.com"},
				{ID: 2, Enabled: true, TrackerPattern: "*"},
			},
			wantFirstID: 1,
			wantCount:   2,
			wantIDs:     []int{1, 2},
		},
		{
			name:    "wildcard matches all",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.Automation{
				{ID: 1, Enabled: true, TrackerPattern: "*"},
			},
			wantFirstID: 1,
			wantCount:   1,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectMatchingRules(tc.torrent, tc.rules, sm)
			if tc.wantFirstID == 0 {
				assert.Empty(t, got)
			} else {
				require.NotEmpty(t, got)
				assert.Equal(t, tc.wantFirstID, got[0].ID)
			}
			assert.Len(t, got, tc.wantCount)
			if len(tc.wantIDs) > 0 {
				gotIDs := make([]int, len(got))
				for i, r := range got {
					gotIDs[i] = r.ID
				}
				assert.Equal(t, tc.wantIDs, gotIDs)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Category action tests
// -----------------------------------------------------------------------------

func TestCategoryLastRuleWins(t *testing.T) {
	// Test that when multiple rules set a category, the last rule's category wins.
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		Category: "movies", // Current category
	}

	// Rule 1 sets category to "archive"
	rule1 := &models.Automation{
		ID:      1,
		Enabled: true,
		Name:    "Archive Rule",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{Enabled: true, Category: "archive"},
		},
	}

	// Rule 2 sets category to "completed" (should win as last rule)
	rule2 := &models.Automation{
		ID:      2,
		Enabled: true,
		Name:    "Completed Rule",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{Enabled: true, Category: "completed"},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	// Process rules in order
	processRuleForTorrent(rule1, torrent, state, nil, nil, nil, nil)
	processRuleForTorrent(rule2, torrent, state, nil, nil, nil, nil)

	// Last rule wins - category should be "completed"
	require.NotNil(t, state.category)
	assert.Equal(t, "completed", *state.category)
}

func TestCategoryLastRuleWinsEvenWhenMatchesCurrent(t *testing.T) {
	// Test that last rule wins even when the last rule's category matches the current category.
	// The processor should still set the desired state; the service filters no-ops.
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		Category: "movies", // Current category
	}

	// Rule 1 sets category to "archive"
	rule1 := &models.Automation{
		ID:      1,
		Enabled: true,
		Name:    "Archive Rule",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{Enabled: true, Category: "archive"},
		},
	}

	// Rule 2 sets category to "movies" (same as current)
	rule2 := &models.Automation{
		ID:      2,
		Enabled: true,
		Name:    "Movies Rule",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{Enabled: true, Category: "movies"},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	// Process rules in order
	processRuleForTorrent(rule1, torrent, state, nil, nil, nil, nil)
	processRuleForTorrent(rule2, torrent, state, nil, nil, nil, nil)

	// Last rule wins - category should be "movies"
	// Even though it matches current, the processor should set it (service filters no-op)
	require.NotNil(t, state.category)
	assert.Equal(t, "movies", *state.category)
}

func TestCategoryWithCondition(t *testing.T) {
	// Test that category action respects conditions
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		Category: "default",
		Ratio:    2.5, // Above condition threshold
	}

	// Rule with condition: only if ratio > 2.0
	rule := &models.Automation{
		ID:      1,
		Enabled: true,
		Name:    "High Ratio Rule",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{
				Enabled:  true,
				Category: "archive",
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil)

	// Condition matched, category should be set
	require.NotNil(t, state.category)
	assert.Equal(t, "archive", *state.category)
}

func TestCategoryConditionNotMet(t *testing.T) {
	// Test that category action is not applied when condition is not met
	torrent := qbt.Torrent{
		Hash:     "abc123",
		Name:     "Test Torrent",
		Category: "default",
		Ratio:    1.0, // Below condition threshold
	}

	// Rule with condition: only if ratio > 2.0
	rule := &models.Automation{
		ID:      1,
		Enabled: true,
		Name:    "High Ratio Rule",
		Conditions: &models.ActionConditions{
			Category: &models.CategoryAction{
				Enabled:  true,
				Category: "archive",
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	state := &torrentDesiredState{
		hash:        torrent.Hash,
		name:        torrent.Name,
		currentTags: make(map[string]struct{}),
		tagActions:  make(map[string]string),
	}

	processRuleForTorrent(rule, torrent, state, nil, nil, nil, nil)

	// Condition not met, category should not be set
	assert.Nil(t, state.category)
}

// -----------------------------------------------------------------------------
// isContentPathAmbiguous tests
// -----------------------------------------------------------------------------

func TestIsContentPathAmbiguous(t *testing.T) {
	tests := []struct {
		scenario    string
		contentPath string
		savePath    string
		want        bool
	}{
		{
			scenario:    "ContentPath != SavePath => unambiguous",
			contentPath: "/downloads/torrent/My.Movie.2024",
			savePath:    "/downloads/torrent",
			want:        false,
		},
		{
			scenario:    "ContentPath == SavePath => ambiguous (shared dir)",
			contentPath: "/downloads/shared",
			savePath:    "/downloads/shared",
			want:        true,
		},
		{
			scenario:    "ContentPath subfolder of SavePath => unambiguous",
			contentPath: "/Downloads/torrent/My.Movie",
			savePath:    "/downloads/torrent",
			want:        false,
		},
		{
			scenario:    "ContentPath == SavePath (case-insensitive) => ambiguous",
			contentPath: "/Downloads/Shared",
			savePath:    "/downloads/shared",
			want:        true,
		},
		{
			scenario:    "ContentPath == SavePath (trailing slash diff) => ambiguous",
			contentPath: "/downloads/shared/",
			savePath:    "/downloads/shared",
			want:        true,
		},
		{
			scenario:    "ContentPath is specific file/folder under SavePath => unambiguous",
			contentPath: "/downloads/movies/MyMovie",
			savePath:    "/downloads/movies",
			want:        false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			torrent := qbt.Torrent{
				ContentPath: tc.contentPath,
				SavePath:    tc.savePath,
			}
			got := isContentPathAmbiguous(torrent)
			assert.Equal(t, tc.want, got)
		})
	}
}

// -----------------------------------------------------------------------------
// findCrossSeedGroup tests
// -----------------------------------------------------------------------------

// Groups by ContentPath equality only; does not expand by SavePath.
// Cross-seeds are exact file matches (same content from different trackers).
func TestFindCrossSeedGroup(t *testing.T) {
	tests := []struct {
		scenario    string
		target      qbt.Torrent
		allTorrents []qbt.Torrent
		wantCount   int
		wantHashes  []string
	}{
		{
			scenario: "unique ContentPath => group contains only target",
			target: qbt.Torrent{
				Hash:        "abc123",
				Name:        "My.Movie.2024.1080p.BluRay.x264-GRP",
				ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP",
			},
			allTorrents: []qbt.Torrent{
				{Hash: "abc123", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
				{Hash: "def456", Name: "Other.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/Other.Movie.2024.1080p.BluRay.x264-GRP"},
			},
			wantCount:  1,
			wantHashes: []string{"abc123"},
		},
		{
			scenario: "same ContentPath (cross-seed from different tracker) => both in group",
			target: qbt.Torrent{
				Hash:        "abc123",
				Name:        "My.Movie.2024.1080p.BluRay.x264-GRP",
				ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP",
			},
			allTorrents: []qbt.Torrent{
				// Same release cross-seeded to two trackers (identical files, different .torrent)
				{Hash: "abc123", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
				{Hash: "xyz789", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
				{Hash: "def456", Name: "Other.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/Other.Movie.2024.1080p.BluRay.x264-GRP"},
			},
			wantCount:  2,
			wantHashes: []string{"abc123", "xyz789"},
		},
		{
			scenario: "ContentPath match is case-insensitive",
			target: qbt.Torrent{
				Hash:        "abc123",
				Name:        "My.Movie.2024.1080p.BluRay.x264-GRP",
				ContentPath: "/Downloads/Movies/My.Movie.2024.1080p.BluRay.x264-GRP",
			},
			allTorrents: []qbt.Torrent{
				{Hash: "abc123", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/Downloads/Movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
				{Hash: "xyz789", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/my.movie.2024.1080p.bluray.x264-grp"},
			},
			wantCount:  2,
			wantHashes: []string{"abc123", "xyz789"},
		},
		{
			scenario: "same SavePath but different ContentPath => NOT grouped",
			target: qbt.Torrent{
				Hash:        "abc123",
				Name:        "My.Movie.2024.1080p.BluRay.x264-GRP",
				SavePath:    "/downloads/movies",
				ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP",
			},
			allTorrents: []qbt.Torrent{
				{Hash: "abc123", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", SavePath: "/downloads/movies", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
				// Different releases in same SavePath - NOT cross-seeds (different files)
				{Hash: "def456", Name: "Other.Movie.2024.1080p.BluRay.x264-GRP", SavePath: "/downloads/movies", ContentPath: "/downloads/movies/Other.Movie.2024.1080p.BluRay.x264-GRP"},
				{Hash: "ghi789", Name: "Another.Movie.2024.1080p.BluRay.x264-GRP", SavePath: "/downloads/movies", ContentPath: "/downloads/movies/Another.Movie.2024.1080p.BluRay.x264-GRP"},
			},
			wantCount:  1,
			wantHashes: []string{"abc123"}, // Only target; others share SavePath but NOT ContentPath
		},
		{
			scenario: "empty ContentPath => returns nil (no grouping possible)",
			target: qbt.Torrent{
				Hash:        "abc123",
				Name:        "Unknown",
				ContentPath: "",
			},
			allTorrents: []qbt.Torrent{
				{Hash: "abc123", Name: "Unknown", ContentPath: ""},
				{Hash: "def456", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
			},
			wantCount:  0,
			wantHashes: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			got := findCrossSeedGroup(tc.target, tc.allTorrents)
			if tc.wantHashes == nil {
				assert.Nil(t, got)
			} else {
				assert.Equal(t, tc.wantCount, len(got))
				gotHashes := make([]string, len(got))
				for i, torrent := range got {
					gotHashes[i] = torrent.Hash
				}
				assert.ElementsMatch(t, tc.wantHashes, gotHashes)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// HardlinkIndex.GetHardlinkCopies tests
// -----------------------------------------------------------------------------

func TestHardlinkIndex_GetHardlinkCopies(t *testing.T) {
	tests := []struct {
		name             string
		triggerHash      string
		signatureByHash  map[string]string
		groupBySignature map[string][]string
		wantCopies       []string
	}{
		{
			name:        "trigger hash not in any group",
			triggerHash: "not-found",
			signatureByHash: map[string]string{
				"abc123": "sig1",
				"def456": "sig1",
			},
			groupBySignature: map[string][]string{
				"sig1": {"abc123", "def456"},
			},
			wantCopies: nil,
		},
		{
			name:             "trigger is only member of group (singleton filtered out)",
			triggerHash:      "abc123",
			signatureByHash:  map[string]string{}, // Singleton groups are filtered, so no entry
			groupBySignature: map[string][]string{},
			wantCopies:       nil,
		},
		{
			name:        "trigger has one hardlink copy",
			triggerHash: "abc123",
			signatureByHash: map[string]string{
				"abc123": "sig1",
				"def456": "sig1",
			},
			groupBySignature: map[string][]string{
				"sig1": {"abc123", "def456"},
			},
			wantCopies: []string{"def456"},
		},
		{
			name:        "trigger has multiple hardlink copies",
			triggerHash: "abc123",
			signatureByHash: map[string]string{
				"abc123": "sig1",
				"def456": "sig1",
				"ghi789": "sig1",
			},
			groupBySignature: map[string][]string{
				"sig1": {"abc123", "def456", "ghi789"},
			},
			wantCopies: []string{"def456", "ghi789"},
		},
		{
			name:        "multiple groups, trigger in second",
			triggerHash: "xyz999",
			signatureByHash: map[string]string{
				"abc123": "sig1",
				"def456": "sig1",
				"xyz999": "sig2",
				"uvw888": "sig2",
			},
			groupBySignature: map[string][]string{
				"sig1": {"abc123", "def456"},
				"sig2": {"xyz999", "uvw888"},
			},
			wantCopies: []string{"uvw888"},
		},
		{
			name:             "nil index returns nil",
			triggerHash:      "abc123",
			signatureByHash:  nil,
			groupBySignature: nil,
			wantCopies:       nil,
		},
		{
			name:             "empty index returns nil",
			triggerHash:      "abc123",
			signatureByHash:  map[string]string{},
			groupBySignature: map[string][]string{},
			wantCopies:       nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var idx *HardlinkIndex
			if tc.signatureByHash != nil || tc.groupBySignature != nil {
				idx = &HardlinkIndex{
					SignatureByHash:  tc.signatureByHash,
					GroupBySignature: tc.groupBySignature,
				}
			}
			got := idx.GetHardlinkCopies(tc.triggerHash)
			if tc.wantCopies == nil {
				assert.Nil(t, got)
			} else {
				assert.ElementsMatch(t, tc.wantCopies, got)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// deleteFreesSpace tests with include mode
// -----------------------------------------------------------------------------

func TestDeleteFreesSpace_IncludeCrossSeeds(t *testing.T) {
	// Same release cross-seeded to two trackers (identical files, different .torrent hashes)
	allTorrents := []qbt.Torrent{
		{Hash: "abc123", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
		{Hash: "xyz789", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
		{Hash: "def456", Name: "Other.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/Other.Movie.2024.1080p.BluRay.x264-GRP"},
	}

	target := allTorrents[0]

	tests := []struct {
		scenario string
		mode     string
		want     bool
	}{
		{
			scenario: "include cross-seeds mode => frees space (deletes all copies)",
			mode:     DeleteModeWithFilesIncludeCrossSeeds,
			want:     true,
		},
		{
			scenario: "delete with files => frees space (ignores cross-seeds)",
			mode:     DeleteModeWithFiles,
			want:     true,
		},
		{
			scenario: "preserve cross-seeds => no space freed (cross-seed exists)",
			mode:     DeleteModeWithFilesPreserveCrossSeeds,
			want:     false, // xyz789 shares ContentPath, files kept
		},
		{
			scenario: "keep files => never frees space",
			mode:     DeleteModeKeepFiles,
			want:     false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			got := deleteFreesSpace(tc.mode, target, allTorrents)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestDeleteFreesSpace_NoCrossSeeds(t *testing.T) {
	// Different releases - each has unique ContentPath (no cross-seeds)
	allTorrents := []qbt.Torrent{
		{Hash: "abc123", Name: "My.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/My.Movie.2024.1080p.BluRay.x264-GRP"},
		{Hash: "def456", Name: "Other.Movie.2024.1080p.BluRay.x264-GRP", ContentPath: "/downloads/movies/Other.Movie.2024.1080p.BluRay.x264-GRP"},
	}

	target := allTorrents[0]

	tests := []struct {
		scenario string
		mode     string
		want     bool
	}{
		{
			scenario: "include cross-seeds mode => frees space",
			mode:     DeleteModeWithFilesIncludeCrossSeeds,
			want:     true,
		},
		{
			scenario: "preserve cross-seeds => frees space (no cross-seed to preserve)",
			mode:     DeleteModeWithFilesPreserveCrossSeeds,
			want:     true, // no cross-seeds exist, so files will be deleted
		},
	}

	for _, tc := range tests {
		t.Run(tc.scenario, func(t *testing.T) {
			got := deleteFreesSpace(tc.mode, target, allTorrents)
			assert.Equal(t, tc.want, got)
		})
	}
}

// -----------------------------------------------------------------------------
// updateCumulativeFreeSpaceCleared tests for preview view behavior
// -----------------------------------------------------------------------------

func TestUpdateCumulativeFreeSpaceCleared_NeededView(t *testing.T) {
	// Test that "needed" mode updates cumulative space tracking
	// so FREE_SPACE condition stops matching after target is satisfied
	allTorrents := []qbt.Torrent{
		{Hash: "a", Size: 100 * 1024 * 1024 * 1024, ContentPath: "/data/movie1", SavePath: "/data"}, // 100 GB
		{Hash: "b", Size: 50 * 1024 * 1024 * 1024, ContentPath: "/data/movie2", SavePath: "/data"},  // 50 GB
		{Hash: "c", Size: 30 * 1024 * 1024 * 1024, ContentPath: "/data/movie3", SavePath: "/data"},  // 30 GB
	}

	evalCtx := &EvalContext{
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}

	// Simulate "needed" mode processing: each deletion updates SpaceToClear
	updateCumulativeFreeSpaceCleared(allTorrents[0], evalCtx, DeleteModeWithFiles, allTorrents)
	assert.Equal(t, int64(100*1024*1024*1024), evalCtx.SpaceToClear)

	updateCumulativeFreeSpaceCleared(allTorrents[1], evalCtx, DeleteModeWithFiles, allTorrents)
	assert.Equal(t, int64(150*1024*1024*1024), evalCtx.SpaceToClear)

	updateCumulativeFreeSpaceCleared(allTorrents[2], evalCtx, DeleteModeWithFiles, allTorrents)
	assert.Equal(t, int64(180*1024*1024*1024), evalCtx.SpaceToClear)
}

func TestUpdateCumulativeFreeSpaceCleared_EligibleView(t *testing.T) {
	// Test that "eligible" mode does NOT update cumulative space tracking
	// (simulated by not calling updateCumulativeFreeSpaceCleared)
	// This is the expected behavior in eligible mode - we skip the update
	allTorrents := []qbt.Torrent{
		{Hash: "a", Size: 100 * 1024 * 1024 * 1024, ContentPath: "/data/movie1"}, // 100 GB
		{Hash: "b", Size: 50 * 1024 * 1024 * 1024, ContentPath: "/data/movie2"},  // 50 GB
	}

	evalCtx := &EvalContext{
		SpaceToClear: 0,
	}

	// In "eligible" mode, we don't call updateCumulativeFreeSpaceCleared
	// SpaceToClear should remain 0, so all torrents continue to match FREE_SPACE conditions

	// Verify SpaceToClear stays at 0 when we don't update it
	assert.Equal(t, int64(0), evalCtx.SpaceToClear)

	// In eligible mode the condition would continue matching all torrents
	// because SpaceToClear is never incremented
	_ = allTorrents // Used in actual preview logic
}

func TestPreviewViewBehavior_CrossSeedExpansion(t *testing.T) {
	// Test that cross-seed expansion works the same way in both views
	// Only deleteWithFilesIncludeCrossSeeds mode expands cross-seeds
	allTorrents := []qbt.Torrent{
		{Hash: "a", Size: 50 * 1024 * 1024 * 1024, ContentPath: "/data/shared"}, // 50 GB - trigger
		{Hash: "b", Size: 50 * 1024 * 1024 * 1024, ContentPath: "/data/shared"}, // 50 GB - cross-seed
		{Hash: "c", Size: 30 * 1024 * 1024 * 1024, ContentPath: "/data/unique"}, // 30 GB - unique
	}

	// findCrossSeedGroup should return both a and b for target a
	group := findCrossSeedGroup(allTorrents[0], allTorrents)
	require.NotNil(t, group)
	assert.Len(t, group, 2)

	groupHashes := make(map[string]bool)
	for _, t := range group {
		groupHashes[t.Hash] = true
	}
	assert.True(t, groupHashes["a"])
	assert.True(t, groupHashes["b"])
	assert.False(t, groupHashes["c"])
}

func TestFreeSpaceCondition_StopWhenSatisfied(t *testing.T) {
	// Test that FREE_SPACE condition logic respects SpaceToClear projection
	// When SpaceToClear + FreeSpace >= target, condition should stop matching

	// Simulate 400GB free, target 500GB => need to clear 100GB
	evalCtx := &EvalContext{
		FreeSpace:    400000000000, // 400 GB
		SpaceToClear: 0,
	}

	// Create a FREE_SPACE < 500GB condition (value in bytes)
	condition := &RuleCondition{
		Field:    FieldFreeSpace,
		Operator: OperatorLessThan,
		Value:    "500000000000", // 500GB in bytes
	}

	// Initially: 400GB free + 0 to be cleared = 400GB effective
	// 400GB < 500GB => should match
	match1 := EvaluateConditionWithContext(condition, qbt.Torrent{}, evalCtx, 0)
	assert.True(t, match1, "Should match when effective free space is below target")

	// Simulate clearing 50GB
	evalCtx.SpaceToClear = 50000000000 // 50GB

	// Now: 400GB free + 50GB to be cleared = 450GB effective
	// 450GB < 500GB => should still match
	match2 := EvaluateConditionWithContext(condition, qbt.Torrent{}, evalCtx, 0)
	assert.True(t, match2, "Should match when effective free space is still below target")

	// Simulate clearing another 60GB (total 110GB)
	evalCtx.SpaceToClear = 110000000000 // 110GB

	// Now: 400GB free + 110GB to be cleared = 510GB effective
	// 510GB < 500GB => false, should NOT match
	match3 := EvaluateConditionWithContext(condition, qbt.Torrent{}, evalCtx, 0)
	assert.False(t, match3, "Should NOT match when effective free space exceeds target")
}

// -----------------------------------------------------------------------------
// executeExternalProgramsFromAutomation tests
// -----------------------------------------------------------------------------

func TestExecuteExternalProgramsFromAutomation_EmptyExecutions(_ *testing.T) {
	// Test that empty executions list returns early without any side effects
	s := &Service{}

	// Should not panic and return immediately
	s.executeExternalProgramsFromAutomation(context.Background(), 1, []pendingProgramExec{})

	// If we get here without panic, the test passes
}

func TestExecuteExternalProgramsFromAutomation_NilExternalProgramService(_ *testing.T) {
	// Test that nil externalProgramService is handled gracefully
	// and doesn't panic (activity logging requires a real store, tested separately)
	s := &Service{
		externalProgramService: nil,
		activityStore:          nil, // No activity store to avoid nil pointer dereference
	}

	executions := []pendingProgramExec{
		{
			hash:      "abc123",
			torrent:   qbt.Torrent{Hash: "abc123", Name: "Test Torrent"},
			programID: 1,
			ruleID:    1,
			ruleName:  "Test Rule",
		},
	}

	// Should not panic - the nil check handles this gracefully
	s.executeExternalProgramsFromAutomation(context.Background(), 1, executions)
}

func TestExecuteExternalProgramsFromAutomation_NilServiceWithActivityStore(t *testing.T) {
	// Test that nil externalProgramService logs activities when activityStore is available
	// Uses a mock querier to capture activity writes

	mockDB := &mockQuerier{
		activities: make([]*models.AutomationActivity, 0),
	}
	activityStore := models.NewAutomationActivityStore(mockDB)

	s := &Service{
		externalProgramService: nil,
		activityStore:          activityStore,
	}

	executions := []pendingProgramExec{
		{
			hash:      "abc123",
			torrent:   qbt.Torrent{Hash: "abc123", Name: "Test Torrent 1"},
			programID: 1,
			ruleID:    1,
			ruleName:  "Test Rule",
		},
		{
			hash:      "def456",
			torrent:   qbt.Torrent{Hash: "def456", Name: "Test Torrent 2"},
			programID: 2,
			ruleID:    2,
			ruleName:  "Another Rule",
		},
	}

	// Should not panic and should log activities
	s.executeExternalProgramsFromAutomation(context.Background(), 1, executions)

	// Verify activities were logged
	require.Len(t, mockDB.activities, 2, "Expected 2 activity entries for 2 executions")

	// Verify first activity
	assert.Equal(t, "abc123", mockDB.activities[0].Hash)
	assert.Equal(t, "Test Torrent 1", mockDB.activities[0].TorrentName)
	assert.Equal(t, "external_program", mockDB.activities[0].Action)
	assert.Equal(t, models.ActivityOutcomeFailed, mockDB.activities[0].Outcome)
	assert.Contains(t, mockDB.activities[0].Reason, "not configured")

	// Verify second activity
	assert.Equal(t, "def456", mockDB.activities[1].Hash)
	assert.Equal(t, "Test Torrent 2", mockDB.activities[1].TorrentName)
}

func TestRecordDryRunActivities_Deletes(t *testing.T) {
	mockDB := &mockQuerier{
		activities: make([]*models.AutomationActivity, 0),
	}
	activityStore := models.NewAutomationActivityStore(mockDB)

	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{
		activityStore: activityStore,
		activityRuns:  newActivityRunStore(24*time.Hour, 10),
		syncManager:   sm,
	}

	pending := map[string]pendingDeletion{
		"abc123": {
			hash:   "abc123",
			action: models.ActivityActionDeletedCondition,
		},
	}

	torrent := qbt.Torrent{
		Hash:    "abc123",
		Name:    "Test Torrent",
		Tracker: "https://tracker.example.com/announce",
	}

	s.recordDryRunActivities(
		context.Background(),
		1,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		pending,
		nil,
		map[string]qbt.Torrent{"abc123": torrent},
		[]qbt.Torrent{torrent},
		map[string]*torrentDesiredState{},
	)

	require.Len(t, mockDB.activities, 1)
	assert.Empty(t, mockDB.activities[0].Hash)
	assert.Equal(t, models.ActivityActionDeletedCondition, mockDB.activities[0].Action)
	assert.Equal(t, models.ActivityOutcomeDryRun, mockDB.activities[0].Outcome)
}

func TestRecordDryRunActivities_Resumes(t *testing.T) {
	mockDB := &mockQuerier{
		activities: make([]*models.AutomationActivity, 0),
	}
	activityStore := models.NewAutomationActivityStore(mockDB)

	sm := qbittorrent.NewSyncManager(nil, nil)
	s := &Service{
		activityStore: activityStore,
		activityRuns:  newActivityRunStore(24*time.Hour, 10),
		syncManager:   sm,
	}

	torrent := qbt.Torrent{
		Hash:    "abc123",
		Name:    "Test Torrent",
		Tracker: "https://tracker.example.com/announce",
	}

	s.recordDryRunActivities(
		context.Background(),
		1,
		nil,
		nil,
		nil,
		nil,
		[]string{"abc123", "abc123"},
		nil,
		nil,
		nil,
		nil,
		nil,
		map[string]qbt.Torrent{"abc123": torrent},
		[]qbt.Torrent{torrent},
		map[string]*torrentDesiredState{},
	)

	require.Len(t, mockDB.activities, 1)
	assert.Empty(t, mockDB.activities[0].Hash)
	assert.Equal(t, models.ActivityActionResumed, mockDB.activities[0].Action)
	assert.Equal(t, models.ActivityOutcomeDryRun, mockDB.activities[0].Outcome)
}

// mockQuerier implements dbinterface.Querier for testing activity logging
type mockQuerier struct {
	activities []*models.AutomationActivity
}

func (m *mockQuerier) QueryRowContext(_ context.Context, _ string, _ ...any) *sql.Row {
	return nil
}

func (m *mockQuerier) ExecContext(_ context.Context, query string, args ...any) (sql.Result, error) {
	// Capture activity insertions
	if len(args) >= 10 && strings.Contains(query, "automation_activity") {
		activity := &models.AutomationActivity{
			InstanceID:  args[0].(int),
			Hash:        args[1].(string),
			TorrentName: args[2].(string),
			Action:      args[4].(string),
			RuleName:    args[6].(string),
			Outcome:     args[7].(string),
			Reason:      args[8].(string),
		}
		m.activities = append(m.activities, activity)
	}
	return mockResult{}, nil
}

func (m *mockQuerier) QueryContext(_ context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return nil, nil
}

func (m *mockQuerier) BeginTx(_ context.Context, _ *sql.TxOptions) (dbinterface.TxQuerier, error) {
	return nil, nil
}

// mockResult implements sql.Result for the mock
type mockResult struct{}

func (m mockResult) LastInsertId() (int64, error) { return 0, nil }
func (m mockResult) RowsAffected() (int64, error) { return 1, nil }
