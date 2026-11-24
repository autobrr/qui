// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

const solitudeRelease = "Solitude.S01E01.1080p.WEB-DL.H.264-QUILL"

type arrAnswers map[string]string

func (a arrAnswers) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	body, ok := a[r.URL.Path]
	if !ok {
		http.Error(w, "boom", http.StatusInternalServerError)
		return
	}
	_, _ = w.Write([]byte(body))
}

func newTwoSonarrService(t *testing.T, first, second http.Handler) (*Service, *models.ArrIDCacheStore) {
	t.Helper()
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, first)
	server := httptest.NewServer(second)
	t.Cleanup(server.Close)
	_, err := service.instanceStore.Create(t.Context(), models.ArrInstanceTypeSonarr, "Second ARR", server.URL, "api-key", nil, nil, true, 0, 15)
	require.NoError(t, err)
	return service, cacheStore
}

var (
	emptySonarr = arrAnswers{
		"/api/v3/parse":         `{"series": null}`,
		"/api/v3/series/lookup": `[]`,
	}
	catalogueSonarr = arrAnswers{
		"/api/v3/parse":         `{"series": null}`,
		"/api/v3/series/lookup": `[{"title": "Solitude", "tvdbId": 471000}]`,
	}
	librarySonarr = arrAnswers{
		"/api/v3/parse":     `{"series": {"id": 42, "title": "Solitude", "tvdbId": 471000}}`,
		"/api/v3/series/42": `{"id": 42, "title": "Solitude", "tvdbId": 471000, "alternateTitles": [{"title": "Kodoku no Yado"}]}`,
	}
	unhydratedSonarr = arrAnswers{
		"/api/v3/parse": librarySonarr["/api/v3/parse"],
	}
	downSonarr = arrAnswers{}
)

// regression: #4478
func TestService_LookupExternalIDsAsksEachLibraryBeforeACatalogue(t *testing.T) {
	for name, instances := range map[string][]http.Handler{
		"library first":   {librarySonarr, catalogueSonarr},
		"catalogue first": {catalogueSonarr, librarySonarr},
	} {
		t.Run(name, func(t *testing.T) {
			service, cacheStore := newTwoSonarrService(t, instances[0], instances[1])

			result, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)

			require.NoError(t, err)
			require.Equal(t, "parse", result.Source)
			require.Equal(t, []string{"Solitude", "Kodoku no Yado"}, result.Titles)
			entry, err := cacheStore.Get(t.Context(), models.ComputeTitleHash(solitudeRelease), string(ContentTypeTV))
			require.NoError(t, err)
			require.Equal(t, result.Titles, entry.Titles)
		})
	}
}

// regression: #4478
func TestService_LookupExternalIDsKeepsAnIncompleteAnswerShortly(t *testing.T) {
	tests := []struct {
		name          string
		first, second http.Handler
		wantSource    string
		wantKept      time.Duration
	}{
		{name: "a library answers", first: downSonarr, second: librarySonarr, wantSource: "parse", wantKept: DefaultPositiveCacheTTL},
		{name: "no instance knows the title", first: emptySonarr, second: emptySonarr, wantSource: "none", wantKept: DefaultNegativeCacheTTL},
		{name: "only a catalogue knows the title", first: emptySonarr, second: catalogueSonarr, wantSource: "lookup", wantKept: DefaultPositiveCacheTTL},
		{name: "the series details fail", first: unhydratedSonarr, second: emptySonarr, wantSource: "parse", wantKept: incompleteCacheTTL},
		{name: "one instance is down and the other has no answer", first: emptySonarr, second: downSonarr, wantSource: "none", wantKept: incompleteCacheTTL},
		{name: "one instance is down and the other answers from its catalogue", first: catalogueSonarr, second: downSonarr, wantSource: "lookup", wantKept: incompleteCacheTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, cacheStore := newTwoSonarrService(t, tt.first, tt.second)

			result, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)

			require.NoError(t, err)
			require.Equal(t, tt.wantSource, result.Source)
			entry, err := cacheStore.Get(t.Context(), models.ComputeTitleHash(solitudeRelease), string(ContentTypeTV))
			require.NoError(t, err)
			require.WithinDuration(t, time.Now().Add(tt.wantKept), entry.ExpiresAt, time.Minute)
		})
	}
}
