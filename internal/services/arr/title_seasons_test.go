// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

var scopedSonarr = arrAnswers{
	"/api/v3/parse": `{"series": {"id": 42, "title": "Solitude", "tvdbId": 471000, "alternateTitles": [
		{"title": "Kodoku no Yado", "seasonNumber": 2}
	]}}`,
	"/api/v3/series/42": `{"id": 42, "title": "Solitude", "tvdbId": 471000, "alternateTitles": [
		{"title": "Kodoku no Yado", "seasonNumber": 2},
		{"title": "Kodoku-no-Yado", "seasonNumber": 3, "sceneSeasonNumber": 1},
		{"title": "Yado Wide", "seasonNumber": -1},
		{"title": "Yado Scene", "sceneSeasonNumber": 4},
		{"title": "Solitude", "sceneSeasonNumber": 1},
		{"title": "Yado Everywhere", "seasonNumber": 5},
		{"title": "Yado Everywhere"},
		{"title": "Yado Specials", "seasonNumber": 0}
	]}`,
}

func TestService_LookupExternalIDsKeepsTheSeasonScopeOfEachTitle(t *testing.T) {
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, scopedSonarr)

	asked, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)
	require.NoError(t, err)
	cached, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)
	require.NoError(t, err)

	require.False(t, asked.FromCache)
	require.True(t, cached.FromCache)
	for _, result := range []*ExternalIDsResult{asked, cached} {
		require.Equal(t, []string{"Solitude", "Kodoku no Yado", "Kodoku-no-Yado", "Yado Wide", "Yado Scene", "Yado Everywhere", "Yado Specials"}, result.Titles)
		for title, want := range map[string][]int{"Kodoku no Yado": {2, 3}, "Kodoku-no-Yado": {2, 3}, "Yado Scene": nil, "Yado Specials": {0}} {
			seasons, scoped := result.TitleSeasons.Of(title)
			require.True(t, scoped, title)
			require.ElementsMatch(t, want, seasons, title)
		}
		for _, title := range []string{"Solitude", "Yado Wide", "Yado Everywhere"} {
			_, scoped := result.TitleSeasons.Of(title)
			require.False(t, scoped, title)
		}
	}
	entry, err := cacheStore.Get(t.Context(), models.ComputeTitleHash(solitudeRelease), string(ContentTypeTV))
	require.NoError(t, err)
	require.Len(t, entry.TitleSeasons, len(asked.TitleSeasons))
	for key, seasons := range asked.TitleSeasons {
		require.Contains(t, entry.TitleSeasons, key)
		require.ElementsMatch(t, seasons, entry.TitleSeasons[key], key)
	}
}
