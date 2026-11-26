// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	alternateTitleInterval = time.Second
	alternateTitleRetry    = 15 * time.Minute
	incompleteAnswerRetry  = 24 * time.Hour
	incompleteAnswerBatch  = time.Hour

	sourceFailuresInARow = 3
	firstSourcePause     = time.Minute
	longestSourcePause   = 15 * time.Minute
)

type alternateTitleQueue struct {
	titles *alternateTitleIndex
	source AlternateTitleSource
	busy   func() bool
	wake   chan struct{}
	every  time.Duration

	waiting []*heldTitle
	asking  *heldTitle
	retries []titleRetry

	searchFailed bool

	failures    int
	answered    bool
	pause       time.Duration
	pausedUntil time.Time
}

type titleRetry struct {
	*heldTitle

	at           time.Time
	owed         bool
	searchFailed bool
}

func (q *alternateTitleQueue) wakeRun() {
	select {
	case q.wake <- struct{}{}:
	default:
	}
}

func (q *alternateTitleQueue) run(ctx context.Context) {
	found, failed := 0, 0
	for {
		title, release, wait := q.next()
		if title == nil {
			if found+failed > 0 {
				log.Debug().Int("titles", found).Int("failed", failed).Msg("crossseed: alternate titles fetched for Held titles")
				found, failed = 0, 0
			}
			select {
			case <-ctx.Done():
				return
			case <-q.wake:
			case <-wait:
			}
			continue
		}
		answer, err := q.source.AlternateTitles(ctx, release)
		if ctx.Err() != nil {
			return
		}
		q.titles.store(title, answer.Titles)
		if len(answer.Titles) > 0 {
			found++
		}
		if err != nil {
			failed++
			searchFailed := errors.Is(err, ErrTitleUnanswered)
			q.retry(titleRetry{
				heldTitle:    title,
				at:           time.Now().Add(alternateTitleRetry),
				owed:         !searchFailed || !q.searchFailed,
				searchFailed: searchFailed,
			})
		} else if answer.Incomplete {
			q.retry(titleRetry{heldTitle: title, at: time.Now().Add(incompleteAnswerRetry).Truncate(incompleteAnswerBatch), owed: true})
		}
		q.asked()
		q.count(answer, err)
		if !answer.Requested {
			continue
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(q.every):
		}
	}
}

func (q *alternateTitleQueue) next() (title *heldTitle, release HeldRelease, wait <-chan time.Time) {
	if q.busy() {
		return nil, HeldRelease{}, nil
	}
	if pause := time.Until(q.pausedUntil); pause > 0 {
		return nil, HeldRelease{}, time.After(pause)
	}
	q.titles.mu.Lock()
	defer q.titles.mu.Unlock()
	for len(q.waiting) > 0 {
		last := len(q.waiting) - 1
		title = q.waiting[last]
		q.waiting = slices.Delete(q.waiting, last, last+1)
		if title.holders > 0 {
			q.asking, q.searchFailed = title, false
			return title, title.release, nil
		}
	}
	for len(q.retries) > 0 {
		first := q.retries[0]
		if time.Now().Before(first.at) {
			return nil, HeldRelease{}, time.After(time.Until(first.at))
		}
		q.retries = q.retries[1:]
		if first.holders > 0 {
			q.asking, q.searchFailed = first.heldTitle, first.searchFailed
			return first.heldTitle, first.release, nil
		}
	}
	q.waiting, q.retries = nil, nil
	return nil, HeldRelease{}, nil
}

func (q *alternateTitleQueue) asked() {
	q.titles.mu.Lock()
	defer q.titles.mu.Unlock()
	q.asking = nil
}

func (q *alternateTitleQueue) owesAnswer(contentType string) bool {
	owed := func(title *heldTitle) bool { return title.holders > 0 && title.identity.contentType == contentType }
	return q.asking != nil && owed(q.asking) ||
		slices.ContainsFunc(q.waiting, owed) ||
		slices.ContainsFunc(q.retries, func(retry titleRetry) bool { return retry.owed && owed(retry.heldTitle) })
}

func (q *alternateTitleQueue) retry(next titleRetry) {
	q.titles.mu.Lock()
	defer q.titles.mu.Unlock()
	i, _ := slices.BinarySearchFunc(q.retries, next.at, func(r titleRetry, at time.Time) int { return r.at.Compare(at) })
	q.retries = slices.Insert(q.retries, i, next)
}

func (q *alternateTitleQueue) count(answer AlternateTitles, err error) {
	if err == nil || errors.Is(err, ErrTitleUnanswered) {
		if answer.Requested {
			q.failures, q.pause, q.answered = 0, 0, true
		}
		return
	}
	q.failures++
	if q.failures < sourceFailuresInARow {
		return
	}
	q.pause = min(max(2*q.pause, firstSourcePause), longestSourcePause)
	q.pausedUntil = time.Now().Add(q.pause)
	event := log.Trace()
	if q.answered {
		event = log.Warn()
	}
	q.answered = false
	event.Err(err).Dur("pause", q.pause).Msg("crossseed: Held titles pauses an Alternate-title source that fails")
}
