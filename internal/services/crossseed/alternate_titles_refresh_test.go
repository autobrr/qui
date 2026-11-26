// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/pkg/releases"
)

type changingAlternateTitles struct {
	*fakeAlternateTitles

	changeMu sync.Mutex
	changes  chan struct{}
}

func newChangingAlternateTitles(fake *fakeAlternateTitles) *changingAlternateTitles {
	return &changingAlternateTitles{fakeAlternateTitles: fake, changes: make(chan struct{})}
}

func (c *changingAlternateTitles) Changed() <-chan struct{} {
	c.changeMu.Lock()
	defer c.changeMu.Unlock()
	return c.changes
}

func (c *changingAlternateTitles) change() {
	c.changeMu.Lock()
	defer c.changeMu.Unlock()
	close(c.changes)
	c.changes = make(chan struct{})
}

type announcedChangingTitles struct {
	announcedAlternateTitles

	changing changingSource
}

func (a announcedChangingTitles) Changed() <-chan struct{} {
	return a.changing.Changed()
}

func TestAlternateTitlesAskAgainOnceTheCacheLifetimeIsOver(t *testing.T) {
	for _, cached := range []bool{false, true} {
		t.Run(fmt.Sprintf("cached %t", cached), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				source := &fakeAlternateTitles{cached: cached, titles: map[string][]string{"Lantern Ward": {"Laternenwache"}}}
				a := runAlternateTitles(t, source)
				time.Sleep(30 * time.Minute)
				title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
				synctest.Wait()
				source.set(func(f *fakeAlternateTitles) { f.titles["Lantern Ward"] = []string{"Laternenwache", "Wachlaterne"} })

				time.Sleep(arr.DefaultPositiveCacheTTL - time.Minute)
				synctest.Wait()
				require.Len(t, source.askTimes(), 1, "an answer the cache still keeps is not asked again")

				time.Sleep(2 * time.Hour)
				synctest.Wait()
				askedAt := source.askTimes()
				require.Len(t, askedAt, 2)
				require.GreaterOrEqual(t, askedAt[1].Sub(askedAt[0]), arr.DefaultPositiveCacheTTL, "the cache has dropped the answer")
				require.Equal(t, []string{"laternenwache", "wachlaterne"}, a.of(title))
			})
		})
	}
}

func TestAlternateTitlesAskEveryHeldTitleAgainWhenTheSourceChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newChangingAlternateTitles(&fakeAlternateTitles{
			titles:  map[string][]string{},
			failsOn: func(title string) bool { return title == "Amber Quay" },
		})
		a := runAlternateTitles(t, source)
		held := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		failing := holdName(a, "Amber.Quay.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		left := holdName(a, "Copper.Fjord.2019.1080p.BluRay.x264-GRP", 1)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, source.askTimes(), 3)
		a.drop(left)

		source.set(func(f *fakeAlternateTitles) {
			f.failsOn = nil
			f.titles["Lantern Ward"] = []string{"Laternenwache"}
			f.titles["Amber Quay"] = []string{"Bernsteinkai"}
		})
		changed := time.Now()
		source.change()
		time.Sleep(time.Minute)
		synctest.Wait()

		askedAt := source.askTimes()[3:]
		require.ElementsMatch(t, []string{"Lantern Ward", "Amber Quay"}, source.askedTitles()[3:], "each held title once, and no title that left")
		require.Equal(t, time.Second, askedAt[1].Sub(askedAt[0]), "the asks keep the rate of the source")
		require.Equal(t, changed, askedAt[0])
		require.Equal(t, []string{"laternenwache"}, a.of(held))
		require.Equal(t, []string{"bernsteinkai"}, a.of(failing), "a failed title does not wait for its retry")

		time.Sleep(alternateTitleRetry)
		synctest.Wait()
		require.Len(t, source.askTimes(), 5, "a title asked again keeps no older retry")
	})
}

func TestAlternateTitlesEndThePauseOfASourceWhenItChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newChangingAlternateTitles(&fakeAlternateTitles{down: true, titles: map[string][]string{"Lantern Ward": {"Laternenwache"}}})
		a := runAlternateTitles(t, source)
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()
		holdNames(a, "Amber Quay", sourceFailuresInARow-1)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, source.askTimes(), sourceFailuresInARow)

		source.set(func(f *fakeAlternateTitles) { f.down = false })
		changed := time.Now()
		source.change()
		synctest.Wait()

		require.Equal(t, []string{"laternenwache"}, a.of(title), "a fixed instance is asked at once")
		require.Equal(t, changed, source.askTimes()[sourceFailuresInARow])
	})
}

