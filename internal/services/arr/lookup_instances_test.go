// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/dbinterface"
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
	unhydratedSonarrWithoutIDs = arrAnswers{
		"/api/v3/parse":         `{"series": {"id": 42, "title": "Solitude"}}`,
		"/api/v3/series/lookup": `[]`,
	}
	unhydratedSearchWithoutIDs = arrAnswers{
		"/api/v3/parse":         `{"series": null}`,
		"/api/v3/series/lookup": `[{"id": 42, "title": "Solitude"}]`,
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
		{name: "a library answers", first: emptySonarr, second: librarySonarr, wantSource: "parse", wantKept: DefaultPositiveCacheTTL},
		{name: "no instance knows the title", first: emptySonarr, second: emptySonarr, wantSource: "none", wantKept: DefaultNegativeCacheTTL},
		{name: "only a catalogue knows the title", first: emptySonarr, second: catalogueSonarr, wantSource: "lookup", wantKept: DefaultPositiveCacheTTL},
		{name: "the series details fail", first: unhydratedSonarr, second: emptySonarr, wantSource: "parse", wantKept: incompleteCacheTTL},
		{name: "the series details fail and the parse has no IDs", first: unhydratedSonarrWithoutIDs, second: emptySonarr, wantSource: "none", wantKept: incompleteCacheTTL},
		{name: "the series details fail and the search has no IDs", first: unhydratedSearchWithoutIDs, second: emptySonarr, wantSource: "none", wantKept: incompleteCacheTTL},
		{name: "one instance is down and the other answers from its library", first: downSonarr, second: librarySonarr, wantSource: "parse", wantKept: incompleteCacheTTL},
		{name: "one instance is down and the other has no answer", first: emptySonarr, second: downSonarr, wantSource: "none", wantKept: incompleteCacheTTL},
		{name: "one instance is down and the other answers from its catalogue", first: catalogueSonarr, second: downSonarr, wantSource: "lookup", wantKept: incompleteCacheTTL},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, cacheStore := newTwoSonarrService(t, tt.first, tt.second)

			result, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)

			require.NoError(t, err)
			require.Equal(t, tt.wantSource, result.Source)
			require.Equal(t, tt.wantKept == incompleteCacheTTL, result.Incomplete)
			entry, err := cacheStore.Get(t.Context(), models.ComputeTitleHash(solitudeRelease), string(ContentTypeTV))
			require.NoError(t, err)
			require.WithinDuration(t, time.Now().Add(tt.wantKept), entry.ExpiresAt, time.Minute)

			cached, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)

			require.NoError(t, err)
			require.True(t, cached.FromCache)
			require.Equal(t, result.Incomplete, cached.Incomplete)
		})
	}
}

// regression: #4478
func TestService_LookupExternalIDsKeepsAMovieWithoutDetailsShortly(t *testing.T) {
	service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeRadarr, arrAnswers{
		"/api/v3/parse": `{"movie": {"id": 7, "title": "Solitude", "tmdbId": 471000}}`,
	})
	const release = "Solitude.2024.1080p.WEB-DL.H.264-QUILL"

	result, err := service.LookupExternalIDs(t.Context(), release, ContentTypeMovie)

	require.NoError(t, err)
	require.Equal(t, []string{"Solitude"}, result.Titles)
	require.True(t, result.Incomplete)
	entry, err := cacheStore.Get(t.Context(), models.ComputeTitleHash(release), string(ContentTypeMovie))
	require.NoError(t, err)
	require.WithinDuration(t, time.Now().Add(incompleteCacheTTL), entry.ExpiresAt, time.Minute)
}

func TestService_LookupExternalIDsMarksACachedRowThatTheInstanceCouldNotRefresh(t *testing.T) {
	tests := []struct {
		name           string
		handler        http.Handler
		wantIncomplete bool
	}{
		{name: "the instance is down", handler: downSonarr, wantIncomplete: true},
		{name: "the instance answers in part", handler: unhydratedSonarrWithoutIDs, wantIncomplete: true},
		{name: "the instance does not know the title", handler: emptySonarr},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			service, cacheStore := newArrLookupTestService(t, models.ArrInstanceTypeSonarr, tt.handler)
			ids := &models.ExternalIDs{TVDbID: 471000}
			require.NoError(t, cacheStore.Set(t.Context(), models.ComputeTitleHash(solitudeRelease), string(ContentTypeTV), nil, ids, false, time.Hour))

			result, err := service.LookupExternalIDs(t.Context(), solitudeRelease, ContentTypeTV)

			require.NoError(t, err)
			require.Equal(t, ids, result.IDs)
			require.True(t, result.FromCache)
			require.Equal(t, tt.wantIncomplete, result.Incomplete)
		})
	}
}

type slowParse struct {
	answers arrAnswers
	delay   time.Duration
}

func (s slowParse) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api/v3/parse" {
		select {
		case <-time.After(s.delay):
		case <-r.Context().Done():
		}
	}
	s.answers.ServeHTTP(w, r)
}

type handlerByURL map[string]http.Handler

func (h handlerByURL) RoundTrip(r *http.Request) (*http.Response, error) {
	recorder := httptest.NewRecorder()
	h["http://"+r.URL.Host].ServeHTTP(recorder, r)
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	return recorder.Result(), nil
}

type failedWrites struct{ dbinterface.Querier }

func (failedWrites) ExecContext(context.Context, string, ...any) (sql.Result, error) {
	return nil, errors.New("no database in the bubble")
}

// regression: #4478
func TestService_LookupExternalIDsLeavesTimeForTheCatalogue(t *testing.T) {
	tests := []struct {
		name          string
		first, second http.Handler
		wantSource    string
	}{
		{name: "the second instance hangs", first: catalogueSonarr, second: slowParse{answers: librarySonarr, delay: time.Hour}, wantSource: "lookup"},
		{name: "the first library is slow", first: slowParse{answers: librarySonarr, delay: 3 * time.Second}, second: emptySonarr, wantSource: "parse"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stored, _ := newTwoSonarrService(t, tt.first, tt.second)
			instances, err := stored.instanceStore.ListEnabledByType(t.Context(), models.ArrInstanceTypeSonarr)
			require.NoError(t, err)
			require.Len(t, instances, 2)
			transport := http.DefaultTransport
			t.Cleanup(func() { http.DefaultTransport = transport })
			http.DefaultTransport = handlerByURL{instances[0].BaseURL: tt.first, instances[1].BaseURL: tt.second}
			service := NewService(stored.instanceStore, models.NewArrIDCacheStore(failedWrites{}))

			synctest.Test(t, func(t *testing.T) {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()

				result, err := service.lookupExternalIDsFromParse(ctx, "", solitudeRelease, ContentTypeTV, instances, true)

				require.NoError(t, err)
				require.Equal(t, tt.wantSource, result.Source)
			})
		})
	}
}
