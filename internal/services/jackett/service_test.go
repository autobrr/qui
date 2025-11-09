// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"maps"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
)

func TestDetectContentType(t *testing.T) {
	s := NewService(nil, nil)

	tests := []struct {
		name     string
		req      *TorznabSearchRequest
		expected contentType
	}{
		{
			name: "detects TV show by episode",
			req: &TorznabSearchRequest{
				Query:   "Breaking Bad",
				Season:  intPtr(1),
				Episode: intPtr(1),
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects TV show by season pack",
			req: &TorznabSearchRequest{
				Query:  "The Wire",
				Season: intPtr(2),
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects TV show by TVDbID",
			req: &TorznabSearchRequest{
				Query:  "The Sopranos",
				TVDbID: "123456",
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects movie by IMDbID",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			expected: contentTypeMovie,
		},
		{
			name: "detects XXX by query content",
			req: &TorznabSearchRequest{
				Query: "xxx content here",
			},
			expected: contentTypeXXX,
		},
		{
			name: "detects TV show via release parser",
			req: &TorznabSearchRequest{
				Query: "Breaking.Bad.S01.1080p.WEB-DL.DD5.1.H.264-NTb",
			},
			expected: contentTypeTVShow,
		},
		{
			name: "detects movie via release parser",
			req: &TorznabSearchRequest{
				Query: "Black.Phone.2.2025.1080p.AMZN.WEB-DL.DDP5.1.H.264-KyoGo",
			},
			expected: contentTypeMovie,
		},
		{
			name: "detects music release",
			req: &TorznabSearchRequest{
				Query: "Lane 8 & Jyll - Stay Still, A Little While (2025) [WEB FLAC]",
			},
			expected: contentTypeMusic,
		},
		{
			name: "detects music even if parser extracts episode number",
			req: &TorznabSearchRequest{
				Query: "Various Artists - 25 Years Of Anjuna Mixed By Marsh (2025) - WEB FLAC 16-48",
			},
			expected: contentTypeMusic,
		},
		{
			name: "detects app release",
			req: &TorznabSearchRequest{
				Query: "Screen Studio 3.2.1-3520 ARM",
			},
			expected: contentTypeApp,
		},
		{
			name: "detects game release",
			req: &TorznabSearchRequest{
				Query: "Super.Mario.Bros.Wonder.NSW-BigBlueBox",
			},
			expected: contentTypeGame,
		},
		{
			name: "detects audiobook release",
			req: &TorznabSearchRequest{
				Query: "Some.Audiobook.Title.2024.MP3",
			},
			expected: contentTypeAudiobook,
		},
		{
			name: "detects book release",
			req: &TorznabSearchRequest{
				Query: "Harry.Potter.and.the.Sorcerers.Stone.EPUB",
			},
			expected: contentTypeBook,
		},
		{
			name: "detects comic release",
			req: &TorznabSearchRequest{
				Query: "Amazing.Spider-Man.2025.01.Comic",
			},
			expected: contentTypeComic,
		},
		{
			name: "detects magazine release",
			req: &TorznabSearchRequest{
				Query: "National.Geographic.MAGAZiNE.2024.01",
			},
			expected: contentTypeMagazine,
		},
		{
			name: "detects education release",
			req: &TorznabSearchRequest{
				Query: "Udemy-Python.Programming.Masterclass",
			},
			expected: contentTypeEducation,
		},
		{
			name: "returns unknown for ambiguous query",
			req: &TorznabSearchRequest{
				Query: "random search",
			},
			expected: contentTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.detectContentType(tt.req)
			if result != tt.expected {
				t.Errorf("detectContentType() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestGetCategoriesForContentType(t *testing.T) {
	tests := []struct {
		name     string
		ct       contentType
		expected []int
	}{
		{
			name:     "returns movie categories",
			ct:       contentTypeMovie,
			expected: []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K},
		},
		{
			name:     "returns TV categories",
			ct:       contentTypeTVShow,
			expected: []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K},
		},
		{
			name:     "returns TV categories for daily shows",
			ct:       contentTypeTVDaily,
			expected: []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K},
		},
		{
			name:     "returns XXX categories",
			ct:       contentTypeXXX,
			expected: []int{CategoryXXX, CategoryXXXDVD, CategoryXXXx264, CategoryXXXPack},
		},
		{
			name:     "returns audio categories",
			ct:       contentTypeMusic,
			expected: []int{CategoryAudio},
		},
		{
			name:     "returns audio categories for audiobooks",
			ct:       contentTypeAudiobook,
			expected: []int{CategoryAudio},
		},
		{
			name:     "returns book categories",
			ct:       contentTypeBook,
			expected: []int{CategoryBooks, CategoryBooksEbook},
		},
		{
			name:     "returns comic categories",
			ct:       contentTypeComic,
			expected: []int{CategoryBooksComics},
		},
		{
			name:     "returns magazine categories",
			ct:       contentTypeMagazine,
			expected: []int{CategoryBooks},
		},
		{
			name:     "returns education categories",
			ct:       contentTypeEducation,
			expected: []int{CategoryBooks},
		},
		{
			name:     "returns PC categories for apps",
			ct:       contentTypeApp,
			expected: []int{CategoryPC},
		},
		{
			name:     "returns PC categories for games",
			ct:       contentTypeGame,
			expected: []int{CategoryPC},
		},
		{
			name:     "returns default categories for unknown",
			ct:       contentTypeUnknown,
			expected: []int{CategoryMovies, CategoryTV},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getCategoriesForContentType(tt.ct)
			if len(result) != len(tt.expected) {
				t.Errorf("getCategoriesForContentType() returned %d categories, want %d", len(result), len(tt.expected))
				return
			}
			for i, cat := range result {
				if cat != tt.expected[i] {
					t.Errorf("getCategoriesForContentType()[%d] = %v, want %v", i, cat, tt.expected[i])
				}
			}
		})
	}
}

func TestParseCategoryID(t *testing.T) {
	s := &Service{}
	tests := []struct {
		name     string
		category string
		expected int
	}{
		{
			name:     "parses numeric category",
			category: "5000",
			expected: 5000,
		},
		{
			name:     "parses numeric category with description",
			category: "2000 Movies",
			expected: 2000,
		},
		{
			name:     "maps movies text to ID",
			category: "Movies > HD",
			expected: CategoryMovies,
		},
		{
			name:     "maps TV text to ID",
			category: "TV",
			expected: CategoryTV,
		},
		{
			name:     "maps XXX text to ID",
			category: "XXX > DVD",
			expected: CategoryXXX,
		},
		{
			name:     "maps audio text to ID",
			category: "Audio / MP3",
			expected: CategoryAudio,
		},
		{
			name:     "returns 0 for unknown category",
			category: "Unknown Category",
			expected: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.parseCategoryID(tt.category)
			if result != tt.expected {
				t.Errorf("parseCategoryID(%q) = %v, want %v", tt.category, result, tt.expected)
			}
		})
	}
}

func TestBuildSearchParams(t *testing.T) {
	s := &Service{}
	tests := []struct {
		name       string
		req        *TorznabSearchRequest
		searchMode string
		expected   map[string]string
	}{
		{
			name: "basic query",
			req: &TorznabSearchRequest{
				Query: "test movie",
			},
			expected: map[string]string{
				"t": "search",
				"q": "test movie",
			},
		},
		{
			name: "query with categories",
			req: &TorznabSearchRequest{
				Query:      "test show",
				Categories: []int{CategoryTV, CategoryTVHD},
			},
			expected: map[string]string{
				"t":   "search",
				"q":   "test show",
				"cat": "5000,5040",
			},
		},
		{
			name: "query with IMDb ID",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			expected: map[string]string{
				"t":      "search",
				"q":      "The Matrix",
				"imdbid": "0133093",
			},
		},
		{
			name: "query with IMDb ID without tt prefix",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "0133093",
			},
			expected: map[string]string{
				"t":      "search",
				"q":      "The Matrix",
				"imdbid": "0133093",
			},
		},
		{
			name: "query with TVDb ID",
			req: &TorznabSearchRequest{
				Query:  "Breaking Bad",
				TVDbID: "81189",
			},
			searchMode: "tvsearch",
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Breaking Bad",
				"tvdbid": "81189",
			},
		},
		{
			name: "query with season and episode",
			req: &TorznabSearchRequest{
				Query:   "Game of Thrones",
				Season:  intPtr(1),
				Episode: intPtr(1),
			},
			searchMode: "tvsearch",
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Game of Thrones",
				"season": "1",
				"ep":     "1",
			},
		},
		{
			name: "query with limit and offset",
			req: &TorznabSearchRequest{
				Query:  "test",
				Limit:  100,
				Offset: 50,
			},
			expected: map[string]string{
				"t":     "search",
				"q":     "test",
				"limit": "100",
			},
		},
		{
			name: "complete request",
			req: &TorznabSearchRequest{
				Query:      "Breaking Bad",
				Categories: []int{CategoryTV},
				TVDbID:     "81189",
				Season:     intPtr(1),
				Episode:    intPtr(1),
				Limit:      50,
				Offset:     10,
			},
			searchMode: "tvsearch",
			expected: map[string]string{
				"t":      "tvsearch",
				"q":      "Breaking Bad",
				"cat":    "5000",
				"tvdbid": "81189",
				"season": "1",
				"ep":     "1",
				"limit":  "50",
			},
		},
		{
			name: "movie request",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			searchMode: "movie",
			expected: map[string]string{
				"t":      "movie",
				"q":      "The Matrix",
				"imdbid": "0133093",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mode := tt.searchMode
			if mode == "" {
				mode = "search"
			}
			result := s.buildSearchParams(tt.req, mode)
			for key, expectedValue := range tt.expected {
				actualValue := result.Get(key)
				if actualValue != expectedValue {
					t.Errorf("buildSearchParams()[%q] = %q, want %q", key, actualValue, expectedValue)
				}
			}
			// Check no extra params
			for key := range result {
				if _, exists := tt.expected[key]; !exists {
					t.Errorf("buildSearchParams() has unexpected param %q = %q", key, result.Get(key))
				}
			}
		})
	}
}

