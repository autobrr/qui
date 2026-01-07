// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
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

