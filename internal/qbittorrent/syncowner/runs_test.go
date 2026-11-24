// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package syncowner_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent/syncowner"
	"github.com/autobrr/qui/internal/testutil"
)

type syncRequests struct {
	mu      sync.Mutex
	started time.Time
	at      []time.Duration
	takes   time.Duration
	fails   bool
}

func (s *syncRequests) RoundTrip(*http.Request) (*http.Response, error) {
	s.mu.Lock()
	s.at = append(s.at, time.Since(s.started))
	takes, fails := s.takes, s.fails
	s.mu.Unlock()
	<-time.After(takes)
	rec := httptest.NewRecorder()
	answer := `{"rid":1,"full_update":true}`
	if fails {
		answer = "no JSON"
	}
	_, _ = rec.WriteString(answer)
	return rec.Result(), nil
}

func (s *syncRequests) sent() []time.Duration {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.at
}

func pass(d time.Duration) {
	time.Sleep(d)
	synctest.Wait()
}

func ownerBubble(t *testing.T, run func(t *testing.T, owner *syncowner.Owner, qb *syncRequests)) {
	t.Helper()
	qb := &syncRequests{}
	client := qbt.NewClient(qbt.Config{Host: "http://qbt.example.invalid"}).WithHTTPClient(&http.Client{Transport: testutil.RoundTripFunc(qb.RoundTrip)})
	owner := syncowner.New(client, qbt.DefaultSyncOptions())
	synctest.Test(t, func(t *testing.T) {
		qb.started = time.Now()
		run(t, owner, qb)
		pass(time.Minute)
	})
}

func TestOwnerSyncsOnceAfterAnAdd(t *testing.T) {
	const ms = time.Millisecond
	type step struct {
		at time.Duration
		do func(*syncowner.Owner)
	}
	add := (*syncowner.Owner).SyncAfterAdd
	syncNow := func(o *syncowner.Owner) { _ = o.Sync(context.Background()) }
	startSync := func(o *syncowner.Owner) { go syncNow(o) }
	read := func(o *syncowner.Owner) { o.Snapshot() }
	startRead := func(o *syncowner.Owner) { go read(o) }
	for name, tc := range map[string]struct {
		takes time.Duration
		fails bool
		steps []step
		want  []time.Duration
	}{
		"2 s after a failed sync starts":            {takes: 300 * ms, fails: true, steps: []step{{0, syncNow}, {500 * ms, add}}, want: []time.Duration{0, 2000 * ms}},
		"2 s after the sync that runs at the add":   {takes: 300 * ms, steps: []step{{0, startSync}, {100 * ms, add}}, want: []time.Duration{0, 2000 * ms}},
		"after the long sync that runs at the add":  {takes: 3000 * ms, steps: []step{{0, startSync}, {100 * ms, add}}, want: []time.Duration{0, 3000 * ms}},
		"another sync takes its place":              {steps: []step{{0, syncNow}, {500 * ms, add}, {1000 * ms, syncNow}}, want: []time.Duration{0, 1000 * ms}},
		"another sync is too early to show the add": {steps: []step{{0, syncNow}, {5000 * ms, add}, {5100 * ms, syncNow}}, want: []time.Duration{0, 5100 * ms, 7100 * ms}},
		"a stale read waits for it":                 {steps: []step{{0, syncNow}, {5000 * ms, add}, {5100 * ms, read}}, want: []time.Duration{0, 5200 * ms}},
		"a stale read without an add":               {steps: []step{{0, syncNow}, {2000 * ms, read}, {5000 * ms, read}}, want: []time.Duration{0, 5000 * ms}},
		"the first read waits for it":               {steps: []step{{0, add}, {100 * ms, read}, {100 * ms, syncNow}}, want: []time.Duration{200 * ms, 200 * ms}},
		"the first read waits for the running sync": {takes: 300 * ms, steps: []step{{0, startRead}, {50 * ms, add}, {100 * ms, read}, {100 * ms, syncNow}}, want: []time.Duration{0, 300 * ms}},
	} {
		t.Run(name, func(t *testing.T) {
			ownerBubble(t, func(t *testing.T, owner *syncowner.Owner, qb *syncRequests) {
				qb.takes, qb.fails = tc.takes, tc.fails
				for _, step := range tc.steps {
					pass(step.at - time.Since(qb.started))
					step.do(owner)
				}
				pass(time.Minute)
				require.Equal(t, tc.want, qb.sent())
			})
		})
	}
}

