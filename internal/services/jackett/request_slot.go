// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

const defaultRequestGap = 60 * time.Second

// rateLimitRecordTimeout bounds the database write that records a 429. The
// write runs apart from the request, so a busy database delays no caller.
const rateLimitRecordTimeout = 10 * time.Second

// requestKind decides which Rate-limit cooldown a request obeys and whether it
// waits for the Request slot. ADR 0046 gives the reasons.
type requestKind int

const (
	requestSearch requestKind = iota
	requestCaps
	requestDirScanDownload
	requestDownload
)

func (k requestKind) scope() string {
	if k == requestDirScanDownload || k == requestDownload {
		return rateLimitScopeGrab
	}
	return rateLimitScopeQuery
}

// holdsSlot reports whether a request of this kind waits for the Request slot
// of the indexer, holds it while open, and starts the Request gap at the answer.
func (k requestKind) holdsSlot(indexer *models.TorznabIndexer) bool {
	return indexer.Backend == models.TorznabBackendNative && (k == requestSearch || k == requestDirScanDownload)
}

type rateLimitRecorder interface {
	RecordError(ctx context.Context, indexerID int, errorMessage, errorCode string) error
}

// requestSlots owns the Request slot of each indexer: the Rate-limit
// cooldowns, the Request gap, and the record of a 429.
type requestSlots struct {
	mu       sync.Mutex
	gap      time.Duration
	indexers map[int]*slotState
	recorder rateLimitRecorder
	publish  func()
	onFree   func()
}

type slotState struct {
	lastAnswer time.Time
	busy       bool
	freed      chan struct{}
	cooldowns  map[string]time.Time
}

func newRequestSlots(gap time.Duration, recorder rateLimitRecorder, publish func()) *requestSlots {
	if gap <= 0 {
		gap = defaultRequestGap
	}
	return &requestSlots{
		gap:      gap,
		indexers: make(map[int]*slotState),
		recorder: recorder,
		publish:  publish,
	}
}

// slotTicket is the right to send one request. The caller reports the answer
// of the indexer with done.
type slotTicket struct {
	slots   *requestSlots
	indexer *models.TorznabIndexer
	kind    requestKind
	holds   bool
	freed   bool
}

// acquire returns a ticket for one request to indexer. A paced kind waits
// until the Request slot is free. It returns a *RateLimitError at once during
// a Rate-limit cooldown, and the context error when ctx ends first.
func (r *requestSlots) acquire(ctx context.Context, indexer *models.TorznabIndexer, kind requestKind) (*slotTicket, error) {
	for {
		r.mu.Lock()
		if err := ctx.Err(); err != nil {
			r.mu.Unlock()
			return nil, err
		}
		ticket, wait, freed, err := r.askLocked(indexer, kind, time.Now())
		r.mu.Unlock()
		if ticket != nil || err != nil {
			return ticket, err
		}

		timer := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-freed:
		case <-timer.C:
		}
		timer.Stop()
	}
}

func (r *requestSlots) setOnFree(onFree func()) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onFree = onFree
}

// tryAcquire is acquire without the wait. When the slot is not free it returns
// no ticket and the shortest time until the slot can be free.
func (r *requestSlots) tryAcquire(indexer *models.TorznabIndexer, kind requestKind) (*slotTicket, time.Duration, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	ticket, wait, _, err := r.askLocked(indexer, kind, time.Now())
	return ticket, wait, err
}

// refusal returns the *RateLimitError that an ask of kind gets now, or nil.
// It takes no slot.
func (r *requestSlots) refusal(indexer *models.TorznabIndexer, kind requestKind) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.refusalLocked(indexer, kind, time.Now())
}

func (r *requestSlots) refusalLocked(indexer *models.TorznabIndexer, kind requestKind, now time.Time) error {
	scope := kind.scope()
	if until := r.stateLocked(indexer.ID).cooldowns[scope]; until.After(now) {
		return &RateLimitError{IndexerID: indexer.ID, IndexerName: indexer.Name, Scope: scope, RetryAt: until}
	}
	return nil
}

