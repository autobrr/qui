package jackett

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
)

func TestSearchReturnsPartialWhenOnlyLiveIndexerFailsWithEmptyCachedCoverage(t *testing.T) {
	store := &mockTorznabIndexerStore{
		indexers: []*models.TorznabIndexer{
			{ID: 1, Name: "First", Enabled: true},
			{ID: 2, Name: "Second", Enabled: true},
			{ID: 3, Name: "Third", Enabled: true},
		},
	}
	s := NewService(store)
	s.searchCacheEnabled = true
	s.searchCacheTTL = time.Hour

	emptyPayload, err := json.Marshal(&SearchResponse{Results: []SearchResult{}, Total: 0})
	if err != nil {
		t.Fatalf("failed to marshal cache payload: %v", err)
	}

	s.searchCache = &fakeSearchCache{
		fetchFn: func(context.Context, string) (*models.TorznabSearchCacheEntry, bool, error) {
			return &models.TorznabSearchCacheEntry{
				ID:                 1,
				CacheKey:           "cache-key",
				Scope:              searchCacheScopeCrossSeed,
				Query:              "some show s01e01 1080p",
				IndexerIDs:         []int{1, 2},
				RequestFingerprint: "fp",
				ResponseData:       emptyPayload,
				TotalResults:       0,
				CachedAt:           time.Now().Add(-10 * time.Minute),
				LastUsedAt:         time.Now().Add(-10 * time.Minute),
				ExpiresAt:          time.Now().Add(50 * time.Minute),
			}, true, nil
		},
	}

	var searchedIndexers []int
	s.searchExecutor = func(_ context.Context, indexers []*models.TorznabIndexer, _ url.Values, _ *searchContext) ([]Result, []int, error) {
		for _, idx := range indexers {
			searchedIndexers = append(searchedIndexers, idx.ID)
		}
		return nil, nil, errors.New("tracker down")
	}

	req := &TorznabSearchRequest{Query: "some show s01e01 1080p"}
	respCh := make(chan *SearchResponse, 1)
	errCh := make(chan error, 1)
	req.OnAllComplete = func(resp *SearchResponse, err error) {
		if err != nil {
			errCh <- err
		} else {
			respCh <- resp
		}
	}

	if searchErr := s.Search(context.Background(), req); searchErr != nil {
		t.Fatalf("Search returned error: %v", searchErr)
	}

	var resp *SearchResponse
	select {
	case resp = <-respCh:
	case err := <-errCh:
		t.Fatalf("search across cached-covered indexers failed entirely because the single live indexer failed: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("search timed out")
	}

	if len(searchedIndexers) != 1 || searchedIndexers[0] != 3 {
		t.Fatalf("expected only uncovered indexer 3 to be live-searched, got %v", searchedIndexers)
	}
	if resp == nil {
		t.Fatal("expected response")
	}
	if !resp.Partial {
		t.Fatalf("expected partial response when live indexer failed, got %+v", resp)
	}
	if len(resp.Results) != 0 || resp.Total != 0 {
		t.Fatalf("expected empty results from cached coverage, got %+v", resp.Results)
	}
	if resp.Cache == nil || resp.Cache.Source != searchCacheSourceCache {
		t.Fatalf("expected cache metadata on fallback response, got %+v", resp.Cache)
	}
}
