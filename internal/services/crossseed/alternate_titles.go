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
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/pkg/stringutils"
)

const (
	alternateTitlePause = time.Second
	alternateTitleRetry = 15 * time.Minute

	sourceFailuresInARow = 3
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
	Titles    []string
	FromCache bool
}

type alternateTitles struct {
	sources []askedSource
	busy    func() bool
	wake    chan struct{}
	pause   time.Duration

	mu      sync.Mutex
	held    map[heldTitleIdentity]*heldTitle
	byTitle map[string][]*heldTitle
	queue   []*heldTitle
	failed  []*heldTitle
	retryAt time.Time
}

type askedSource struct {
	AlternateTitleSource

	failures    int
	pausedUntil time.Time
}

type heldTitleIdentity struct {
	key, contentType string
	year             int
}

type heldTitle struct {
	key        string
	release    HeldRelease
	addedOn    int64
	holders    int
	alternates []string
}

func newAlternateTitles(sources []AlternateTitleSource, busy func() bool) *alternateTitles {
	asked := make([]askedSource, len(sources))
	for i, source := range sources {
		asked[i].AlternateTitleSource = source
	}
	return &alternateTitles{
		sources: asked,
		busy:    busy,
		wake:    make(chan struct{}, 1),
		pause:   alternateTitlePause,
		held:    map[heldTitleIdentity]*heldTitle{},
		byTitle: map[string][]*heldTitle{},
	}
}

func (a *alternateTitles) hold(read namedRelease, keys []string, askName string, addedOn int64) *heldTitle {
	if len(a.sources) == 0 || len(keys) == 0 {
		return nil
	}
	release := HeldRelease{
		TorrentName: read.release.Title,
		Title:       read.release.Title,
		Year:        read.release.Year,
		ContentType: DetermineContentType(read.release).ContentType,
	}
	identity := heldTitleIdentity{key: keys[0], contentType: release.ContentType, year: release.Year}
	a.mu.Lock()
	defer a.mu.Unlock()
	title := a.held[identity]
	if title == nil {
		title = &heldTitle{key: keys[0], release: release, addedOn: math.MaxInt64}
		a.held[identity] = title
		a.queue = append(a.queue, title)
		a.wakeRun()
	}
	title.holders++
	if askName != "" && cmp.Or(cmp.Compare(addedOn, title.addedOn), strings.Compare(askName, title.release.TorrentName)) < 0 {
		title.release.TorrentName, title.addedOn = askName, addedOn
	}
	return title
}

func (a *alternateTitles) release(title *heldTitle) {
	if title == nil {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	title.holders--
	if title.holders > 0 {
		return
	}
	delete(a.held, heldTitleIdentity{key: title.key, contentType: title.release.ContentType, year: title.release.Year})
	for _, alternate := range title.alternates {
		dropFrom(a.byTitle, stringutils.NormalizeForMatching(alternate), title)
	}
}

func (a *alternateTitles) of(title *heldTitle) []string {
	if title == nil {
		return nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return title.alternates
}

func (a *alternateTitles) namedByAny(keys []string) []*heldTitle {
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

func (a *alternateTitles) wakeRun() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *alternateTitles) run(ctx context.Context) {
	found, failed := 0, 0
	for {
		title, release, retry := a.next()
		if title == nil {
			if found+failed > 0 {
				log.Debug().Int("titles", found).Int("failed", failed).Msg("crossseed: alternate titles fetched for Held titles")
				found, failed = 0, 0
			}
			select {
			case <-ctx.Done():
				return
			case <-a.wake:
			case <-retry:
			}
			continue
		}
		titles, fromCache, ok := a.ask(ctx, release)
		if ctx.Err() != nil {
			return
		}
		if len(titles) > 0 {
			found++
		}
		if !ok {
			failed++
		}
		a.store(title, titles, ok)
		if fromCache {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(a.pause):
		}
	}
}

func (a *alternateTitles) next() (title *heldTitle, release HeldRelease, retry <-chan time.Time) {
	if a.busy() {
		return nil, HeldRelease{}, nil
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if !time.Now().Before(a.retryAt) {
		a.queue, a.failed = append(a.failed, a.queue...), nil
	}
	for len(a.queue) > 0 {
		last := len(a.queue) - 1
		title = a.queue[last]
		a.queue = slices.Delete(a.queue, last, last+1)
		if title.holders > 0 {
			return title, title.release, nil
		}
	}
	a.queue = nil
	if len(a.failed) > 0 {
		retry = time.After(time.Until(a.retryAt))
	}
	return nil, HeldRelease{}, retry
}

func (a *alternateTitles) ask(ctx context.Context, release HeldRelease) (titles []string, fromCache, ok bool) {
	fromCache, ok = true, true
	for i := range a.sources {
		source := &a.sources[i]
		if time.Now().Before(source.pausedUntil) {
			ok = false
			continue
		}
		answer, err := source.AlternateTitles(ctx, release)
		if err != nil {
			ok, fromCache = false, false
			source.failures++
			if source.failures >= sourceFailuresInARow {
				source.pausedUntil = a.retryAt
			}
			log.Trace().Err(err).Str("title", release.Title).Msg("crossseed: an Alternate-title source failed")
			continue
		}
		if !answer.FromCache {
			source.failures = 0
		}
		titles = append(titles, answer.Titles...)
		fromCache = fromCache && answer.FromCache
	}
	return titles, fromCache, ok
}

func (a *alternateTitles) store(title *heldTitle, titles []string, ok bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if title.holders == 0 {
		return
	}
	for _, alternate := range titles {
		key := stringutils.NormalizeForMatching(alternate)
		if key == "" || key == title.key || slices.Contains(a.byTitle[key], title) {
			continue
		}
		title.alternates = append(title.alternates, alternate)
		a.byTitle[key] = append(a.byTitle[key], title)
	}
	if ok {
		return
	}
	if len(a.failed) == 0 {
		a.retryAt = time.Now().Add(alternateTitleRetry)
	}
	a.failed = append(a.failed, title)
}
