// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"io"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

// timedSearcher answers each search after a fixed delay and records when each
// search started and completed. A zero delay stands for a cached answer. The
// mutex keeps the record whole when two runs search at once without the lock.
type timedSearcher struct {
	mu        sync.Mutex
	delay     time.Duration
	responses []*jackett.SearchResponse
	queries   []string
	starts    []time.Time
	ends      []time.Time
}

func (s *timedSearcher) SearchWithScope(_ context.Context, req *jackett.TorznabSearchRequest, _ string) error {
	s.mu.Lock()
	s.starts = append(s.starts, time.Now())
	s.queries = append(s.queries, req.Query)
	pass := len(s.starts) - 1
	s.mu.Unlock()
	response := &jackett.SearchResponse{RequestedIndexerIDs: []int{1}, CoveredIndexerIDs: []int{1}}
	if pass < len(s.responses) {
		response = s.responses[pass]
	}
	go func() {
		time.Sleep(s.delay)
		s.mu.Lock()
		s.ends = append(s.ends, time.Now())
		s.mu.Unlock()
		req.OnAllComplete(response, nil)
	}()
	return nil
}

// intervalTestService takes a parser built outside the synctest bubble: the
// parser's cache starts a goroutine that never exits.
func intervalTestService(parser *Parser, searcher *timedSearcher) *Service {
	return &Service{parser: parser, searcher: NewSearcher(searcher, parser)}
}

func runIntervalSearchee(ctx context.Context, svc *Service, name string, enabled map[int]struct{}) searcheeOutcome {
	return runIntervalSearcheeIfPending(ctx, svc, name, enabled, nil)
}

func runIntervalSearcheeIfPending(ctx context.Context, svc *Service, name string, enabled map[int]struct{}, stillPending func() bool) searcheeOutcome {
	l := zerolog.New(io.Discard)
	searchee := &Searchee{
		Name:  name,
		Path:  "/data/" + name,
		Files: []*ScannedFile{{Path: "/data/" + name + "/video.mkv", RelPath: "video.mkv", Size: 1000}},
	}
	settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict}
	_, outcome := svc.processSearchee(ctx, &models.DirScanDirectory{ID: 1}, searchee, settings, NewMatcher(MatchModeStrict, 0), 1, enabled, stillPending, &l)
	return outcome
}

