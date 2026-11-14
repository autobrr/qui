// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
)

// answer429 sends one request of kind to indexer that the indexer answers
// with 429 and the given Retry-After header.
// It returns the end of the Rate-limit cooldown.
func answer429(t *testing.T, slots *requestSlots, indexer *models.TorznabIndexer, kind requestKind, retryAfter string) time.Time {
	t.Helper()
	ticket, _, err := slots.tryAcquire(indexer, kind)
	require.NoError(t, err)
	require.NotNil(t, ticket)
	var rateLimitErr *RateLimitError
	require.ErrorAs(t, ticket.done(t.Context(), &responseError{StatusCode: http.StatusTooManyRequests, RetryAfter: retryAfter}), &rateLimitErr)
	return rateLimitErr.RetryAt
}

func nativeIndexer() *models.TorznabIndexer {
	return &models.TorznabIndexer{
		ID:           1,
		Name:         "Native",
		BaseURL:      "http://indexer.test/api",
		Backend:      models.TorznabBackendNative,
		Enabled:      true,
		Capabilities: []string{"search"},
		Categories:   []models.TorznabIndexerCategory{{CategoryID: 2000, CategoryName: "Movies"}, {CategoryID: 5000, CategoryName: "TV"}},
	}
}

func TestRequestSlots_OnlySearchesAndDirScanDownloadsWaitOnNativeIndexers(t *testing.T) {
	tests := []struct {
		name     string
		backend  models.TorznabBackend
		kind     requestKind
		wantWait bool
	}{
		{name: "native search", backend: models.TorznabBackendNative, kind: requestSearch, wantWait: true},
		{name: "native dir scan download", backend: models.TorznabBackendNative, kind: requestDirScanDownload, wantWait: true},
		{name: "native caps", backend: models.TorznabBackendNative, kind: requestCaps},
		{name: "native download", backend: models.TorznabBackendNative, kind: requestDownload},
		{name: "prowlarr search", backend: models.TorznabBackendProwlarr, kind: requestSearch},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				slots := newRequestSlots(time.Minute, nil, nil)
				indexer := &models.TorznabIndexer{ID: 1, Backend: tt.backend}
				search, _, err := slots.tryAcquire(indexer, requestSearch)
				require.NoError(t, err)
				require.NoError(t, search.done(t.Context(), nil))

				ticket, wait, err := slots.tryAcquire(indexer, tt.kind)
				require.NoError(t, err)
				if tt.wantWait {
					assert.Nil(t, ticket)
					assert.Equal(t, time.Minute, wait)
				} else {
					assert.NotNil(t, ticket)
				}
			})
		})
	}
}

func TestRequestSlots_GapCountsFromTheAnswer(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slots := newRequestSlots(time.Minute, nil, nil)
		indexer := nativeIndexer()

		search, err := slots.acquire(t.Context(), indexer, requestSearch)
		require.NoError(t, err)
		acquired := make(chan time.Time, 1)
		go func() {
			ticket, err := slots.acquire(t.Context(), indexer, requestDirScanDownload)
			if err == nil {
				acquired <- time.Now()
				_ = ticket.done(t.Context(), nil)
			}
		}()

		time.Sleep(20 * time.Second) // a slow answer
		synctest.Wait()
		require.Empty(t, acquired, "a download went while the search was open")
		answeredAt := time.Now()
		require.NoError(t, search.done(t.Context(), nil))

		assert.Equal(t, time.Minute, (<-acquired).Sub(answeredAt))
	})
}

func TestRequestSlots_LateAnswerStartsCooldownButKeepsTheGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slots := newRequestSlots(time.Minute, nil, nil)
		indexer := nativeIndexer()

		search, err := slots.acquire(t.Context(), indexer, requestSearch)
		require.NoError(t, err)
		require.ErrorIs(t, search.done(t.Context(), context.DeadlineExceeded), context.DeadlineExceeded)
		timedOutAt := time.Now()

		time.Sleep(10 * time.Second)
		var rateLimitErr *RateLimitError
		require.ErrorAs(t, search.done(t.Context(), &responseError{StatusCode: http.StatusTooManyRequests, RetryAfter: "300"}), &rateLimitErr)

		_, _, err = slots.tryAcquire(indexer, requestSearch)
		require.ErrorAs(t, err, &rateLimitErr)
		_, err = slots.acquire(t.Context(), indexer, requestDirScanDownload)
		require.NoError(t, err)
		assert.Equal(t, time.Minute, time.Since(timedOutAt), "a late answer must not move the Request gap")
	})
}

