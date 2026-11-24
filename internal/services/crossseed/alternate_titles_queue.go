// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"slices"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	alternateTitleInterval = time.Second
	alternateTitleRetry    = 15 * time.Minute

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
	failed  []failedTitle

	failures    int
	answered    bool
	pause       time.Duration
	pausedUntil time.Time
}

type failedTitle struct {
	*heldTitle

	retryAt time.Time
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
			q.retry(title)
		}
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
			return title, title.release, nil
		}
	}
	for len(q.failed) > 0 {
		first := q.failed[0]
		if first.holders > 0 && time.Now().Before(first.retryAt) {
			return nil, HeldRelease{}, time.After(time.Until(first.retryAt))
		}
		q.failed = q.failed[1:]
		if first.holders > 0 {
			return first.heldTitle, first.release, nil
		}
	}
	q.waiting, q.failed = nil, nil
	return nil, HeldRelease{}, nil
}

func (q *alternateTitleQueue) retry(title *heldTitle) {
	q.titles.mu.Lock()
	defer q.titles.mu.Unlock()
	q.failed = append(q.failed, failedTitle{heldTitle: title, retryAt: time.Now().Add(alternateTitleRetry)})
}

func (q *alternateTitleQueue) count(answer AlternateTitles, err error) {
	if err == nil {
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
