// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"errors"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

var ErrTitleUnanswered = errors.New("the source is up but cannot answer for this title")

type AlternateTitleSource interface {
	AlternateTitles(ctx context.Context, held HeldRelease) (AlternateTitles, error)
}

type HeldRelease struct {
	TorrentName string
	Title       string
	Year        int
	ContentType string
}

type AlternateTitles struct {
	Titles     []string
	Seasons    map[string][]int
	Requested  bool
	Incomplete bool
}

type alternateTitleIndex struct {
	queues []*alternateTitleQueue

	mu      sync.Mutex
	held    map[heldTitleIdentity]*heldTitle
	byTitle map[string][]*heldTitle
}

type heldTitleIdentity struct {
	key, contentType string
	year             int
}

type heldTitle struct {
	identity   heldTitleIdentity
	release    HeldRelease
	addedOn    int64
	holders    int
	alternates []string
	seasons    map[string][]int
}

func (t *heldTitle) fits(key string, season int) bool {
	seasons, scoped := t.seasons[key]
	return !scoped || season <= 0 || slices.Contains(seasons, season)
}

func newAlternateTitleIndex(sources []AlternateTitleSource, busy func() bool) *alternateTitleIndex {
	a := &alternateTitleIndex{held: map[heldTitleIdentity]*heldTitle{}, byTitle: map[string][]*heldTitle{}}
	for _, source := range sources {
		a.queues = append(a.queues, &alternateTitleQueue{titles: a, source: source, busy: busy, wake: make(chan struct{}, 1), every: alternateTitleInterval})
	}
	return a
}

func (a *alternateTitleIndex) hold(read namedRelease, keys []string, askName string, addedOn int64) *heldTitle {
	if len(a.queues) == 0 || len(keys) == 0 {
		return nil
	}
	release := HeldRelease{
		TorrentName: read.release.Title,
		Title:       read.release.Title,
		Year:        read.release.Year,
		ContentType: string(releases.DetermineContentType(read.release).ContentType),
	}
	identity := heldTitleIdentity{key: keys[0], contentType: release.ContentType, year: release.Year}
	a.mu.Lock()
	defer a.mu.Unlock()
	title := a.held[identity]
	if title == nil {
		title = &heldTitle{identity: identity, release: release, addedOn: math.MaxInt64}
		a.held[identity] = title
		for _, queue := range a.queues {
			if len(queue.waiting) == 0 {
				queue.wakeRun()
			}
			queue.waiting = append(queue.waiting, title)
		}
	}
	title.holders++
	if askName != "" && cmp.Or(cmp.Compare(addedOn, title.addedOn), strings.Compare(askName, title.release.TorrentName)) < 0 {
		title.release.TorrentName, title.addedOn = askName, addedOn
	}
	return title
}

func (a *alternateTitleIndex) drop(title *heldTitle) {
	if title == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	title.holders--
	if title.holders > 0 {
		return
	}
	delete(a.held, title.identity)
	for _, alternate := range title.alternates {
		dropFrom(a.byTitle, alternate, title)
	}
}

func (a *alternateTitleIndex) of(title *heldTitle, season int) []string {
	if title == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if len(title.seasons) == 0 {
		return title.alternates
	}
	var alternates []string
	for _, key := range title.alternates {
		if title.fits(key, season) {
			alternates = append(alternates, key)
		}
	}
	return alternates
}

func (a *alternateTitleIndex) namedByAny(keys []string, season int) []*heldTitle {
	a.mu.Lock()
	defer a.mu.Unlock()
	var titles []*heldTitle
	for _, key := range keys {
		for _, title := range a.byTitle[key] {
			if title.fits(key, season) && !slices.Contains(titles, title) {
				titles = append(titles, title)
			}
		}
	}
	return titles
}

func (a *alternateTitleIndex) store(title *heldTitle, answer AlternateTitles) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if title.holders == 0 {
		return
	}
	for _, alternate := range answer.Titles {
		key := stringutils.NormalizeForMatching(alternate)
		if key == "" || key == title.identity.key {
			continue
		}
		seasons, scoped := answer.Seasons[key]
		if slices.Contains(title.alternates, key) {
			title.widenScope(key, seasons, scoped)
			continue
		}
		title.alternates = append(title.alternates, key)
		a.byTitle[key] = append(a.byTitle[key], title)
		if scoped {
			if title.seasons == nil {
				title.seasons = map[string][]int{}
			}
			title.seasons[key] = slices.Clone(seasons)
		}
	}
}

func (t *heldTitle) widenScope(key string, seasons []int, scoped bool) {
	known, wasScoped := t.seasons[key]
	if !wasScoped {
		return
	}
	if !scoped {
		delete(t.seasons, key)
		return
	}
	for _, season := range seasons {
		if !slices.Contains(known, season) {
			known = append(known, season)
		}
	}
	t.seasons[key] = known
}

func (a *alternateTitleIndex) caughtUp(contentType string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return !slices.ContainsFunc(a.queues, func(queue *alternateTitleQueue) bool { return queue.owesAnswer(contentType) })
}

func (a *alternateTitleIndex) wakeRun() {
	for _, queue := range a.queues {
		queue.wakeRun()
	}
}

func (a *alternateTitleIndex) run(ctx context.Context) {
	var queues sync.WaitGroup
	for _, queue := range a.queues {
		queues.Go(func() { queue.run(ctx) })
	}
	queues.Wait()
}