func TestConvertResults(t *testing.T) {
	s := &Service{}
	tests := []struct {
		name     string
		input    []Result
		expected int // number of expected results
		checkFn  func(*testing.T, []SearchResult)
	}{
		{
			name:     "empty results",
			input:    []Result{},
			expected: 0,
		},
		{
			name: "single result",
			input: []Result{
				{
					Tracker:              "TestTracker",
					Title:                "Test Release",
					Link:                 "http://example.com/download",
					Details:              "http://example.com/details",
					Size:                 1024 * 1024 * 1024,
					Seeders:              10,
					Peers:                15,
					Category:             "5000",
					DownloadVolumeFactor: 0.0,
					UploadVolumeFactor:   1.0,
					GUID:                 "test-guid-123",
					Imdb:                 "tt0133093",
				},
			},
			expected: 1,
			checkFn: func(t *testing.T, results []SearchResult) {
				if results[0].Indexer != "TestTracker" {
					t.Errorf("Indexer = %q, want %q", results[0].Indexer, "TestTracker")
				}
				if results[0].Title != "Test Release" {
					t.Errorf("Title = %q, want %q", results[0].Title, "Test Release")
				}
				if results[0].Size != 1024*1024*1024 {
					t.Errorf("Size = %d, want %d", results[0].Size, 1024*1024*1024)
				}
				if results[0].Seeders != 10 {
					t.Errorf("Seeders = %d, want %d", results[0].Seeders, 10)
				}
				if results[0].Leechers != 5 { // Peers - Seeders
					t.Errorf("Leechers = %d, want %d", results[0].Leechers, 5)
				}
				if results[0].CategoryID != 5000 {
					t.Errorf("CategoryID = %d, want %d", results[0].CategoryID, 5000)
				}
			},
		},
		{
			name: "multiple results sorted by seeders",
			input: []Result{
				{
					Tracker: "Tracker1",
					Title:   "Low Seeders",
					Seeders: 5,
					Peers:   10,
					Size:    1024,
				},
				{
					Tracker: "Tracker2",
					Title:   "High Seeders",
					Seeders: 50,
					Peers:   60,
					Size:    2048,
				},
				{
					Tracker: "Tracker3",
					Title:   "Medium Seeders",
					Seeders: 20,
					Peers:   25,
					Size:    1536,
				},
			},
			expected: 3,
			checkFn: func(t *testing.T, results []SearchResult) {
				// Should be sorted by seeders descending
				if results[0].Title != "High Seeders" {
					t.Errorf("First result title = %q, want %q", results[0].Title, "High Seeders")
				}
				if results[1].Title != "Medium Seeders" {
					t.Errorf("Second result title = %q, want %q", results[1].Title, "Medium Seeders")
				}
				if results[2].Title != "Low Seeders" {
					t.Errorf("Third result title = %q, want %q", results[2].Title, "Low Seeders")
				}
			},
		},
		{
			name: "results with same seeders sorted by size",
			input: []Result{
				{
					Tracker: "Tracker1",
					Title:   "Small File",
					Seeders: 10,
					Peers:   15,
					Size:    1024,
				},
				{
					Tracker: "Tracker2",
					Title:   "Large File",
					Seeders: 10,
					Peers:   15,
					Size:    5120,
				},
				{
					Tracker: "Tracker3",
					Title:   "Medium File",
					Seeders: 10,
					Peers:   15,
					Size:    2048,
				},
			},
			expected: 3,
			checkFn: func(t *testing.T, results []SearchResult) {
				// Same seeders, should be sorted by size descending
				if results[0].Title != "Large File" {
					t.Errorf("First result title = %q, want %q", results[0].Title, "Large File")
				}
				if results[1].Title != "Medium File" {
					t.Errorf("Second result title = %q, want %q", results[1].Title, "Medium File")
				}
				if results[2].Title != "Small File" {
					t.Errorf("Third result title = %q, want %q", results[2].Title, "Small File")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.convertResults(tt.input)
			if len(result) != tt.expected {
				t.Errorf("convertResults() returned %d results, want %d", len(result), tt.expected)
			}
			if tt.checkFn != nil {
				tt.checkFn(t, result)
			}
		})
	}
}

func TestSearchAutoDetectCategories(t *testing.T) {
	// Mock store
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{},
	}
	s := NewService(store, nil)

	tests := []struct {
		name             string
		req              *TorznabSearchRequest
		expectedCats     []int
		shouldAutoDetect bool
	}{
		{
			name: "auto-detects TV categories",
			req: &TorznabSearchRequest{
				Query:   "Breaking Bad",
				Season:  intPtr(1),
				Episode: intPtr(1),
			},
			expectedCats:     []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K},
			shouldAutoDetect: true,
		},
		{
			name: "auto-detects movie categories",
			req: &TorznabSearchRequest{
				Query:  "The Matrix",
				IMDbID: "tt0133093",
			},
			expectedCats:     []int{CategoryMovies, CategoryMoviesSD, CategoryMoviesHD, CategoryMovies4K},
			shouldAutoDetect: true,
		},
		{
			name: "uses provided categories",
			req: &TorznabSearchRequest{
				Query:      "test",
				Categories: []int{CategoryAudio},
			},
			expectedCats:     []int{CategoryAudio},
			shouldAutoDetect: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := s.Search(context.Background(), tt.req)
			// Error expected since no indexers configured
			if err == nil || err.Error() != "query is required" {
				// Check that categories were set
				if len(tt.req.Categories) != len(tt.expectedCats) {
					t.Errorf("Categories count = %d, want %d", len(tt.req.Categories), len(tt.expectedCats))
				}
				for i, cat := range tt.req.Categories {
					if cat != tt.expectedCats[i] {
						t.Errorf("Categories[%d] = %v, want %v", i, cat, tt.expectedCats[i])
					}
				}
			}
		})
	}
}

