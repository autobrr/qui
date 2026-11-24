// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

var alternateTitlesMatcher = (&Service{releaseCache: NewReleaseCache()}).matcher()

type fakeAlternateTitles struct {
	mu        sync.Mutex
	titles    map[string][]string
	fromCache bool
	down      bool
	asked     []HeldRelease
	askedAt   []time.Time
}

func (f *fakeAlternateTitles) AlternateTitles(_ context.Context, held HeldRelease) (AlternateTitles, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.asked = append(f.asked, held)
	f.askedAt = append(f.askedAt, time.Now())
	if f.down {
		return AlternateTitles{}, errors.New("source down")
	}
	return AlternateTitles{Titles: f.titles[held.Title], FromCache: f.fromCache}, nil
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

func holdName(a *alternateTitles, name string, addedOn int64) *heldTitle {
	read := alternateTitlesMatcher.readName(name)
	return a.hold(read, alternateTitlesMatcher.titleKeys(read), name, addedOn)
}

func runAlternateTitles(t *testing.T, sources ...AlternateTitleSource) *alternateTitles {
	t.Helper()
	a := newAlternateTitles(sources, func() bool { return false })
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
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{}
		a := runAlternateTitles(t, source)
		for i := range 5 {
			holdName(a, fmt.Sprintf("Lantern Ward %c S01E01 1080p WEB-DL H.264-GRP", 'A'+i), 1)
		}

		time.Sleep(time.Minute)

		require.Len(t, source.askedAt, 5)
		for i, at := range source.askedAt[1:] {
			require.GreaterOrEqual(t, at.Sub(source.askedAt[i]), time.Second)
		}
		require.LessOrEqual(t, source.askedAt[4].Sub(source.askedAt[0]), 5*time.Second)
	})
}

func TestAlternateTitlesFromACacheDoNotWait(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{fromCache: true}
		a := runAlternateTitles(t, source)
		started := time.Now()
		for i := range 50 {
			holdName(a, fmt.Sprintf("Lantern Ward %s S01E01 1080p WEB-DL H.264-GRP", strings.Repeat("x", i+1)), 1)
		}

		synctest.Wait()

		require.Len(t, source.askedAt, 50)
		require.Zero(t, time.Since(started))
	})
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
		holdName(a, "Lantern.Ward.S01E03.1080p.WEB-DL.H.264-GRP", 9)
		time.Sleep(time.Hour)

		require.ElementsMatch(t, []HeldRelease{
			{TorrentName: "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", Title: "Lantern Ward", ContentType: "tv"},
			{TorrentName: "Copper.Fjord.2019.1080p.BluRay.x264-GRP", Title: "Copper Fjord", Year: 2019, ContentType: "movie"},
			{TorrentName: "Copper.Fjord.2024.1080p.BluRay.x264-GRP", Title: "Copper Fjord", Year: 2024, ContentType: "movie"},
		}, source.asked)
	})
}

func TestAlternateTitlesAskWithTheOldestNameThatCarriesTheTitle(t *testing.T) {
	tests := []struct {
		name  string
		holds []struct {
			torrent string
			addedOn int64
		}
		read string
		want string
	}{
		{
			name: "oldest torrent",
			holds: []struct {
				torrent string
				addedOn int64
			}{
				{"Lantern.Ward.S01E05.1080p.WEB-DL.H.264-GRP", 30},
				{"Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP", 20},
				{"Lantern.Ward.S01E06.1080p.WEB-DL.H.264-GRP", 40},
			},
			want: "Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP",
		},
		{
			name: "same time",
			holds: []struct {
				torrent string
				addedOn int64
			}{
				{"Lantern.Ward.S01E05.1080p.WEB-DL.H.264-GRP", 20},
				{"Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP", 20},
			},
			want: "Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP",
		},
		{
			name: "season folder after its files are read",
			holds: []struct {
				torrent string
				addedOn int64
			}{{"", 10}},
			read: "Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP",
			want: "Lantern Ward",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				source := &fakeAlternateTitles{}
				a := newAlternateTitles([]AlternateTitleSource{source}, func() bool { return false })
				for _, held := range tt.holds {
					read := alternateTitlesMatcher.readName(cmp.Or(tt.read, held.torrent))
					a.hold(read, alternateTitlesMatcher.titleKeys(read), held.torrent, held.addedOn)
				}
				ctx, cancel := context.WithCancel(t.Context())
				go a.run(ctx)
				synctest.Wait()
				cancel()

				require.Len(t, source.asked, 1)
				require.Equal(t, tt.want, source.asked[0].TorrentName)
			})
		})
	}
}

