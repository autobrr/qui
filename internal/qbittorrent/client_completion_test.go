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
	client.SetTorrentCompletionHandler(func(_ context.Context, instanceID int, torrent qbt.Torrent) {
		if instanceID != 7 {
			t.Fatalf("unexpected instanceID: %d", instanceID)
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
}

func TestHandleCompletionUpdatesFiresOnceWhenCompletionOnAppears(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 9}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, instanceID int, torrent qbt.Torrent) {
		if instanceID != 9 {
			t.Fatalf("unexpected instanceID: %d", instanceID)
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
