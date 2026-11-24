// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/testutil/logbudget"
)

var alternateTitlesMatcher = (&Service{releaseCache: NewReleaseCache()}).matcher()

type fakeAlternateTitles struct {
	mu      sync.Mutex
	titles  map[string][]string
	cached  bool
	down    bool
	partial bool
	hangs   bool
	takes   time.Duration
	failsOn func(title string) bool
	asked   []HeldRelease
	askedAt []time.Time
}

func (f *fakeAlternateTitles) AlternateTitles(ctx context.Context, held HeldRelease) (AlternateTitles, error) {
	f.mu.Lock()
	hangs, takes := f.hangs, f.takes
	f.mu.Unlock()
	if hangs {
		<-ctx.Done()
		return AlternateTitles{Requested: true}, ctx.Err()
	}
	<-time.After(takes)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, held)
	f.askedAt = append(f.askedAt, time.Now())
	if f.down || f.failsOn != nil && f.failsOn(held.Title) {
		return AlternateTitles{Requested: true}, errors.New("source down")
	}
	return AlternateTitles{Titles: f.titles[held.Title], Requested: !f.cached, Incomplete: f.partial}, nil
}

func (f *fakeAlternateTitles) askedTitles() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	titles := make([]string, len(f.asked))
	for i, held := range f.asked {
		titles[i] = held.Title
	}
	return titles
}

func (f *fakeAlternateTitles) set(change func(f *fakeAlternateTitles)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	change(f)
}

func holdName(a *alternateTitleIndex, name string, addedOn int64) *heldTitle {
	read := alternateTitlesMatcher.readName(name)
	return a.hold(read, alternateTitlesMatcher.titleKeys(read), name, addedOn)
}

func holdNames(a *alternateTitleIndex, title string, count int) {
	for i := range count {
		holdName(a, fmt.Sprintf("%s %s S01E01 1080p WEB-DL H.264-GRP", title, strings.Repeat("x", i+1)), 1)
	}
}

func runAlternateTitles(t *testing.T, sources ...AlternateTitleSource) *alternateTitleIndex {
	t.Helper()
	a := newAlternateTitleIndex(sources, func() bool { return false })
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		a.run(ctx)
	}()
	t.Cleanup(func() {
		cancel()
		<-done
	})
	return a
}

func TestAlternateTitlesAskOneTitleASecond(t *testing.T) {
	for _, down := range []bool{false, true} {
		t.Run(fmt.Sprintf("down %t", down), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				cached, source := &fakeAlternateTitles{cached: true}, &fakeAlternateTitles{down: down}
				a := runAlternateTitles(t, source, cached)
				started := time.Now()
				for i := range sourceFailuresInARow {
					holdName(a, fmt.Sprintf("Lantern Ward %c S01E01 1080p WEB-DL H.264-GRP", 'A'+i), 1)
				}

				time.Sleep(time.Minute)
				synctest.Wait()

				require.Len(t, source.askedAt, sourceFailuresInARow)
				for i, at := range source.askedAt[1:] {
					require.Equal(t, time.Second, at.Sub(source.askedAt[i]))
				}
				require.Equal(t, []time.Time{started, started, started}, cached.askedAt, "an answer from a cache does not wait")
			})
		})
	}
}

func TestAlternateTitlesAskOncePerTitle(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{}
		a := runAlternateTitles(t, source)
		for i, name := range []string{
			"Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP",
			"Lantern Ward S01E02 2160p WEB-DL H.265-OTHER",
			"Copper.Fjord.2019.1080p.BluRay.x264-GRP",
			"Copper.Fjord.2024.1080p.BluRay.x264-GRP",
		} {
			holdName(a, name, int64(i))
		}
		time.Sleep(time.Hour)
		synctest.Wait()
		holdName(a, "Lantern.Ward.S01E03.1080p.WEB-DL.H.264-GRP", 9)
		time.Sleep(time.Hour)
		synctest.Wait()

		require.ElementsMatch(t, []HeldRelease{
			{TorrentName: "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", Title: "Lantern Ward", ContentType: "tv"},
			{TorrentName: "Copper.Fjord.2019.1080p.BluRay.x264-GRP", Title: "Copper Fjord", Year: 2019, ContentType: "movie"},
			{TorrentName: "Copper.Fjord.2024.1080p.BluRay.x264-GRP", Title: "Copper Fjord", Year: 2024, ContentType: "movie"},
		}, source.asked)
	})
}

func TestAlternateTitlesKeepTheTitlesOfEachSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Lantern Ward", "Laternenwache"}}}
		second := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Guardia Lanterna", "laternenwache"}}}
		a := runAlternateTitles(t, first, second, &fakeAlternateTitles{down: true})
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()

		require.ElementsMatch(t, []string{"laternenwache", "guardia lanterna"}, a.of(title))
		require.Equal(t, []*heldTitle{title}, a.namedByAny([]string{"nobody", "laternenwache"}))
		require.Equal(t, []*heldTitle{title}, a.namedByAny([]string{"guardia lanterna", "laternenwache"}))
		require.Empty(t, a.namedByAny([]string{"lantern ward"}))
	})
}

