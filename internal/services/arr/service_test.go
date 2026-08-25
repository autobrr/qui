// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"context"
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"

	"github.com/autobrr/qui/internal/dbinterface"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type testQuerier struct {
	db *sql.DB
}

func (q *testQuerier) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.db.QueryRowContext(ctx, query, args...)
}

func (q *testQuerier) ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return q.db.ExecContext(ctx, query, args...)
}

func (q *testQuerier) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.db.QueryContext(ctx, query, args...)
}

func (q *testQuerier) BeginTx(ctx context.Context, opts *sql.TxOptions) (dbinterface.TxQuerier, error) {
	return q.db.BeginTx(ctx, opts)
}

func openTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	db.SetMaxOpenConns(1)

	_, err = db.ExecContext(t.Context(), `
		CREATE TABLE arr_id_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title_hash TEXT NOT NULL,
			content_type TEXT NOT NULL CHECK(content_type IN ('movie', 'tv', 'anime', 'unknown')),
			arr_instance_id INTEGER,
			imdb_id TEXT,
			tmdb_id INTEGER,
			tvdb_id INTEGER,
			tvmaze_id INTEGER,
			titles_json TEXT,
			is_negative BOOLEAN DEFAULT 0,
			cached_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			expires_at TIMESTAMP NOT NULL,
			UNIQUE(title_hash, content_type)
		)
	`)
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db
}

func TestService_getArrTypeForContent(t *testing.T) {
	// Create a minimal service for testing internal method
	s := &Service{}

	tests := []struct {
		name        string
		contentType ContentType
		want        models.ArrInstanceType
	}{
		{
			name:        "movie maps to radarr",
			contentType: ContentTypeMovie,
			want:        models.ArrInstanceTypeRadarr,
		},
		{
			name:        "tv maps to sonarr",
			contentType: ContentTypeTV,
			want:        models.ArrInstanceTypeSonarr,
		},
		{
			name:        "anime maps to sonarr",
			contentType: ContentTypeAnime,
			want:        models.ArrInstanceTypeSonarr,
		},
		{
			name:        "unknown returns empty",
			contentType: ContentTypeUnknown,
			want:        "",
		},
		{
			name:        "empty string returns empty",
			contentType: "",
			want:        "",
		},
		{
			name:        "invalid content type returns empty",
			contentType: ContentType("invalid"),
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.getArrTypeForContent(tt.contentType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestService_LookupExternalIDsReturnsCacheCancellation(t *testing.T) {
	tests := []struct {
		name    string
		context func(t *testing.T) context.Context
		wantErr error
	}{
		{
			name: "context canceled",
			context: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
			wantErr: context.Canceled,
		},
		{
			name: "deadline exceeded",
			context: func(t *testing.T) context.Context {
				t.Helper()
				ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
				t.Cleanup(cancel)
				return ctx
			},
			wantErr: context.DeadlineExceeded,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cacheStore := models.NewArrIDCacheStore(&testQuerier{db: openTestDB(t)})
			s := &Service{
				cacheStore:       cacheStore,
				nextCacheCleanup: time.Now().Add(time.Hour),
			}

			result, err := s.LookupExternalIDs(tt.context(t), "Example Movie", ContentTypeMovie)

			require.Error(t, err)
			require.ErrorIs(t, err, tt.wantErr)
			assert.Nil(t, result)
		})
	}
}

func TestService_LookupExternalIDsUsesNegativeCache(t *testing.T) {
	ctx := context.Background()
	title := "Breaking Bad S01E01"

	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		t.Fatalf("unexpected ARR request after negative cache hit: %s", r.URL.Path)
	}))

	titleHash := models.ComputeTitleHash(title)
	require.NoError(t, cacheStore.Set(ctx, titleHash, string(ContentTypeTV), nil, nil, true, time.Hour))

	result, err := service.LookupExternalIDs(ctx, title, ContentTypeTV)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.FromCache)
	require.Equal(t, "cache", result.Source)
	require.Nil(t, result.IDs)
}

