// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// Synthetic fixture per repo rule: yearless one-word title with a group tag
// ending in digits, so ARR's parse extracts a bogus year from the group and
// resolves nothing (issue #2272). The service test asserts the lookup term is
// exactly "Solitude", which fails loudly if rls stops parsing it that way.
const yearlessDigitGroupRelease = "Solitude.1080p.AMZN.WEB-DL.DDP5.1.H.264-QX2006.mkv"

func TestClient_LookupByTerm_Radarr(t *testing.T) {
	tests := []struct {
		name         string
		term         string
		year         int
		responseCode int
		responseBody string
		wantTMDbID   int
		wantNil      bool
		wantErr      bool
	}{
		{
			name:         "exact match returns IDs",
			term:         "Solitude",
			responseCode: http.StatusOK,
			responseBody: `[{"title": "Solitude", "tmdbId": 1234, "imdbId": "tt1234412"}]`,
			wantTMDbID:   1234,
		},
		{
			name:         "in-library match preferred over first match",
			term:         "Solitude",
			responseCode: http.StatusOK,
			responseBody: `[{"title": "Solitude", "tmdbId": 1111}, {"id": 42, "title": "Solitude", "tmdbId": 2222}]`,
			wantTMDbID:   2222,
		},
		{
			name:         "year disambiguates same-title remakes",
			term:         "Solitude",
			year:         1993,
			responseCode: http.StatusOK,
			responseBody: `[{"id": 42, "title": "Solitude", "year": 2019, "tmdbId": 9999}, {"title": "Solitude", "year": 1993, "tmdbId": 1234}]`,
			wantTMDbID:   1234,
		},
		{
			name:         "year mismatch refuses even in-library candidate",
			term:         "Solitude",
			year:         1993,
			responseCode: http.StatusOK,
			responseBody: `[{"id": 42, "title": "Solitude", "year": 2019, "tmdbId": 9999}]`,
			wantNil:      true,
		},
		{
			name:         "ambiguous titles without year refuse",
			term:         "Solitude",
			responseCode: http.StatusOK,
			responseBody: `[{"title": "Solitude", "year": 2019, "tmdbId": 9999}, {"title": "Solitude", "year": 1993, "tmdbId": 1234}]`,
			wantNil:      true,
		},
		{
			name:         "ambiguous in-library titles without year refuse",
			term:         "Solitude",
			responseCode: http.StatusOK,
			responseBody: `[{"id": 42, "title": "Solitude", "year": 2019, "tmdbId": 9999}, {"id": 43, "title": "Solitude", "year": 1993, "tmdbId": 1234}]`,
			wantNil:      true,
		},
		{
			name:         "in-library ambiguity resolved by year",
			term:         "Solitude",
			year:         1993,
			responseCode: http.StatusOK,
			responseBody: `[{"id": 42, "title": "Solitude", "year": 2019, "tmdbId": 9999}, {"id": 43, "title": "Solitude", "year": 1993, "tmdbId": 1234}]`,
			wantTMDbID:   1234,
		},
		{
			name:         "exact year beats adjacent-year in-library candidate",
			term:         "Solitude",
			year:         2024,
			responseCode: http.StatusOK,
			responseBody: `[{"id": 42, "title": "Solitude", "year": 2023, "tmdbId": 9999}, {"title": "Solitude", "year": 2024, "tmdbId": 1234}]`,
			wantTMDbID:   1234,
		},
		{
			name:         "normalized title match",
			term:         "solitude",
			responseCode: http.StatusOK,
			responseBody: `[{"title": "Solitude!", "tmdbId": 1234}]`,
			wantTMDbID:   1234,
		},
		{
			name:         "original title match",
			term:         "La Soledad",
			responseCode: http.StatusOK,
			responseBody: `[{"title": "The Loneliness", "originalTitle": "La Soledad", "tmdbId": 4321}]`,
			wantTMDbID:   4321,
		},
		{
			name:         "no candidate matches term",
			term:         "Solitude",
			responseCode: http.StatusOK,
			responseBody: `[{"title": "Solitude Standing", "tmdbId": 9999}, {"title": "Alone in Solitude", "tmdbId": 8888}]`,
			wantNil:      true,
		},
		{
			name:         "empty result set",
			term:         "Solitude",
			responseCode: http.StatusOK,
			responseBody: `[]`,
			wantNil:      true,
		},
		{
			name:         "server error",
			term:         "Solitude",
			responseCode: http.StatusInternalServerError,
			responseBody: `boom`,
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v3/movie/lookup", r.URL.Path)
				assert.Equal(t, tt.term, r.URL.Query().Get("term"))
				w.WriteHeader(tt.responseCode)
				_, _ = w.Write([]byte(tt.responseBody))
			}))
			defer server.Close()

			client := NewClient(server.URL, "test-api-key", nil, nil, models.ArrInstanceTypeRadarr, 15)
			result, err := client.LookupByTerm(context.Background(), tt.term, tt.year)

			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			if tt.wantNil {
				require.Nil(t, result)
				return
			}
			require.NotNil(t, result)
			require.NotNil(t, result.IDs)
			assert.Equal(t, tt.wantTMDbID, result.IDs.TMDbID)
		})
	}
}

