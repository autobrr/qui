// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"math"
	"slices"
	"strings"
	"sync"

	"github.com/autobrr/qui/pkg/releases"
	"github.com/autobrr/qui/pkg/stringutils"
)

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

func (a *alternateTitleIndex) of(title *heldTitle) []string {
	if title == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return title.alternates
}

func (a *alternateTitleIndex) namedByAny(keys []string) []*heldTitle {
	a.mu.Lock()
	defer a.mu.Unlock()
	var titles []*heldTitle
	for _, key := range keys {
		for _, title := range a.byTitle[key] {
			if !slices.Contains(titles, title) {
				titles = append(titles, title)
			}
		}
	}
	return titles
}

func (a *alternateTitleIndex) store(title *heldTitle, titles []string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if title.holders == 0 {
		return
	}
	for _, alternate := range titles {
		key := stringutils.NormalizeForMatching(alternate)
		if key == "" || key == title.identity.key || slices.Contains(title.alternates, key) {
			continue
		}
		title.alternates = append(title.alternates, key)
		a.byTitle[key] = append(a.byTitle[key], title)
	}
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
