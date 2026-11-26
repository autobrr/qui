// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
	"time"
)

func TestReleaseCache_Parse(t *testing.T) {
	cache := NewReleaseCache()

	testName := "Show.Name.S01E05.1080p.WEB-DL.x264-GROUP"

	start := time.Now()
	release1 := cache.Parse(testName)
	firstParseDuration := time.Since(start)

	if release1.Title != "Show Name" {
		t.Errorf("Expected title 'Show Name', got %q", release1.Title)
	}
	if release1.Series != 1 {
		t.Errorf("Expected series 1, got %d", release1.Series)
	}
	if release1.Episode != 5 {
		t.Errorf("Expected episode 5, got %d", release1.Episode)
	}

	start = time.Now()
	release2 := cache.Parse(testName)
	cachedParseDuration := time.Since(start)

	if release2.Title != release1.Title {
		t.Errorf("Cached parse returned different title")
	}
	if release2.Series != release1.Series {
		t.Errorf("Cached parse returned different series")
	}
	if release2.Episode != release1.Episode {
		t.Errorf("Cached parse returned different episode")
	}

	t.Logf("First parse: %v, Cached parse: %v", firstParseDuration, cachedParseDuration)
}
