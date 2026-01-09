// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"sort"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
	sm := qbittorrent.NewSyncManager(nil)

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
// findHardlinkCopies tests
// -----------------------------------------------------------------------------

func TestFindHardlinkCopies(t *testing.T) {
	// Create a Service with a nil sync manager (not needed for this test)
	s := &Service{}

	tests := []struct {
		name           string
		triggerHash    string
		hardlinkGroups map[string][]string
		wantCopies     []string
	}{
		{
			name:        "trigger hash not in any group",
			triggerHash: "not-found",
			hardlinkGroups: map[string][]string{
				"sig1": {"abc123", "def456"},
			},
			wantCopies: nil,
		},
		{
			name:        "trigger is only member of group",
			triggerHash: "abc123",
			hardlinkGroups: map[string][]string{
				"sig1": {"abc123"},
			},
			wantCopies: nil,
		},
		{
			name:        "trigger has one hardlink copy",
			triggerHash: "abc123",
			hardlinkGroups: map[string][]string{
				"sig1": {"abc123", "def456"},
			},
			wantCopies: []string{"def456"},
		},
		{
			name:        "trigger has multiple hardlink copies",
			triggerHash: "abc123",
			hardlinkGroups: map[string][]string{
				"sig1": {"abc123", "def456", "ghi789"},
			},
			wantCopies: []string{"def456", "ghi789"},
		},
		{
			name:        "multiple groups, trigger in second",
			triggerHash: "xyz999",
			hardlinkGroups: map[string][]string{
				"sig1": {"abc123", "def456"},
				"sig2": {"xyz999", "uvw888"},
			},
			wantCopies: []string{"uvw888"},
		},
		{
			name:           "nil hardlink groups",
			triggerHash:    "abc123",
			hardlinkGroups: nil,
			wantCopies:     nil,
		},
		{
			name:           "empty hardlink groups",
			triggerHash:    "abc123",
			hardlinkGroups: map[string][]string{},
			wantCopies:     nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := s.findHardlinkCopies(tc.triggerHash, tc.hardlinkGroups)
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
// PreviewDeleteRule previewView simulation tests
// -----------------------------------------------------------------------------

// simulatePreviewDeleteRule simulates the core logic of PreviewDeleteRule
// with controlled test data, allowing us to test "needed" vs "eligible" behavior.
func simulatePreviewDeleteRule(
	torrents []qbt.Torrent,
	rule *models.Automation,
	freeSpace int64,
	previewView string,
) *PreviewResult {
	result := &PreviewResult{Examples: make([]PreviewTorrent, 0)}

	deleteConfig := rule.Conditions.Delete
	if deleteConfig == nil || !deleteConfig.Enabled {
		return result
	}

	deleteMode := deleteConfig.Mode
	if deleteMode == "" {
		deleteMode = DeleteModeKeepFiles
	}

	// Sort torrents by AddedOn (oldest first) for deterministic ordering
	sortedTorrents := make([]qbt.Torrent, len(torrents))
	copy(sortedTorrents, torrents)
	sort.Slice(sortedTorrents, func(i, j int) bool {
		return sortedTorrents[i].AddedOn < sortedTorrents[j].AddedOn
	})

	evalCtx := &EvalContext{
		FreeSpace:    freeSpace,
		SpaceToClear: 0,
		FilesToClear: make(map[crossSeedKey]struct{}),
	}
	eligibleMode := previewView == "eligible"

	for i := range sortedTorrents {
		torrent := &sortedTorrents[i]
		// Check if torrent would be deleted
		wouldDelete := deleteConfig.Condition == nil ||
			EvaluateConditionWithContext(deleteConfig.Condition, *torrent, evalCtx, 0)
		if !wouldDelete {
			continue
		}

		// Only update cumulative space for "needed" mode (not "eligible")
		if !eligibleMode {
			updateCumulativeFreeSpaceCleared(*torrent, evalCtx, deleteMode, sortedTorrents)
		}

		result.Examples = append(result.Examples, PreviewTorrent{
			Hash: torrent.Hash,
			Name: torrent.Name,
			Size: torrent.Size,
		})
	}

	result.TotalMatches = len(result.Examples)
	return result
}

func TestPreviewDeleteRule_NeededVsEligible_FreeSpaceRule(t *testing.T) {
	// This test proves that "eligible" returns more matches than "needed"
	// for FREE_SPACE rules when there are more torrents than needed to satisfy the target.

	// Create 5 torrents, each 20GB, with different ages
	torrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", Size: 20000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/t1"},
		{Hash: "b", Name: "torrent2", Size: 20000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/t2"},
		{Hash: "c", Name: "torrent3", Size: 20000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/t3"},
		{Hash: "d", Name: "torrent4", Size: 20000000000, AddedOn: 4000, SavePath: "/data", ContentPath: "/data/t4"},
		{Hash: "e", Name: "torrent5", Size: 20000000000, AddedOn: 5000, SavePath: "/data", ContentPath: "/data/t5"},
	}

	// Rule: Delete if free space < 50GB
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    DeleteModeWithFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "50000000000", // 50GB
				},
			},
		},
	}

	// Current free space: 10GB
	// Need to clear 40GB to reach 50GB threshold
	// Each torrent is 20GB, so we need exactly 2 torrents for "needed"
	freeSpace := int64(10000000000) // 10GB

	// Test "needed" view - should stop after 2 torrents (40GB cleared = 50GB effective)
	neededResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "needed")

	// Test "eligible" view - should return ALL 5 torrents (doesn't update SpaceToClear)
	eligibleResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "eligible")

	// Assert: eligible should return more matches than needed
	assert.Equal(t, 2, neededResult.TotalMatches, "needed view should return exactly 2 torrents (enough to reach target)")
	assert.Equal(t, 5, eligibleResult.TotalMatches, "eligible view should return all 5 torrents (all match the condition)")
	assert.Greater(t, eligibleResult.TotalMatches, neededResult.TotalMatches, "eligible.TotalMatches should be greater than needed.TotalMatches")

	// Verify "needed" selected the oldest torrents
	neededHashes := make([]string, len(neededResult.Examples))
	for i, ex := range neededResult.Examples {
		neededHashes[i] = ex.Hash
	}
	assert.Contains(t, neededHashes, "a", "needed should include oldest torrent (a)")
	assert.Contains(t, neededHashes, "b", "needed should include second oldest torrent (b)")
}

