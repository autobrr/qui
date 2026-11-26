// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/logbudget"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type alternateTitleContract struct {
	source  AlternateTitleSource
	known   HeldRelease
	titles  []string
	unknown HeldRelease
	fail    func()
	hang    func()
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
		require.True(t, answer.Requested, "an answer that cost a request counts for the rate")
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
		require.True(t, answer.Requested)
	})
	for name, breakSource := range map[string]func(alternateTitleContract){
		"check while the source fails": func(c alternateTitleContract) { c.fail() },
		"check while the source hangs": func(c alternateTitleContract) { c.hang() },
	} {
		t.Run(name, func(t *testing.T) {
			torrent := qbt.Torrent{Hash: heldHash('b', 1), Name: arrSeriesName, Size: heldSingleSize, TotalSize: heldSingleSize, Progress: 1, AddedOn: 1}
			rig := newHeldRig(t, serveHeldLibrary(t, []qbt.Torrent{torrent}, nil))
			c := newContract(t)
			breakSource(c)
			asked := make(chan struct{})
			held, _ := rig.heldTitles(t.Context(), announcedAlternateTitles{c.source, sync.OnceFunc(func() { close(asked) })})
			rig.waitReady(held)
			<-asked

			require.True(t, checkAnnounce(t, held, arrSeriesName, 0).CanCrossSeed, "the title on its own")
			require.False(t, checkAnnounce(t, held, strings.Replace(arrSeriesName, "Lantern.Ward", "Laternenwache", 1), 0).CanCrossSeed)
		})
	}
}

type announcedAlternateTitles struct {
	AlternateTitleSource

	asking func()
}

func (a announcedAlternateTitles) AlternateTitles(ctx context.Context, held HeldRelease) (AlternateTitles, error) {
	a.asking()
	return a.AlternateTitleSource.AlternateTitles(ctx, held)
}

type fakeArrServer struct {
	mu         sync.Mutex
	down       bool
	hangs      bool
	noSeries   bool
	lookupDown bool
	parsed     map[string]string
	requests   []string
	cache      *models.ArrIDCacheStore
}

