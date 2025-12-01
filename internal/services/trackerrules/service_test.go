// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package trackerrules

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultConfig(t *testing.T) {
	cfg := DefaultConfig()

	assert.Equal(t, 20*time.Second, cfg.ScanInterval)
	assert.Equal(t, 2*time.Minute, cfg.SkipWithin)
	assert.Equal(t, 150, cfg.MaxBatchHashes)
}

func TestNewService(t *testing.T) {
	tests := []struct {
		name     string
		cfg      Config
		expected Config
	}{
		{
			name:     "default config",
			cfg:      DefaultConfig(),
			expected: DefaultConfig(),
		},
		{
			name: "zero scan interval uses default",
			cfg: Config{
				ScanInterval:   0,
				SkipWithin:     1 * time.Minute,
				MaxBatchHashes: 100,
			},
			expected: Config{
				ScanInterval:   20 * time.Second,
				SkipWithin:     1 * time.Minute,
				MaxBatchHashes: 100,
			},
		},
		{
			name: "negative skip within uses default",
			cfg: Config{
				ScanInterval:   10 * time.Second,
				SkipWithin:     -1 * time.Minute,
				MaxBatchHashes: 100,
			},
			expected: Config{
				ScanInterval:   10 * time.Second,
				SkipWithin:     2 * time.Minute,
				MaxBatchHashes: 100,
			},
		},
		{
			name: "zero max batch uses default",
			cfg: Config{
				ScanInterval:   10 * time.Second,
				SkipWithin:     1 * time.Minute,
				MaxBatchHashes: 0,
			},
			expected: Config{
				ScanInterval:   10 * time.Second,
				SkipWithin:     1 * time.Minute,
				MaxBatchHashes: 150,
			},
		},
		{
			name: "all zeros uses all defaults",
			cfg: Config{
				ScanInterval:   0,
				SkipWithin:     0,
				MaxBatchHashes: 0,
			},
			expected: DefaultConfig(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(tt.cfg, nil, nil, nil)

			require.NotNil(t, svc)
			assert.Equal(t, tt.expected.ScanInterval, svc.cfg.ScanInterval)
			assert.Equal(t, tt.expected.SkipWithin, svc.cfg.SkipWithin)
			assert.Equal(t, tt.expected.MaxBatchHashes, svc.cfg.MaxBatchHashes)
		})
	}
}

