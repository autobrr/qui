package jackett

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/autobrr/qui/internal/models"
)

const (
	defaultMinRequestInterval = 60 * time.Second
)

var priorityMultipliers = map[RateLimitPriority]float64{
	RateLimitPriorityInteractive: 0.1,
	RateLimitPriorityRSS:         0.5,
	RateLimitPriorityCompletion:  0.7,
	RateLimitPriorityBackground:  1.0,
}

type RateLimitPriority string

const (
	RateLimitPriorityInteractive RateLimitPriority = "interactive"
	RateLimitPriorityRSS         RateLimitPriority = "rss"
	RateLimitPriorityCompletion  RateLimitPriority = "completion"
	RateLimitPriorityBackground  RateLimitPriority = "background"
)

type RateLimitOptions struct {
	Priority    RateLimitPriority
	MinInterval time.Duration
	MaxWait     time.Duration
}

type RateLimitWaitError struct {
	IndexerID   int
	IndexerName string
	Wait        time.Duration
	MaxWait     time.Duration
	Priority    RateLimitPriority
}

func (e *RateLimitWaitError) Error() string {
	indexer := fmt.Sprintf("indexer %d", e.IndexerID)
	if e.IndexerName != "" {
		indexer = fmt.Sprintf("%s (%d)", e.IndexerName, e.IndexerID)
	}
	return fmt.Sprintf("%s blocked by torznab rate limit: requires %s wait but maximum allowed is %s", indexer, e.Wait, e.MaxWait)
}

func (e *RateLimitWaitError) Is(target error) bool {
	_, ok := target.(*RateLimitWaitError)
	return ok
}

type indexerRateState struct {
	lastRequest    time.Duration
	cooldownUntil  time.Duration
	hourlyRequests []time.Duration
	dailyRequests  []time.Duration
}

type RateLimiter struct {
	mu          sync.Mutex
	minInterval time.Duration
	states      map[int]*indexerRateState
	startTime   time.Time
}

func NewRateLimiter(minInterval time.Duration) *RateLimiter {
	if minInterval <= 0 {
		minInterval = defaultMinRequestInterval
	}
	return &RateLimiter{
		minInterval: minInterval,
		states:      make(map[int]*indexerRateState),
		startTime:   time.Now(),
	}
}

func (r *RateLimiter) BeforeRequest(ctx context.Context, indexer *models.TorznabIndexer, opts *RateLimitOptions) error {
	if indexer == nil {
		return nil
	}

	cfg := r.resolveOptions(opts)

	r.mu.Lock()
	defer r.mu.Unlock()

	for {
		now := time.Since(r.startTime)
		wait := r.computeWaitLocked(indexer, now, cfg.MinInterval)
		if wait <= 0 {
			r.recordLocked(indexer.ID, now)
			return nil
		}

		if cfg.MaxWait > 0 && wait > cfg.MaxWait {
			return &RateLimitWaitError{
				IndexerID:   indexer.ID,
				IndexerName: indexer.Name,
				Wait:        wait,
				MaxWait:     cfg.MaxWait,
				Priority:    cfg.Priority,
			}
		}

		timer := time.NewTimer(wait)
		r.mu.Unlock()
		if cfg.Priority == RateLimitPriorityRSS {
			<-timer.C
			r.mu.Lock()
		} else {
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
}

func (r *RateLimiter) RecordRequest(indexerID int, ts time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	var dur time.Duration
	if ts.IsZero() {
		dur = time.Since(r.startTime)
	} else {
		dur = ts.Sub(r.startTime)
	}
	r.recordLocked(indexerID, dur)
}

func (r *RateLimiter) SetCooldown(indexerID int, until time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	cooldownDur := until.Sub(r.startTime)
	if cooldownDur > state.cooldownUntil {
		state.cooldownUntil = cooldownDur
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
		cooldownDur := until.Sub(r.startTime)
		if cooldownDur > state.cooldownUntil {
			state.cooldownUntil = cooldownDur
		}
	}
}

func (r *RateLimiter) ClearCooldown(indexerID int) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	state.cooldownUntil = 0
}

// IsInCooldown checks if an indexer is currently in cooldown without blocking
func (r *RateLimiter) IsInCooldown(indexerID int) (bool, time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()

	state := r.getStateLocked(indexerID)
	now := time.Since(r.startTime)
	if state.cooldownUntil > 0 && state.cooldownUntil > now {
		return true, r.startTime.Add(state.cooldownUntil)
	}
	return false, time.Time{}
}

// GetCooldownIndexers returns a list of indexer IDs that are currently in cooldown
func (r *RateLimiter) GetCooldownIndexers() map[int]time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()

	cooldowns := make(map[int]time.Time)
	now := time.Since(r.startTime)
	for indexerID, state := range r.states {
		if state.cooldownUntil > 0 && state.cooldownUntil > now {
			cooldowns[indexerID] = r.startTime.Add(state.cooldownUntil)
		}
	}
	return cooldowns
}