func TestClient_LookupByTerm_Sonarr(t *testing.T) {
	t.Run("match not in library skips hydration", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			assert.Equal(t, "/api/v3/series/lookup", r.URL.Path)
			assert.Equal(t, "Solitude", r.URL.Query().Get("term"))
			_, _ = w.Write([]byte(`[{"title": "Solitude", "tvdbId": 471000}]`))
		}))
		defer server.Close()

		client := NewClient(server.URL, "test-api-key", nil, nil, models.ArrInstanceTypeSonarr, 15)
		result, err := client.LookupByTerm(context.Background(), "Solitude", 0)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.IDs)
		assert.Equal(t, 471000, result.IDs.TVDbID)
	})

	t.Run("in-library match hydrates alternate titles", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v3/series/lookup":
				_, _ = w.Write([]byte(`[{"id": 42, "title": "Solitude", "tvdbId": 471000}]`))
			case "/api/v3/series/42":
				_, _ = w.Write([]byte(`{"id": 42, "title": "Solitude", "tvdbId": 471000, "alternateTitles": [
					{"title": "Kodoku no Solitude"}
				]}`))
			default:
				t.Errorf("unexpected ARR request: %s", r.URL.Path)
				http.NotFound(w, r)
			}
		}))
		defer server.Close()

		client := NewClient(server.URL, "test-api-key", nil, nil, models.ArrInstanceTypeSonarr, 15)
		result, err := client.LookupByTerm(context.Background(), "Solitude", 0)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.IDs)
		assert.Equal(t, 471000, result.IDs.TVDbID)
		assert.Contains(t, result.Titles, "Kodoku no Solitude")
	})

	t.Run("ambiguous in-library titles without year refuse", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v3/series/lookup" {
				t.Errorf("unexpected ARR request: %s", r.URL.Path)
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`[{"id": 42, "title": "Solitude", "tvdbId": 471000}, {"id": 43, "title": "Solitude", "tvdbId": 471001}]`))
		}))
		defer server.Close()

		client := NewClient(server.URL, "test-api-key", nil, nil, models.ArrInstanceTypeSonarr, 15)
		result, err := client.LookupByTerm(context.Background(), "Solitude", 0)

		require.NoError(t, err)
		require.Nil(t, result)
	})

	t.Run("exact year beats adjacent-year in-library candidate", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/v3/series/lookup" {
				t.Errorf("unexpected ARR request: %s", r.URL.Path)
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write([]byte(`[{"id": 42, "title": "Solitude", "year": 2023, "tvdbId": 9999}, {"title": "Solitude", "year": 2024, "tvdbId": 471000}]`))
		}))
		defer server.Close()

		client := NewClient(server.URL, "test-api-key", nil, nil, models.ArrInstanceTypeSonarr, 15)
		result, err := client.LookupByTerm(context.Background(), "Solitude", 2024)

		require.NoError(t, err)
		require.NotNil(t, result)
		require.NotNil(t, result.IDs)
		assert.Equal(t, 471000, result.IDs.TVDbID)
	})

	t.Run("no match returns nil", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte(`[{"title": "Solitude Standing", "tvdbId": 9999}]`))
		}))
		defer server.Close()

		client := NewClient(server.URL, "test-api-key", nil, nil, models.ArrInstanceTypeSonarr, 15)
		result, err := client.LookupByTerm(context.Background(), "Solitude", 0)

		require.NoError(t, err)
		require.Nil(t, result)
	})
}

func TestClient_LookupByTerm_UnsupportedType(t *testing.T) {
	client := NewClient("http://localhost:1", "test-api-key", nil, nil, models.ArrInstanceType("lidarr"), 15)
	_, err := client.LookupByTerm(context.Background(), "Solitude", 0)
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported instance type")
}