func TestAlternateTitlesKeepTheTitlesOfEachSource(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		first := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Lantern Ward", "Laternenwache"}}}
		second := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Guardia Lanterna", "laternenwache"}}}
		a := runAlternateTitles(t, first, second)
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)

		require.Empty(t, a.of(title))
		synctest.Wait()

		require.Equal(t, []string{"Laternenwache", "Guardia Lanterna"}, a.of(title))
		require.Equal(t, []*heldTitle{title}, a.namedByAny([]string{"nobody", "laternenwache"}))
		require.Equal(t, []*heldTitle{title}, a.namedByAny([]string{"guardia lanterna", "laternenwache"}))
		require.Empty(t, a.namedByAny([]string{"lantern ward"}))
	})
}

func TestAlternateTitlesAskAgainAfterASourceFails(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		good := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Laternenwache"}}}
		failing := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Guardia Lanterna"}}, down: true}
		a := runAlternateTitles(t, good, failing)
		title := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)

		time.Sleep(alternateTitleRetry - time.Second)
		require.Len(t, failing.askedAt, 1)
		require.Equal(t, []string{"Laternenwache"}, a.of(title), "the other source still counts")

		failing.set(func(f *fakeAlternateTitles) { f.down = false })
		time.Sleep(time.Minute)
		require.Equal(t, []string{"Laternenwache", "Guardia Lanterna"}, a.of(title))

		time.Sleep(24 * time.Hour)
		require.Len(t, failing.askedAt, 2)
	})
}

func TestAlternateTitlesForgetATitleThatNoTorrentHolds(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{titles: map[string][]string{"Lantern Ward": {"Laternenwache"}, "Copper Fjord": {"Kupferfjord"}}}
		a := runAlternateTitles(t, source)
		first := holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		second := holdName(a, "Lantern.Ward.S01E02.1080p.WEB-DL.H.264-GRP", 1)
		queued := holdName(a, "Copper.Fjord.S01E01.1080p.WEB-DL.H.264-GRP", 1)
		a.release(queued)
		time.Sleep(time.Minute)
		require.Equal(t, []string{"Lantern Ward"}, source.askedTitles(), "a title that left before its turn is not asked")

		a.release(first)
		require.Equal(t, []*heldTitle{second}, a.namedByAny([]string{"laternenwache"}))
		a.release(second)
		require.Empty(t, a.namedByAny([]string{"laternenwache"}))

		holdName(a, "Lantern.Ward.S01E03.1080p.WEB-DL.H.264-GRP", 1)
		time.Sleep(time.Minute)
		require.Len(t, a.namedByAny([]string{"laternenwache"}), 1)
	})
}

func TestAlternateTitlesWaitWhileHeldTitlesIsBuilt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		source := &fakeAlternateTitles{}
		building := true
		a := newAlternateTitles([]AlternateTitleSource{source}, func() bool { return building })
		go a.run(t.Context())
		holdName(a, "Lantern.Ward.S01E05.1080p.WEB-DL.H.264-GRP", 20)
		synctest.Wait()
		require.Empty(t, source.asked)
		holdName(a, "Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP", 10)
		synctest.Wait()
		building = false
		a.wakeRun()
		synctest.Wait()

		require.Len(t, source.asked, 1)
		require.Equal(t, "Lantern.Ward.S01E04.1080p.WEB-DL.H.264-GRP", source.asked[0].TorrentName)
	})
}

func TestAlternateTitlesWithoutASourceHoldNothing(t *testing.T) {
	a := newAlternateTitles(nil, nil)

	require.Nil(t, holdName(a, "Lantern.Ward.S01E01.1080p.WEB-DL.H.264-GRP", 1))
	require.Empty(t, a.of(nil))
	a.release(nil)
}