func TestRequestSlots_ContextEndTakesNoSlot(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slots := newRequestSlots(time.Minute, nil, nil)
		indexer := nativeIndexer()
		search, err := slots.acquire(t.Context(), indexer, requestSearch)
		require.NoError(t, err)
		require.NoError(t, search.done(t.Context(), nil))

		waitCtx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_, err = slots.acquire(waitCtx, indexer, requestDirScanDownload)
		require.ErrorIs(t, err, context.DeadlineExceeded)

		time.Sleep(time.Minute)
		_, err = slots.acquire(waitCtx, indexer, requestDirScanDownload)
		require.ErrorIs(t, err, context.DeadlineExceeded, "an ended context must not take a free slot")
		_, wait, err := slots.tryAcquire(indexer, requestSearch)
		require.NoError(t, err)
		assert.Zero(t, wait)
	})
}

func TestRequestSlots_RateLimitCooldown(t *testing.T) {
	tests := []struct {
		name         string
		kind         requestKind
		retryAfter   string
		wantDuration time.Duration
		blocked      []requestKind
		open         []requestKind
	}{
		{name: "search uses Retry-After", kind: requestSearch, retryAfter: "30", wantDuration: 30 * time.Second, blocked: []requestKind{requestSearch, requestCaps}, open: []requestKind{requestDownload, requestDirScanDownload}},
		{name: "caps without Retry-After waits one minute", kind: requestCaps, wantDuration: time.Minute, blocked: []requestKind{requestSearch, requestCaps}, open: []requestKind{requestDownload, requestDirScanDownload}},
		{name: "download blocks only downloads", kind: requestDownload, retryAfter: "90", wantDuration: 90 * time.Second, blocked: []requestKind{requestDownload, requestDirScanDownload}, open: []requestKind{requestSearch, requestCaps}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				slots := newRequestSlots(time.Second, nil, nil)
				indexer := &models.TorznabIndexer{ID: 1, Name: "Limited", Backend: models.TorznabBackendProwlarr}
				retryAt := answer429(t, slots, indexer, tt.kind, tt.retryAfter)
				assert.Equal(t, tt.wantDuration, time.Until(retryAt))

				for _, kind := range tt.blocked {
					_, err := slots.acquire(t.Context(), indexer, kind)
					var refusal *RateLimitError
					require.ErrorAs(t, err, &refusal)
					assert.Equal(t, retryAt, refusal.RetryAt)
				}
				for _, kind := range tt.open {
					_, err := slots.acquire(t.Context(), indexer, kind)
					require.NoError(t, err)
				}

				time.Sleep(tt.wantDuration)
				_, err := slots.acquire(t.Context(), indexer, tt.kind)
				require.NoError(t, err, "the cooldown must end at Retry-After")
			})
		})
	}
}

func TestRequestSlots_ShorterCooldownKeepsTheLongerOne(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		slots := newRequestSlots(time.Second, nil, nil)
		indexer := &models.TorznabIndexer{ID: 1, Name: "Limited", Backend: models.TorznabBackendProwlarr}
		first, _, err := slots.tryAcquire(indexer, requestSearch)
		require.NoError(t, err)
		second, _, err := slots.tryAcquire(indexer, requestSearch)
		require.NoError(t, err)

		var longer, shorter *RateLimitError
		require.ErrorAs(t, first.done(t.Context(), &responseError{StatusCode: http.StatusTooManyRequests, RetryAfter: "120"}), &longer)
		require.ErrorAs(t, second.done(t.Context(), &responseError{StatusCode: http.StatusTooManyRequests, RetryAfter: "10"}), &shorter)
		assert.Equal(t, longer.RetryAt, shorter.RetryAt)
		assert.Equal(t, 2*time.Minute, time.Until(shorter.RetryAt))
	})
}

// stalledRecorder is a database whose writer stays busy: RecordError returns
// only when its context ends.
type stalledRecorder struct {
	calls chan string
	ended chan error
}

func (r *stalledRecorder) RecordError(ctx context.Context, _ int, errorMessage, _ string) error {
	r.calls <- errorMessage
	<-ctx.Done()
	r.ended <- ctx.Err()
	return ctx.Err()
}

