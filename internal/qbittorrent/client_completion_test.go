// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/testutil/syncsnap"
)

func syncOf(t testing.TB, src *syncsnap.Source, byHash map[string]qbt.Torrent) qbt.SyncUpdate {
	t.Helper()
	torrents := make([]qbt.Torrent, 0, len(byHash))
	for hash, torrent := range byHash {
		torrent.Hash = hash
		torrents = append(torrents, torrent)
	}
	return src.Update(t, torrents...)
}

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
			name: "boundary timestamp at 86400",
			torrent: &qbt.Torrent{
				CompletionOn: 86400,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
			want: false,
		},
		{
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
			name: "qbit 4.x west-of-UTC sentinel with full data",
			torrent: &qbt.Torrent{
				CompletionOn: 28800,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
			want: false,
		},
		{
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

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 7}

		seen := make(chan qbt.Torrent, 1)
		wrongID := make(chan int, 1)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, instanceID int, torrent qbt.Torrent) {
			if instanceID != 7 {
				select {
				case wrongID <- instanceID:
				default:
				}
			}
			seen <- torrent
		})

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"abc": {
				Hash:         "abc",
				Name:         "Done",
				CompletionOn: 1700000123,
				Progress:     0.3,
				AmountLeft:   700,
				State:        qbt.TorrentStateCheckingResumeData,
			},
		}))

		requireNoTorrentEvent(t, seen)
		requireNoIntEvent(t, wrongID)

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"abc": {
				Hash:         "abc",
				Name:         "Done",
				CompletionOn: 1700000123,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
		}))

		requireNoTorrentEvent(t, seen)
		requireNoIntEvent(t, wrongID)
	})
}

func TestHandleCompletionUpdatesFiresOnceWhenCompletionOnAppears(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 9}

		seen := make(chan qbt.Torrent, 2)
		wrongID := make(chan int, 1)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, instanceID int, torrent qbt.Torrent) {
			if instanceID != 9 {
				select {
				case wrongID <- instanceID:
				default:
				}
			}
			seen <- torrent
		})

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"def": {
				Hash:         "def",
				Name:         "Still downloading",
				CompletionOn: -1,
				Progress:     0.50,
				State:        qbt.TorrentStateDownloading,
			},
		}))

		requireNoTorrentEvent(t, seen)
		requireNoIntEvent(t, wrongID)

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"def": {
				Hash:         "def",
				Name:         "Done now",
				CompletionOn: 1700000999,
				Progress:     1.0,
				State:        qbt.TorrentStateUploading,
			},
		}))

		if torrent := requireTorrentEvent(t, seen, "expected a completion event"); torrent.Hash != "def" {
			t.Fatalf("unexpected hash: %q", torrent.Hash)
		}
		requireNoIntEvent(t, wrongID)

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"def": {
				Hash:         "def",
				Name:         "Done now",
				CompletionOn: 1700000999,
				Progress:     1.0,
				State:        qbt.TorrentStateStalledUp,
			},
		}))

		requireNoTorrentEvent(t, seen)
		requireNoIntEvent(t, wrongID)
	})
}

func TestHandleCompletionUpdatesRearmsAfterFailedRecheck(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 3}

		seen := make(chan qbt.Torrent, 2)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			seen <- torrent
		})

		update := func(torrent qbt.Torrent) {
			client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{"ghi": torrent}))
		}

		update(qbt.Torrent{Hash: "ghi", CompletionOn: -1, Progress: 0.9, AmountLeft: 100, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireTorrentEvent(t, seen, "expected first completion event")

		update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 0.3, AmountLeft: 700, State: qbt.TorrentStateCheckingUp})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 0.5, AmountLeft: 500, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireTorrentEvent(t, seen, "expected completion event after re-download finished")

		update(qbt.Torrent{Hash: "ghi", CompletionOn: 1700000999, Progress: 1.0, State: qbt.TorrentStateStalledUp})
		requireNoTorrentEvent(t, seen)
	})
}

func TestHandleCompletionUpdatesIgnoresWestOfUTCSentinel(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 4}

		seen := make(chan qbt.Torrent, 2)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			seen <- torrent
		})

		update := func(torrent qbt.Torrent) {
			client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{"jkl": torrent}))
		}

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		}))

		update(qbt.Torrent{Hash: "jkl", CompletionOn: 28800, Progress: 0.04, AmountLeft: 960, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "jkl", CompletionOn: 1700002000, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireTorrentEvent(t, seen, "expected completion event when download finished")

		update(qbt.Torrent{Hash: "jkl", CompletionOn: 1700002000, Progress: 1.0, State: qbt.TorrentStateStalledUp})
		requireNoTorrentEvent(t, seen)
	})
}

