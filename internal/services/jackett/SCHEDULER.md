# Torznab Search Scheduler

This document explains the current scheduler implementation for coordinating Torznab searches across multiple indexers with rate limiting.

## Overview

The scheduler uses a **fully async, job-based model**. When a search is submitted:

1. `Submit()` returns immediately with a job ID
2. Tasks are queued and dispatched based on rate limits and priority
3. Results are delivered via callbacks (`OnComplete` per indexer, `OnJobDone` when all finish)

The key insight is that rate limit checking happens at **dispatch time**, not inside workers. This allows the scheduler to execute ready indexers immediately while deferring blocked ones for later retry.

## Architecture

```
┌─────────────────────────────────────────────────────────────────────┐
│                           Submit()                                  │
│  - Creates tasks for each indexer                                   │
│  - Registers job state for completion tracking                      │
│  - Sends tasks to submitCh (non-blocking)                           │
│  - Returns immediately with jobID                                   │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                            loop()                                   │
│  - Single goroutine event loop                                      │
│  - Handles: submitCh, completeCh, stopCh, coalesce timer            │
│  - Coalesces rapid submissions (5ms delay) to batch dispatch        │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        enqueueTasks()                               │
│  - Pushes tasks to priority heap                                    │
│  - Priority: interactive(0) > RSS(1) > completion(2) > background(3)│
│  - Within same priority: FIFO by creation time                      │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        dispatchTasks()                              │
│  For each task in priority order:                                   │
│  1. Context cancelled? → complete with error                        │
│  2. Indexer already in-flight? → re-queue (blocked)                 │
│  3. NextWait() > MaxWait? → complete with RateLimitWaitError (skip) │
│  4. NextWait() > 0? → re-queue (blocked)                            │
│  5. Worker available? → execute                                     │
│  6. No worker? → re-queue (blocked)                                 │
│                                                                     │
│  After: schedule retry timer for earliest blocked task              │
└─────────────────────────────────────────────────────────────────────┘
                                    │
                                    ▼
┌─────────────────────────────────────────────────────────────────────┐
│                        executeTask()                                │
│  - Runs in goroutine (limited by worker pool semaphore)             │
│  - RecordRequest() BEFORE execution (reserves rate limit slot)      │
│  - Executes the actual search                                       │
│  - Calls OnComplete callback with results                           │
│  - Tracks job completion, calls OnJobDone when all tasks done       │
│  - Releases worker, signals completeCh to trigger next dispatch     │
└─────────────────────────────────────────────────────────────────────┘
```

## Key Components

### RateLimiter

Located at the top of `scheduler.go`. Tracks per-indexer request timing and enforces rate limits.

**State tracked per indexer:**
- `lastRequest` - timestamp of most recent request
- `cooldownUntil` - external cooldown (e.g., from 429 response)
- `hourlyRequests` - sliding window for hourly limit
- `dailyRequests` - sliding window for daily limit

**Key methods:**
- `NextWait(indexer, opts)` - Returns how long until indexer can be called (0 = ready now)
- `RecordRequest(indexerID, ts)` - Records that a request was made (called before execution)
- `SetCooldown(indexerID, until)` - Sets external cooldown (e.g., Retry-After header)

**Priority multipliers** reduce the minimum interval for higher-priority requests:
```go
RateLimitPriorityInteractive: 0.1  // 10% of base interval
RateLimitPriorityRSS:         0.5  // 50% of base interval
RateLimitPriorityCompletion:  0.7  // 70% of base interval
RateLimitPriorityBackground:  1.0  // Full base interval (default 60s)
```

### Priority Heap

Tasks are stored in a min-heap ordered by:
1. Priority (lower = higher priority)
2. Creation time (earlier = higher priority within same level)

This ensures interactive searches always run before RSS, which run before background tasks.

### Worker Pool

A semaphore (`chan struct{}` with capacity `maxWorkers`, default 10) limits concurrent executions. When a worker slot is unavailable, tasks are re-queued.

### Job State Tracking

Each job (which may span multiple indexers) is tracked in `jobs map[uint64]*jobState`:
```go
type jobState struct {
    totalTasks     int         // Number of indexers in this job
    completedTasks int         // How many have finished
    callbacks      JobCallbacks // To call OnJobDone when complete
}
```

When `completedTasks >= totalTasks`, `OnJobDone` is called and the job is cleaned up.

## Request Lifecycle

### 1. Submission

```go
jobID, err := scheduler.Submit(ctx, SubmitRequest{
    Indexers:  indexers,
    Params:    searchParams,
    Meta:      &searchContext{rateLimit: &RateLimitOptions{Priority: RateLimitPriorityRSS}},
    Callbacks: JobCallbacks{
        OnComplete: func(jobID uint64, indexer *models.TorznabIndexer, results []Result, coverage []int, err error) {
            // Called for each indexer when it finishes
        },
        OnJobDone: func(jobID uint64) {
            // Called once when ALL indexers are done
        },
    },
    ExecFn: service.executeIndexerSearch,
})
// Returns immediately - does NOT wait for results
```