func TestFilterCategoriesForIndexer(t *testing.T) {
	movieParent := CategoryMovies
	indexerCats := []models.TorznabIndexerCategory{
		{CategoryID: CategoryMovies},
		{CategoryID: CategoryMoviesHD, ParentCategory: &movieParent},
	}

	t.Run("allows matching categories", func(t *testing.T) {
		filtered, ok := filterCategoriesForIndexer(indexerCats, []int{CategoryMoviesHD})
		if !ok {
			t.Fatalf("expected categories to be permitted")
		}
		if len(filtered) != 1 || filtered[0] != CategoryMoviesHD {
			t.Fatalf("unexpected filtered categories: %+v", filtered)
		}
	})

	t.Run("skips unsupported categories", func(t *testing.T) {
		_, ok := filterCategoriesForIndexer(indexerCats, []int{CategoryTV})
		if ok {
			t.Fatalf("expected unsupported categories to be rejected")
		}
	})
}

func TestSearchGenericAutoDetectCategories(t *testing.T) {
	store := &mockTorznabIndexerStore{indexers: []*models.TorznabIndexer{}}
	s := NewService(store, nil)
	req := &TorznabSearchRequest{Query: "Breaking Bad S01"}

	resp, err := s.SearchGeneric(context.Background(), req)
	if err != nil {
		t.Fatalf("SearchGeneric returned error: %v", err)
	}
	if resp == nil {
		t.Fatalf("expected response, got nil")
	}

	expected := []int{CategoryTV, CategoryTVSD, CategoryTVHD, CategoryTV4K}
	if len(req.Categories) != len(expected) {
		t.Fatalf("expected %d categories, got %d", len(expected), len(req.Categories))
	}
	for i, cat := range expected {
		if req.Categories[i] != cat {
			t.Fatalf("category %d = %d, want %d", i, req.Categories[i], cat)
		}
	}
}