func TestHandleCompletionUpdatesStoppedStatesAreOneWay(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 5}

		seen := make(chan qbt.Torrent, 2)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			seen <- torrent
		})

		update := func(torrent qbt.Torrent) {
			client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{"mno": torrent}))
		}

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		}))

		update(qbt.Torrent{Hash: "mno", CompletionOn: -1, Progress: 0.9, AmountLeft: 100, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateStoppedUp})
		requireTorrentEvent(t, seen, "expected completion event for completion landing in stopped state")

		update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 0.4, AmountLeft: 600, State: qbt.TorrentStateStoppedUp})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "mno", CompletionOn: 1700003000, Progress: 1.0, State: qbt.TorrentStateStoppedUp})
		requireNoTorrentEvent(t, seen)
	})
}

func TestHandleCompletionUpdatesCheckingStatesKeepPriorState(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 6}

		seen := make(chan qbt.Torrent, 2)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			seen <- torrent
		})

		update := func(torrent qbt.Torrent) {
			client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{"pqr": torrent}))
		}

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		}))

		update(qbt.Torrent{Hash: "pqr", CompletionOn: -1, Progress: 0.8, AmountLeft: 200, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)
		update(qbt.Torrent{Hash: "pqr", CompletionOn: 1700004000, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireTorrentEvent(t, seen, "expected completion event")

		update(qbt.Torrent{Hash: "pqr", CompletionOn: 1700004000, Progress: 0.3, AmountLeft: 700, State: qbt.TorrentStateCheckingUp})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "pqr", CompletionOn: 1700004000, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireNoTorrentEvent(t, seen)
	})
}

func TestHandleCompletionUpdatesZeroWantedFinishFiresOnce(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 8}

		seen := make(chan qbt.Torrent, 2)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			seen <- torrent
		})

		update := func(torrent qbt.Torrent) {
			client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{"stu": torrent}))
		}

		client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{
			"zzz": {Hash: "zzz", CompletionOn: 1700000000, Progress: 1.0, State: qbt.TorrentStateStalledUp},
		}))

		update(qbt.Torrent{Hash: "stu", CompletionOn: -1, Progress: 0.05, AmountLeft: 950, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "stu", CompletionOn: 1700005000, Progress: 0.0, AmountLeft: 0, State: qbt.TorrentStateStalledUp})
		requireTorrentEvent(t, seen, "expected completion event for zero-wanted finish")

		update(qbt.Torrent{Hash: "stu", CompletionOn: 1700005000, Progress: 0.0, AmountLeft: 0, State: qbt.TorrentStateStalledUp})
		requireNoTorrentEvent(t, seen)
	})
}

func requireNoTorrentEvent(t *testing.T, ch <-chan qbt.Torrent) {
	t.Helper()

	synctest.Wait()
	select {
	case torrent := <-ch:
		t.Fatalf("unexpected completion event: hash=%q name=%q state=%q completionOn=%d",
			torrent.Hash,
			torrent.Name,
			torrent.State,
			torrent.CompletionOn,
		)
	default:
	}
}

func requireTorrentEvent(t *testing.T, ch <-chan qbt.Torrent, msg string) qbt.Torrent {
	t.Helper()

	synctest.Wait()
	select {
	case torrent := <-ch:
		return torrent
	default:
		t.Fatal(msg)
		return qbt.Torrent{}
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

func TestHandleCompletionUpdatesStartupAfterFailedRecheckFires(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 10}

		seen := make(chan qbt.Torrent, 2)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			seen <- torrent
		})

		update := func(torrent qbt.Torrent) {
			client.handleCompletionUpdates(syncOf(t, src, map[string]qbt.Torrent{"vwx": torrent}))
		}

		update(qbt.Torrent{Hash: "vwx", CompletionOn: 1700006000, Progress: 0.5, AmountLeft: 500, State: qbt.TorrentStateDownloading})
		requireNoTorrentEvent(t, seen)

		update(qbt.Torrent{Hash: "vwx", CompletionOn: 1700006000, Progress: 1.0, State: qbt.TorrentStateUploading})
		requireTorrentEvent(t, seen, "expected completion event after startup mid-redownload baseline")

		update(qbt.Torrent{Hash: "vwx", CompletionOn: 1700006000, Progress: 1.0, State: qbt.TorrentStateStalledUp})
		requireNoTorrentEvent(t, seen)
	})
}