### 2. Dispatch Decision

For each queued task, the scheduler checks:

```go
wait := rateLimiter.NextWait(indexer, rateOpts)
maxWait := getMaxWait(item)  // From RateLimitOptions.MaxWait

if maxWait > 0 && wait > maxWait {
    // SKIP: Would wait too long, return error immediately
    OnComplete(jobID, indexer, nil, nil, &RateLimitWaitError{...})
}

if wait > 0 {
    // BLOCKED: Re-queue, will retry when rate limit expires
    heap.Push(&taskQueue, item)
}

// READY: Execute now
```

### 3. Execution

When a task executes:

1. **Reserve slot**: `RecordRequest()` is called BEFORE the search runs
2. **Search**: The actual HTTP request to the indexer
3. **Callback**: `OnComplete` fires with results (in a goroutine)
4. **Cleanup**: Worker released, in-flight tracking cleared
5. **Chain**: `completeCh` signaled to potentially unblock more tasks

### 4. Retry Timer

If tasks are blocked waiting for rate limits, a timer is scheduled for the earliest unblock time:

```go
// Find minimum wait among all blocked tasks
minWait := findMinWait(taskQueue)
time.AfterFunc(minWait, func() {
    completeCh <- struct{}{}  // Triggers dispatchTasks()
})
```

This ensures blocked tasks are retried as soon as their rate limit window opens.

## RSS Deduplication

RSS searches use `pendingRSS map[int]struct{}` to prevent duplicate submissions for the same indexer:

```go
// On submit (for RSS priority tasks):
if _, exists := s.pendingRSS[idx.ID]; exists {
    continue  // Skip - already have pending RSS for this indexer
}
s.pendingRSS[idx.ID] = struct{}{}

// On completion:
delete(s.pendingRSS, task.indexer.ID)
```

This prevents RSS starvation where rapid submissions could queue up many identical requests.

## Error Handling

### Rate Limit Exceeded (MaxWait)

When `NextWait() > MaxWait`, a `RateLimitWaitError` is returned via `OnComplete`:

```go
type RateLimitWaitError struct {
    IndexerID   int
    IndexerName string
    Wait        time.Duration  // How long we'd need to wait
    MaxWait     time.Duration  // Our budget
    Priority    RateLimitPriority
}
```

The indexer is skipped for this search but NOT put in cooldown.

### Context Cancellation

If the request context is cancelled, tasks are completed with `ctx.Err()`.

### Panics

Worker panics are recovered and converted to errors via `OnComplete`. The worker pool slot is still released to prevent deadlock.

## Configuration

```go
const (
    defaultMinRequestInterval = 60 * time.Second  // Base rate limit interval
    dispatchCoalesceDelay     = 5 * time.Millisecond
    defaultMaxWorkers         = 10

    // MaxWait defaults by priority (applied when RateLimitOptions.MaxWait is 0)
    rssMaxWait        = 15 * time.Second
    backgroundMaxWait = 60 * time.Second
    // Completion and Interactive: no limit (0) - will queue and wait as long as needed
)
```

The `getMaxWait()` function applies these defaults based on priority when no explicit MaxWait is provided:

```go
func (s *searchScheduler) getMaxWait(item *taskItem) time.Duration {
    // Explicit MaxWait takes precedence
    if item.task.meta != nil && item.task.meta.rateLimit != nil && item.task.meta.rateLimit.MaxWait > 0 {
        return item.task.meta.rateLimit.MaxWait
    }

    // Apply priority-based defaults
    if item.task.meta != nil && item.task.meta.rateLimit != nil {
        switch item.task.meta.rateLimit.Priority {
        case RateLimitPriorityRSS:
            return rssMaxWait         // 15s
        case RateLimitPriorityCompletion:
            return 0                  // No limit - queues for batch completions
        case RateLimitPriorityBackground:
            return backgroundMaxWait  // 60s
        case RateLimitPriorityInteractive:
            return 0                  // No limit
        }
    }
    return 0  // No meta/rateLimit = no limit
}
```

## Thread Safety

- All scheduler state is protected by `s.mu`
- Callbacks run in separate goroutines (safe to do blocking work)
- The event loop is single-threaded (submitCh/completeCh/stopCh)
- Worker execution is concurrent (up to maxWorkers)

## Testing

See `scheduler_test.go` for tests covering:
- Basic submission and callback delivery
- Priority ordering
- Rate limit blocking and retry
- MaxWait skip behavior (explicit and priority-based defaults)
- Concurrent execution limits
- RSS deduplication
- Panic recovery
- Context cancellation
- Error propagation through callbacks