func TestService_LookupExternalIDsKeepsLegacyPositiveCacheWhenAliasHydrationMisses(t *testing.T) {
	ctx := context.Background()
	title := "Haibara-kun no Tsuyokute Seishun New Game S01E01"
	legacyIDs := &models.ExternalIDs{
		TVDbID: 471000,
		TMDbID: 316424,
		IMDbID: "tt39122622",
	}

	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"series": null}`))
		default:
			http.NotFound(w, r)
		}
	}))

	titleHash := models.ComputeTitleHash(title)
	require.NoError(t, cacheStore.Set(ctx, titleHash, string(ContentTypeTV), nil, legacyIDs, false, time.Hour))

	result, err := service.LookupExternalIDs(ctx, title, ContentTypeTV)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.True(t, result.FromCache)
	require.Equal(t, "cache", result.Source)
	require.False(t, result.TitlesKnown)
	require.Empty(t, result.Titles)
	require.Equal(t, legacyIDs, result.IDs)

	cacheEntry, err := cacheStore.Get(ctx, titleHash, string(ContentTypeTV))
	require.NoError(t, err)
	require.False(t, cacheEntry.IsNegative)
	require.False(t, cacheEntry.HasTitles)
	require.Equal(t, *legacyIDs, cacheEntry.ExternalIDs)
}

func TestService_LookupExternalIDsHydratesLegacyPositiveCacheTitles(t *testing.T) {
	ctx := context.Background()
	title := "Haibara-kun no Tsuyokute Seishun New Game S01E01"
	legacyIDs := &models.ExternalIDs{
		TVDbID: 471000,
		TMDbID: 316424,
		IMDbID: "tt39122622",
	}

	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{
				"series": {
					"title": "Haibara's Teenage New Game+",
					"alternateTitles": [
						{"title": "Haibara-kun no Tsuyokute Seishun New Game"}
					],
					"tvdbId": 471000,
					"tmdbId": 316424,
					"imdbId": "tt39122622"
				}
			}`))
		default:
			http.NotFound(w, r)
		}
	}))

	titleHash := models.ComputeTitleHash(title)
	require.NoError(t, cacheStore.Set(ctx, titleHash, string(ContentTypeTV), nil, legacyIDs, false, time.Hour))

	result, err := service.LookupExternalIDs(ctx, title, ContentTypeTV)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.False(t, result.FromCache)
	require.Equal(t, "parse", result.Source)
	require.True(t, result.TitlesKnown)
	require.Equal(t, legacyIDs, result.IDs)
	require.Equal(t, []string{
		"Haibara's Teenage New Game+",
		"Haibara-kun no Tsuyokute Seishun New Game",
	}, result.Titles)

	cacheEntry, err := cacheStore.Get(ctx, titleHash, string(ContentTypeTV))
	require.NoError(t, err)
	require.False(t, cacheEntry.IsNegative)
	require.True(t, cacheEntry.HasTitles)
	require.Equal(t, result.Titles, cacheEntry.Titles)
	require.Equal(t, *legacyIDs, cacheEntry.ExternalIDs)
}

func TestService_LookupExternalIDsUsesParseOnly(t *testing.T) {
	service, _ := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"movie": {"tmdbId": 27205, "imdbId": "tt1375666"}}`))
		default:
			t.Fatalf("unexpected ARR request: %s", r.URL.Path)
		}
	}))

	result, err := service.LookupExternalIDs(context.Background(), "Inception", ContentTypeMovie)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "parse", result.Source)
	require.Equal(t, &models.ExternalIDs{TMDbID: 27205, IMDbID: "tt1375666"}, result.IDs)
}

func newArrLookupTestService(t *testing.T, instanceType models.ArrInstanceType, handler http.Handler) (*Service, *models.ArrIDCacheStore) {
	t.Helper()

	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, "arr-service")

	key := []byte("01234567890123456789012345678901")
	instanceStore, err := models.NewArrInstanceStore(db, key)
	require.NoError(t, err)
	_, err = instanceStore.Create(context.Background(), instanceType, "Test ARR", server.URL, "api-key", nil, nil, true, 1, 15)
	require.NoError(t, err)

	cacheStore := models.NewArrIDCacheStore(db)
	service := NewService(instanceStore, cacheStore)
	service.nextCacheCleanup = time.Now().Add(time.Hour)

	return service, cacheStore
}

func TestService_LookupSeasonEpisodeTotalReturnsSeasonTitles(t *testing.T) {
	// Pins the wiring at the return site: season-pack alias matching consumes
	// result.Titles, so dropping the titlesForSeason call would silently kill the
	// feature while every other test stays green.
	//
	// The mock mirrors real Sonarr: /parse embeds the series WITHOUT alternateTitles
	// (only SeriesController populates them), so the service must hydrate via
	// /series/{id} to see aliases at all.
	service, _ := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"series": {"id": 7, "title": "Oshi no Ko"}}`))
		case "/api/v3/series/7":
			_, _ = w.Write([]byte(`{"id": 7, "title": "Oshi no Ko", "alternateTitles": [
				{"title": "Oshi no Ko Romaji"},
				{"title": "Oshi no Ko 2nd Season", "seasonNumber": 2},
				{"title": "Oshi no Ko 3rd Season", "seasonNumber": 3}
			]}`))
		case "/api/v3/episode":
			_, _ = w.Write([]byte(`[
				{"id": 1, "seasonNumber": 2, "episodeNumber": 1},
				{"id": 2, "seasonNumber": 2, "episodeNumber": 2}
			]`))
		default:
			t.Fatalf("unexpected ARR request: %s", r.URL.Path)
		}
	}))

	result, err := service.LookupSeasonEpisodeTotal(context.Background(), "Oshi.no.Ko.S02.1080p.WEB.x264-GRP", 2)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.TotalEpisodes)
	require.Contains(t, result.Titles, "Oshi no Ko")
	require.Contains(t, result.Titles, "Oshi no Ko Romaji")
	require.Contains(t, result.Titles, "Oshi no Ko 2nd Season", "same-season alias must ride along")
	require.NotContains(t, result.Titles, "Oshi no Ko 3rd Season", "other-season alias must be filtered")
}

