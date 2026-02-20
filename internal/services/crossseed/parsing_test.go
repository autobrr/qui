// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/assert"
)

// TestDetermineContentType tests the unified content type detection including
// expanded JAV/RIAJ/date/xxx corner cases.
func TestDetermineContentType(t *testing.T) {
	tests := []struct {
		name        string
		release     rls.Release
		wantType    string
		wantCats    []int
		wantSearch  string
		wantCaps    []string
		wantIsMusic bool
	}{
		{
			name:        "Movie",
			release:     rls.Release{Type: rls.Movie, Title: "Test Movie", Year: 2024},
			wantType:    "movie",
			wantCats:    []int{2000},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "TV Episode",
			release:     rls.Release{Type: rls.Episode, Title: "Test Show", Series: 1, Episode: 1},
			wantType:    "tv",
			wantCats:    []int{5000},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "TV Series",
			release:     rls.Release{Type: rls.Series, Title: "Test Show", Series: 1},
			wantType:    "tv",
			wantCats:    []int{5000},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "Music",
			release:     rls.Release{Type: rls.Music, Artist: "Test Artist", Title: "Test Album"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "Audiobook",
			release:     rls.Release{Type: rls.Audiobook, Title: "Test Audiobook"},
			wantType:    "audiobook",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "Book",
			release:     rls.Release{Type: rls.Book, Title: "Test Book"},
			wantType:    "book",
			wantCats:    []int{8000},
			wantSearch:  "book",
			wantCaps:    []string{"book-search"},
			wantIsMusic: false,
		},
		{
			name:        "Comic",
			release:     rls.Release{Type: rls.Comic, Title: "Test Comic"},
			wantType:    "comic",
			wantCats:    []int{8000},
			wantSearch:  "book",
			wantCaps:    []string{"book-search"},
			wantIsMusic: false,
		},
		{
			name:        "Game",
			release:     rls.Release{Type: rls.Game, Title: "Test Game"},
			wantType:    "game",
			wantCats:    []int{4000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "App",
			release:     rls.Release{Type: rls.App, Title: "Test App"},
			wantType:    "app",
			wantCats:    []int{4000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Unknown with Series/Episode (TV fallback)",
			release:     rls.Release{Type: rls.Unknown, Title: "Test", Series: 1, Episode: 1},
			wantType:    "tv",
			wantCats:    []int{5000},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "Unknown with Year (Movie fallback)",
			release:     rls.Release{Type: rls.Unknown, Title: "Test", Year: 2024},
			wantType:    "movie",
			wantCats:    []int{2000},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "Adult content (date pattern)",
			release:     rls.Release{Type: rls.Episode, Title: "FakeStudioZ 010124_001-1PON", Series: 1, Episode: 1},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "JAV (4-letter) -> strip -> parse as TV",
			release:     rls.Release{Type: rls.Unknown, Title: "AAEJ-123 Some Show S01E02 1080p"},
			wantType:    "tv",
			wantCats:    []int{5000},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "JAV (3-letter) -> strip -> parse as Movie",
			release:     rls.Release{Type: rls.Unknown, Title: "IPX-123 Big Movie 1080p"},
			wantType:    "movie",
			wantCats:    []int{2000},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "lowercase jav code -> TV",
			release:     rls.Release{Type: rls.Unknown, Title: "ipx-123 Some Show S02E03 720p"},
			wantType:    "tv",
			wantCats:    []int{5000},
			wantSearch:  "tvsearch",
			wantCaps:    []string{"tv-search"},
			wantIsMusic: false,
		},
		{
			name:        "JAV-strip -> music detection",
			release:     rls.Release{Type: rls.Unknown, Title: "IPX-123 Test Artist - Test Album (2020) [GROUP]"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "RIAJ code -> music detection",
			release:     rls.Release{Type: rls.Unknown, Title: "ABCD-1234 Some Album"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "Mainstream movie xXx franchise",
			release:     rls.Release{Type: rls.Movie, Title: "xXx", Year: 2002},
			wantType:    "movie",
			wantCats:    []int{2000},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "Mainstream movie xXx Return of Xander Cage",
			release:     rls.Release{Type: rls.Movie, Title: "xXx return of xander cage", Year: 2017},
			wantType:    "movie",
			wantCats:    []int{2000},
			wantSearch:  "movie",
			wantCaps:    []string{"movie-search"},
			wantIsMusic: false,
		},
		{
			name:        "Music artist XXXTentacion not adult",
			release:     rls.Release{Type: rls.Music, Artist: "XXXTentacion", Title: "17"},
			wantType:    "music",
			wantCats:    []int{3000},
			wantSearch:  "music",
			wantCaps:    []string{"music-search", "audio-search"},
			wantIsMusic: true,
		},
		{
			name:        "xxx inside word not adult",
			release:     rls.Release{Type: rls.Unknown, Title: "fooxxxbar sample"},
			wantType:    "unknown",
			wantCats:    []int{},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Date pattern (adult) without extra markers",
			release:     rls.Release{Type: rls.Unknown, Title: "010124_001 Some title"},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Bracketed date pattern triggers adult",
			release:     rls.Release{Type: rls.Unknown, Title: "[2023.08.01] Some Title"},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "xxx in subtitle triggers adult",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioX", Subtitle: "25 11 21 FakeActress XXX 2160p MP4-WRB"},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "xxx in collection triggers adult",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioX", Collection: "XXX", Year: 2025},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Porn scene naming with XXX in title",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioX XXX FakeActress 2160p MP4-WRB", Year: 2025},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Porn scene naming 2 with XXX in title",
			release:     rls.Release{Type: rls.Unknown, Title: "StudioY XXX FakeActress2 1080p MP4-WRB", Year: 2025},
			wantType:    "adult",
			wantCats:    []int{6000},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
		{
			name:        "Unknown without hints",
			release:     rls.Release{Type: rls.Unknown, Title: "Test"},
			wantType:    "unknown",
			wantCats:    []int{},
			wantSearch:  "search",
			wantCaps:    []string{},
			wantIsMusic: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := DetermineContentType(&tt.release)

			assert.Equal(t, tt.wantType, result.ContentType)
			assert.Equal(t, tt.wantCats, result.Categories)
			assert.Equal(t, tt.wantSearch, result.SearchType)
			assert.Equal(t, tt.wantCaps, result.RequiredCaps)
			assert.Equal(t, tt.wantIsMusic, result.IsMusic)
		})
	}
}

// TestGameSceneGroupDetection verifies that releases from known game scene groups
// are correctly detected as games via the rls library's group detection.
func TestGameSceneGroupDetection(t *testing.T) {
	tests := []struct {
		name     string
		release  string
		wantType string
		wantCats []int
	}{
		{
			name:     "RUNE game release",
			release:  "Oddsparks.An.Automation.Adventure.Coaster.Rush-RUNE",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "CODEX game release",
			release:  "Some.Game.v1.0-CODEX",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "SKIDROW game release",
			release:  "Another.Game-SKIDROW",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "PLAZA game release",
			release:  "Game.Update.v1.2-PLAZA",
			wantType: "game",
			wantCats: []int{4000},
		},
		{
			name:     "Movie release unchanged",
			release:  "Random.Movie.2024.1080p.BluRay.x264-GROUP",
			wantType: "movie",
			wantCats: []int{2000},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parsed := rls.ParseString(tt.release)
			result := DetermineContentType(&parsed)

			assert.Equal(t, tt.wantType, result.ContentType, "content type mismatch for %s", tt.release)
			assert.Equal(t, tt.wantCats, result.Categories, "categories mismatch for %s", tt.release)
		})
	}
}