func TestStateHandlersPruneRemovedTorrents(t *testing.T) {
	t.Parallel()

	client := &Client{instanceID: 11}
	src := syncsnap.NewSource()
	update := func(hashes ...string) {
		torrents := make(map[string]qbt.Torrent, len(hashes))
		for _, hash := range hashes {
			torrents[hash] = qbt.Torrent{Hash: hash, State: qbt.TorrentStateDownloading}
		}
		synced := syncOf(t, src, torrents)
		client.handleCompletionUpdates(synced)
		client.handleAddedUpdates(synced)
	}

	update("abc", "def")
	update("def")

	if _, ok := client.completionState["abc"]; ok || len(client.completionState) != 1 {
		t.Fatalf("completionState kept removed torrent: %v", client.completionState)
	}
	if _, ok := client.addedState["abc"]; ok || len(client.addedState) != 1 {
		t.Fatalf("addedState kept removed torrent: %v", client.addedState)
	}
}

func TestStateHandlersReadAPartialUpdate(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		client := &Client{instanceID: 15}
		completed := make(chan qbt.Torrent, 4)
		client.SetTorrentCompletionHandler(nil, func(_ context.Context, _ int, torrent qbt.Torrent) {
			completed <- torrent
		})
		added := make(chan []*qbt.Torrent, 4)
		client.SetTorrentAddedHandler(func(_ context.Context, _ int, torrents []*qbt.Torrent) {
			added <- torrents
		})
		handle := func(update qbt.SyncUpdate) {
			client.handleCompletionUpdates(update)
			client.handleAddedUpdates(update)
		}
		requireAdded := func(want ...string) {
			t.Helper()
			synctest.Wait()
			var got []string
			select {
			case batch := <-added:
				for _, torrent := range batch {
					got = append(got, torrent.Hash)
				}
			default:
			}
			if !slices.Equal(got, want) {
				t.Fatalf("added torrents = %v, want %v", got, want)
			}
		}

		handle(syncOf(t, src, map[string]qbt.Torrent{
			"downloading": {CompletionOn: -1, AmountLeft: 100, State: qbt.TorrentStateDownloading},
			"seeding":     {CompletionOn: 1700000000, State: qbt.TorrentStateUploading},
		}))
		requireNoTorrentEvent(t, completed)
		requireAdded()

		handle(src.Partial(t, `{"rid":2,"torrents":{
			"downloading":{"completion_on":1700000999,"amount_left":0,"state":"uploading"},
			"seeding":{"upspeed":4096},
			"new":{"added_on":1700001000,"amount_left":50,"state":"downloading"}}}`))
		if torrent := requireTorrentEvent(t, completed, "expected a completion event"); torrent.Hash != "downloading" {
			t.Fatalf("completed torrent = %q, want downloading", torrent.Hash)
		}
		requireNoTorrentEvent(t, completed)
		requireAdded("new")

		handle(src.Partial(t, `{"rid":3,"torrents":{"seeding":{"upspeed":0}}}`))
		requireNoTorrentEvent(t, completed)
		requireAdded()

		handle(src.Partial(t, `{"rid":4,"torrents_removed":["new"]}`))
		requireAdded()
		handle(src.Partial(t, `{"rid":5,"torrents":{"new":{"added_on":1700002000,"amount_left":50,"state":"downloading"}}}`))
		requireNoTorrentEvent(t, completed)
		requireAdded("new")
	})
}

func completeBurst(t testing.TB, src *syncsnap.Source, client *Client, n int) {
	for _, complete := range []bool{false, true} {
		torrents := make(map[string]qbt.Torrent, n)
		for i := range n {
			hash := fmt.Sprintf("%040x", i)
			torrent := qbt.Torrent{Hash: hash, State: qbt.TorrentStateDownloading, Progress: 0.5, AmountLeft: 1, CompletionOn: -1}
			if complete {
				torrent = qbt.Torrent{Hash: hash, State: qbt.TorrentStateUploading, Progress: 1, CompletionOn: 1700000000}
			}
			torrents[hash] = torrent
		}
		client.handleCompletionUpdates(syncOf(t, src, torrents))
	}
}

