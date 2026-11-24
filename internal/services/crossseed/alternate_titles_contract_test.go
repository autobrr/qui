// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type alternateTitleContract struct {
	source  AlternateTitleSource
	known   HeldRelease
	titles  []string
	unknown HeldRelease
	fail    func()
}

func testAlternateTitleSource(t *testing.T, newContract func(t *testing.T) alternateTitleContract) {
	t.Helper()
	t.Run("known release", func(t *testing.T) {
		c := newContract(t)
		answer, err := c.source.AlternateTitles(t.Context(), c.known)
		require.NoError(t, err)
		for _, title := range c.titles {
			require.Contains(t, answer.Titles, title)
		}
	})
	t.Run("unknown release", func(t *testing.T) {
		c := newContract(t)
		answer, err := c.source.AlternateTitles(t.Context(), c.unknown)
		require.NoError(t, err)
		require.Empty(t, answer.Titles)
	})
	t.Run("failure", func(t *testing.T) {
		c := newContract(t)
		c.fail()
		answer, err := c.source.AlternateTitles(t.Context(), c.known)
		require.Error(t, err)
		require.Empty(t, answer.Titles)
	})
}

type fakeArrServer struct {
	mu       sync.Mutex
	down     bool
	parsed   map[string]string
	requests []string
}

func (f *fakeArrServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.URL.Path)
	if f.down {
		http.Error(w, "down", http.StatusServiceUnavailable)
		return
	}
	type alternateTitle struct {
		Title string `json:"title"`
	}
	type series struct {
		ID              int              `json:"id"`
		Title           string           `json:"title"`
		TVDbID          int              `json:"tvdbId"`
		AlternateTitles []alternateTitle `json:"alternateTitles"`
	}
	type movie struct {
		ID              int              `json:"id"`
		Title           string           `json:"title"`
		TMDbID          int              `json:"tmdbId"`
		AlternateTitles []alternateTitle `json:"alternateTitles"`
	}
	type parse struct {
		Series *series `json:"series"`
		Movie  *movie  `json:"movie"`
	}
	switch r.URL.Path {
	case "/api/v3/parse":
		var answer parse
		if kind := f.parsed[r.FormValue("title")]; kind == "series" {
			answer.Series = &series{ID: 7, Title: "Lantern Ward", TVDbID: 70}
		} else if kind == "movie" {
			answer.Movie = &movie{ID: 8, Title: "Copper Fjord", TMDbID: 80}
		}
		_ = json.MarshalWrite(w, answer)
	case "/api/v3/series/7":
		_ = json.MarshalWrite(w, series{ID: 7, Title: "Lantern Ward", TVDbID: 70, AlternateTitles: []alternateTitle{{Title: "Laternenwache"}}})
	case "/api/v3/movie/8":
		_ = json.MarshalWrite(w, movie{ID: 8, Title: "Copper Fjord", TMDbID: 80, AlternateTitles: []alternateTitle{{Title: "Kupferfjord"}}})
	case "/api/v3/series/lookup", "/api/v3/movie/lookup":
		_ = json.MarshalWrite(w, []series{})
	default:
		http.NotFound(w, r)
	}
}

func (f *fakeArrServer) takeRequests() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	requests := f.requests
	f.requests = nil
	return requests
}

func newFakeArr(t *testing.T) (*fakeArrServer, *arr.Service) {
	t.Helper()
	fake := &fakeArrServer{parsed: map[string]string{arrSeriesName: "series", arrMovieName: "movie"}}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)
	db := testdb.NewMigratedSQLite(t, "alternate-titles")
	instances, err := models.NewArrInstanceStore(db, []byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	for _, instanceType := range []models.ArrInstanceType{models.ArrInstanceTypeSonarr, models.ArrInstanceTypeRadarr} {
		_, err = instances.Create(t.Context(), instanceType, string(instanceType), srv.URL, "api-key", nil, nil, true, 1, 15)
		require.NoError(t, err)
	}
	return fake, arr.NewService(instances, models.NewArrIDCacheStore(db))
}

const (
	arrSeriesName = "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP"
	arrMovieName  = "Copper.Fjord.2019.1080p.BluRay.x264-GRP"
)

var (
	heldSeries  = HeldRelease{TorrentName: arrSeriesName, Title: "Lantern Ward", ContentType: "tv"}
	heldMovie   = HeldRelease{TorrentName: arrMovieName, Title: "Copper Fjord", Year: 2019, ContentType: "movie"}
	heldUnknown = HeldRelease{TorrentName: heldMissName, Title: "Nobody Home", ContentType: "tv"}
)

func TestArrAlternateTitlesContract(t *testing.T) {
	for name, known := range map[string]HeldRelease{"Laternenwache": heldSeries, "Kupferfjord": heldMovie} {
		t.Run(known.ContentType, func(t *testing.T) {
			testAlternateTitleSource(t, func(t *testing.T) alternateTitleContract {
				fake, service := newFakeArr(t)
				return alternateTitleContract{
					source: NewArrAlternateTitles(service),
					known:  known, titles: []string{name}, unknown: heldUnknown,
					fail: func() {
						fake.mu.Lock()
						defer fake.mu.Unlock()
						fake.down = true
					},
				}
			})
		})
	}
}

func TestFakeAlternateTitlesContract(t *testing.T) {
	testAlternateTitleSource(t, func(*testing.T) alternateTitleContract {
		fake := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Laternenwache"}}}
		return alternateTitleContract{
			source: fake,
			known:  heldSeries, titles: []string{"Laternenwache"}, unknown: heldUnknown,
			fail: func() { fake.set(func(f *fakeAlternateTitles) { f.down = true }) },
		}
	})
}

func TestArrAlternateTitlesReadTheArrIDCache(t *testing.T) {
	fake, service := newFakeArr(t)
	source := NewArrAlternateTitles(service)
	for _, held := range []HeldRelease{heldSeries, heldMovie, heldUnknown} {
		first, err := source.AlternateTitles(t.Context(), held)
		require.NoError(t, err)
		require.False(t, first.FromCache)
		require.NotEmpty(t, fake.takeRequests())

		again, err := source.AlternateTitles(t.Context(), held)
		require.NoError(t, err)
		require.True(t, again.FromCache)
		require.Equal(t, first.Titles, again.Titles)
		require.Empty(t, fake.takeRequests())
	}
}

func TestArrAlternateTitlesSkipContentThatSonarrAndRadarrDoNotHold(t *testing.T) {
	fake, service := newFakeArr(t)

	answer, err := NewArrAlternateTitles(service).AlternateTitles(t.Context(), HeldRelease{TorrentName: "Some Band - Some Album (2019) [FLAC]", Title: "Some Album", Year: 2019, ContentType: "music"})

	require.NoError(t, err)
	require.Equal(t, AlternateTitles{FromCache: true}, answer)
	require.Empty(t, fake.takeRequests())
}