func TestRequestSlots_RateLimitAnswerDoesNotWaitForTheErrorRecord(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		recorder := &stalledRecorder{calls: make(chan string, 1), ended: make(chan error, 1)}
		slots := newRequestSlots(time.Second, recorder, nil)
		ticket, _, err := slots.tryAcquire(nativeIndexer(), requestSearch)
		require.NoError(t, err)

		answered := make(chan error, 1)
		go func() {
			answered <- ticket.done(t.Context(), &responseError{StatusCode: http.StatusTooManyRequests})
		}()
		synctest.Wait()

		select {
		case err := <-answered:
			var rateLimitErr *RateLimitError
			require.ErrorAs(t, err, &rateLimitErr)
		default:
			t.Fatal("the 429 answer waited for the database write")
		}
		assert.Contains(t, <-recorder.calls, "Rate limit triggered")

		time.Sleep(rateLimitRecordTimeout)
		synctest.Wait()
		select {
		case err := <-recorder.ended:
			require.ErrorIs(t, err, context.DeadlineExceeded)
		default:
			t.Fatal("the error record must end at its timeout")
		}
	})
}

func TestWithMinRequestInterval(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		indexer := nativeIndexer()
		for _, tc := range []struct {
			opts     []ServiceOption
			wantWait time.Duration
		}{
			{wantWait: defaultRequestGap},
			{opts: []ServiceOption{WithMinRequestInterval(time.Millisecond)}, wantWait: time.Millisecond},
		} {
			service := newService(nil, releases.DefaultParser, tc.opts...)
			search, _, err := service.slots.tryAcquire(indexer, requestSearch)
			require.NoError(t, err)
			require.NoError(t, search.done(t.Context(), nil))
			_, wait, err := service.slots.tryAcquire(indexer, requestDirScanDownload)
			require.NoError(t, err)
			assert.Equal(t, tc.wantWait, wait)
			service.searchScheduler.Stop()
		}
	})
}

// handlerTransport answers in-process, so a test of an indexer client can run
// in synctest.Test.
type handlerTransport struct{ handler http.Handler }

func (h handlerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	h.handler.ServeHTTP(rec, r)
	if err := r.Context().Err(); err != nil {
		return nil, err
	}
	return rec.Result(), nil
}

// indexerStub stands in for a Native indexer and logs the time of each request.
// A test that uses it must not call t.Parallel, because it replaces
// http.DefaultTransport, which every indexer client uses.
type indexerStub struct {
	mu     sync.Mutex
	hits   []stubHit
	answer func(w http.ResponseWriter, r *http.Request, kind string)
}

type stubHit struct {
	kind string
	at   time.Time
}

func serveIndexer(t *testing.T, answer func(w http.ResponseWriter, r *http.Request, kind string)) *indexerStub {
	t.Helper()
	stub := &indexerStub{answer: answer}
	original := http.DefaultTransport
	http.DefaultTransport = handlerTransport{handler: stub}
	t.Cleanup(func() { http.DefaultTransport = original })
	return stub
}

func (s *indexerStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	kind := "search"
	switch {
	case strings.HasSuffix(r.URL.Path, "/download"):
		kind = "download"
	case r.URL.Query().Get("t") == "caps":
		kind = "caps"
	}
	s.mu.Lock()
	s.hits = append(s.hits, stubHit{kind: kind, at: time.Now()})
	s.mu.Unlock()
	if s.answer != nil {
		s.answer(w, r, kind)
		return
	}
	writeStubAnswer(w, kind)
}

func writeStubAnswer(w http.ResponseWriter, kind string) {
	switch kind {
	case "download":
		_, _ = w.Write([]byte(testTorrentPayload))
	case "caps":
		w.Header().Set("Content-Type", "application/xml")
		_, _ = w.Write([]byte(bookOnlyCapsXML))
	default:
		w.Header().Set("Content-Type", "application/rss+xml")
		_, _ = w.Write([]byte(`<rss version="2.0"><channel><title>Test</title></channel></rss>`))
	}
}

func (s *indexerStub) times(kind string) []time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []time.Time
	for _, hit := range s.hits {
		if hit.kind == kind {
			out = append(out, hit.at)
		}
	}
	return out
}

// newStubService shares the release parser, because a new one starts a
// goroutine that never ends, and synctest.Test waits for every goroutine.
func newStubService(t *testing.T, indexers ...*models.TorznabIndexer) *Service {
	t.Helper()
	service := newService(&mockTorznabIndexerStore{indexers: indexers}, releases.DefaultParser)
	t.Cleanup(service.searchScheduler.Stop)
	return service
}

// startSearch runs one queued search on indexer and returns its result.
func startSearch(t *testing.T, service *Service, indexer *models.TorznabIndexer) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	require.NoError(t, service.SearchGeneric(t.Context(), &TorznabSearchRequest{
		Query:         "synthetic query",
		IndexerIDs:    []int{indexer.ID},
		CacheMode:     CacheModeBypass,
		OnAllComplete: func(_ *SearchResponse, err error) { done <- err },
	}))
	return done
}