func TestHandleCompletionUpdatesBurstRunsOnBoundedGoroutines(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		const burst = 10_000
		client := &Client{instanceID: 12}

		var running, maxRunning, calls atomic.Int64
		release := make(chan struct{})
		client.SetTorrentCompletionHandler(nil, func(context.Context, int, qbt.Torrent) {
			n := running.Add(1)
			for {
				prev := maxRunning.Load()
				if n <= prev || maxRunning.CompareAndSwap(prev, n) {
					break
				}
			}
			<-release
			running.Add(-1)
			calls.Add(1)
		})

		completeBurst(t, src, client, burst)

		synctest.Wait()
		got := maxRunning.Load()
		close(release)
		if got == 0 || got > handlerQueueWorkers {
			t.Fatalf("handlers running at once = %d, want 1..%d", got, handlerQueueWorkers)
		}

		synctest.Wait()
		if got := calls.Load(); got != burst {
			t.Fatalf("handler calls = %d, want %d", got, burst)
		}
	})
}

func TestHandleCompletionUpdatesNotifiesWhileSearchesWait(t *testing.T) {
	t.Parallel()

	const burst = handlerQueueWorkers + 1
	client := &Client{instanceID: 14}
	src := syncsnap.NewSource()

	var notified atomic.Int64
	lane := make(chan struct{})
	defer close(lane)
	client.SetTorrentCompletionHandler(
		func(context.Context, int, qbt.Torrent) { notified.Add(1) },
		func(context.Context, int, qbt.Torrent) { <-lane },
	)

	completeBurst(t, src, client, burst)

	if got := notified.Load(); got != burst {
		t.Fatalf("notified = %d while every search worker waits, want %d", got, burst)
	}
}

func TestHandleAddedUpdatesBurstCallsHandlerOncePerSync(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		const burst = 10_000
		client := &Client{instanceID: 13}

		batches := make(chan []*qbt.Torrent, burst)
		client.SetTorrentAddedHandler(func(_ context.Context, _ int, torrents []*qbt.Torrent) {
			batches <- torrents
		})

		client.handleAddedUpdates(syncOf(t, src, map[string]qbt.Torrent{"seed": {Hash: "seed"}}))

		now := time.Now().Unix()
		torrents := map[string]qbt.Torrent{
			"seed": {Hash: "seed"},
			"old":  {Hash: "old", AddedOn: now - 3600},
		}
		for i := range burst {
			hash := fmt.Sprintf("%040x", i)
			torrents[hash] = qbt.Torrent{Hash: hash, AddedOn: now}
		}
		client.handleAddedUpdates(syncOf(t, src, torrents))

		synctest.Wait()
		select {
		case batch := <-batches:
			if len(batch) != burst {
				t.Fatalf("batch size = %d, want %d", len(batch), burst)
			}
		default:
			t.Fatal("expected one torrent-added batch")
		}
		select {
		case batch := <-batches:
			t.Fatalf("unexpected second batch of %d torrents", len(batch))
		default:
		}
	})
}

func TestHandleAddedUpdatesRunsOnBoundedGoroutines(t *testing.T) {
	t.Parallel()

	src := syncsnap.NewSource()
	synctest.Test(t, func(t *testing.T) {
		const syncs = 40
		client := &Client{instanceID: 14}

		var running, maxRunning, calls atomic.Int64
		release := make(chan struct{})
		client.SetTorrentAddedHandler(func(context.Context, int, []*qbt.Torrent) {
			n := running.Add(1)
			for {
				prev := maxRunning.Load()
				if n <= prev || maxRunning.CompareAndSwap(prev, n) {
					break
				}
			}
			<-release
			running.Add(-1)
			calls.Add(1)
		})

		torrents := map[string]qbt.Torrent{"seed": {Hash: "seed"}}
		client.handleAddedUpdates(syncOf(t, src, torrents))
		for i := range syncs {
			hash := fmt.Sprintf("%040x", i)
			torrents[hash] = qbt.Torrent{Hash: hash, AddedOn: time.Now().Unix()}
			client.handleAddedUpdates(syncOf(t, src, maps.Clone(torrents)))
		}

		synctest.Wait()
		got := maxRunning.Load()
		close(release)
		if got == 0 || got > handlerQueueWorkers {
			t.Fatalf("handlers running at once = %d, want 1..%d", got, handlerQueueWorkers)
		}

		synctest.Wait()
		if got := calls.Load(); got != syncs {
			t.Fatalf("handler calls = %d, want %d", got, syncs)
		}
	})
}