func TestAlternateTitlesAskAQueuedTitleOnceWhenTheSourceChanges(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newChangingAlternateTitles(&fakeAlternateTitles{})
		a := runAlternateTitles(t, source)
		holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()
		holdName(a, "Amber.Quay.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		holdName(a, "Copper.Fjord.2019.1080p.BluRay.x264-GRP", 1)
		source.change()
		time.Sleep(time.Hour)
		synctest.Wait()

		require.ElementsMatch(t, []string{"Lantern Ward", "Amber Quay", "Copper Fjord", "Lantern Ward"}, source.askedTitles(),
			"a title asked before the change is asked again, a queued title once")
	})
}

func TestAlternateTitlesKeepNoRetryForATitleThatLeft(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{cached: true}
		a := runAlternateTitles(t, source)
		holdNames(a, "Amber Quay", 10)
		synctest.Wait()
		most := 0
		for i := range 100 {
			title := holdName(a, fmt.Sprintf("Lantern Ward %s S01E01 1080p WEB-DL H.264-GRP", strings.Repeat("x", i+1)), 1)
			synctest.Wait()
			a.mu.Lock()
			most = max(most, len(a.queues[0].retries))
			a.mu.Unlock()
			a.drop(title)
		}

		require.Equal(t, 2*11, most, "retries of titles that left are dropped once the retries reach twice the held titles")
	})
}

func TestAlternateTitlesOweEveryTitleThatASourceChangeAsksAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newChangingAlternateTitles(&fakeAlternateTitles{})
		var building atomic.Bool
		a := newAlternateTitleIndex([]AlternateTitleSource{source}, building.Load)
		go a.run(t.Context())
		holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()
		building.Store(true)
		time.Sleep(arr.DefaultPositiveCacheTTL + 2*time.Hour)
		synctest.Wait()
		require.Len(t, source.askTimes(), 1)

		source.change()
		synctest.Wait()
		require.False(t, a.caughtUp(string(releases.ContentTypeTV)), "a title that came due before the change is owed too")

		building.Store(false)
		a.wakeRun()
		synctest.Wait()
		require.Len(t, source.askTimes(), 2)
		require.True(t, a.caughtUp(string(releases.ContentTypeTV)))
	})
}

func TestArrAlternateTitlesAskAgainWhenAnInstanceIsAdded(t *testing.T) {
	fake := &fakeArrServer{parsed: map[string]string{arrSeriesName: "series"}}
	db := testdb.NewMigratedSQLite(t, "alternate-titles-instances")
	instances, err := models.NewArrInstanceStore(db, []byte(strings.Repeat("k", 32)))
	require.NoError(t, err)
	fake.cache = models.NewArrIDCacheStore(db)
	service := arr.NewService(instances, fake.cache)
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	asked := make(chan struct{}, 3)
	sonarr := NewArrAlternateTitles(service)[0]
	changing, ok := sonarr.(changingSource)
	require.True(t, ok, "the arr source tells when its instances change")
	a := runAlternateTitles(t, announcedChangingTitles{announcedAlternateTitles{sonarr, func() { asked <- struct{}{} }}, changing})
	a.queues[0].every = 0
	title := holdName(a, arrSeriesName, 1)
	awaitAsk(t, asked)
	require.Empty(t, a.of(title))

	_, err = instances.Create(t.Context(), models.ArrInstanceTypeSonarr, "sonarr", srv.URL, "api-key", nil, nil, true, 1, 15)
	require.NoError(t, err)
	service.NoteInstancesChanged()
	awaitAsk(t, asked)
	holdName(a, arrMovieName, 1)
	awaitAsk(t, asked)

	require.Contains(t, a.of(title), "laternenwache")
}

func awaitAsk(t *testing.T, asked <-chan struct{}) {
	t.Helper()
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("the source was not asked")
	}
}

func TestAlternateTitlesOweTheTitlesThatASourceChangeAsksAgain(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := newChangingAlternateTitles(&fakeAlternateTitles{})
		a := runAlternateTitles(t, source)
		holdNames(a, "Lantern Ward", 3)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.True(t, a.caughtUp(string(releases.ContentTypeTV)))

		source.change()
		synctest.Wait()
		require.Len(t, source.askTimes(), 4)
		require.False(t, a.caughtUp(string(releases.ContentTypeTV)), "titles still to ask after the change")

		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, source.askTimes(), 6)
		require.True(t, a.caughtUp(string(releases.ContentTypeTV)))
	})
}

func TestAlternateTitlesAskAgainInTheOrderOfTheFirstAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{cached: true}
		a := runAlternateTitles(t, source)
		for _, name := range []string{
			"Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP",
			"Amber.Quay.S01E01.1080p.WEB-DL.H.264-GRP",
			"Copper.Fjord.2019.1080p.BluRay.x264-GRP",
		} {
			holdName(a, name, 1)
			synctest.Wait()
		}

		time.Sleep(arr.DefaultPositiveCacheTTL + 2*time.Hour)
		synctest.Wait()

		require.Equal(t, []string{"Lantern Ward", "Amber Quay", "Copper Fjord", "Lantern Ward", "Amber Quay", "Copper Fjord"}, source.askedTitles(),
			"answers due in the same hour keep their order")
	})
}
