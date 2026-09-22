package gazellemusic

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
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

// TestClientTalksToATestServer is the positive half of the dial guard: a client
// pointed at a loopback server must still work, over the guarded transport.
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

// TestDialGuardPanicsOnLiveTracker is the negative half. Calling the shared
// transport's dialer directly keeps the panic on this goroutine, so it can be
// recovered. Through an http.Client it lands on the transport's own dial
// goroutine and takes the whole run down, which is the point: callers log a
// failed lookup and carry on, so a returned error would keep the test green
// while the request went out.
func TestDialGuardPanicsOnLiveTracker(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("expected the dial guard to panic on a non-loopback address")
		}
	}()

	// 192.0.2.1 is TEST-NET-1 (RFC 5737), reserved for documentation. The guard
	// fires before connect, so nothing leaves the machine either way.
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
			_, err = c.SearchByFilename(t.Context(), "track")
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
		})
	}
}

// A denial whose text reads like Gazelle's not-found reply must still stop the
// lookup, not count as a hash miss.
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