func (r *RateLimiter) computeWaitLocked(indexer *models.TorznabIndexer, now time.Duration, minInterval time.Duration) time.Duration {
	if minInterval <= 0 {
		minInterval = r.minInterval
	}
	state := r.getStateLocked(indexer.ID)
	r.pruneLocked(state, now)

	var wait time.Duration

	if state.cooldownUntil > 0 && state.cooldownUntil > now {
		wait = state.cooldownUntil - now
	}

	if minInterval > 0 && state.lastRequest >= 0 {
		next := state.lastRequest + minInterval
		if next > now {
			delay := next - now
			if delay > wait {
				wait = delay
			}
		}
	}

	if limit := derefLimit(indexer.HourlyRequestLimit); limit > 0 {
		if len(state.hourlyRequests) >= limit {
			oldest := state.hourlyRequests[0]
			readyAt := oldest + time.Hour
			if readyAt > now {
				delay := readyAt - now
				if delay > wait {
					wait = delay
				}
			}
		}
	}

	if limit := derefLimit(indexer.DailyRequestLimit); limit > 0 {
		if len(state.dailyRequests) >= limit {
			oldest := state.dailyRequests[0]
			readyAt := oldest + 24*time.Hour
			if readyAt > now {
				delay := readyAt - now
				if delay > wait {
					wait = delay
				}
			}
		}
	}

	return wait
}

func (r *RateLimiter) pruneLocked(state *indexerRateState, now time.Duration) {
	cutoffHour := now - 1*time.Hour
	cutoffDay := now - 24*time.Hour

	idx := 0
	for _, ts := range state.hourlyRequests {
		if ts > cutoffHour {
			break
		}
		idx++
	}
	if idx > 0 {
		state.hourlyRequests = state.hourlyRequests[idx:]
	}

	idx = 0
	for _, ts := range state.dailyRequests {
		if ts > cutoffDay {
			break
		}
		idx++
	}
	if idx > 0 {
		state.dailyRequests = state.dailyRequests[idx:]
	}
}

func (r *RateLimiter) getStateLocked(indexerID int) *indexerRateState {
	state, ok := r.states[indexerID]
	if !ok {
		state = &indexerRateState{lastRequest: -1}
		r.states[indexerID] = state
	}
	return state
}

func (r *RateLimiter) recordLocked(indexerID int, ts time.Duration) {
	state := r.getStateLocked(indexerID)
	state.lastRequest = ts
	state.hourlyRequests = append(state.hourlyRequests, ts)
	state.dailyRequests = append(state.dailyRequests, ts)
	r.pruneLocked(state, ts)
}

// NextWait returns the amount of time the caller would need to wait before a request could be made
// against the provided indexer using the supplied options. This is a non-blocking helper used by
// the job scheduler to decide if a request can run immediately.
func (r *RateLimiter) NextWait(indexer *models.TorznabIndexer, opts *RateLimitOptions) time.Duration {
	if indexer == nil {
		return 0
	}
	cfg := r.resolveOptions(opts)
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Since(r.startTime)
	return r.computeWaitLocked(indexer, now, cfg.MinInterval)
}

func (r *RateLimiter) resolveOptions(opts *RateLimitOptions) RateLimitOptions {
	cfg := RateLimitOptions{
		Priority:    RateLimitPriorityBackground,
		MinInterval: r.minInterval,
	}

	if opts != nil {
		if opts.Priority != "" {
			cfg.Priority = opts.Priority
		}
		if opts.MinInterval > 0 {
			cfg.MinInterval = opts.MinInterval
		}
		if opts.MaxWait > 0 {
			cfg.MaxWait = opts.MaxWait
		}
	}

	if cfg.MinInterval <= 0 {
		cfg.MinInterval = defaultMinRequestInterval
	}

	if multiplier, ok := priorityMultipliers[cfg.Priority]; ok {
		cfg.MinInterval = time.Duration(float64(cfg.MinInterval) * multiplier)
	}

	return cfg
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
