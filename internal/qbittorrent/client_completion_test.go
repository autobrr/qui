// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestIsTorrentCompleteRequiresCompletionOnAndProgress(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		torrent *qbt.Torrent
		want    bool
	}{
		{
			name: "completed with full progress",
			torrent: &qbt.Torrent{
				CompletionOn: 1700000123,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
			want: true,
		},
		{
			// completion_on set once, then a failed recheck-on-completion
			// knocked the torrent back to downloading. Not complete.
			name: "completion_on set but data incomplete",
			torrent: &qbt.Torrent{
				CompletionOn: 1700000123,
				Progress:     0.12,
				State:        qbt.TorrentStateDownloading,
			},
			want: false,
		},
		{
			name: "downloading without completion_on",
			torrent: &qbt.Torrent{
				CompletionOn: -1,
				Progress:     0.5,
				State:        qbt.TorrentStateDownloading,
			},
			want: false,
		},
		{
			// qbit 4.2-4.6 serialize never-completed as minus the host's 1970
			// UTC offset: positive west of UTC (+28800 US Pacific). Data being
			// present (seed-mode add) must not make this look completed.
			name: "qbit 4.x west-of-UTC sentinel with full data",
			torrent: &qbt.Torrent{
				CompletionOn: 28800,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
			want: false,
		},
		{
			// qbit 4.1 serializes never-completed as uint32(-1).
			name: "qbit 4.1 uint32 sentinel with full data",
			torrent: &qbt.Torrent{
				CompletionOn: 4294967295,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
			want: false,
		},
		{
			name:    "nil torrent",
			torrent: nil,
			want:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isTorrentComplete(tt.torrent); got != tt.want {
				t.Fatalf("isTorrentComplete() = %v, want %v", got, tt.want)
			}
		})
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
				CompletionOn: 1700000123,
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
				CompletionOn: 1700000123,
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
				CompletionOn: 1700000999,
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
				CompletionOn: 1700000999,
				Progress:     1.0,
				State:        qbt.TorrentStateStalledUp,
			},
		},
	})

	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
	requireNoIntEvent(t, wrongID)
}

func TestHandleCompletionUpdatesRearmsAfterFailedRecheck(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 3}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) {
		seen <- torrent
	})

	update := func(torrent qbt.Torrent) {
		client.handleCompletionUpdates(&qbt.MainData{
			Torrents: map[string]qbt.Torrent{"ghi": torrent},
		})
	}

	// Baseline: torrent still downloading.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: -1, Progress: 0.9, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Finishes: completion event fires.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected first completion event")
	}

	// Recheck-on-completion runs; verification progress must not touch state.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 0.3, State: qbt.TorrentStateCheckingUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Recheck failed: qbit re-downloads. completion_on stays set. Re-arms.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 0.5, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Real completion: must fire again.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700001000, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event after re-download finished")
	}

	// Steady seeding: no re-fire.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700001000, Progress: 1.0, State: qbt.TorrentStateStalledUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
}

// qbit 4.2-4.6 on a host west of UTC: every fresh torrent is born with a
// positive completion_on sentinel. The hook must not fire at add time, and
// must fire once when the download actually finishes (issue report: search
// ran seconds after adding, then never retried).
func TestHandleCompletionUpdatesIgnoresWestOfUTCSentinel(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 4}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) {
		seen <- torrent
	})

	update := func(torrent qbt.Torrent) {
		client.handleCompletionUpdates(&qbt.MainData{
			Torrents: map[string]qbt.Torrent{"jkl": torrent},
		})
	}

	// Init baseline with an unrelated torrent so "jkl" arrives mid-run.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		},
	})

	// Fresh add: positive sentinel, barely any data. Must not fire.
	update(qbt.Torrent{Hash: "jkl", CompletionOn: 28800, Progress: 0.04, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Real completion: fires once.
	update(qbt.Torrent{Hash: "jkl", CompletionOn: 1700002000, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event when download finished")
	}

	update(qbt.Torrent{Hash: "jkl", CompletionOn: 1700002000, Progress: 1.0, State: qbt.TorrentStateStalledUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
}

// A completion can land directly in a stopped state (e.g. share-ratio limit 0
// stops the torrent the instant it finishes). That must fire exactly once,
// and stop/resume cycles or stale verification progress in stopped states
// must never re-arm or re-fire.
func TestHandleCompletionUpdatesStoppedStatesAreOneWay(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 5}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) {
		seen <- torrent
	})

	update := func(torrent qbt.Torrent) {
		client.handleCompletionUpdates(&qbt.MainData{
			Torrents: map[string]qbt.Torrent{"mno": torrent},
		})
	}

	// Init baseline with an unrelated torrent so "mno" arrives mid-run.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		},
	})

	// Downloading, incomplete.
	update(qbt.Torrent{Hash: "mno", CompletionOn: -1, Progress: 0.9, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Finishes straight into stoppedUP (ratio limit 0): must fire.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateStoppedUp})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event for completion landing in stopped state")
	}

	// Stale verification fraction while stopped must not un-mark it.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 0.4, State: qbt.TorrentStateStoppedUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Resume: complete and already handled, no re-fire.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateUploading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Stop again: still handled, no re-fire.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateStoppedUp})
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

func requireNoIntEvent(t *testing.T, ch <-chan int) {
	t.Helper()

	select {
	case got := <-ch:
		t.Fatalf("unexpected instanceID reported from handler goroutine: %d", got)
	default:
	}
}