func (r *requestSlots) askLocked(indexer *models.TorznabIndexer, kind requestKind, now time.Time) (*slotTicket, time.Duration, <-chan struct{}, error) {
	if err := r.refusalLocked(indexer, kind, now); err != nil {
		return nil, 0, nil, err
	}
	state := r.stateLocked(indexer.ID)
	if !kind.holdsSlot(indexer) {
		return &slotTicket{slots: r, indexer: indexer, kind: kind}, 0, nil, nil
	}
	if state.busy {
		return nil, r.gap, state.freed, nil
	}
	if !state.lastAnswer.IsZero() {
		if next := state.lastAnswer.Add(r.gap); next.After(now) {
			return nil, next.Sub(now), nil, nil
		}
	}
	state.busy = true
	state.freed = make(chan struct{})
	return &slotTicket{slots: r, indexer: indexer, kind: kind, holds: true}, 0, nil, nil
}

func (r *requestSlots) stateLocked(indexerID int) *slotState {
	state, ok := r.indexers[indexerID]
	if !ok {
		state = &slotState{cooldowns: make(map[string]time.Time)}
		r.indexers[indexerID] = state
	}
	return state
}

// done gives the ticket the answer of the indexer, or the error that ended
// the request. The first call frees the slot and starts the Request gap. A 429
// at any call starts the Rate-limit cooldown of the kind, starts the record of
// the error on the indexer, and returns a *RateLimitError. Any other err comes
// back as is.
func (t *slotTicket) done(ctx context.Context, err error) error {
	r := t.slots
	retryAfter, rateLimited := detectRateLimit(err)

	r.mu.Lock()
	now := time.Now()
	state := r.stateLocked(t.indexer.ID)
	freedSlot := t.holds && !t.freed
	if freedSlot {
		t.freed = true
		state.busy = false
		state.lastAnswer = now
		close(state.freed)
	}
	scope := t.kind.scope()
	retryAt := now.Add(retryAfter)
	if rateLimited {
		if until := state.cooldowns[scope]; until.After(retryAt) {
			retryAt = until
		} else {
			state.cooldowns[scope] = retryAt
		}
	}
	onFree := r.onFree
	r.mu.Unlock()

	if freedSlot && onFree != nil {
		onFree()
	}
	if !rateLimited {
		return err
	}
	return r.recordRateLimit(ctx, t.indexer, scope, retryAfter, retryAt, err)
}

func (r *requestSlots) recordRateLimit(ctx context.Context, indexer *models.TorznabIndexer, scope string, retryAfter time.Duration, retryAt time.Time, cause error) *RateLimitError {
	localRetryAt := retryAt.In(time.Local)
	if r.recorder != nil {
		message := fmt.Sprintf("Rate limit triggered for %s %s requests until %s", indexer.Name, scope, localRetryAt.Format(time.RFC3339))
		// A late 429 comes after the caller's context ended, and it is still recorded.
		recordCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), rateLimitRecordTimeout)
		go func() {
			defer cancel()
			if err := r.recorder.RecordError(recordCtx, indexer.ID, message, "rate_limit"); err != nil {
				log.Debug().Err(err).Int("indexer_id", indexer.ID).Msg("Failed to record rate-limit error")
			}
		}()
	}

	log.Warn().
		Int("indexer_id", indexer.ID).
		Str("indexer", indexer.Name).
		Str("scope", scope).
		Dur("retry_after", retryAfter).
		Time("retry_at", localRetryAt).
		Err(cause).
		Msg("Rate limit applied to indexer")

	if r.publish != nil {
		r.publish()
	}

	return &RateLimitError{IndexerID: indexer.ID, IndexerName: indexer.Name, Scope: scope, RetryAt: retryAt}
}

// cooldownIndexers returns the end of each Rate-limit cooldown of scope that
// has not ended, by indexer ID.
func (r *requestSlots) cooldownIndexers(scope string) map[int]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	cooldowns := make(map[int]time.Time)
	for indexerID, state := range r.indexers {
		if until := state.cooldowns[scope]; until.After(now) {
			cooldowns[indexerID] = until
		}
	}
	return cooldowns
}
