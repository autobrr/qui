// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"testing"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/stringutils"
)

func TestBuildTorznabRequest(t *testing.T) {
	t.Parallel()

	contentInfo := func(contentType string) ContentTypeInfo {
		info, ok := RuleContentTypeInfo(contentType)
		require.True(t, ok)
		return info
	}
	tv := contentInfo("tv")
	movie := contentInfo("movie")
	music := contentInfo("music")
	audiobook := contentInfo("audiobook")

	const (
		episodeName  = "Azure.Compass.S02E03.1080p.WEB.x264-GROUP"
		absoluteName = "[KiraSubs] Azure Compass - 81 (1080p) [ABCD1234].mkv"
		movieName    = "Example.Movie.2019.1080p.BluRay.x264-GROUP"
	)
	mapS04E15 := &models.EpisodeMap{Absolute: 81, Season: 4, Episode: 15}
	tvdb := &models.ExternalIDs{TVDbID: 424242}

	tests := []struct {
		name  string
		input torznabRequestInput
		// want gets the fields that every request shares from the input of the row.
		want          jackett.TorznabSearchRequest
		wantAltTitle  string
		wantAltSeason *int
		seasonNumeral int
	}{
		{
			name:  "an empty query is generated from the name",
			input: torznabRequestInput{torrentName: episodeName, contentInfo: tv},
			want:  jackett.TorznabSearchRequest{Query: "Azure Compass", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name: "a typed query is trimmed and keeps the season and episode of the name",
			input: torznabRequestInput{
				torrentName: episodeName, contentInfo: tv,
				query: "  typed words  ", limit: 7, cacheMode: jackett.CacheModeBypass, indexerIDs: []int{4, 9},
			},
			want: jackett.TorznabSearchRequest{Query: "typed words", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: 7, CacheMode: jackett.CacheModeBypass, IndexerIDs: []int{4, 9}},
		},
		{
			name:  "a typed query with no categories gets no season, episode, or year",
			input: torznabRequestInput{torrentName: "Azure.Compass.2019.S02E03.1080p.WEB.x264-GROUP", query: "typed words"},
			want:  jackett.TorznabSearchRequest{Query: "typed words", Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "a typed query on music gets no season",
			input: torznabRequestInput{torrentName: "Sable Wren - Season 2 (2024) WEB FLAC", contentInfo: music, query: "typed words"},
			want:  jackett.TorznabSearchRequest{Query: "typed words", Artist: "Sable Wren", Album: "Season 2", Year: 2024, Categories: music.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "forced music keeps a numeric album title that parses as an episode",
			input: torznabRequestInput{torrentName: "Sable Wren - 9 (2025) [WEB FLAC]", contentInfo: music},
			want:  jackett.TorznabSearchRequest{Query: "Sable Wren 9", Artist: "Sable Wren", Album: "9", Year: 2025, Categories: music.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "forced music keeps an album title that parses as a series",
			input: torznabRequestInput{torrentName: "Sable Wren - Season 2 (2024) WEB FLAC", contentInfo: music},
			want:  jackett.TorznabSearchRequest{Query: "Sable Wren Season 2", Artist: "Sable Wren", Album: "Season 2", Year: 2024, Categories: music.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "an audiobook gets the artist and album but not the music parse of the query",
			input: torznabRequestInput{torrentName: "Sable Wren - 9 (2025) [WEB FLAC]", contentInfo: audiobook},
			want:  jackett.TorznabSearchRequest{Query: "Sable Wren", Artist: "Sable Wren", Album: "9", Year: 2025, Categories: audiobook.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "a movie gets the year",
			input: torznabRequestInput{torrentName: movieName, contentInfo: movie},
			want:  jackett.TorznabSearchRequest{Query: "Example Movie", Year: 2019, Categories: movie.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "TV gets no year",
			input: torznabRequestInput{torrentName: "Azure.Compass.2019.S02E03.1080p.WEB.x264-GROUP", contentInfo: tv},
			want:  jackett.TorznabSearchRequest{Query: "Azure Compass", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name: "every external ID goes on the request and turns off the alternate title",
			input: torznabRequestInput{
				torrentName: episodeName, contentInfo: tv, arrTitles: []string{"Other Title"},
				externalIDs: &models.ExternalIDs{IMDbID: "tt1234567", TVDbID: 424242, TMDbID: 77, TVMazeID: 88},
			},
			want: jackett.TorznabSearchRequest{
				Query: "Azure Compass", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit,
				IMDbID: "tt1234567", TVDbID: "424242", TMDbID: 77, TVMazeID: 88, OmitQueryForIDs: true,
			},
		},
		{
			name:  "the Episode map goes on a request with IDs when the absolute number matches",
			input: torznabRequestInput{torrentName: absoluteName, contentInfo: tv, externalIDs: tvdb, episodeMap: mapS04E15},
			want: jackett.TorznabSearchRequest{
				Query: "Azure Compass", Episode: new(81), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit,
				TVDbID: "424242", OmitQueryForIDs: true, EpisodeMap: mapS04E15,
			},
		},
		{
			name:  "the Episode map stays off a request with no IDs",
			input: torznabRequestInput{torrentName: absoluteName, contentInfo: tv, episodeMap: mapS04E15},
			want:  jackett.TorznabSearchRequest{Query: "Azure Compass", Episode: new(81), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name: "the Episode map stays off when its absolute number is another episode",
			input: torznabRequestInput{
				torrentName: absoluteName, contentInfo: tv, externalIDs: tvdb,
				episodeMap: &models.EpisodeMap{Absolute: 80, Season: 4, Episode: 14},
			},
			want: jackett.TorznabSearchRequest{Query: "Azure Compass", Episode: new(81), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit, TVDbID: "424242", OmitQueryForIDs: true},
		},
		{
			name:  "the Episode map stays off a release with a season",
			input: torznabRequestInput{torrentName: episodeName, contentInfo: tv, externalIDs: tvdb, episodeMap: &models.EpisodeMap{Absolute: 3, Season: 4, Episode: 15}},
			want:  jackett.TorznabSearchRequest{Query: "Azure Compass", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit, TVDbID: "424242", OmitQueryForIDs: true},
		},
		{
			name:  "the Episode map stays off a release with no episode",
			input: torznabRequestInput{torrentName: movieName, contentInfo: movie, externalIDs: tvdb, episodeMap: &models.EpisodeMap{Season: 4, Episode: 15}},
			want:  jackett.TorznabSearchRequest{Query: "Example Movie", Year: 2019, Categories: movie.Categories, Limit: torznabCrossSeedSearchLimit, TVDbID: "424242", OmitQueryForIDs: true},
		},
		{
			name:         "a search with no IDs gets the alternate title",
			input:        torznabRequestInput{torrentName: episodeName, contentInfo: tv, arrTitles: []string{"Other Title"}},
			want:         jackett.TorznabSearchRequest{Query: "Azure Compass", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit},
			wantAltTitle: "Other Title",
		},
		{
			name:         "IDs from a tag keep the alternate title",
			input:        torznabRequestInput{torrentName: episodeName, contentInfo: tv, arrTitles: []string{"Other Title"}, externalIDs: tvdb, tagSourcedIDs: true},
			want:         jackett.TorznabSearchRequest{Query: "Azure Compass", Season: new(2), Episode: new(3), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit, TVDbID: "424242", OmitQueryForIDs: true},
			wantAltTitle: "Other Title",
		},
		{
			name:          "a Season numeral goes on the alternate title as its season",
			input:         torznabRequestInput{torrentName: "[GRP] Kaiju Squad 100 III (BD 1080p HEVC FLAC) [Dual-Audio]", contentInfo: tv},
			seasonNumeral: 3,
			want:          jackett.TorznabSearchRequest{Query: "Kaiju Squad 100 III", Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit},
			wantAltTitle:  "Kaiju Squad 100",
			wantAltSeason: new(3),
		},
		{
			name:  "the name of a season group gets the season and the full category list",
			input: torznabRequestInput{torrentName: "Azure Compass S02", contentInfo: tv},
			want:  jackett.TorznabSearchRequest{Query: "Azure Compass", Season: new(2), Categories: tv.Categories, Limit: torznabCrossSeedSearchLimit},
		},
		{
			name:  "the name of a season group that ends in a year parses as music",
			input: torznabRequestInput{torrentName: "Azure Compass 2019", contentInfo: music},
			want:  jackett.TorznabSearchRequest{Query: "Azure Compass", Album: "Azure Compass", Year: 2019, Categories: music.Categories, Limit: torznabCrossSeedSearchLimit},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := matcher{releaseCache: NewReleaseCache()}
			in := tt.input
			in.source = m.readName(in.torrentName)
			in.source.seasonNumeral = tt.seasonNumeral

			got := m.buildTorznabRequest(in)

			want := tt.want
			want.ReleaseName = in.torrentName
			want.ReturnAllResults = true
			require.Equal(t, gatherInput{
				req:            &want,
				tagSourcedIDs:  in.tagSourcedIDs,
				altTitle:       tt.wantAltTitle,
				altTitleSeason: tt.wantAltSeason,
				torrentName:    in.torrentName,
			}, got)
		})
	}
}

// The search dialog sends the query that the user typed to the indexer.
func TestSearchTorrentMatches_DialogSendsTypedQuery(t *testing.T) {
	const (
		sourceHash = "4c1b7f0e9d3a2865bd10c7e4f2a93b6d8e015c72"
		sourceName = "Azure.Compass.S02E03.1080p.WEB.x264-GROUP"
	)

	var mu sync.Mutex
	var params []url.Values
	tracker := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("t") != "caps" {
			mu.Lock()
			params = append(params, r.URL.Query())
			mu.Unlock()
		}
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="UTF-8"?><rss version="2.0"><channel></channel></rss>`))
	}))
	t.Cleanup(tracker.Close)

	instance := &models.Instance{ID: 1, Name: "main"}
	source := qbt.Torrent{Hash: sourceHash, Name: sourceName, Size: 1000, TotalSize: 1000, Progress: 1, Tracker: "https://example.invalid/announce"}
	svc := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		jackettService: newJackettServiceWithIndexers([]*models.TorznabIndexer{{
			ID: 1, Name: "TV Indexer", Enabled: true, BaseURL: tracker.URL, Backend: models.TorznabBackendNative, TimeoutSeconds: 5,
			Capabilities: []string{"search", "tv-search", "tv-search-q", "tv-search-season", "tv-search-ep"},
			Categories:   []models.TorznabIndexerCategory{{IndexerID: 1, CategoryID: 5000, CategoryName: "TV"}},
		}}),
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{source}, map[string]qbt.TorrentFiles{
			sourceHash: {{Name: sourceName + ".mkv", Size: 1000}},
		}),
		asyncFilteringCache: ttlcache.New[string, *AsyncIndexerFilteringState](),
		releaseCache:        NewReleaseCache(),
		searchResultCache:   ttlcache.New[string, cachedTorrentSearchResults](),
		stringNormalizer:    stringutils.NewDefaultNormalizer(),
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return models.DefaultCrossSeedAutomationSettings(), nil
		},
	}

	_, err := svc.SearchTorrentMatches(t.Context(), instance.ID, sourceHash, TorrentSearchOptions{Query: "  typed words  "})
	require.NoError(t, err)

	mu.Lock()
	defer mu.Unlock()
	require.NotEmpty(t, params, "indexer was never queried")
	require.Equal(t, "typed words", params[0].Get("q"))
	require.Equal(t, "2", params[0].Get("season"))
	require.Equal(t, "3", params[0].Get("ep"))
}