func TestAlternateTitlesAskFailedTitlesAfterNewOnes(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{failsOn: func(title string) bool { return title != "Copper Fjord" }}
		a := runAlternateTitles(t, source)
		holdNames(a, "Lantern Ward", 10)
		time.Sleep(6 * time.Hour)
		synctest.Wait()
		asked := len(source.askedTitles())

		holdName(a, "Copper.Fjord.2019.1080p.BluRay.x264-GRP", 1)
		time.Sleep(longestSourcePause)
		synctest.Wait()

		require.Equal(t, "Copper Fjord", source.askedTitles()[asked])
	})
}

func TestAlternateTitlesAskEachFailedTitleAfterItsOwnWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{failsOn: func(title string) bool { return title != "Amber Quay" }}
		a := runAlternateTitles(t, source)
		started := time.Now()
		holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		time.Sleep(time.Minute)
		synctest.Wait()
		holdName(a, "Amber.Quay.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		time.Sleep(alternateTitleRetry - 2*time.Minute)
		synctest.Wait()
		holdName(a, "Copper.Fjord.2019.1080p.BluRay.x264-GRP", 1)
		gone := holdName(a, "Quiet.Harbor.2020.1080p.BluRay.x264-GRP", 1)
		time.Sleep(10 * time.Second)
		synctest.Wait()
		source.set(func(f *fakeAlternateTitles) { f.failsOn = nil })
		a.drop(gone)
		time.Sleep(time.Hour)
		synctest.Wait()

		require.Len(t, source.asked, 6, "the title that left is not asked again")
		require.Equal(t, "Lantern Ward", source.asked[4].Title)
		require.Equal(t, alternateTitleRetry, source.askedAt[4].Sub(started))
		require.Equal(t, "Copper Fjord", source.asked[5].Title)
		require.GreaterOrEqual(t, source.askedAt[5].Sub(started), 2*alternateTitleRetry-time.Minute)
	})
}

func TestAlternateTitlesAskAgainAfterAnIncompleteAnswer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{partial: true, titles: map[string][]string{"Lantern Ward": {"Laternenwache"}}}
		a := runAlternateTitles(t, source)
		started := time.Now()
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		holdNames(a, "Amber Quay", 9)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, source.askedAt, 10, "a source that answers is not paused")
		require.Equal(t, []string{"laternenwache"}, a.of(title))

		source.set(func(f *fakeAlternateTitles) { f.failsOn = func(string) bool { return true } })
		time.Sleep(time.Minute)
		synctest.Wait()
		holdName(a, "Copper.Fjord.2019.1080p.BluRay.x264-GRP", 1)
		time.Sleep(time.Minute)
		synctest.Wait()
		source.set(func(f *fakeAlternateTitles) {
			f.failsOn, f.partial = nil, false
			f.titles["Lantern Ward"] = []string{"Laternenwache", "Wachlaterne"}
		})
		time.Sleep(incompleteAnswerRetry - 4*time.Minute)
		synctest.Wait()
		require.Len(t, source.askedAt, 12, "a title that failed waits less than one with an incomplete answer")
		require.Equal(t, "Copper Fjord", source.asked[11].Title)

		time.Sleep(10 * time.Minute)
		synctest.Wait()
		require.Len(t, source.askedAt, 22)
		require.Equal(t, incompleteAnswerRetry, source.askedAt[12].Sub(started))
		require.Equal(t, []string{"laternenwache", "wachlaterne"}, a.of(title))

		time.Sleep(3 * incompleteAnswerRetry)
		synctest.Wait()
		require.Len(t, source.askedAt, 22, "a complete answer is final")
	})
}

func TestAlternateTitlesLogOneLineForTheRetriesOfAnHour(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const titles = 100
		source := &fakeAlternateTitles{partial: true, takes: 300 * time.Millisecond, titles: map[string][]string{}}
		for i := range titles {
			source.titles["Lantern Ward "+strings.Repeat("x", i+1)] = []string{"Laternenwache"}
		}
		a := runAlternateTitles(t, source)
		holdNames(a, "Lantern Ward", titles)
		time.Sleep(time.Hour)
		synctest.Wait()

		source.set(func(f *fakeAlternateTitles) { f.takes, f.partial = 0, false })
		lines := logbudget.Count(t, func() {
			time.Sleep(2 * incompleteAnswerRetry)
			synctest.Wait()
		})

		require.Len(t, source.askedAt, 2*titles)
		require.Equal(t, 1, lines[logbudget.Line{Level: "debug", Message: "crossseed: alternate titles fetched for Held titles"}])
	})
}