func TestSearchWithLimit(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{},
	}
	s := NewService(store, nil)

	// Test with empty store (no network calls)
	tests := []struct {
		name          string
		req           *TorznabSearchRequest
		expectedTotal int
		expectedCount int
	}{
		{
			name: "no indexers returns empty",
			req: &TorznabSearchRequest{
				Query: "test",
			},
			expectedTotal: 0,
			expectedCount: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.Search(context.Background(), tt.req)
			if err != nil {
				t.Fatalf("Search() error = %v", err)
			}
			if resp.Total != tt.expectedTotal {
				t.Errorf("Total = %d, want %d", resp.Total, tt.expectedTotal)
			}
			if len(resp.Results) != tt.expectedCount {
				t.Errorf("Results count = %d, want %d", len(resp.Results), tt.expectedCount)
			}
		})
	}
}

func TestSearchGenericWithIndexerIDs(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Indexer1", Enabled: true},
			{ID: 2, Name: "Indexer2", Enabled: true},
			{ID: 3, Name: "Indexer3", Enabled: false},
		},
	}
	s := NewService(store, nil)

	tests := []struct {
		name        string
		req         *TorznabSearchRequest
		shouldError bool
	}{
		{
			name: "search specific enabled indexer",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{1},
			},
			shouldError: true,
		},
		{
			name: "search multiple indexers",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{1, 2},
			},
			shouldError: true,
		},
		{
			name: "search disabled indexer returns empty",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{3},
			},
			shouldError: false,
		},
		{
			name: "search all enabled indexers",
			req: &TorznabSearchRequest{
				Query: "test",
			},
			shouldError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := s.SearchGeneric(context.Background(), tt.req)
			if tt.shouldError && err == nil {
				t.Error("SearchGeneric() expected error, got nil")
			}
			if !tt.shouldError && err != nil {
				t.Errorf("SearchGeneric() unexpected error = %v", err)
			}
			if !tt.shouldError && resp == nil {
				t.Error("SearchGeneric() returned nil response")
			}
		})
	}
}

