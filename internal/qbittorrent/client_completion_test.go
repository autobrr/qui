// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestIsTorrentCompleteUsesCompletionOn(t *testing.T) {
	t.Parallel()

	torrent := &qbt.Torrent{
		Hash:         "abc",
		Name:         "Example",
		CompletionOn: 123,
		Progress:     0.12,
		State:        qbt.TorrentStateCheckingResumeData,
	}

	if !isTorrentComplete(torrent) {
		t.Fatal("expected torrent to be treated as complete when CompletionOn is set")
	}
}

func TestHandleCompletionUpdatesDoesNotSpamOnStartupStateFlap(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 7}

	seen := make(chan qbt.Torrent, 1)
	wrongID := make(chan int, 1)
	client.SetTorrentCompletionHandler(func(_ context.Context, instanceID int, torrent qbt.Torrent) {
		if instanceID != 7 {
			select {
			case wrongID <- instanceID:
			default:
			}
		}
		seen <- torrent
	})

	// Startup snapshot: completion set, but state in a transient phase.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"abc": {
				Hash:         "abc",
				Name:         "Done",
				CompletionOn: 123,
				Progress:     1.0,
				State:        qbt.TorrentStateCheckingResumeData,
			},
		},
	})

	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
	requireNoIntEvent(t, wrongID)

	// Post-startup: state normalizes; this must not look like a fresh completion.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"abc": {
				Hash:         "abc",
				Name:         "Done",
				CompletionOn: 123,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
		},
	})

	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
	requireNoIntEvent(t, wrongID)
}

func TestHandleCompletionUpdatesFiresOnceWhenCompletionOnAppears(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 9}

	seen := make(chan qbt.Torrent, 2)
	wrongID := make(chan int, 1)
	client.SetTorrentCompletionHandler(func(_ context.Context, instanceID int, torrent qbt.Torrent) {
		if instanceID != 9 {
			select {
			case wrongID <- instanceID:
			default:
			}
		}
		seen <- torrent
	})

	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"def": {
				Hash:         "def",
				Name:         "Still downloading",
				CompletionOn: -1,
				Progress:     0.50,
				State:        qbt.TorrentStateDownloading,
			},
		},
	})

	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
	requireNoIntEvent(t, wrongID)

	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"def": {
				Hash:         "def",
				Name:         "Done now",
				CompletionOn: 999,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
		},
	})

	select {
	case torrent := <-seen:
		if torrent.Hash != "def" {
			t.Fatalf("unexpected hash: %q", torrent.Hash)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected a completion event")
	}
	requireNoIntEvent(t, wrongID)

	// Another update should not re-fire.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"def": {
				Hash:         "def",
				Name:         "Done now",
				CompletionOn: 999,
				Progress:     1.0,
				State:        qbt.TorrentStateStalledUp,
			},
		},
	})

	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
	requireNoIntEvent(t, wrongID)
}

func requireNoTorrentEvent(t *testing.T, ch <-chan qbt.Torrent, d time.Duration) {
	t.Helper()

	select {
	case torrent := <-ch:
		t.Fatalf("unexpected completion event: hash=%q name=%q state=%q completionOn=%d",
			torrent.Hash,
			torrent.Name,
			torrent.State,
			torrent.CompletionOn,
		)
	case <-time.After(d):
	}
}

func requireNoIntEvent(t *testing.T, ch <-chan int) {
	t.Helper()

	select {
	case got := <-ch:
		t.Fatalf("unexpected instanceID reported from handler goroutine: %d", got)
	default:
	}
}
