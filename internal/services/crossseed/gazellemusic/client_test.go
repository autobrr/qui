package gazellemusic

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
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
