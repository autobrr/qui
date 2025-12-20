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
// Helper functions
// -----------------------------------------------------------------------------

func ptr[T any](v T) *T {
	return &v
}
