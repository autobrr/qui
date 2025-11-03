// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestDetectContentType(t *testing.T) {
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
			name: "returns unknown for ambiguous query",
			req: &TorznabSearchRequest{
				Query: "random search",
			},
			expected: contentTypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectContentType(tt.req)
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
		name     string
		req      *TorznabSearchRequest
		expected map[string]string
	}{
		{
			name: "basic query",
			req: &TorznabSearchRequest{
				Query: "test movie",
			},
			expected: map[string]string{
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
			expected: map[string]string{
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
			expected: map[string]string{
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
				"q":      "test",
				"limit":  "100",
				"offset": "50",
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
			expected: map[string]string{
				"q":      "Breaking Bad",
				"cat":    "5000",
				"tvdbid": "81189",
				"season": "1",
				"ep":     "1",
				"limit":  "50",
				"offset": "10",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.buildSearchParams(tt.req)
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
	s := NewService(store)

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
			if err != nil && err.Error() != "query is required" {
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

func TestSearchWithLimit(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{},
	}
	s := NewService(store)

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
	s := NewService(store)

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
			shouldError: false,
		},
		{
			name: "search multiple indexers",
			req: &TorznabSearchRequest{
				Query:      "test",
				IndexerIDs: []int{1, 2},
			},
			shouldError: false,
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
			shouldError: false,
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

// Helper functions
func intPtr(i int) *int {
	return &i
}

// Mock store for testing
type mockTorznabIndexerStore struct {
	indexers []*models.TorznabIndexer
}

func (m *mockTorznabIndexerStore) Get(ctx context.Context, id int) (*models.TorznabIndexer, error) {
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