func TestService_LookupExternalIDsFallsBackToTitleLookup(t *testing.T) {
	lookupCalled := false
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			// The issue #2272 mis-parse: year lifted from the digit-suffixed
			// group tag, no movie resolved, no IDs.
			_, _ = w.Write([]byte(`{"movie": null, "parsedMovieInfo": {"movieTitle": "Solitude  AMZN WEB-DL DDP5 1 -QX", "year": 2006}}`))
		case "/api/v3/movie/lookup":
			lookupCalled = true
			assert.Equal(t, "Solitude", r.URL.Query().Get("term"))
			_, _ = w.Write([]byte(`[{"title": "Solitude", "tmdbId": 1234, "imdbId": "tt1234412"}]`))
		default:
			t.Errorf("unexpected ARR request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	ctx := context.Background()
	result, err := service.LookupExternalIDs(ctx, yearlessDigitGroupRelease, ContentTypeMovie)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, lookupCalled)
	require.Equal(t, "lookup", result.Source)
	require.Equal(t, &models.ExternalIDs{TMDbID: 1234, IMDbID: "tt1234412"}, result.IDs)

	cacheEntry, err := cacheStore.Get(ctx, models.ComputeTitleHash(yearlessDigitGroupRelease), string(ContentTypeMovie))
	require.NoError(t, err)
	require.False(t, cacheEntry.IsNegative)
	require.Equal(t, *result.IDs, cacheEntry.ExternalIDs)
}

func TestService_LookupExternalIDsFallsBackWhenParseFails(t *testing.T) {
	service, _ := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			http.Error(w, "boom", http.StatusInternalServerError)
		case "/api/v3/movie/lookup":
			_, _ = w.Write([]byte(`[{"title": "Solitude", "tmdbId": 1234}]`))
		default:
			t.Errorf("unexpected ARR request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	result, err := service.LookupExternalIDs(context.Background(), yearlessDigitGroupRelease, ContentTypeMovie)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "lookup", result.Source)
	require.Equal(t, &models.ExternalIDs{TMDbID: 1234}, result.IDs)
}

func TestService_LookupExternalIDsSkipsNegativeCacheWhenBothLegsFail(t *testing.T) {
	// A transient ARR outage (parse AND lookup erroring) must surface as an
	// error — not a look-alike "no IDs" result — and must not write a negative
	// cache entry that suppresses ID lookups after the instance recovers.
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))

	ctx := context.Background()
	result, err := service.LookupExternalIDs(ctx, yearlessDigitGroupRelease, ContentTypeMovie)

	require.Error(t, err)
	require.Nil(t, result)

	_, err = cacheStore.Get(ctx, models.ComputeTitleHash(yearlessDigitGroupRelease), string(ContentTypeMovie))
	require.Error(t, err, "no cache entry may be written when no endpoint answered")
}

func TestService_LookupExternalIDsNoNegativeCacheWhenLookupErrors(t *testing.T) {
	// Parse answering empty is exactly the state the title-lookup fallback
	// distrusts, so an erroring fallback leaves the cycle inconclusive: no
	// negative cache, and the failure is surfaced as an error.
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"movie": null}`))
		case "/api/v3/movie/lookup":
			http.Error(w, "boom", http.StatusInternalServerError)
		default:
			t.Errorf("unexpected ARR request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	ctx := context.Background()
	result, err := service.LookupExternalIDs(ctx, yearlessDigitGroupRelease, ContentTypeMovie)

	require.Error(t, err)
	require.Nil(t, result)

	_, err = cacheStore.Get(ctx, models.ComputeTitleHash(yearlessDigitGroupRelease), string(ContentTypeMovie))
	require.Error(t, err, "an inconclusive lookup cycle must not negative-cache")
}

func TestService_LookupExternalIDsCachesNegativeWhenLookupEmpty(t *testing.T) {
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"movie": null}`))
		case "/api/v3/movie/lookup":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Errorf("unexpected ARR request: %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))

	ctx := context.Background()
	result, err := service.LookupExternalIDs(ctx, yearlessDigitGroupRelease, ContentTypeMovie)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Nil(t, result.IDs)

	cacheEntry, err := cacheStore.Get(ctx, models.ComputeTitleHash(yearlessDigitGroupRelease), string(ContentTypeMovie))
	require.NoError(t, err)
	require.True(t, cacheEntry.IsNegative)
}