func TestService_LookupSeasonEpisodeTotalKeepsTitlesWhenSeasonHasNoEpisodes(t *testing.T) {
	// Anime is often stored in Sonarr as one absolute-numbered season, so the requested
	// season has no episode rows. The aliases must survive as a partial result so the
	// caller's metadata-total fallback can still alias-match; dropping them killed alias
	// matching in exactly the degraded path it was built for.
	service, _ := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"series": {"id": 7, "title": "Oshi no Ko"}}`))
		case "/api/v3/series/7":
			_, _ = w.Write([]byte(`{"id": 7, "title": "Oshi no Ko", "alternateTitles": [
				{"title": "Oshi no Ko Romaji"}
			]}`))
		case "/api/v3/episode":
			_, _ = w.Write([]byte(`[]`))
		default:
			t.Fatalf("unexpected ARR request: %s", r.URL.Path)
		}
	}))

	result, err := service.LookupSeasonEpisodeTotal(context.Background(), "Oshi.no.Ko.S02.1080p.WEB.x264-GRP", 2)

	require.NoError(t, err)
	require.NotNil(t, result, "aliases must survive an empty season episode list")
	require.Equal(t, 0, result.TotalEpisodes)
	require.Contains(t, result.Titles, "Oshi no Ko")
	require.Contains(t, result.Titles, "Oshi no Ko Romaji")
}

func TestService_LookupSeasonEpisodeTotalSurvivesHydrationFailure(t *testing.T) {
	// If /series/{id} fails, the lookup must still return the episode total with the
	// canonical title instead of erroring out or dropping the result.
	service, _ := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v3/parse":
			_, _ = w.Write([]byte(`{"series": {"id": 7, "title": "Oshi no Ko"}}`))
		case "/api/v3/series/7":
			w.WriteHeader(http.StatusInternalServerError)
		case "/api/v3/episode":
			_, _ = w.Write([]byte(`[
				{"id": 1, "seasonNumber": 2, "episodeNumber": 1},
				{"id": 2, "seasonNumber": 2, "episodeNumber": 2}
			]`))
		default:
			t.Fatalf("unexpected ARR request: %s", r.URL.Path)
		}
	}))

	result, err := service.LookupSeasonEpisodeTotal(context.Background(), "Oshi.no.Ko.S02.1080p.WEB.x264-GRP", 2)

	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, 2, result.TotalEpisodes)
	require.Equal(t, []string{"Oshi no Ko"}, result.Titles)
}

func TestSeasonLookupTitles_SuppressesExpansionForOtherSeasonAliasPacks(t *testing.T) {
	// A pack titled by an alias that Sonarr maps to a different season (e.g.
	// "Show 2nd Season" labeled S01) numbers its episodes alias-locally. Expanding
	// to the canonical title would bridge canonical wrong-season locals onto it, so
	// alias expansion must be suppressed entirely for that lookup.
	season := func(i int) *int { return &i }
	series := &SonarrSeries{
		Title: "Oshi no Ko",
		AlternateTitles: []AlternateTitle{
			{Title: "Oshi no Ko Romaji"},
			{Title: "Oshi no Ko 2nd Season", SeasonNumber: season(2), SceneSeasonNumber: season(1)},
			{Title: "Oshi no Ko Kai", SceneSeasonNumber: season(2)},
			// XEM scene mappings often carry the bare canonical title per season. It
			// prefixes every pack of the show, so treating it as an alias-titled pack
			// would suppress alias expansion for all canonically-titled packs.
			{Title: "Oshi no Ko", SceneSeasonNumber: season(1)},
		},
	}

	// Alias-titled pack labeled S01, but the alias maps to Sonarr season 2: no expansion.
	// SceneSeasonNumber is 1 here, so treating scene equality as alignment (matchesSeason)
	// would wrongly let this pack through; this case pins that it must not.
	require.Nil(t, seasonLookupTitles(series, "Oshi.no.Ko.2nd.Season.S01.1080p.WEB.x264-GRP", 1))

	// Alias scoped only by scene season: its Sonarr season is unknown, so a pack titled
	// by it must not expand either, regardless of the labeled season.
	require.Nil(t, seasonLookupTitles(series, "Oshi.no.Ko.Kai.S01.1080p.WEB.x264-GRP", 1))
	require.Nil(t, seasonLookupTitles(series, "Oshi.no.Ko.Kai.S02.1080p.WEB.x264-GRP", 2))

	// Same pack labeled with the alias's own Sonarr season: expansion is safe.
	s2 := seasonLookupTitles(series, "Oshi.no.Ko.2nd.Season.S02.1080p.WEB.x264-GRP", 2)
	require.Contains(t, s2, "Oshi no Ko")
	require.Contains(t, s2, "Oshi no Ko 2nd Season")

	// Canonical-titled pack is unaffected by the guard.
	s1 := seasonLookupTitles(series, "Oshi.no.Ko.S01.1080p.WEB.x264-GRP", 1)
	require.Contains(t, s1, "Oshi no Ko")
	require.Contains(t, s1, "Oshi no Ko Romaji")
}

func TestTitlesForSeason_KeepsSeriesWideAndSameSeasonOnly(t *testing.T) {
	season := func(i int) *int { return &i }
	series := &SonarrSeries{
		Title: "Oshi no Ko",
		AlternateTitles: []AlternateTitle{
			{Title: "Oshi no Ko Romaji"},                                 // series-wide (season absent)
			{Title: "Oshi no Ko Wide", SeasonNumber: season(-1)},         // series-wide (-1)
			{Title: "Oshi no Ko 2nd Season", SeasonNumber: season(2)},    // scoped to season 2
			{Title: "Oshi no Ko Scene S3", SceneSeasonNumber: season(3)}, // scene-scoped only: Sonarr season unknown
			// The real Sonarr scene-mapping shape: SeasonNumber is the Sonarr season,
			// SceneSeasonNumber the release-label season. Scene equality must NOT count
			// as season alignment, or this season-2 alias bridges season-2 episodes
			// onto every season-1 pack.
			{Title: "Oshi no Ko Scene Mapped", SeasonNumber: season(2), SceneSeasonNumber: season(1)},
		},
	}

	// Season 2: keep series-wide + the season-2 aliases, drop the rest.
	s2 := titlesForSeason(series, 2)
	require.Contains(t, s2, "Oshi no Ko")
	require.Contains(t, s2, "Oshi no Ko Romaji")
	require.Contains(t, s2, "Oshi no Ko Wide")
	require.Contains(t, s2, "Oshi no Ko 2nd Season", "same-season alias must be kept (bridges 2nd-season locals)")
	require.Contains(t, s2, "Oshi no Ko Scene Mapped", "Sonarr-season-2 alias must be kept for season 2")
	require.NotContains(t, s2, "Oshi no Ko Scene S3", "a different season's alias must be dropped")

	// Season 1: no season-scoped alias applies. In particular the scene-mapped alias
	// (sceneSeasonNumber 1) must not leak in via scene equality.
	s1 := titlesForSeason(series, 1)
	require.Contains(t, s1, "Oshi no Ko Romaji")
	require.NotContains(t, s1, "Oshi no Ko 2nd Season", "season-2 alias must not bridge into a season-1 pack")
	require.NotContains(t, s1, "Oshi no Ko Scene Mapped", "sceneSeasonNumber must not count as season alignment")
	require.NotContains(t, s1, "Oshi no Ko Scene S3")

	// A scene-only-scoped alias attaches to no season, its own scene season included.
	require.NotContains(t, titlesForSeason(series, 3), "Oshi no Ko Scene S3")

	// titlesFromSeries stays season-blind (search path unchanged): all titles present.
	all := titlesFromSeries(series)
	require.Contains(t, all, "Oshi no Ko 2nd Season")
	require.Contains(t, all, "Oshi no Ko Scene S3")
}

func TestNewService(t *testing.T) {
	s := NewService(nil, nil)

	assert.NotNil(t, s)
	assert.Equal(t, DefaultPositiveCacheTTL, s.positiveTTL)
	assert.Equal(t, DefaultNegativeCacheTTL, s.negativeTTL)
}

func TestService_WithPositiveTTL(t *testing.T) {
	s := NewService(nil, nil)
	customTTL := 30 * time.Minute

	result := s.WithPositiveTTL(customTTL)

	assert.Same(t, s, result, "should return same service for chaining")
	assert.Equal(t, customTTL, s.positiveTTL)
}

func TestService_WithNegativeTTL(t *testing.T) {
	s := NewService(nil, nil)
	customTTL := 15 * time.Minute

	result := s.WithNegativeTTL(customTTL)

	assert.Same(t, s, result, "should return same service for chaining")
	assert.Equal(t, customTTL, s.negativeTTL)
}

func TestService_TTLChaining(t *testing.T) {
	s := NewService(nil, nil).
		WithPositiveTTL(4 * time.Hour).
		WithNegativeTTL(30 * time.Minute)

	assert.Equal(t, 4*time.Hour, s.positiveTTL)
	assert.Equal(t, 30*time.Minute, s.negativeTTL)
}

func TestExternalIDsResult_Structure(t *testing.T) {
	// Test that ExternalIDsResult fields are correctly structured
	ids := &models.ExternalIDs{
		IMDbID:   "tt1234567",
		TMDbID:   12345,
		TVDbID:   67890,
		TVMazeID: 11111,
	}
	instanceID := 42

	result := ExternalIDsResult{
		IDs:           ids,
		FromCache:     true,
		ArrInstanceID: &instanceID,
		ContentType:   ContentTypeTV,
	}

	assert.Equal(t, ids, result.IDs)
	assert.True(t, result.FromCache)
	assert.Equal(t, 42, *result.ArrInstanceID)
	assert.Equal(t, ContentTypeTV, result.ContentType)
}

func TestExternalIDsResult_NilIDs(t *testing.T) {
	// Test negative cache result
	result := ExternalIDsResult{
		IDs:           nil,
		FromCache:     true,
		ArrInstanceID: nil,
		ContentType:   ContentTypeMovie,
	}

	assert.Nil(t, result.IDs)
	assert.True(t, result.FromCache)
	assert.Nil(t, result.ArrInstanceID)
	assert.Equal(t, ContentTypeMovie, result.ContentType)
}

func TestDebugResolveResult_Structure(t *testing.T) {
	result := DebugResolveResult{
		Title:              "Breaking Bad S01E01",
		TitleHash:          "abc123",
		ContentType:        ContentTypeTV,
		CacheHit:           false,
		InstancesAvailable: 2,
		InstanceResults: []DebugInstanceResult{
			{
				InstanceID:   1,
				InstanceName: "Sonarr 1",
				InstanceType: "sonarr",
				IDs: &models.ExternalIDs{
					TVDbID: 81189,
				},
			},
		},
	}

	assert.Equal(t, "Breaking Bad S01E01", result.Title)
	assert.Equal(t, "abc123", result.TitleHash)
	assert.Equal(t, ContentTypeTV, result.ContentType)
	assert.False(t, result.CacheHit)
	assert.Equal(t, 2, result.InstancesAvailable)
	assert.Len(t, result.InstanceResults, 1)
	assert.Equal(t, 81189, result.InstanceResults[0].IDs.TVDbID)
}

func TestDebugInstanceResult_WithError(t *testing.T) {
	result := DebugInstanceResult{
		InstanceID:   1,
		InstanceName: "Sonarr",
		InstanceType: "sonarr",
		IDs:          nil,
		Error:        "connection timeout",
	}

	assert.Equal(t, 1, result.InstanceID)
	assert.Equal(t, "Sonarr", result.InstanceName)
	assert.Equal(t, "sonarr", result.InstanceType)
	assert.Nil(t, result.IDs)
	assert.Equal(t, "connection timeout", result.Error)
}

func TestContentType_Constants(t *testing.T) {
	// Verify content type constant values
	assert.Equal(t, ContentTypeMovie, ContentType("movie"))
	assert.Equal(t, ContentTypeTV, ContentType("tv"))
	assert.Equal(t, ContentTypeAnime, ContentType("anime"))
	assert.Equal(t, ContentTypeUnknown, ContentType("unknown"))
}

func TestDefaultTTL_Values(t *testing.T) {
	// Verify default TTL values match expected configuration
	assert.Equal(t, 30*24*time.Hour, DefaultPositiveCacheTTL)
	assert.Equal(t, 1*time.Hour, DefaultNegativeCacheTTL)
}