func TestAlternateTitlesPauseASourceThatIsDown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const titles = 500
		good, down := &fakeAlternateTitles{cached: true}, &fakeAlternateTitles{}
		a := runAlternateTitles(t, good, down)
		holdName(a, "Copper.Fjord.2019.1080p.BluRay.x264-GRP", 1)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, down.askedAt, 1)

		down.set(func(f *fakeAlternateTitles) { f.down = true })
		pauses := logbudget.Count(t, func() {
			holdNames(a, "Lantern Ward", titles)
			time.Sleep(3 * time.Hour)
			synctest.Wait()
		})
		require.Len(t, good.askedAt, titles+1, "the other source still answers")
		asked := down.askedAt[1:]
		waits := make([]time.Duration, len(asked)-1)
		for i := range waits {
			waits[i] = asked[i+1].Sub(asked[i]).Round(time.Minute)
		}
		require.Equal(t, []time.Duration{0, 0, time.Minute, 2 * time.Minute, 4 * time.Minute, 8 * time.Minute, 15 * time.Minute, 15 * time.Minute}, waits[:8])
		for _, wait := range waits[8:] {
			require.Equal(t, longestSourcePause, wait)
		}
		message := "crossseed: Held titles pauses an Alternate-title source that fails"
		require.Equal(t, 1, pauses[logbudget.Line{Level: "warn", Message: message}])
		require.Equal(t, len(asked)-sourceFailuresInARow, pauses[logbudget.Line{Level: "trace", Message: message}])

		down.set(func(f *fakeAlternateTitles) { f.down = false })
		recovered := len(down.askedAt)
		time.Sleep(longestSourcePause + titles*time.Second + alternateTitleRetry)
		synctest.Wait()
		require.Len(t, down.askedAt, recovered+titles, "a source that recovers answers for each title")

		down.set(func(f *fakeAlternateTitles) { f.down = true })
		again := len(down.askedAt)
		holdNames(a, "Amber Quay", titles)
		time.Sleep(30 * time.Second)
		synctest.Wait()
		require.Len(t, down.askedAt, again+sourceFailuresInARow)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, down.askedAt, again+sourceFailuresInARow+1, "the first pause is a minute again")
	})
}

func TestAlternateTitlesCountFailuresInARowPastCachedAnswers(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{cached: true, failsOn: func(title string) bool { return len(title)%2 == 0 }}
		a := runAlternateTitles(t, source)
		holdNames(a, "Lantern Ward", 20)

		time.Sleep(30 * time.Second)
		synctest.Wait()

		failed := 0
		for _, title := range source.askedTitles() {
			if len(title)%2 == 0 {
				failed++
			}
		}
		require.Equal(t, sourceFailuresInARow, failed, "a cached answer does not show that the source is up")
	})
}

func TestAlternateTitlesForgetATitleThatNoTorrentHolds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Laternenwache"}, "Copper Fjord": {"Kupferfjord"}}}
		a := runAlternateTitles(t, source)
		first := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		second := holdName(a, "Lantern.Ward.S01E02.1080p.WEB-DL.H.264-GRP", 1)
		synctest.Wait()
		queued := holdName(a, "Copper.Fjord.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		a.drop(queued)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Equal(t, []string{"Lantern Ward"}, source.askedTitles(), "a title that left before its turn is not asked")

		a.drop(first)
		require.Equal(t, []*heldTitle{second}, a.namedByAny([]string{"laternenwache"}))
		a.drop(second)
		require.Empty(t, a.namedByAny([]string{"laternenwache"}))
		a.store(second, []string{"Wachlaterne"})
		require.Empty(t, a.namedByAny([]string{"wachlaterne"}), "an answer for a title that left during its fetch")

		holdName(a, "Lantern.Ward.S01E03.1080p.WEB-DL.H.264-GRP", 1)
		time.Sleep(time.Minute)
		synctest.Wait()
		require.Len(t, a.namedByAny([]string{"laternenwache"}), 1)
	})
}

func TestAlternateTitlesWaitForTheBuildAndAskWithTheOldestName(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{}
		building := true
		a := newAlternateTitleIndex([]AlternateTitleSource{source}, func() bool { return building })
		go a.run(t.Context())
		holdName(a, "Lantern.Ward.S01E05.1080p.WEB-DL.H.264-GRP", 20)
		synctest.Wait()
		require.Empty(t, source.asked)
		holdName(a, "Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP", 10)
		holdName(a, "Lantern.Ward.S01E03.1080p.WEB-DL.H.264-GRP", 10)
		holdName(a, "Lantern.Ward.S01E06.1080p.WEB-DL.H.264-GRP", 30)
		synctest.Wait()
		building = false
		a.wakeRun()
		synctest.Wait()

		require.Len(t, source.asked, 1)
		require.Equal(t, "Lantern.Ward.S01E03.1080p.WEB-DL.H.264-GRP", source.asked[0].TorrentName)
	})
}

func TestAlternateTitlesWithoutASourceHoldNothing(t *testing.T) {
	a := newAlternateTitleIndex(nil, nil)

	require.Nil(t, holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1))
	require.Empty(t, a.of(nil))
	a.drop(nil)
}