func download(service *Service, indexer *models.TorznabIndexer, pace bool) error {
	_, err := service.DownloadTorrent(context.Background(), TorrentDownloadRequest{IndexerID: indexer.ID, DownloadURL: indexer.BaseURL + "/download", Pace: pace})
	return err
}

// regression: #2735
func TestDirScanDownloadWaitsForOpenSearch(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		searchOpen := make(chan struct{})
		stub := serveIndexer(t, func(w http.ResponseWriter, _ *http.Request, kind string) {
			if kind == "search" {
				close(searchOpen)
				time.Sleep(8 * time.Second)
			}
			writeStubAnswer(w, kind)
		})
		indexer := nativeIndexer()
		service := newStubService(t, indexer)

		searchDone := startSearch(t, service, indexer)
		<-searchOpen
		downloadDone := make(chan error, 1)
		go func() { downloadDone <- download(service, indexer, true) }()

		require.NoError(t, <-searchDone)
		require.NoError(t, <-downloadDone)
		searches, downloads := stub.times("search"), stub.times("download")
		require.Len(t, downloads, 1)
		assert.Equal(t, 8*time.Second+defaultRequestGap, downloads[0].Sub(searches[0]))
	})
}

// regression: #2735
func TestDirScanDownloadRetryWaitsForRequestGap(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var tries int
		stub := serveIndexer(t, func(w http.ResponseWriter, _ *http.Request, kind string) {
			if tries++; tries == 1 {
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			writeStubAnswer(w, kind)
		})
		indexer := nativeIndexer()
		service := newStubService(t, indexer)

		require.NoError(t, download(service, indexer, true))
		downloads := stub.times("download")
		require.Len(t, downloads, 2)
		assert.Equal(t, defaultRequestGap, downloads[1].Sub(downloads[0]))
	})
}

func TestSearchWaitsOneGapAfterDirScanDownload(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := serveIndexer(t, nil)
		indexer := nativeIndexer()
		service := newStubService(t, indexer)

		require.NoError(t, download(service, indexer, true))
		require.NoError(t, <-startSearch(t, service, indexer))
		assert.Equal(t, defaultRequestGap, stub.times("search")[0].Sub(stub.times("download")[0]))
	})
}

func TestRequestsOutsideTheSlotGoAtOnceAndKeepTheGap(t *testing.T) {
	for _, tc := range []struct {
		name string
		kind string
		send func(*Service, *models.TorznabIndexer) error
	}{
		{name: "download outside Dir Scan", kind: "download", send: func(s *Service, idx *models.TorznabIndexer) error { return download(s, idx, false) }},
		{name: "capabilities sync", kind: "caps", send: func(s *Service, idx *models.TorznabIndexer) error {
			_, err := s.SyncIndexerCaps(context.Background(), idx.ID)
			return err
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				searchOpen := make(chan struct{}, 1)
				stub := serveIndexer(t, func(w http.ResponseWriter, _ *http.Request, kind string) {
					if kind == "search" {
						searchOpen <- struct{}{}
						time.Sleep(8 * time.Second)
					}
					writeStubAnswer(w, kind)
				})
				indexer := nativeIndexer()
				service := newStubService(t, indexer)

				searchDone := startSearch(t, service, indexer)
				<-searchOpen
				sentAt := time.Now()
				require.NoError(t, tc.send(service, indexer))
				require.Equal(t, []time.Time{sentAt}, stub.times(tc.kind), "the request must go during the open search")
				require.NoError(t, <-searchDone)
				firstAnswer := time.Now()

				time.Sleep(30 * time.Second)
				require.NoError(t, tc.send(service, indexer))
				require.NoError(t, <-startSearch(t, service, indexer))
				assert.Equal(t, defaultRequestGap, stub.times("search")[1].Sub(firstAnswer), "the request must not move the Request gap")
			})
		})
	}
}

func TestDirScanDownloadCancelledDuringWaitSendsNothing(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		stub := serveIndexer(t, nil)
		indexer := nativeIndexer()
		service := newStubService(t, indexer)
		require.NoError(t, <-startSearch(t, service, indexer))

		ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
		defer cancel()
		_, err := service.DownloadTorrent(ctx, TorrentDownloadRequest{IndexerID: indexer.ID, DownloadURL: indexer.BaseURL + "/download", Pace: true})
		require.ErrorIs(t, err, context.DeadlineExceeded)
		assert.Empty(t, stub.times("download"))
	})
}
