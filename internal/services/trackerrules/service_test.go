// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackerrules

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
// shouldDeleteTorrent tests
// -----------------------------------------------------------------------------

func TestShouldDeleteTorrent(t *testing.T) {
	ratioLimit := 2.0
	seedingLimit := int64(60) // 60 minutes = 3600 seconds

	tests := []struct {
		name    string
		torrent qbt.Torrent
		rule    *models.TrackerRule
		want    bool
	}{
		// Progress checks
		{
			name:    "incomplete torrent not deleted",
			torrent: qbt.Torrent{Progress: 0.5, Ratio: 3.0, SeedingTime: 7200},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit},
			want:    false,
		},
		{
			name:    "completed torrent can be deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0, SeedingTime: 7200},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit},
			want:    true,
		},

		// DeleteMode checks
		{
			name:    "nil delete mode not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: nil, RatioLimit: &ratioLimit},
			want:    false,
		},
		{
			name:    "empty delete mode not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: ptr(""), RatioLimit: &ratioLimit},
			want:    false,
		},
		{
			name:    "none delete mode not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: ptr("none"), RatioLimit: &ratioLimit},
			want:    false,
		},
		{
			name:    "delete mode enabled",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit},
			want:    true,
		},
		{
			name:    "deleteWithFiles mode enabled",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: ptr("deleteWithFiles"), RatioLimit: &ratioLimit},
			want:    true,
		},

		// Limit configuration checks
		{
			name:    "no limits configured not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0, SeedingTime: 7200},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete")},
			want:    false,
		},
		{
			name:    "zero ratio limit not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: ptr(0.0)},
			want:    false,
		},
		{
			name:    "zero seeding limit not deleted",
			torrent: qbt.Torrent{Progress: 1.0, SeedingTime: 7200},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), SeedingTimeLimitMinutes: ptr(int64(0))},
			want:    false,
		},

		// Ratio limit checks
		{
			name:    "ratio below limit not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 1.5},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit},
			want:    false,
		},
		{
			name:    "ratio at limit deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 2.0},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit},
			want:    true,
		},
		{
			name:    "ratio above limit deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit},
			want:    true,
		},

		// Seeding time limit checks (SeedingTime is in seconds, limit is in minutes)
		{
			name:    "seeding time below limit not deleted",
			torrent: qbt.Torrent{Progress: 1.0, SeedingTime: 3000}, // 50 minutes
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), SeedingTimeLimitMinutes: &seedingLimit},
			want:    false,
		},
		{
			name:    "seeding time at limit deleted",
			torrent: qbt.Torrent{Progress: 1.0, SeedingTime: 3600}, // 60 minutes
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), SeedingTimeLimitMinutes: &seedingLimit},
			want:    true,
		},
		{
			name:    "seeding time above limit deleted",
			torrent: qbt.Torrent{Progress: 1.0, SeedingTime: 7200}, // 120 minutes
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), SeedingTimeLimitMinutes: &seedingLimit},
			want:    true,
		},

		// OR logic: either condition triggers deletion
		{
			name:    "ratio met seeding not met - deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0, SeedingTime: 1800}, // ratio met, 30 min seeding
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit, SeedingTimeLimitMinutes: &seedingLimit},
			want:    true,
		},
		{
			name:    "ratio not met seeding met - deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 1.0, SeedingTime: 7200}, // ratio not met, seeding met
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit, SeedingTimeLimitMinutes: &seedingLimit},
			want:    true,
		},
		{
			name:    "both conditions met - deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 3.0, SeedingTime: 7200},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit, SeedingTimeLimitMinutes: &seedingLimit},
			want:    true,
		},
		{
			name:    "neither condition met - not deleted",
			torrent: qbt.Torrent{Progress: 1.0, Ratio: 1.0, SeedingTime: 1800},
			rule:    &models.TrackerRule{DeleteMode: ptr("delete"), RatioLimit: &ratioLimit, SeedingTimeLimitMinutes: &seedingLimit},
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := shouldDeleteTorrent(tc.torrent, tc.rule)
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
// selectRule tests
// -----------------------------------------------------------------------------

func TestSelectRule(t *testing.T) {
	// Create a minimal SyncManager for domain extraction
	sm := qbittorrent.NewSyncManager(nil)

	tests := []struct {
		name    string
		torrent qbt.Torrent
		rules   []*models.TrackerRule
		wantID  int // 0 means expect nil
	}{
		{
			name:    "no rules returns nil",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules:   []*models.TrackerRule{},
			wantID:  0,
		},
		{
			name:    "disabled rule skipped",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: false, TrackerPattern: "tracker.example.com"},
			},
			wantID: 0,
		},
		{
			name:    "enabled rule matches",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "tracker.example.com"},
			},
			wantID: 1,
		},
		{
			name:    "first matching rule wins",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "tracker.example.com"},
				{ID: 2, Enabled: true, TrackerPattern: "*"},
			},
			wantID: 1,
		},
		{
			name:    "wildcard matches all",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*"},
			},
			wantID: 1,
		},

		// Category matching
		{
			name:    "category match",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "tv"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{"tv"}},
			},
			wantID: 1,
		},
		{
			name:    "category mismatch",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "movies"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{"tv"}},
			},
			wantID: 0,
		},
		{
			name:    "category case insensitive",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "TV"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{"tv"}},
			},
			wantID: 1,
		},
		{
			name:    "no category filter matches all",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "anything"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{}},
			},
			wantID: 1,
		},
		{
			name:    "multiple categories any match",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "movies"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{"tv", "movies"}},
			},
			wantID: 1,
		},

		// Tag matching - ANY mode (default)
		{
			name:    "tag any mode match",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Tags: "tagA, tagB"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Tags: []string{"tagA"}, TagMatchMode: models.TagMatchModeAny},
			},
			wantID: 1,
		},
		{
			name:    "tag any mode one of many",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Tags: "tagA"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Tags: []string{"tagA", "tagB", "tagC"}, TagMatchMode: models.TagMatchModeAny},
			},
			wantID: 1,
		},
		{
			name:    "tag any mode no match",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Tags: "tagX"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Tags: []string{"tagA", "tagB"}, TagMatchMode: models.TagMatchModeAny},
			},
			wantID: 0,
		},

		// Tag matching - ALL mode
		{
			name:    "tag all mode all present",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Tags: "tagA, tagB, tagC"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Tags: []string{"tagA", "tagB"}, TagMatchMode: models.TagMatchModeAll},
			},
			wantID: 1,
		},
		{
			name:    "tag all mode missing one",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Tags: "tagA"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Tags: []string{"tagA", "tagB"}, TagMatchMode: models.TagMatchModeAll},
			},
			wantID: 0,
		},

		// No tag filter matches all
		{
			name:    "no tag filter matches all",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Tags: "anything"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Tags: []string{}},
			},
			wantID: 1,
		},

		// Combined filters
		{
			name:    "combined category and tag match",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "tv", Tags: "tagA"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{"tv"}, Tags: []string{"tagA"}},
			},
			wantID: 1,
		},
		{
			name:    "combined category match tag mismatch",
			torrent: qbt.Torrent{Hash: "abc", Tracker: "http://tracker.example.com/announce", Category: "tv", Tags: "tagX"},
			rules: []*models.TrackerRule{
				{ID: 1, Enabled: true, TrackerPattern: "*", Categories: []string{"tv"}, Tags: []string{"tagA"}},
			},
			wantID: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := selectRule(tc.torrent, tc.rules, sm)
			if tc.wantID == 0 {
				assert.Nil(t, got)
			} else {
				require.NotNil(t, got)
				assert.Equal(t, tc.wantID, got.ID)
			}
		})
	}
}

// -----------------------------------------------------------------------------
// Helper functions
// -----------------------------------------------------------------------------

func ptr[T any](v T) *T {
	return &v
}