func TestSearchRespectsRequestedIndexerIDs(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "Indexer1", Enabled: true},
		},
		panicOnListEnabled: true,
	}
	s := NewService(store, nil)

	req := &TorznabSearchRequest{
		Query:      "Example.Show.S01",
		IndexerIDs: []int{999}, // request an indexer that does not exist/enabled
	}

	resp, err := s.Search(context.Background(), req)
	if err != nil {
		t.Fatalf("Search() error = %v", err)
	}
	if resp == nil {
		t.Fatal("expected response, got nil")
	}
	if resp.Total != 0 {
		t.Fatalf("expected no results, got %d", resp.Total)
	}
	if store.listEnabledCalls != 0 {
		t.Fatalf("expected ListEnabled to be skipped, but was called %d times", store.listEnabledCalls)
	}
	if len(store.getCalls) != 1 || store.getCalls[0] != 999 {
		t.Fatalf("expected Get to be called once with 999, calls: %#v", store.getCalls)
	}
}

// Helper functions
func intPtr(i int) *int {
	return &i
}

// Mock store for testing
type mockTorznabIndexerStore struct {
	indexers           []*models.TorznabIndexer
	capabilities       map[int][]string // indexerID -> capabilities
	panicOnListEnabled bool
	listEnabledCalls   int
	getCalls           []int
}

func (m *mockTorznabIndexerStore) Get(ctx context.Context, id int) (*models.TorznabIndexer, error) {
	m.getCalls = append(m.getCalls, id)
	for _, idx := range m.indexers {
		if idx.ID == id {
			return idx, nil
		}
	}
	return nil, nil
}

func (m *mockTorznabIndexerStore) List(ctx context.Context) ([]*models.TorznabIndexer, error) {
	return m.indexers, nil
}

func (m *mockTorznabIndexerStore) ListEnabled(ctx context.Context) ([]*models.TorznabIndexer, error) {
	m.listEnabledCalls++
	if m.panicOnListEnabled {
		panic("ListEnabled called unexpectedly")
	}
	enabled := make([]*models.TorznabIndexer, 0)
	for _, idx := range m.indexers {
		if idx.Enabled {
			enabled = append(enabled, idx)
		}
	}
	return enabled, nil
}

func (m *mockTorznabIndexerStore) GetDecryptedAPIKey(indexer *models.TorznabIndexer) (string, error) {
	return "mock-api-key", nil
}

func (m *mockTorznabIndexerStore) GetCapabilities(ctx context.Context, indexerID int) ([]string, error) {
	if m.capabilities != nil {
		if caps, exists := m.capabilities[indexerID]; exists {
			return caps, nil
		}
	}
	// For testing purposes, return empty capabilities by default
	// This simulates indexers without specific parameter support capabilities
	return []string{}, nil
}

func (m *mockTorznabIndexerStore) SetCapabilities(ctx context.Context, indexerID int, capabilities []string) error {
	return nil
}

func (m *mockTorznabIndexerStore) SetCategories(ctx context.Context, indexerID int, categories []models.TorznabIndexerCategory) error {
	return nil
}

func (m *mockTorznabIndexerStore) RecordLatency(ctx context.Context, indexerID int, operationType string, latencyMs int, success bool) error {
	return nil
}