func TestLimitHashBatch(t *testing.T) {
	tests := []struct {
		name     string
		hashes   []string
		max      int
		expected [][]string
	}{
		{
			name:     "empty hashes",
			hashes:   []string{},
			max:      10,
			expected: [][]string{{}},
		},
		{
			name:     "single hash",
			hashes:   []string{"hash1"},
			max:      10,
			expected: [][]string{{"hash1"}},
		},
		{
			name:     "hashes within limit",
			hashes:   []string{"hash1", "hash2", "hash3"},
			max:      5,
			expected: [][]string{{"hash1", "hash2", "hash3"}},
		},
		{
			name:     "hashes at limit",
			hashes:   []string{"hash1", "hash2", "hash3"},
			max:      3,
			expected: [][]string{{"hash1", "hash2", "hash3"}},
		},
		{
			name:     "hashes over limit single split",
			hashes:   []string{"hash1", "hash2", "hash3", "hash4", "hash5"},
			max:      3,
			expected: [][]string{{"hash1", "hash2", "hash3"}, {"hash4", "hash5"}},
		},
		{
			name:     "hashes over limit multiple splits",
			hashes:   []string{"hash1", "hash2", "hash3", "hash4", "hash5", "hash6", "hash7"},
			max:      2,
			expected: [][]string{{"hash1", "hash2"}, {"hash3", "hash4"}, {"hash5", "hash6"}, {"hash7"}},
		},
		{
			name:     "zero max returns single batch",
			hashes:   []string{"hash1", "hash2"},
			max:      0,
			expected: [][]string{{"hash1", "hash2"}},
		},
		{
			name:     "negative max returns single batch",
			hashes:   []string{"hash1", "hash2"},
			max:      -1,
			expected: [][]string{{"hash1", "hash2"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := limitHashBatch(tt.hashes, tt.max)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchesTracker(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		domains  []string
		expected bool
	}{
		{
			name:     "empty pattern",
			pattern:  "",
			domains:  []string{"tracker.example.com"},
			expected: false,
		},
		{
			name:     "empty domains",
			pattern:  "tracker.example.com",
			domains:  []string{},
			expected: false,
		},
		{
			name:     "exact match",
			pattern:  "tracker.example.com",
			domains:  []string{"tracker.example.com"},
			expected: true,
		},
		{
			name:     "case insensitive match",
			pattern:  "Tracker.Example.COM",
			domains:  []string{"tracker.example.com"},
			expected: true,
		},
		{
			name:     "domain in list",
			pattern:  "tracker.example.com",
			domains:  []string{"other.com", "tracker.example.com", "another.com"},
			expected: true,
		},
		{
			name:     "no match",
			pattern:  "tracker.example.com",
			domains:  []string{"other.com", "different.com"},
			expected: false,
		},
		{
			name:     "comma separated patterns",
			pattern:  "first.com, second.com, third.com",
			domains:  []string{"second.com"},
			expected: true,
		},
		{
			name:     "semicolon separated patterns",
			pattern:  "first.com; second.com; third.com",
			domains:  []string{"third.com"},
			expected: true,
		},
		{
			name:     "pipe separated patterns",
			pattern:  "first.com|second.com|third.com",
			domains:  []string{"first.com"},
			expected: true,
		},
		{
			name:     "mixed separators",
			pattern:  "first.com, second.com; third.com|fourth.com",
			domains:  []string{"fourth.com"},
			expected: true,
		},
		{
			name:     "glob pattern with asterisk",
			pattern:  "*.example.com",
			domains:  []string{"tracker.example.com"},
			expected: true,
		},
		{
			name:     "glob pattern with question mark",
			pattern:  "tracker?.example.com",
			domains:  []string{"tracker1.example.com"},
			expected: true,
		},
		{
			name:     "glob no match",
			pattern:  "*.other.com",
			domains:  []string{"tracker.example.com"},
			expected: false,
		},
		{
			name:     "suffix pattern with dot prefix",
			pattern:  ".example.com",
			domains:  []string{"tracker.example.com"},
			expected: true,
		},
		{
			name:     "suffix pattern no match",
			pattern:  ".other.com",
			domains:  []string{"tracker.example.com"},
			expected: false,
		},
		{
			name:     "whitespace handling in pattern",
			pattern:  "  tracker.example.com  ",
			domains:  []string{"tracker.example.com"},
			expected: true,
		},
		{
			name:     "empty token in pattern",
			pattern:  "first.com,,second.com",
			domains:  []string{"second.com"},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := matchesTracker(tt.pattern, tt.domains)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestTorrentHasTag(t *testing.T) {
	tests := []struct {
		name      string
		tags      string
		candidate string
		expected  bool
	}{
		{
			name:      "empty tags",
			tags:      "",
			candidate: "tag1",
			expected:  false,
		},
		{
			name:      "single tag match",
			tags:      "tag1",
			candidate: "tag1",
			expected:  true,
		},
		{
			name:      "single tag no match",
			tags:      "tag1",
			candidate: "tag2",
			expected:  false,
		},
		{
			name:      "multiple tags first match",
			tags:      "tag1,tag2,tag3",
			candidate: "tag1",
			expected:  true,
		},
		{
			name:      "multiple tags middle match",
			tags:      "tag1,tag2,tag3",
			candidate: "tag2",
			expected:  true,
		},
		{
			name:      "multiple tags last match",
			tags:      "tag1,tag2,tag3",
			candidate: "tag3",
			expected:  true,
		},
		{
			name:      "multiple tags no match",
			tags:      "tag1,tag2,tag3",
			candidate: "tag4",
			expected:  false,
		},
		{
			name:      "case insensitive match",
			tags:      "Tag1,TAG2,tag3",
			candidate: "TAG1",
			expected:  true,
		},
		{
			name:      "whitespace handling",
			tags:      "tag1 , tag2 , tag3",
			candidate: "tag2",
			expected:  true,
		},
		{
			name:      "partial match not allowed",
			tags:      "mytag",
			candidate: "tag",
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := torrentHasTag(tt.tags, tt.candidate)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestSanitizeTrackerHost(t *testing.T) {
	tests := []struct {
		name      string
		urlOrHost string
		expected  string
	}{
		{
			name:      "empty string",
			urlOrHost: "",
			expected:  "",
		},
		{
			name:      "whitespace only",
			urlOrHost: "   ",
			expected:  "",
		},
		{
			name:      "simple domain",
			urlOrHost: "tracker.example.com",
			expected:  "tracker.example.com",
		},
		{
			name:      "domain with whitespace",
			urlOrHost: "  tracker.example.com  ",
			expected:  "tracker.example.com",
		},
		{
			name:      "url with scheme returns empty",
			urlOrHost: "https://tracker.example.com",
			expected:  "",
		},
		{
			name:      "domain with path",
			urlOrHost: "tracker.example.com/announce",
			expected:  "tracker.example.com",
		},
		{
			name:      "domain with port",
			urlOrHost: "tracker.example.com:6969",
			expected:  "tracker.example.com",
		},
		{
			name:      "domain with special characters",
			urlOrHost: "tracker.example.com!@#$%",
			expected:  "tracker.example.com",
		},
		{
			name:      "ip address",
			urlOrHost: "192.168.1.1",
			expected:  "192.168.1.1",
		},
		{
			name:      "ip address with port",
			urlOrHost: "192.168.1.1:8080",
			expected:  "192.168.1.1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := sanitizeTrackerHost(tt.urlOrHost)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestService_NilSafety(t *testing.T) {
	t.Run("Start with nil service", func(t *testing.T) {
		var s *Service
		// Should not panic
		s.Start(nil)
	})
}
