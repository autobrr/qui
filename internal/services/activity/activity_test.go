// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package activity

import (
	"testing"
	"time"
)

func TestHubFanOutToAllSubscribers(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch1, unsub1 := h.Subscribe()
	defer unsub1()
	ch2, unsub2 := h.Subscribe()
	defer unsub2()

	h.Publish(Event{Kind: KindBackupRun, InstanceID: 7})

	for i, ch := range []<-chan Event{ch1, ch2} {
		select {
		case ev := <-ch:
			if ev.Kind != KindBackupRun || ev.InstanceID != 7 {
				t.Fatalf("subscriber %d got unexpected event: %+v", i, ev)
			}
			if ev.Timestamp.IsZero() {
				t.Fatalf("subscriber %d: expected timestamp to be set", i)
			}
		case <-time.After(time.Second):
			t.Fatalf("subscriber %d did not receive event", i)
		}
	}
}

func TestHubPublishPreservesProvidedTimestamp(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch, unsub := h.Subscribe()
	defer unsub()

	ts := time.Unix(1700000000, 0).UTC()
	h.Publish(Event{Kind: KindSearchHistory, Timestamp: ts})

	select {
	case ev := <-ch:
		if !ev.Timestamp.Equal(ts) {
			t.Fatalf("expected timestamp %v, got %v", ts, ev.Timestamp)
		}
	case <-time.After(time.Second):
		t.Fatal("did not receive event")
	}
}

func TestHubDropsWhenSubscriberBufferFull(t *testing.T) {
	h := NewHub()
	defer h.Close()

	// Subscribe but never drain; the buffered channel fills and further events drop.
	_, unsub := h.Subscribe()
	defer unsub()

	total := subscriberBuffer + 10
	for range total {
		h.Publish(Event{Kind: KindDirScanRun})
	}

	if dropped := h.Dropped(); dropped == 0 {
		t.Fatal("expected dropped count > 0 when subscriber buffer is full")
	}
}

func TestHubUnsubscribeStopsDelivery(t *testing.T) {
	h := NewHub()
	defer h.Close()

	ch, unsub := h.Subscribe()
	unsub()

	// Channel is closed on unsubscribe; a receive should observe the closed state.
	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected channel to be closed after unsubscribe")
		}
	case <-time.After(time.Second):
		t.Fatal("expected closed channel receive to return immediately")
	}

	// Publishing after unsubscribe must not panic.
	h.Publish(Event{Kind: KindTrackerIcons})
}

func TestHubUnsubscribeIsIdempotent(_ *testing.T) {
	h := NewHub()
	defer h.Close()

	_, unsub := h.Subscribe()
	unsub()
	unsub() // must not panic or double-close
}

func TestHubCloseClosesSubscribersAndStopsPublishing(t *testing.T) {
	h := NewHub()

	ch, _ := h.Subscribe()
	h.Close()

	select {
	case _, ok := <-ch:
		if ok {
			t.Fatal("expected subscriber channel closed after hub Close")
		}
	case <-time.After(time.Second):
		t.Fatal("expected closed channel receive to return immediately")
	}

	// Subscribe after close returns an already-closed channel.
	ch2, unsub2 := h.Subscribe()
	defer unsub2()
	select {
	case _, ok := <-ch2:
		if ok {
			t.Fatal("expected post-close Subscribe to return a closed channel")
		}
	case <-time.After(time.Second):
		t.Fatal("expected closed channel receive to return immediately")
	}

	// Publish after close is a no-op and must not panic.
	h.Publish(Event{Kind: KindBackupRun})
}

func TestNopPublisherDoesNotPanic(_ *testing.T) {
	var p Publisher = NopPublisher{}
	p.Publish(Event{Kind: KindAutomationActivity})
}
