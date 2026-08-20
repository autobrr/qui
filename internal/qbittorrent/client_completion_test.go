// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestIsTorrentCompleteRequiresStampAndZeroBytesLeft(t *testing.T) {
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
				AmountLeft:   880,
				State:        qbt.TorrentStateDownloading,
			},
			want: false,
		},
		{
			// One day past the epoch is still sentinel space; real
			// completion timestamps sit far above it.
			name: "boundary timestamp at 86400",
			torrent: &qbt.Torrent{
				CompletionOn: 86400,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
			want: false,
		},
		{
			// qbit can transiently serialize a negative amount_left when
			// total_wanted_done overshoots total_wanted; still complete.
			name: "negative amount_left overshoot",
			torrent: &qbt.Torrent{
				CompletionOn: 1700000123,
				Progress:     1.0,
				AmountLeft:   -100,
				State:        qbt.TorrentStateUploading,
			},
			want: true,
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

	// Startup snapshot: a completed torrent caught mid-recheck. Progress and
	// amount_left carry verification fractions here, so the init baseline
	// must key on the stamp alone or the end of the recheck looks like a
	// fresh completion.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"abc": {
				Hash:         "abc",
				Name:         "Done",
				CompletionOn: 1700000123,
				Progress:     0.3,
				AmountLeft:   700,
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
	update(qbt.Torrent{Hash: "ghi", CompletionOn: -1, Progress: 0.9, AmountLeft: 100, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Finishes: completion event fires.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected first completion event")
	}

	// Recheck-on-completion runs; verification progress must not touch state.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 0.3, AmountLeft: 700, State: qbt.TorrentStateCheckingUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Recheck failed: qbit re-downloads. completion_on stays set (libtorrent
	// never clears it on the recheck path), so amount_left is what re-arms.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 0.5, AmountLeft: 500, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Real completion: must fire again. The stamp does not change; finished()
	// only re-stamps a zeroed completed_time.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event after re-download finished")
	}

	// Steady seeding: no re-fire.
	update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateStalledUp})
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
	update(qbt.Torrent{Hash: "jkl", CompletionOn: 28800, Progress: 0.04, AmountLeft: 960, State: qbt.TorrentStateDownloading})
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
	update(qbt.Torrent{Hash: "mno", CompletionOn: -1, Progress: 0.9, AmountLeft: 100, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Finishes straight into stoppedUP (ratio limit 0): must fire.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateStoppedUp})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event for completion landing in stopped state")
	}

	// Stale verification fraction while stopped must not un-mark it.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 0.4, AmountLeft: 600, State: qbt.TorrentStateStoppedUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Resume: complete and already handled, no re-fire.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateUploading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Stop again: still handled, no re-fire.
	update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateStoppedUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
}

// A recheck of a completed torrent reports verification fractions in both
// progress and amount_left. The checking-state gate must keep the last known
// state; otherwise the recheck window un-marks the torrent and the return to
// seeding fires a duplicate completion.
func TestHandleCompletionUpdatesCheckingStatesKeepPriorState(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 6}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) {
		seen <- torrent
	})

	update := func(torrent qbt.Torrent) {
		client.handleCompletionUpdates(&qbt.MainData{
			Torrents: map[string]qbt.Torrent{"pqr": torrent},
		})
	}

	// Init baseline with an unrelated torrent so "pqr" arrives mid-run.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		},
	})

	// Downloads and completes: fires once.
	update(qbt.Torrent{Hash: "pqr", CompletionOn: -1, Progress: 0.8, AmountLeft: 200, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
	update(qbt.Torrent{Hash: "pqr", CompletionOn: 1700004000, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event")
	}

	// Mid-recheck snapshot: verification fraction in amount_left.
	update(qbt.Torrent{Hash: "pqr", CompletionOn: 1700004000, Progress: 0.3, AmountLeft: 700, State: qbt.TorrentStateCheckingUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Recheck passes, back to seeding: must NOT re-fire.
	update(qbt.Torrent{Hash: "pqr", CompletionOn: 1700004000, Progress: 1.0, State: qbt.TorrentStateUploading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
}

// Deselecting every file makes qbit finish the torrent at zero bytes: it
// stamps completion_on and serializes progress 0 (total_wanted == 0) with
// amount_left 0. Firing here matches qbit's own finished semantics, and
// cross-seed rejects the search downstream via the completion-wait poller
// (applyCompletionPollResultsLocked's Progress < 1 gate), so only the
// notification observes it. Deliberate: fire exactly once.
func TestHandleCompletionUpdatesZeroWantedFinishFiresOnce(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 8}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) {
		seen <- torrent
	})

	update := func(torrent qbt.Torrent) {
		client.handleCompletionUpdates(&qbt.MainData{
			Torrents: map[string]qbt.Torrent{"stu": torrent},
		})
	}

	// Init baseline with an unrelated torrent so "stu" arrives mid-run.
	client.handleCompletionUpdates(&qbt.MainData{
		Torrents: map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		},
	})

	// Fresh add, downloading normally.
	update(qbt.Torrent{Hash: "stu", CompletionOn: -1, Progress: 0.05, AmountLeft: 950, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// User deselects every file: qbit finishes it at zero bytes.
	update(qbt.Torrent{Hash: "stu", CompletionOn: 1700005000, Progress: 0.0, AmountLeft: 0, State: qbt.TorrentStateStalledUp})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event for zero-wanted finish")
	}

	// Steady state: no re-fire.
	update(qbt.Torrent{Hash: "stu", CompletionOn: 1700005000, Progress: 0.0, AmountLeft: 0, State: qbt.TorrentStateStalledUp})
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

// A failed recheck can happen while qui is down: at startup the torrent is
// actively re-downloading with its stamp intact (libtorrent never clears it
// on the recheck path) and bytes missing. The baseline must record it as
// incomplete or the real completion never fires. Checking/stopped snapshots
// keep the stamp-only baseline; their byte counts are unreliable.
func TestHandleCompletionUpdatesStartupAfterFailedRecheckFires(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 10}

	seen := make(chan qbt.Torrent, 2)
	client.SetTorrentCompletionHandler(func(_ context.Context, _ int, torrent qbt.Torrent) {
		seen <- torrent
	})

	update := func(torrent qbt.Torrent) {
		client.handleCompletionUpdates(&qbt.MainData{
			Torrents: map[string]qbt.Torrent{"vwx": torrent},
		})
	}

	// Startup snapshot: re-downloading after a failed recheck, stamp intact.
	update(qbt.Torrent{Hash: "vwx", CompletionOn: 1700006000, Progress: 0.5, AmountLeft: 500, State: qbt.TorrentStateDownloading})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)

	// Finishes for real: must fire.
	update(qbt.Torrent{Hash: "vwx", CompletionOn: 1700006000, Progress: 1.0, State: qbt.TorrentStateUploading})
	select {
	case <-seen:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("expected completion event after startup mid-redownload baseline")
	}

	// Steady seeding: no re-fire.
	update(qbt.Torrent{Hash: "vwx", CompletionOn: 1700006000, Progress: 1.0, State: qbt.TorrentStateStalledUp})
	requireNoTorrentEvent(t, seen, 200*time.Millisecond)
}