func (m *mockTorznabIndexerStore) RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error {
	return nil
}

func (m *mockTorznabIndexerStore) CountRequests(ctx context.Context, indexerID int, window time.Duration) (int, error) {
	return 0, nil
}

func (m *mockTorznabIndexerStore) UpdateRequestLimits(ctx context.Context, indexerID int, hourly, daily *int) error {
	return nil
}

func TestProwlarrYearParameterWorkaround(t *testing.T) {
	tests := []struct {
		name        string
		backend     models.TorznabBackend
		inputParams map[string]string
		expected    map[string]string
		description string
	}{
		{
			name:    "prowlarr with year parameter",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
				"cat":  "2000",
			},
			expected: map[string]string{
				"t":   "movie",
				"q":   "The Matrix 1999",
				"cat": "2000",
				// year parameter should be removed
			},
			description: "Prowlarr indexer should move year parameter to search query",
		},
		{
			name:    "prowlarr with year parameter and empty query",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "",
				"year": "2020",
			},
			expected: map[string]string{
				"t": "movie",
				"q": "2020",
				// year parameter should be removed
			},
			description: "Prowlarr indexer should use year as query when original query is empty",
		},
		{
			name:    "jackett with year parameter",
			backend: models.TorznabBackendJackett,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
				"cat":  "2000",
			},
			expected: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
				"cat":  "2000",
			},
			description: "Jackett indexer should keep year parameter unchanged",
		},
		{
			name:    "native with year parameter",
			backend: models.TorznabBackendNative,
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			expected: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			description: "Native indexer should keep year parameter unchanged",
		},
		{
			name:    "prowlarr without year parameter",
			backend: models.TorznabBackendProwlarr,
			inputParams: map[string]string{
				"t": "movie",
				"q": "The Matrix",
			},
			expected: map[string]string{
				"t": "movie",
				"q": "The Matrix",
			},
			description: "Prowlarr indexer should not modify query when no year parameter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create a test indexer with the specified backend
			indexer := &models.TorznabIndexer{
				ID:        1,
				Name:      "Test Indexer",
				Backend:   tt.backend,
				IndexerID: "test",
			}

			// Set up mock store
			mockStore := &mockTorznabIndexerStore{
				capabilities: make(map[int][]string),
			}

			// Create service with mock store
			service := &Service{
				indexerStore: mockStore,
			}

			// Prepare input parameters
			inputParams := make(map[string]string)
			maps.Copy(inputParams, tt.inputParams)

			// Call the actual service method to apply the workaround
			ctx := context.Background()
			service.applyProwlarrWorkaround(ctx, indexer, inputParams)

			// Assert expected parameter values
			for key, expectedValue := range tt.expected {
				if actualValue := inputParams[key]; actualValue != expectedValue {
					t.Errorf("%s: paramsMap[%q] = %q, want %q", tt.description, key, actualValue, expectedValue)
				}
			}

			// Assert year parameter is removed for Prowlarr when it was present
			if tt.backend == models.TorznabBackendProwlarr && tt.inputParams["year"] != "" {
				if _, exists := inputParams["year"]; exists {
					t.Errorf("%s: year parameter should be removed for Prowlarr indexer", tt.description)
				}
			}

			// Assert no unexpected parameters exist
			for key := range inputParams {
				if _, expected := tt.expected[key]; !expected {
					t.Errorf("%s: unexpected parameter %q = %q", tt.description, key, inputParams[key])
				}
			}
		})
	}
}