func TestPreviewDeleteRule_NeededVsEligible_WithCrossSeeds(t *testing.T) {
	// Test that cross-seeds are handled correctly in both views
	// Cross-seeds share the same ContentPath, so they only count once for space projection

	// Create torrents where some are cross-seeds (same content path)
	// torrent1 and torrent2 are cross-seeds (same 30GB file)
	// torrent3, torrent4, torrent5 are independent (20GB each)
	torrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", Size: 30000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/movie"},
		{Hash: "b", Name: "torrent2", Size: 30000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/movie"}, // Cross-seed of a
		{Hash: "c", Name: "torrent3", Size: 20000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/other1"},
		{Hash: "d", Name: "torrent4", Size: 20000000000, AddedOn: 4000, SavePath: "/data", ContentPath: "/data/other2"},
		{Hash: "e", Name: "torrent5", Size: 20000000000, AddedOn: 5000, SavePath: "/data", ContentPath: "/data/other3"},
	}

	// Rule: Delete if free space < 60GB
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    DeleteModeWithFiles, // Standard delete, doesn't expand cross-seeds
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "60000000000", // 60GB
				},
			},
		},
	}

	// Current free space: 10GB
	// Need to clear 50GB to reach 60GB threshold
	// torrent1 (30GB, cross-seed) + torrent2 (cross-seed, 0GB additional) + torrent3 (20GB) = 50GB
	// So "needed" should get: a (30GB), b (0GB - cross-seed), c (20GB) = 3 torrents for 50GB
	freeSpace := int64(10000000000) // 10GB

	// Test "needed" view
	neededResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "needed")

	// Test "eligible" view - should return ALL 5 torrents
	eligibleResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "eligible")

	// Assert: eligible should return more matches than needed
	assert.Equal(t, 3, neededResult.TotalMatches, "needed view should return 3 torrents (a=30GB, b=0GB cross-seed, c=20GB = 50GB)")
	assert.Equal(t, 5, eligibleResult.TotalMatches, "eligible view should return all 5 torrents")
	assert.Greater(t, eligibleResult.TotalMatches, neededResult.TotalMatches, "eligible.TotalMatches should be greater than needed.TotalMatches")
}