func TestOwnerKeepsTwoSecondsBetweenSyncsInABurstOfAdds(t *testing.T) {
	ownerBubble(t, func(t *testing.T, owner *syncowner.Owner, qb *syncRequests) {
		for range 100 {
			owner.SyncAfterAdd()
			pass(100 * time.Millisecond)
		}
		pass(time.Minute)
		sent := qb.sent()
		require.Len(t, sent, 6)
		for i := range sent[1:] {
			require.GreaterOrEqual(t, sent[i+1]-sent[i], 2*time.Second)
		}
		require.GreaterOrEqual(t, sent[5], 10*time.Second, "the last sync shows the last add")
	})
}

func TestOwnerPendingSyncIsARunningOrDueSync(t *testing.T) {
	const ms = time.Millisecond
	startSync := func(o *syncowner.Owner) { go func() { _ = o.Sync(context.Background()) }() }
	staleRead := func(o *syncowner.Owner) { pass(time.Minute); o.Snapshot() }
	addAfter := func(wait time.Duration) func(*syncowner.Owner) {
		return func(o *syncowner.Owner) { pass(wait); o.SyncAfterAdd() }
	}
	for name, tc := range map[string]struct {
		takes  time.Duration
		fails  bool
		before func(*syncowner.Owner)
		ends   time.Duration
		syncs  int
	}{
		"data is stale":                {before: func(*syncowner.Owner) { pass(time.Minute) }, syncs: 1},
		"a stale read syncs and fails": {takes: 300 * ms, fails: true, before: staleRead, ends: 300 * ms, syncs: 2},
		"a sync is due":                {takes: 300 * ms, before: addAfter(1000 * ms), ends: 1300 * ms, syncs: 2},
		"a sync is due later":          {before: addAfter(900 * ms), syncs: 2},
		"a sync runs and another is due later": {takes: 300 * ms, before: func(o *syncowner.Owner) {
			startSync(o)
			addAfter(0)(o)
		}, ends: 300 * ms, syncs: 3},
		// regression: #4475
		"a sync is due while another runs": {takes: 1500 * ms, before: func(o *syncowner.Owner) {
			addAfter(time.Minute)(o)
			addAfter(100 * ms)(o)
			pass(1200 * ms)
		}, ends: 2400 * ms, syncs: 3},
	} {
		t.Run(name, func(t *testing.T) {
			ownerBubble(t, func(t *testing.T, owner *syncowner.Owner, qb *syncRequests) {
				require.NoError(t, owner.Sync(t.Context()))
				qb.takes, qb.fails = tc.takes, tc.fails
				tc.before(owner)
				synctest.Wait()

				started := time.Now()
				pending := owner.PendingSync(time.Second)

				require.Equal(t, tc.ends > 0, pending != nil)
				if pending != nil {
					<-pending
					require.Equal(t, tc.ends, time.Since(started))
				}
				pass(2 * time.Minute)
				require.Len(t, qb.sent(), tc.syncs, "the question starts no sync")
			})
		})
	}
}

func TestOwnerReadWaitsOneRequestTimeoutForTheFirstSync(t *testing.T) {
	ownerBubble(t, func(t *testing.T, owner *syncowner.Owner, qb *syncRequests) {
		qb.takes = 5 * time.Minute
		started := time.Now()

		require.Nil(t, owner.Snapshot())

		require.Equal(t, qbt.DefaultTimeout, time.Since(started))
		pass(time.Hour)
	})
}
