// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSplitPatterns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		want    []string
	}{
		{
			name:    "empty string",
			pattern: "",
			want:    nil,
		},
		{
			name:    "single value",
			pattern: "tracker.example.com",
			want:    []string{"tracker.example.com"},
		},
		{
			name:    "comma separated",
			pattern: "tracker1.com,tracker2.com,tracker3.com",
			want:    []string{"tracker1.com", "tracker2.com", "tracker3.com"},
		},
		{
			name:    "semicolon separated",
			pattern: "tracker1.com;tracker2.com;tracker3.com",
			want:    []string{"tracker1.com", "tracker2.com", "tracker3.com"},
		},
		{
			name:    "pipe separated",
			pattern: "tracker1.com|tracker2.com|tracker3.com",
			want:    []string{"tracker1.com", "tracker2.com", "tracker3.com"},
		},
		{
			name:    "mixed separators",
			pattern: "tracker1.com,tracker2.com;tracker3.com|tracker4.com",
			want:    []string{"tracker1.com", "tracker2.com", "tracker3.com", "tracker4.com"},
		},
		{
			name:    "with whitespace",
			pattern: " tracker1.com , tracker2.com ; tracker3.com ",
			want:    []string{"tracker1.com", "tracker2.com", "tracker3.com"},
		},
		{
			name:    "deduplication",
			pattern: "tracker1.com,tracker1.com,tracker2.com",
			want:    []string{"tracker1.com", "tracker2.com"},
		},
		{
			name:    "empty parts are skipped",
			pattern: "tracker1.com,,tracker2.com,",
			want:    []string{"tracker1.com", "tracker2.com"},
		},
		{
			name:    "only separators",
			pattern: ",;|",
			want:    nil,
		},
		{
			name:    "whitespace only parts",
			pattern: "tracker1.com,   ,tracker2.com",
			want:    []string{"tracker1.com", "tracker2.com"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := splitPatterns(tt.pattern)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNormalizeTrackerPattern(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		pattern string
		domains []string
		want    string
	}{
		{
			name:    "empty pattern and domains",
			pattern: "",
			domains: nil,
			want:    "",
		},
		{
			name:    "pattern only",
			pattern: "tracker1.com,tracker2.com",
			domains: nil,
			want:    "tracker1.com,tracker2.com",
		},
		{
			name:    "domains override pattern",
			pattern: "old-tracker.com",
			domains: []string{"new1.com", "new2.com"},
			want:    "new1.com,new2.com",
		},
		{
			name:    "empty domains uses pattern",
			pattern: "tracker.com",
			domains: []string{},
			want:    "tracker.com",
		},
		{
			name:    "pattern with whitespace",
			pattern: "  tracker.com  ",
			domains: nil,
			want:    "tracker.com",
		},
		{
			name:    "dedup and normalize",
			pattern: "tracker1.com,tracker1.com,tracker2.com",
			domains: nil,
			want:    "tracker1.com,tracker2.com",
		},
		{
			name:    "mixed separators normalized",
			pattern: "a.com;b.com|c.com",
			domains: nil,
			want:    "a.com,b.com,c.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := normalizeTrackerPattern(tt.pattern, tt.domains)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNullableHelpers(t *testing.T) {
	t.Parallel()

	t.Run("nullableString", func(t *testing.T) {
		t.Parallel()

		// nil returns nil
		assert.Nil(t, nullableString(nil))

		// non-nil returns value
		s := "test"
		assert.Equal(t, "test", nullableString(&s))
	})

	t.Run("nullableInt64", func(t *testing.T) {
		t.Parallel()

		// nil returns nil
		assert.Nil(t, nullableInt64(nil))

		// non-nil returns value
		i := int64(42)
		assert.Equal(t, int64(42), nullableInt64(&i))
	})

	t.Run("nullableFloat64", func(t *testing.T) {
		t.Parallel()

		// nil returns nil
		assert.Nil(t, nullableFloat64(nil))

		// non-nil returns value
		f := float64(3.14)
		assert.Equal(t, float64(3.14), nullableFloat64(&f))
	})

	t.Run("boolToInt", func(t *testing.T) {
		t.Parallel()

		assert.Equal(t, 1, boolToInt(true))
		assert.Equal(t, 0, boolToInt(false))
	})
}

func TestTrackerRuleStruct(t *testing.T) {
	t.Parallel()

	t.Run("TrackerDomains populated from pattern", func(t *testing.T) {
		t.Parallel()

		rule := &TrackerRule{
			ID:             1,
			InstanceID:     1,
			Name:           "Test Rule",
			TrackerPattern: "tracker1.com,tracker2.com",
		}
		rule.TrackerDomains = splitPatterns(rule.TrackerPattern)

		assert.Len(t, rule.TrackerDomains, 2)
		assert.Equal(t, "tracker1.com", rule.TrackerDomains[0])
		assert.Equal(t, "tracker2.com", rule.TrackerDomains[1])
	})

	t.Run("all optional fields nil", func(t *testing.T) {
		t.Parallel()

		rule := &TrackerRule{
			ID:         1,
			InstanceID: 1,
			Name:       "Minimal Rule",
			Enabled:    true,
		}

		assert.Nil(t, rule.Category)
		assert.Nil(t, rule.Tag)
		assert.Nil(t, rule.UploadLimitKiB)
		assert.Nil(t, rule.DownloadLimitKiB)
		assert.Nil(t, rule.RatioLimit)
		assert.Nil(t, rule.SeedingTimeLimitMinutes)
	})

	t.Run("all optional fields set", func(t *testing.T) {
		t.Parallel()

		category := "Movies"
		tag := "private"
		uploadLimit := int64(1000)
		downloadLimit := int64(2000)
		ratioLimit := 2.5
		seedingTime := int64(10080)

		rule := &TrackerRule{
			ID:                      1,
			InstanceID:              1,
			Name:                    "Full Rule",
			Category:                &category,
			Tag:                     &tag,
			UploadLimitKiB:          &uploadLimit,
			DownloadLimitKiB:        &downloadLimit,
			RatioLimit:              &ratioLimit,
			SeedingTimeLimitMinutes: &seedingTime,
			Enabled:                 true,
		}

		assert.Equal(t, "Movies", *rule.Category)
		assert.Equal(t, "private", *rule.Tag)
		assert.Equal(t, int64(1000), *rule.UploadLimitKiB)
		assert.Equal(t, int64(2000), *rule.DownloadLimitKiB)
		assert.Equal(t, 2.5, *rule.RatioLimit)
		assert.Equal(t, int64(10080), *rule.SeedingTimeLimitMinutes)
	})
}
