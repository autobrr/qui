package gazellemusic

import "testing"

func TestNewClient_SharesLimiterPerHost(t *testing.T) {
	c1, err := NewClient("https://redacted.sh", "key1")
	if err != nil {
		t.Fatalf("NewClient 1: %v", err)
	}
	c2, err := NewClient("https://redacted.sh", "key2")
	if err != nil {
		t.Fatalf("NewClient 2: %v", err)
	}

	if c1.limiter != c2.limiter {
		t.Fatalf("expected limiter to be shared for same host")
	}
}

func TestNewClient_DifferentHostsHaveDifferentLimiters(t *testing.T) {
	red, err := NewClient("https://redacted.sh", "key1")
	if err != nil {
		t.Fatalf("NewClient red: %v", err)
	}
	ops, err := NewClient("https://orpheus.network", "key2")
	if err != nil {
		t.Fatalf("NewClient ops: %v", err)
	}

	if red.limiter == ops.limiter {
		t.Fatalf("expected different limiter instances across hosts")
	}
}