func TestProcessSearchee_SearchInterval(t *testing.T) {
	t.Parallel()

	enabled := map[int]struct{}{1: {}}
	parser := NewParser(nil)

	t.Run("the next item waits the interval after the previous item's search ends", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{delay: 5 * time.Second}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)
			first := len(searcher.starts)
			runIntervalSearchee(t.Context(), svc, "Other.Movie.2023.1080p.WEB-DL", enabled)

			require.Greater(t, len(searcher.starts), first)
			require.Equal(t, 60*time.Second, searcher.starts[first].Sub(searcher.ends[first-1]))
		})
	})

	t.Run("a cached answer still makes the next item wait", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)
			first := len(searcher.starts)
			runIntervalSearchee(t.Context(), svc, "Other.Movie.2023.1080p.WEB-DL", enabled)

			require.Equal(t, crossseed.MinTorznabSearchInterval, searcher.starts[first].Sub(searcher.ends[first-1]))
		})
	})

	t.Run("the first item does not wait and returns when its last search ends", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			start := time.Now()
			searcher := &timedSearcher{delay: 5 * time.Second}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)

			require.Equal(t, start, searcher.starts[0])
			require.Equal(t, searcher.ends[len(searcher.ends)-1], time.Now(), "the item returns as soon as its last search ends")
		})
	})

	t.Run("the retry searches for one item run back to back", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{delay: 5 * time.Second}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)

			require.GreaterOrEqual(t, len(searcher.starts), 2, "an empty answer makes the yearless retry")
			require.Equal(t, searcher.ends[0], searcher.starts[1])
		})
	})

	t.Run("an item that sends no search does not wait or move the time of the last search", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)
			first := len(searcher.starts)
			time.Sleep(20 * time.Second)

			before := time.Now()
			runIntervalSearchee(t.Context(), svc, "No.Indexers.2022.1080p.WEB-DL", nil)
			require.Equal(t, before, time.Now())
			require.Len(t, searcher.starts, first)

			runIntervalSearchee(t.Context(), svc, "Other.Movie.2023.1080p.WEB-DL", enabled)
			require.Equal(t, crossseed.MinTorznabSearchInterval, searcher.starts[first].Sub(searcher.ends[first-1]))
		})
	})

	t.Run("a run that starts soon after the previous run waits the rest of the interval", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)
			first := len(searcher.starts)
			time.Sleep(20 * time.Second)

			before := time.Now()
			runIntervalSearchee(t.Context(), svc, "Other.Movie.2023.1080p.WEB-DL", enabled)

			require.Equal(t, 40*time.Second, searcher.starts[first].Sub(before))
		})
	})

	t.Run("two runs at once search one item at a time with the interval between them", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{delay: 5 * time.Second}
			svc := intervalTestService(parser, searcher)

			var wg sync.WaitGroup
			for _, name := range []string{"Example.Movie.2024.1080p.WEB-DL", "Other.Movie.2023.1080p.WEB-DL"} {
				wg.Go(func() { runIntervalSearchee(t.Context(), svc, name, enabled) })
			}
			wg.Wait()

			perItem := len(searcher.starts) / 2
			require.Equal(t, 60*time.Second, searcher.starts[perItem].Sub(searcher.ends[perItem-1]))
		})
	})

	t.Run("cancel while another run searches returns at once without a search", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{delay: 30 * time.Second}
			svc := intervalTestService(parser, searcher)

			other, stopOther := context.WithCancel(t.Context())
			done := make(chan struct{})
			go func() {
				runIntervalSearchee(other, svc, "Example.Movie.2024.1080p.WEB-DL", enabled)
				close(done)
			}()
			synctest.Wait()

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			before := time.Now()
			outcome := runIntervalSearchee(ctx, svc, "Other.Movie.2023.1080p.WEB-DL", enabled)
			stopOther()
			<-done

			require.Equal(t, 10*time.Second, time.Since(before))
			require.Equal(t, searcheeOutcome{}, outcome)
			require.Len(t, searcher.starts, 1, "only the other run searched")
			time.Sleep(searcher.delay) // let the abandoned search answer before the bubble ends
		})
	})

	t.Run("cancel during the wait returns at once without a search", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "Example.Movie.2024.1080p.WEB-DL", enabled)
			first := len(searcher.starts)

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			before := time.Now()
			outcome := runIntervalSearchee(ctx, svc, "Other.Movie.2023.1080p.WEB-DL", enabled)

			require.Equal(t, 10*time.Second, time.Since(before))
			require.Equal(t, searcheeOutcome{}, outcome)
			require.Len(t, searcher.starts, first)
		})
	})

	t.Run("a waiting webhook item searches before a full-scan item", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "First.Item.1080p.WEB-DL", enabled)

			// Both items become free to search at the same instant, so each round is
			// a tie. Without the priority rule a round goes either way.
			for round := range 8 {
				lastEnd := time.Now()
				full := func() { runIntervalSearchee(t.Context(), svc, "Full.Item.1080p.WEB-DL", enabled) }
				webhook := func() { runIntervalSearchee(withWebhookRun(t.Context()), svc, "Webhook.Item.1080p.WEB-DL", enabled) }
				if round%2 == 1 {
					full, webhook = webhook, full
				}

				var wg sync.WaitGroup
				wg.Go(full)
				time.Sleep(10 * time.Second)
				wg.Go(webhook)
				wg.Wait()

				n := len(searcher.queries)
				require.Equal(t, []string{"Webhook Item", "Full Item"}, searcher.queries[n-2:], "round %d", round)
				require.Equal(t, 60*time.Second, searcher.starts[n-2].Sub(lastEnd), "the webhook item waits only the rest of the interval")
			}
		})
	})

	t.Run("a canceled webhook item does not hold back a full-scan item", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "First.Item.1080p.WEB-DL", enabled)
			lastEnd := time.Now()

			webhookCtx, cancel := context.WithTimeout(withWebhookRun(t.Context()), 10*time.Second)
			defer cancel()
			var wg sync.WaitGroup
			wg.Go(func() { runIntervalSearchee(webhookCtx, svc, "Webhook.Item.1080p.WEB-DL", enabled) })
			wg.Go(func() { runIntervalSearchee(t.Context(), svc, "Full.Item.1080p.WEB-DL", enabled) })
			wg.Wait()

			require.Equal(t, []string{"First Item", "Full Item"}, searcher.queries)
			require.Equal(t, 60*time.Second, searcher.starts[1].Sub(lastEnd))
		})
	})

	t.Run("an item that another run finished during the wait skips its search and keeps the interval", func(t *testing.T) {
		t.Parallel()
		synctest.Test(t, func(t *testing.T) {
			searcher := &timedSearcher{}
			svc := intervalTestService(parser, searcher)

			runIntervalSearchee(t.Context(), svc, "First.Item.1080p.WEB-DL", enabled)
			lastEnd := time.Now()

			var checkedAt time.Time
			outcome := runIntervalSearcheeIfPending(t.Context(), svc, "Finished.Item.1080p.WEB-DL", enabled, func() bool {
				checkedAt = time.Now()
				return false
			})
			require.Equal(t, searcheeOutcome{}, outcome)
			require.Equal(t, 60*time.Second, checkedAt.Sub(lastEnd), "the item checks its state after its turn comes, not before the wait")

			runIntervalSearchee(t.Context(), svc, "Next.Item.1080p.WEB-DL", enabled)
			require.Equal(t, []string{"First Item", "Next Item"}, searcher.queries)
			require.Equal(t, 60*time.Second, searcher.starts[1].Sub(lastEnd), "the skipped item does not restart the interval")
		})
	})
}
