package jackett

import (
	"context"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/models"
)

const (
	defaultMinRequestInterval = 2 * time.Second
)

type indexerRateState struct {
	lastRequest    time.Time
	cooldownUntil  time.Time
	hourlyRequests []time.Time
	dailyRequests  []time.Time
}

type RateLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	states      map[int]*indexerRateState
}

func NewRateLimiter(minInterval time.Duration) *RateLimiter {
	if minInterval <= 0 {
		minInterval = defaultMinRequestInterval
	}
	return &RateLimiter{
		minInterval: minInterval,
		states:      make(map[int]*indexerRateState),
	}
}

func (r *RateLimiter) BeforeRequest(ctx context.Context, indexer *models.TorznabIndexer) error {
	if indexer == nil {
		return nil
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	for {
		now := time.Now()
		wait := r.computeWaitLocked(indexer, now)
		if wait <= 0 {
			r.recordLocked(indexer.ID, now)
			return nil
		}

		timer := time.NewTimer(wait)
		r.mu.Unlock()
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			r.mu.Lock()
			return ctx.Err()
		case <-timer.C:
			r.mu.Lock()
		}
	}
}

func (r *RateLimiter) RecordRequest(indexerID int, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if ts.IsZero() {
		ts = time.Now()
	}
	r.recordLocked(indexerID, ts)
}

func (r *RateLimiter) SetCooldown(indexerID int, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	if until.After(state.cooldownUntil) {
		state.cooldownUntil = until
	}
}

// LoadCooldowns seeds the rate limiter with pre-existing cooldown windows.
func (r *RateLimiter) LoadCooldowns(cooldowns map[int]time.Time) {
	if len(cooldowns) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for indexerID, until := range cooldowns {
		if until.IsZero() {
			continue
		}
		state := r.getStateLocked(indexerID)
		if until.After(state.cooldownUntil) {
			state.cooldownUntil = until
		}
	}
}

func (r *RateLimiter) ClearCooldown(indexerID int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	state.cooldownUntil = time.Time{}
}

// IsInCooldown checks if an indexer is currently in cooldown without blocking
func (r *RateLimiter) IsInCooldown(indexerID int) (bool, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	now := time.Now()
	if !state.cooldownUntil.IsZero() && state.cooldownUntil.After(now) {
		return true, state.cooldownUntil
	}
	return false, time.Time{}
}

// GetCooldownIndexers returns a list of indexer IDs that are currently in cooldown
func (r *RateLimiter) GetCooldownIndexers() map[int]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	cooldowns := make(map[int]time.Time)
	now := time.Now()
	for indexerID, state := range r.states {
		if !state.cooldownUntil.IsZero() && state.cooldownUntil.After(now) {
			cooldowns[indexerID] = state.cooldownUntil
		}
	}
	return cooldowns
}

func (r *RateLimiter) computeWaitLocked(indexer *models.TorznabIndexer, now time.Time) time.Duration {
	state := r.getStateLocked(indexer.ID)
	r.pruneLocked(state, now)

	var wait time.Duration

	if !state.cooldownUntil.IsZero() && state.cooldownUntil.After(now) {
		wait = state.cooldownUntil.Sub(now)
	}

	if r.minInterval > 0 && !state.lastRequest.IsZero() {
		next := state.lastRequest.Add(r.minInterval)
		if next.After(now) {
			delay := next.Sub(now)
			if delay > wait {
				wait = delay
			}
		}
	}

	if limit := derefLimit(indexer.HourlyRequestLimit); limit > 0 {
		if len(state.hourlyRequests) >= limit {
			oldest := state.hourlyRequests[0]
			readyAt := oldest.Add(time.Hour)
			if readyAt.After(now) {
				delay := readyAt.Sub(now)
				if delay > wait {
					wait = delay
				}
			}
		}
	}

	if limit := derefLimit(indexer.DailyRequestLimit); limit > 0 {
		if len(state.dailyRequests) >= limit {
			oldest := state.dailyRequests[0]
			readyAt := oldest.Add(24 * time.Hour)
			if readyAt.After(now) {
				delay := readyAt.Sub(now)
				if delay > wait {
					wait = delay
				}
			}
		}
	}

	return wait
}

func (r *RateLimiter) pruneLocked(state *indexerRateState, now time.Time) {
	cutoffHour := now.Add(-1 * time.Hour)
	cutoffDay := now.Add(-24 * time.Hour)

	idx := 0
	for _, ts := range state.hourlyRequests {
		if ts.After(cutoffHour) {
			break
		}
		idx++
	}
	if idx > 0 {
		state.hourlyRequests = append([]time.Time(nil), state.hourlyRequests[idx:]...)
	}

	idx = 0
	for _, ts := range state.dailyRequests {
		if ts.After(cutoffDay) {
			break
		}
		idx++
	}
	if idx > 0 {
		state.dailyRequests = append([]time.Time(nil), state.dailyRequests[idx:]...)
	}
}

func (r *RateLimiter) getStateLocked(indexerID int) *indexerRateState {
	state, ok := r.states[indexerID]
	if !ok {
		state = &indexerRateState{}
		r.states[indexerID] = state
	}
	return state
}

func (r *RateLimiter) recordLocked(indexerID int, ts time.Time) {
	state := r.getStateLocked(indexerID)
	state.lastRequest = ts
	state.hourlyRequests = append(state.hourlyRequests, ts)
	state.dailyRequests = append(state.dailyRequests, ts)
	r.pruneLocked(state, ts)
}

func derefLimit(limit *int) int {
	if limit == nil {
		return 0
	}
	if *limit < 0 {
		return 0
	}
	return *limit
}
