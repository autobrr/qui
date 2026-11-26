package gazellemusic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/time/rate"
)

func TestNewClient_SharesLimiterPerHost(t *testing.T) {
	c1, err := NewClient("redacted.sh", "", "key1")
	if err != nil {
		t.Fatalf("NewClient 1: %v", err)
	}
	c2, err := NewClient("redacted.sh", "", "key2")
	if err != nil {
		t.Fatalf("NewClient 2: %v", err)
	}

	if c1.limiter != c2.limiter {
		t.Fatalf("expected limiter to be shared for same host")
	}
}

func TestNewClient_DifferentHostsHaveDifferentLimiters(t *testing.T) {
	red, err := NewClient("redacted.sh", "", "key1")
	if err != nil {
		t.Fatalf("NewClient red: %v", err)
	}
	ops, err := NewClient("orpheus.network", "", "key2")
	if err != nil {
		t.Fatalf("NewClient ops: %v", err)
	}

	if red.limiter == ops.limiter {
		t.Fatalf("expected different limiter instances across hosts")
	}
}

func TestNewClient_DefaultsToTrackerSite(t *testing.T) {
	c, err := NewClient("redacted.sh", "", "key")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	if c.baseURL != "https://redacted.sh" {
		t.Fatalf("expected the tracker site as the default, got %q", c.baseURL)
	}
}

func TestClientTalksToATestServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","response":{"group":{"id":3,"name":"Album"},"torrent":{"id":7,"size":123}}}`))
	}))
	defer server.Close()

	client, err := NewClient("orpheus.network", server.URL, "key")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if client.SourceFlag() != "OPS" {
		t.Fatalf("expected the tracker identity to drive the source flag, got %q", client.SourceFlag())
	}

	result, err := client.SearchByHash(context.Background(), "abc")
	if err != nil {
		t.Fatalf("SearchByHash: %v", err)
	}
	if result == nil || result.TorrentID != 7 {
		t.Fatalf("expected the test server's torrent, got %+v", result)
	}
}

func TestRequestQueuePastDeadlineIsDeadlineExceeded(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"status":"success","response":{}}`))
	}))
	defer server.Close()
	client, err := NewClient("orpheus.network", server.URL, "key")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	client.limiter = rate.NewLimiter(rate.Every(2*time.Second), 1)
	client.limiter.Allow()

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	_, err = client.SearchByHash(ctx, "abc")

	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("expected context.DeadlineExceeded, got %v", err)
	}
	if ctx.Err() != nil {
		t.Fatalf("the request must fail before the deadline, not wait for it")
	}
}

func TestDialGuardPanicsOnLiveTracker(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected the dial guard to panic on a non-loopback address")
		}
	}()

	_, _ = sharedTransport.DialContext(t.Context(), "tcp", "192.0.2.1:9")
}

func TestClientClassifiesAccessDenied(t *testing.T) {
	tests := []struct {
		name       string
		status     int
		body       string
		wantDenied bool
		wantText   string
	}{
		{
			name:       "OPS ip ban",
			status:     http.StatusOK,
			body:       `{"status":"failure","error":"Your IP address has been banned."}`,
			wantDenied: true,
			wantText:   "Your IP address has been banned.",
		},
		{
			name:       "OPS wrong key",
			status:     http.StatusOK,
			body:       `{"status":"failure","error":"invalid token","info":{"source":"Orpheus","version":1}}`,
			wantDenied: true,
			wantText:   "invalid token",
		},
		{
			name:       "RED wrong key",
			status:     http.StatusUnauthorized,
			body:       `{"status":"failure","error":"bad credentials"}`,
			wantDenied: true,
			wantText:   "bad credentials",
		},
		{
			name:       "short body with invalid UTF-8",
			status:     http.StatusForbidden,
			body:       "denied \xff",
			wantDenied: true,
			wantText:   "denied",
		},
		{
			name:     "other api failure",
			status:   http.StatusOK,
			body:     `{"status":"failure","error":"rate limit exceeded"}`,
			wantText: "rate limit exceeded",
		},
		{
			name:     "other api failure that says banned",
			status:   http.StatusOK,
			body:     `{"status":"failure","error":"This format is banned."}`,
			wantText: "This format is banned.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()

			c, err := NewClient("redacted.sh", server.URL, "key")
			if err != nil {
				t.Fatalf("NewClient: %v", err)
			}
			_, searchErr := c.SearchByFilename(t.Context(), "track")
			_, downloadErr := c.DownloadTorrent(t.Context(), 1)
			for _, err := range []error{searchErr, downloadErr} {
				if err == nil {
					t.Fatal("expected an error")
				}
				if got := errors.Is(err, ErrAccessDenied); got != tt.wantDenied {
					t.Fatalf("errors.Is(err, ErrAccessDenied) = %v, want %v (err: %v)", got, tt.wantDenied, err)
				}
				if !strings.Contains(err.Error(), tt.wantText) {
					t.Fatalf("error %q does not carry the tracker text %q", err, tt.wantText)
				}
				if !utf8.ValidString(err.Error()) {
					t.Fatalf("error %q is not valid UTF-8; Postgres rejects it in the run record", err)
				}
			}
		})
	}
}

func TestSearchByHashKeepsAccessDenied(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"status":"failure","error":"bad parameters"}`))
	}))
	defer server.Close()

	c, err := NewClient("redacted.sh", server.URL, "key")
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	if _, err := c.SearchByHash(t.Context(), "abc"); !errors.Is(err, ErrAccessDenied) {
		t.Fatalf("SearchByHash error = %v, want ErrAccessDenied", err)
	}
}