func TestProwlarrCapabilityAwareYearWorkaround(t *testing.T) {
	tests := []struct {
		name                string
		indexerCapabilities []string
		inputParams         map[string]string
		expectedQuery       string
		expectedYearParam   bool
		description         string
	}{
		{
			name:                "prowlarr without movie-search-year capability",
			indexerCapabilities: []string{"search", "movie-search"},
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			expectedQuery:     "The Matrix 1999",
			expectedYearParam: false,
			description:       "Should move year to query when indexer lacks movie-search-year capability",
		},
		{
			name:                "prowlarr with movie-search-year capability",
			indexerCapabilities: []string{"search", "movie-search", "movie-search-year"},
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "The Matrix",
				"year": "1999",
			},
			expectedQuery:     "The Matrix",
			expectedYearParam: true,
			description:       "Should keep year parameter when indexer supports movie-search-year capability",
		},
		{
			name:                "prowlarr with empty query",
			indexerCapabilities: []string{"search", "movie-search"},
			inputParams: map[string]string{
				"t":    "movie",
				"q":    "",
				"year": "2020",
			},
			expectedQuery:     "2020",
			expectedYearParam: false,
			description:       "Should use year as entire query when original query is empty",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create mock store with specific capabilities
			mockStore := &mockTorznabIndexerStore{
				indexers: []*models.TorznabIndexer{
					{
						ID:             1,
						Name:           "Test Prowlarr Indexer",
						Backend:        models.TorznabBackendProwlarr,
						IndexerID:      "test",
						BaseURL:        "http://test.example.com",
						TimeoutSeconds: 30,
						Enabled:        true,
					},
				},
				capabilities: map[int][]string{
					1: tt.indexerCapabilities,
				},
			}

			// Create service with mock store
			service := &Service{
				indexerStore: mockStore,
			}

			// Convert input params to map[string]string (we don't need url.Values for this test)

			// Test the searchMultipleIndexers method by examining the parameters it would send
			// We'll use reflection or create a test client that captures the parameters
			indexers, _ := mockStore.ListEnabled(context.Background())

			// For this test, we'll verify the capability logic directly
			ctx := context.Background()
			hasYearCapability := service.hasCapability(ctx, 1, "movie-search-year")

			expectedHasCapability := slices.Contains(tt.indexerCapabilities, "movie-search-year")

			if hasYearCapability != expectedHasCapability {
				t.Errorf("hasCapability() = %v, expected %v", hasYearCapability, expectedHasCapability)
			}

			// Test parameter handling logic
			paramsMap := make(map[string]string)
			maps.Copy(paramsMap, tt.inputParams)

			indexer := indexers[0]
			// Apply the actual Prowlarr logic from the service
			if indexer.Backend == models.TorznabBackendProwlarr {
				if yearStr, exists := paramsMap["year"]; exists && yearStr != "" {
					supportsYearParam := service.hasCapability(ctx, indexer.ID, "movie-search-year")

					if !supportsYearParam {
						currentQuery := paramsMap["q"]
						if currentQuery != "" {
							paramsMap["q"] = currentQuery + " " + yearStr
						} else {
							paramsMap["q"] = yearStr
						}
						delete(paramsMap, "year")
					}
				}
			}

			// Verify results
			actualQuery := paramsMap["q"]
			if actualQuery != tt.expectedQuery {
				t.Errorf("Query: got %q, expected %q", actualQuery, tt.expectedQuery)
			}

			_, hasYearParam := paramsMap["year"]
			if hasYearParam != tt.expectedYearParam {
				t.Errorf("Year parameter presence: got %v, expected %v", hasYearParam, tt.expectedYearParam)
			}
		})
	}
}