func (f *fakeArrServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	hangs := f.hangs
	f.mu.Unlock()
	if hangs {
		<-r.Context().Done()
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = append(f.requests, r.URL.Path)
	if f.down || f.noSeries && strings.HasPrefix(r.URL.Path, "/api/v3/series/") || f.lookupDown && strings.HasSuffix(r.URL.Path, "/lookup") {
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

func (f *fakeArrServer) set(change func(f *fakeArrServer)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
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
	fake.cache = models.NewArrIDCacheStore(db)
	return fake, arr.NewService(instances, fake.cache)
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

func arrSourceOf(service *arr.Service, held HeldRelease) AlternateTitleSource {
	sources := NewArrAlternateTitles(service)
	if held.ContentType == "movie" {
		return sources[1]
	}
	return sources[0]
}

func TestArrAlternateTitlesContract(t *testing.T) {
	for name, known := range map[string]HeldRelease{"Laternenwache": heldSeries, "Kupferfjord": heldMovie} {
		t.Run(known.ContentType, func(t *testing.T) {
			testAlternateTitleSource(t, func(t *testing.T) alternateTitleContract {
				fake, service := newFakeArr(t)
				return alternateTitleContract{
					source: arrSourceOf(service, known),
					known:  known, titles: []string{name}, unknown: heldUnknown,
					fail: func() { fake.set(func(f *fakeArrServer) { f.down = true }) },
					hang: func() { fake.set(func(f *fakeArrServer) { f.hangs = true }) },
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
			hang: func() { fake.set(func(f *fakeAlternateTitles) { f.hangs = true }) },
		}
	})
}

func TestArrAlternateTitlesReadTheArrIDCache(t *testing.T) {
	fake, service := newFakeArr(t)
	for _, held := range []HeldRelease{heldSeries, heldMovie, heldUnknown} {
		source := arrSourceOf(service, held)
		first, err := source.AlternateTitles(t.Context(), held)
		require.NoError(t, err)
		require.True(t, first.Requested)
		require.NotEmpty(t, fake.takeRequests())

		again, err := source.AlternateTitles(t.Context(), held)
		require.NoError(t, err)
		require.False(t, again.Requested)
		require.Equal(t, first.Titles, again.Titles)
		require.Empty(t, fake.takeRequests())
	}
}

func TestArrAlternateTitlesSkipContentOfAnotherSource(t *testing.T) {
	fake, service := newFakeArr(t)
	sources := NewArrAlternateTitles(service)
	album := HeldRelease{TorrentName: "Some Band - Some Album (2019) [FLAC]", Title: "Some Album", Year: 2019, ContentType: "music"}
	for _, skipped := range []struct {
		source AlternateTitleSource
		held   HeldRelease
	}{{sources[0], heldMovie}, {sources[0], album}, {sources[1], heldSeries}, {sources[1], album}} {
		answer, err := skipped.source.AlternateTitles(t.Context(), skipped.held)

		require.NoError(t, err)
		require.Equal(t, AlternateTitles{}, answer)
	}
	require.Empty(t, fake.takeRequests())
}

func TestArrAlternateTitlesCountACachedRowThatIsFetchedAgain(t *testing.T) {
	fake, service := newFakeArr(t)
	fake.down = true
	hash, ids := models.ComputeTitleHash(arrSeriesName), &models.ExternalIDs{TVDbID: 70}
	require.NoError(t, fake.cache.SetWithTitles(t.Context(), hash, string(arr.ContentTypeTV), nil, ids, []string{"Lantern Ward"}, nil, false, false, time.Hour))

	answer, err := arrSourceOf(service, heldSeries).AlternateTitles(t.Context(), heldSeries)

	require.NoError(t, err)
	require.NotEmpty(t, fake.takeRequests())
	require.True(t, answer.Requested)
}

func TestArrAlternateTitlesMarkAnIncompleteAnswer(t *testing.T) {
	for _, incomplete := range []bool{false, true} {
		t.Run(strconv.FormatBool(incomplete), func(t *testing.T) {
			fake, service := newFakeArr(t)
			fake.noSeries = incomplete

			answer, err := arrSourceOf(service, heldSeries).AlternateTitles(t.Context(), heldSeries)

			require.NoError(t, err)
			require.Contains(t, answer.Titles, "Lantern Ward")
			require.True(t, answer.Requested)
			require.Equal(t, incomplete, answer.Incomplete)
		})
	}
}

func TestAlternateTitlesFetchLogBudget(t *testing.T) {
	fetch := func(t *testing.T, titles int, prepare func(fake *fakeArrServer, service *arr.Service) *arr.Service) func() {
		t.Helper()
		fake, service := newFakeArr(t)
		service = prepare(fake, service)
		drained := make(chan struct{})
		stop := sync.OnceFunc(func() { close(drained) })
		var a *alternateTitleIndex
		a = newAlternateTitleIndex(NewArrAlternateTitles(service)[:1], func() bool {
			a.mu.Lock()
			defer a.mu.Unlock()
			if len(a.queues[0].waiting) == 0 || !a.queues[0].pausedUntil.IsZero() {
				stop()
			}
			return false
		})
		a.queues[0].every = 0
		for i := range titles {
			name := fmt.Sprintf("Lantern Ward %s S01E01 1080p WEB-DL H.264-GRP", strings.Repeat("x", i+1))
			if i%2 == 0 {
				fake.parsed[name] = "series"
			}
			holdName(a, name, 1)
		}
		return func() {
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() {
				defer close(done)
				a.run(ctx)
			}()
			<-drained
			cancel()
			<-done
		}
	}
	answering := func(_ *fakeArrServer, service *arr.Service) *arr.Service { return service }
	down := func(fake *fakeArrServer, service *arr.Service) *arr.Service {
		fake.down = true
		return service
	}
	lookupFails := func(fake *fakeArrServer, service *arr.Service) *arr.Service {
		fake.lookupDown = true
		return service
	}
	for name, prepare := range map[string]func(*fakeArrServer, *arr.Service) *arr.Service{"answering": answering, "down": down, "lookup fails": lookupFails} {
		t.Run(name, func(t *testing.T) {
			small, large := logbudget.Count(t, fetch(t, 10, prepare)), logbudget.Count(t, fetch(t, 200, prepare))
			require.Equal(t, 1, large[logbudget.Line{Level: "debug", Message: "crossseed: alternate titles fetched for Held titles"}])
			logbudget.AssertFlat(t, small, large)
		})
	}
	t.Run("no Sonarr or Radarr", func(t *testing.T) {
		logbudget.AssertSilent(t, fetch(t, 20, func(*fakeArrServer, *arr.Service) *arr.Service {
			db := testdb.NewMigratedSQLite(t, "no-arr")
			instances, err := models.NewArrInstanceStore(db, []byte(strings.Repeat("k", 32)))
			require.NoError(t, err)
			return arr.NewService(instances, models.NewArrIDCacheStore(db))
		}))
	})
}

func TestArrAlternateTitlesPauseOnlyASourceThatIsDown(t *testing.T) {
	for name, c := range map[string]struct {
		breakArr func(f *fakeArrServer)
		paused   bool
	}{
		"title searches fail": {func(f *fakeArrServer) { f.lookupDown = true }, false},
		"Radarr is down":      {func(f *fakeArrServer) { f.down = true }, true},
	} {
		t.Run(name, func(t *testing.T) {
			fake, service := newFakeArr(t)
			c.breakArr(fake)
			drained := make(chan struct{})
			stop := sync.OnceFunc(func() { close(drained) })
			var a *alternateTitleIndex
			a = newAlternateTitleIndex(NewArrAlternateTitles(service)[1:], func() bool {
				a.mu.Lock()
				defer a.mu.Unlock()
				if len(a.queues[0].waiting) == 0 || !a.queues[0].pausedUntil.IsZero() {
					stop()
				}
				return false
			})
			a.queues[0].every = 0
			for i := range 2 * sourceFailuresInARow {
				holdName(a, fmt.Sprintf("Copper Fjord %s 2019 1080p BluRay x264-GRP", strings.Repeat("x", i+1)), 1)
			}
			ctx, cancel := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() {
				defer close(done)
				a.run(ctx)
			}()
			<-drained
			cancel()
			<-done

			a.mu.Lock()
			defer a.mu.Unlock()
			require.Equal(t, c.paused, !a.queues[0].pausedUntil.IsZero())
			require.NotEmpty(t, a.queues[0].retries, "a failed title is asked again after its own wait")
		})
	}
}