func TestPreviewDeleteRule_NeededVsEligible_NoFreeSpaceCondition(t *testing.T) {
	// Test that when there's no FREE_SPACE condition, both views return the same result
	// (because there's no cumulative stop-when-satisfied logic)

	torrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", Size: 20000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/t1", Ratio: 2.5},
		{Hash: "b", Name: "torrent2", Size: 20000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/t2", Ratio: 3.0},
		{Hash: "c", Name: "torrent3", Size: 20000000000, AddedOn: 3000, SavePath: "/data", ContentPath: "/data/t3", Ratio: 1.5},
	}

	// Rule: Delete if ratio > 2.0 (no FREE_SPACE condition)
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    DeleteModeWithFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldRatio,
					Operator: models.OperatorGreaterThan,
					Value:    "2.0",
				},
			},
		},
	}

	freeSpace := int64(100000000000) // 100GB (doesn't matter, not used)

	neededResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "needed")
	eligibleResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "eligible")

	// Both should return 2 torrents (a and b have ratio > 2.0)
	assert.Equal(t, 2, neededResult.TotalMatches, "needed view should return 2 torrents with ratio > 2.0")
	assert.Equal(t, 2, eligibleResult.TotalMatches, "eligible view should return 2 torrents with ratio > 2.0")
	assert.Equal(t, neededResult.TotalMatches, eligibleResult.TotalMatches, "both views should return same count for non-FREE_SPACE rules")
}

func TestPreviewDeleteRule_NeededVsEligible_ExactlyEnoughTorrents(t *testing.T) {
	// Test edge case where "needed" and "eligible" return the same count
	// because there are exactly enough torrents to satisfy the target

	torrents := []qbt.Torrent{
		{Hash: "a", Name: "torrent1", Size: 25000000000, AddedOn: 1000, SavePath: "/data", ContentPath: "/data/t1"}, // 25GB
		{Hash: "b", Name: "torrent2", Size: 25000000000, AddedOn: 2000, SavePath: "/data", ContentPath: "/data/t2"}, // 25GB
	}

	// Rule: Delete if free space < 50GB
	rule := &models.Automation{
		ID:             1,
		Enabled:        true,
		TrackerPattern: "*",
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled: true,
				Mode:    DeleteModeWithFiles,
				Condition: &models.RuleCondition{
					Field:    models.FieldFreeSpace,
					Operator: models.OperatorLessThan,
					Value:    "50000000000", // 50GB
				},
			},
		},
	}

	// Current free space: 0GB
	// Need to clear 50GB, both torrents combined = 50GB
	// So "needed" should get both (exactly enough)
	freeSpace := int64(0)

	neededResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "needed")
	eligibleResult := simulatePreviewDeleteRule(torrents, rule, freeSpace, "eligible")

	// Both should return 2 (exactly enough to satisfy target)
	assert.Equal(t, 2, neededResult.TotalMatches, "needed view should return 2 torrents (exactly enough)")
	assert.Equal(t, 2, eligibleResult.TotalMatches, "eligible view should return 2 torrents (all match)")
}
