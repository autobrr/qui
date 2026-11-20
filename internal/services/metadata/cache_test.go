// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"testing"
	"testing/synctest"
	"time"
)

func TestCacheKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		titleA   string
		titleB   string
		season   int
		wantSame bool
	}{
		{
			name:     "leading and trailing whitespace",
			titleA:   "  Breaking Bad  ",
			titleB:   "breaking bad",
			season:   2,
			wantSame: true,
		},
		{
			name:     "different titles",
			titleA:   "Breaking Bad",
			titleB:   "Better Call Saul",
			season:   1,
			wantSame: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			keyA := cacheKey(tt.titleA, tt.season)
			keyB := cacheKey(tt.titleB, tt.season)

			if got := keyA == keyB; got != tt.wantSame {
				t.Errorf("cacheKey(%q, %d) == cacheKey(%q, %d): got %v, want %v",
					tt.titleA, tt.season, tt.titleB, tt.season, got, tt.wantSame)
			}
		})
	}
}

func TestResultCache_HitAfterSet(t *testing.T) {
	t.Parallel()

	c := newResultCache()
	c.Set("Breaking Bad", 1, 7)

	count, ok := c.Get("Breaking Bad", 1)
	if !ok {
		t.Fatal("expected cache hit, got miss")
	}
	if count != 7 {
		t.Errorf("got count %d, want 7", count)
	}
}

func TestResultCache_Miss(t *testing.T) {
	t.Parallel()

	c := newResultCache()

	_, ok := c.Get("Nonexistent Show", 1)
	if ok {
		t.Fatal("expected cache miss for unknown key")
	}
}

func TestResultCache_NormalizedKeyHit(t *testing.T) {
	t.Parallel()

	c := newResultCache()
	c.Set("Breaking Bad", 2, 13)

	count, ok := c.Get("BREAKING BAD", 2)
	if !ok {
		t.Fatal("expected cache hit with different casing, got miss")
	}
	if count != 13 {
		t.Errorf("got count %d, want 13", count)
	}
}

func TestResultCache_TTLExpiry(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		c := newResultCache()
		c.Set("Expired Show", 1, 10)

		time.Sleep(cacheTTL + time.Second)

		if _, ok := c.Get("Expired Show", 1); ok {
			t.Fatal("expected cache miss for expired entry")
		}
	})
}