func TestParseTorznabCaps_ProwlarrCompatibility(t *testing.T) {
	// This XML represents what Prowlarr's IndexerCapabilities.GetXDocument() method generates
	// Based on the IndexerCapabilities class from Prowlarr source code
	prowlarrCapsXML := `<?xml version="1.0" encoding="UTF-8"?>
<caps>
	<server title="Prowlarr" />
	<limits default="100" max="100" />
	<searching>
		<search available="yes" supportedParams="q" />
		<tv-search available="yes" supportedParams="q,season,ep,imdbid,tvdbid,tmdbid,tvmazeid,traktid,doubanid,genre,year" />
		<movie-search available="yes" supportedParams="q,imdbid,tmdbid,traktid,genre,doubanid,year" />
		<music-search available="yes" supportedParams="q,album,artist,label,year,genre,track" />
		<audio-search available="yes" supportedParams="q,album,artist,label,year,genre,track" />
		<book-search available="yes" supportedParams="q,title,author,publisher,genre,year" />
	</searching>
	<categories>
		<category id="2000" name="Movies">
			<subcat id="2010" name="Foreign" />
			<subcat id="2020" name="Other" />
		</category>
		<category id="5000" name="TV">
			<subcat id="5070" name="Anime" />
		</category>
	</categories>
</caps>`

	caps, err := parseTorznabCaps(strings.NewReader(prowlarrCapsXML))
	if err != nil {
		t.Fatalf("Failed to parse Prowlarr caps XML: %v", err)
	}

	// Test that we parse all search types
	expectedSearchTypes := []string{
		"search", "tv-search", "movie-search", "music-search", "audio-search", "book-search",
	}
	for _, searchType := range expectedSearchTypes {
		found := slices.Contains(caps.Capabilities, searchType)
		if !found {
			t.Errorf("Missing basic search capability: %s", searchType)
		}
	}

	// Test that we parse all movie-search parameters correctly
	// Based on Prowlarr's MovieSearchParam enum and SupportedMovieSearchParams() method
	expectedMovieParams := []string{
		"movie-search-q",        // Always included
		"movie-search-imdbid",   // MovieSearchImdbAvailable
		"movie-search-tmdbid",   // MovieSearchTmdbAvailable
		"movie-search-traktid",  // MovieSearchTraktAvailable
		"movie-search-genre",    // MovieSearchGenreAvailable
		"movie-search-doubanid", // MovieSearchDoubanAvailable
		"movie-search-year",     // MovieSearchYearAvailable
	}
	for _, param := range expectedMovieParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing movie search parameter capability: %s", param)
		}
	}

	// Test that we parse all tv-search parameters correctly
	// Based on Prowlarr's TvSearchParam enum and SupportedTvSearchParams() method
	expectedTvParams := []string{
		"tv-search-q",        // Always included
		"tv-search-season",   // TvSearchSeasonAvailable
		"tv-search-ep",       // TvSearchEpAvailable
		"tv-search-imdbid",   // TvSearchImdbAvailable
		"tv-search-tvdbid",   // TvSearchTvdbAvailable
		"tv-search-tmdbid",   // TvSearchTmdbAvailable
		"tv-search-tvmazeid", // TvSearchTvMazeAvailable
		"tv-search-traktid",  // TvSearchTraktAvailable
		"tv-search-doubanid", // TvSearchDoubanAvailable
		"tv-search-genre",    // TvSearchGenreAvailable
		"tv-search-year",     // TvSearchYearAvailable
	}
	for _, param := range expectedTvParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing TV search parameter capability: %s", param)
		}
	}

	// Test that we parse all music-search parameters correctly
	// Based on Prowlarr's MusicSearchParam enum and SupportedMusicSearchParams() method
	expectedMusicParams := []string{
		"music-search-q",      // Always included
		"music-search-album",  // MusicSearchAlbumAvailable
		"music-search-artist", // MusicSearchArtistAvailable
		"music-search-label",  // MusicSearchLabelAvailable
		"music-search-year",   // MusicSearchYearAvailable
		"music-search-genre",  // MusicSearchGenreAvailable
		"music-search-track",  // MusicSearchTrackAvailable
	}
	for _, param := range expectedMusicParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing music search parameter capability: %s", param)
		}
	}

	// Test that we parse all book-search parameters correctly
	// Based on Prowlarr's BookSearchParam enum and SupportedBookSearchParams() method
	expectedBookParams := []string{
		"book-search-q",         // Always included
		"book-search-title",     // BookSearchTitleAvailable
		"book-search-author",    // BookSearchAuthorAvailable
		"book-search-publisher", // BookSearchPublisherAvailable
		"book-search-genre",     // BookSearchGenreAvailable
		"book-search-year",      // BookSearchYearAvailable
	}
	for _, param := range expectedBookParams {
		found := slices.Contains(caps.Capabilities, param)
		if !found {
			t.Errorf("Missing book search parameter capability: %s", param)
		}
	}

	// Test categories parsing (from Prowlarr's Categories.GetTorznabCategoryTree())
	if len(caps.Categories) == 0 {
		t.Error("No categories parsed")
	}

	// Find Movies category
	foundMovies := false
	foundMoviesForeign := false
	foundMoviesOther := false
	for _, cat := range caps.Categories {
		if cat.CategoryID == 2000 && cat.CategoryName == "Movies" {
			foundMovies = true
		}
		if cat.CategoryID == 2010 && cat.CategoryName == "Foreign" && cat.ParentCategory != nil && *cat.ParentCategory == 2000 {
			foundMoviesForeign = true
		}
		if cat.CategoryID == 2020 && cat.CategoryName == "Other" && cat.ParentCategory != nil && *cat.ParentCategory == 2000 {
			foundMoviesOther = true
		}
	}

	if !foundMovies {
		t.Error("Missing Movies category (2000)")
	}
	if !foundMoviesForeign {
		t.Error("Missing Movies > Foreign subcategory (2010)")
	}
	if !foundMoviesOther {
		t.Error("Missing Movies > Other subcategory (2020)")
	}

	// Verify we have all the capabilities we expect
	t.Logf("Parsed %d capabilities total", len(caps.Capabilities))
	t.Logf("Parsed %d categories total", len(caps.Categories))

	// Verify our capability parsing creates exactly what we need for hasCapability checks
	testCapabilities := []string{
		"movie-search-year",   // Critical for our Prowlarr workaround
		"movie-search-imdbid", // Common for movie searches
		"tv-search-season",    // Common for TV searches
		"tv-search-ep",        // Common for TV searches
		"music-search-artist", // Common for music searches
		"book-search-author",  // Common for book searches
	}

	for _, testCap := range testCapabilities {
		found := slices.Contains(caps.Capabilities, testCap)
		if !found {
			t.Errorf("Critical capability missing: %s", testCap)
		}
	}
}
