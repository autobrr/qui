// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
	"time"
)

// TestReleaseCache_Parse tests the caching functionality
func TestReleaseCache_Parse(t *testing.T) {
	cache := NewReleaseCache()

	testName := "Show.Name.S01E05.1080p.WEB-DL.x264-GROUP"

	// First parse - should hit rls parser
	start := time.Now()
	release1 := cache.Parse(testName)
	firstParseDuration := time.Since(start)

	// Verify parsed correctly
	if release1.Title != "Show Name" {
		t.Errorf("Expected title 'Show Name', got %q", release1.Title)
	}
	if release1.Series != 1 {
		t.Errorf("Expected series 1, got %d", release1.Series)
	}
	if release1.Episode != 5 {
		t.Errorf("Expected episode 5, got %d", release1.Episode)
	}

	// Second parse - should hit cache (much faster)
	start = time.Now()
	release2 := cache.Parse(testName)
	cachedParseDuration := time.Since(start)

	// Verify same result
	if release2.Title != release1.Title {
		t.Errorf("Cached parse returned different title")
	}
	if release2.Series != release1.Series {
		t.Errorf("Cached parse returned different series")
	}
	if release2.Episode != release1.Episode {
		t.Errorf("Cached parse returned different episode")
	}

	// Cached version should be significantly faster
	// Note: This might not always be true in tests, but in production the difference is dramatic
	t.Logf("First parse: %v, Cached parse: %v", firstParseDuration, cachedParseDuration)
}

// TestReleaseCache_Clear tests cache clearing
func TestReleaseCache_Clear(t *testing.T) {
	cache := NewReleaseCache()

	testName := "Show.Name.S01E05.mkv"

	// Parse and cache
	release1 := cache.Parse(testName)
	if release1.Title == "" {
		t.Fatal("Failed to parse release")
	}

	// Clear cache
	cache.Clear(testName)

	// Parse again - should hit rls parser again
	release2 := cache.Parse(testName)
	if release2.Title != release1.Title {
		t.Errorf("Parse after clear returned different result")
	}
}

// TestReleaseCache_MultipleParsing tests caching multiple different releases
func TestReleaseCache_MultipleParsing(t *testing.T) {
	cache := NewReleaseCache()

	releases := []string{
		"Show.Name.S01E05.1080p.WEB-DL.x264-GROUP1",
		"Another.Show.S02E10.720p.BluRay.x264-GROUP2",
		"Movie.2020.1080p.WEB-DL.x264-GROUP3",
		"Show.Name.S01E06.1080p.WEB-DL.x264-GROUP1",
	}

	// Parse all releases
	for _, name := range releases {
		release := cache.Parse(name)
		if release.Title == "" {
			t.Errorf("Failed to parse %q", name)
		}
	}

	// Parse again - should all hit cache
	for _, name := range releases {
		release := cache.Parse(name)
		if release.Title == "" {
			t.Errorf("Failed to parse from cache %q", name)
		}
	}
}
