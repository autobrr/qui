// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"maps"
	"slices"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

type fakeSinceStore struct {
	mu         sync.Mutex
	rows       map[string]time.Time
	failList   bool
	failPut    bool
	failDelete bool
	puts       int
	deletes    int
}

func (f *fakeSinceStore) ListUnregisteredSince(context.Context, int) (map[string]time.Time, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failList {
		return nil, errors.New("database is locked")
	}
	return maps.Clone(f.rows), nil
}

func (f *fakeSinceStore) PutUnregisteredSince(_ context.Context, _ int, since map[string]time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.puts++
	if f.failPut {
		return errors.New("database is locked")
	}
	if f.rows == nil {
		f.rows = make(map[string]time.Time)
	}
	maps.Copy(f.rows, since)
	return nil
}

func (f *fakeSinceStore) DeleteUnregisteredSince(_ context.Context, _ int, hashes []string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.deletes++
	if f.failDelete {
		return errors.New("database is locked")
	}
	for _, hash := range hashes {
		delete(f.rows, hash)
	}
	return nil
}

func (f *fakeSinceStore) stored() map[string]time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return maps.Clone(f.rows)
}

type sinceRig struct {
	*flagsRig
	store *fakeSinceStore
	now   time.Time
}

func newSinceRig(t *testing.T, webAPI string) *sinceRig {
	t.Helper()
	rig := &sinceRig{flagsRig: newFlagsRigAt(t, webAPI), store: &fakeSinceStore{}, now: time.Now().Truncate(time.Second)}
	rig.sm.trackerHealthNow = func() time.Time { return rig.now }
	rig.sm.SetUnregisteredSinceStore(rig.store)
	rig.set(func() {
		down := rig.torrents[flagsHashDown]
		down.trackers = []qbt.TorrentTracker{{Url: "https://down.example.invalid/announce/abcdef123456", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}
	})
	return rig
}

func (rig *sinceRig) answer(status qbt.TrackerStatus, message string) {
	rig.set(func() {
		tracker := &rig.torrents[flagsHashDown].trackers[0]
		tracker.Status, tracker.Message = status, message
	})
}

func (rig *sinceRig) since(t *testing.T) (time.Time, bool) {
	t.Helper()
	return rig.sm.GetTrackerHealthSnapshot(1).UnregisteredSince(flagsHashDown)
}

func (rig *sinceRig) requireSince(t *testing.T, want time.Time, msg string) {
	t.Helper()
	since, ok := rig.since(t)
	require.True(t, ok, msg)
	require.True(t, want.Equal(since), "%s: since %v, want %v", msg, since, want)
	require.True(t, want.Equal(rig.store.stored()[flagsHashDown]), "%s: the store holds the same time", msg)
}

func (rig *sinceRig) requireNoSince(t *testing.T, msg string) {
	t.Helper()
	_, ok := rig.since(t)
	require.False(t, ok, msg)
	require.NotContains(t, rig.store.stored(), flagsHashDown, msg)
}

func TestUnregisteredSinceComesBackFromTheStoreAfterARestart(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	started := rig.now
	rig.pass(t)
	rig.requireSince(t, started, "first run")
	requireUnregisteredBy(t, rig, flagsHashDown, "down.example.invalid")

	rig.now = rig.now.Add(time.Hour)
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.answer(qbt.TrackerStatusNotWorking, "timed out")
	rig.pass(t)
	rig.requireSince(t, started, "a tracker that is down keeps the time")

	rig.sm.StopTrackerHealthRefresh(1)
	rig.sm.trackerHealthMu.Lock()
	clear(rig.sm.trackerHealthSince)
	rig.sm.trackerHealthMu.Unlock()
	rig.now = rig.now.Add(48 * time.Hour)
	rig.answer(qbt.TrackerStatusNotContacted, "")
	rig.set(func() { rig.torrents[flagsHashDown].flags = [3]bool{} })
	require.True(t, fetched(rig.pass(t), flagsHashDown), "a torrent with a stored time and no flags is fetched when the refresh starts")
	rig.requireSince(t, started, "the time continues after a restart of qui and of qBittorrent")

	rig.answer(qbt.TrackerStatusNotWorking, "Unregistered torrent")
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.pass(t)
	rig.requireSince(t, started, "the verdict after the restart keeps the old time")
}

func TestUnregisteredSinceIsNotTrackedUntilTheStoreLoads(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.store.failList = true
	rig.pass(t)
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashDown])
	require.False(t, rig.sm.GetTrackerHealthSnapshot(1).UnregisteredSinceLoaded(), "delete rules see that the times are not loaded")
	rig.requireNoSince(t, "no time before the store loads")

	rig.store.failList = false
	rig.pass(t)
	require.True(t, rig.sm.GetTrackerHealthSnapshot(1).UnregisteredSinceLoaded())
	rig.requireSince(t, rig.now, "the next pass loads the store and starts the time")
}

func TestUnregisteredSinceRetriesAFailedWrite(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.store.failPut = true
	started := rig.now
	rig.pass(t)
	require.NotContains(t, rig.store.stored(), flagsHashDown)

	rig.store.failPut = false
	rig.now = rig.now.Add(time.Minute)
	rig.pass(t)
	rig.requireSince(t, started, "the next pass writes the time that the failed write missed")
}

func TestUnregisteredSinceWritesANewTimeWhenARemovalFails(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.pass(t)
	rig.store.failDelete = true
	rig.store.rows["other"] = rig.now.Add(-30 * 24 * time.Hour)
	rig.sm.trackerHealthMu.Lock()
	rig.sm.sinceFor(1).stored["other"] = rig.store.rows["other"]
	rig.sm.trackerHealthMu.Unlock()

	rig.now = rig.now.Add(time.Hour)
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.answer(qbt.TrackerStatusOK, "")
	rig.pass(t)
	rig.now = rig.now.Add(time.Hour)
	rig.answer(qbt.TrackerStatusNotWorking, "Unregistered torrent")
	rig.pass(t)
	rig.requireSince(t, rig.now, "the new time is written while the removal of another row fails")
}

func TestUnregisteredSinceStaysOverAStopOfTheRefresh(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.pass(t)
	rig.store.failDelete = true
	rig.now = rig.now.Add(time.Hour)
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.answer(qbt.TrackerStatusOK, "")
	rig.pass(t)
	require.Contains(t, rig.store.stored(), flagsHashDown, "the removal failed")

	rig.sm.StopTrackerHealthRefresh(1)
	rig.answer(qbt.TrackerStatusNotContacted, "")
	rig.pass(t)
	_, ok := rig.since(t)
	require.False(t, ok, "an edit or a switch of the instance keeps the removal that the store missed")
}

func TestUnregisteredSinceEditRemovesTheStoredTimeAtOnce(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.pass(t)
	require.NoError(t, rig.sm.EditTorrentTracker(t.Context(), 1, flagsHashDown, "https://down.example.invalid/announce/abcdef123456", "https://new.example.invalid/announce"))
	require.NotContains(t, rig.store.stored(), flagsHashDown, "a restart right after the edit finds no old time")

	rig.now = rig.now.Add(time.Hour)
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.answer(qbt.TrackerStatusNotContacted, "")
	rig.pass(t)
	rig.requireNoSince(t, "the next pass after the edit starts no time")
}

func TestUnregisteredSinceDropsTheStoredTimeOfAnEditBeforeTheLoad(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.store.rows = map[string]time.Time{flagsHashDown: rig.now.Add(-30 * 24 * time.Hour)}
	rig.sm.trackerHealthMu.Lock()
	rig.sm.markTrackersEdited(1, []string{flagsHashDown}, rig.now)
	rig.sm.trackerHealthMu.Unlock()
	rig.sm.loadUnregisteredSince(t.Context(), 1)
	require.NotContains(t, rig.sm.unregisteredSinceHashes(1), flagsHashDown, "the load leaves out the time of a torrent whose tracker changed")

	rig.sm.storeUnregisteredSince(t.Context(), 1)
	require.Empty(t, rig.store.stored(), "the next write removes the row")
}

func TestUnregisteredSinceIsRemoved(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(t *testing.T, rig *sinceRig)
	}{
		{"the torrent was added again after the time", func(_ *testing.T, rig *sinceRig) {
			rig.answer(qbt.TrackerStatusNotContacted, "")
			rig.set(func() { rig.torrents[flagsHashDown].addedOn = rig.now.Unix() })
		}},
		{"the user adds a tracker to many torrents", func(t *testing.T, rig *sinceRig) {
			require.NoError(t, rig.sm.BulkAddTrackers(t.Context(), 1, []string{flagsHashDown, flagsHashError}, "https://new.example.invalid/announce"))
			rig.answer(qbt.TrackerStatusNotContacted, "")
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			rig := newSinceRig(t, "2.15.1")
			rig.pass(t)
			rig.requireSince(t, rig.now, "the verdict starts the time")

			rig.now = rig.now.Add(time.Hour)
			rig.sm.trackerHealthDetailMaxAge = 0
			tt.change(t, rig)
			rig.pass(t)
			rig.requireNoSince(t, tt.name)
		})
	}
}

func TestUnregisteredSinceRestartsAfterARemoval(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.pass(t)
	rig.set(func() { rig.torrents[flagsHashDown].state = string(qbt.TorrentStateStoppedUp) })
	rig.pass(t)
	rig.requireNoSince(t, "stopped")

	rig.now = rig.now.Add(30 * 24 * time.Hour)
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.set(func() { rig.torrents[flagsHashDown].state = string(qbt.TorrentStateStalledUp) })
	rig.pass(t)
	rig.requireSince(t, rig.now, "the first verdict after the start starts a new time")
}

func TestUnregisteredSinceKeepsRowsBeforeTheFirstSync(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	old := rig.now.Add(-72 * time.Hour)
	rig.store.rows = map[string]time.Time{flagsHashDown: old}
	rig.sm.loadUnregisteredSince(t.Context(), 1)

	rig.sm.applyTrackerHealthRefreshResult(1, nil, nil, nil, false, rig.now, true)
	rig.sm.storeUnregisteredSince(t.Context(), 1)
	require.Equal(t, map[string]time.Time{flagsHashDown: old}, rig.store.stored(), "a pass before the first sync removes no row")

	rig.set(func() { rig.failSync = true })
	rig.sm.refreshTrackerHealthCounts(t.Context(), 1)
	require.Equal(t, map[string]time.Time{flagsHashDown: old}, rig.store.stored(), "a refresh while the first sync fails removes no row")

	rig.sm.applyTrackerHealthRefreshResult(1, nil, nil, nil, true, rig.now, true)
	rig.sm.storeUnregisteredSince(t.Context(), 1)
	require.Empty(t, rig.store.stored(), "a completed sync without the torrent removes its row")
}

func TestUnregisteredSinceIgnoresAnEditBeforeAPassThatStartedEarlier(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.sync(t)
	rig.sm.loadUnregisteredSince(t.Context(), 1)
	started := rig.now
	rig.now = rig.now.Add(time.Second)
	rig.sm.clearTrackerHealthAndRefetch(t.Context(), 1, []string{flagsHashDown})

	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	torrents := snapshotTorrents(client.SyncOwner().SnapshotUnchecked())
	var details TrackerLookup
	rig.set(func() { details = TrackerLookup{flagsHashDown: rig.torrents[flagsHashDown].trackers} })

	rig.sm.applyTrackerHealthRefreshResult(1, torrents, details, nil, true, started, true)
	_, ok := rig.since(t)
	require.False(t, ok, "a pass that started before the edit does not bring the time back")

	rig.sm.applyTrackerHealthRefreshResult(1, torrents, details, nil, true, rig.now.Add(time.Second), true)
	_, ok = rig.since(t)
	require.True(t, ok, "a pass that started after the edit starts the time")
}

func TestTrackersReadStartsTheTimeAndIgnoresPseudoRows(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.answer(qbt.TrackerStatusNotWorking, "timed out")
	rig.pass(t)
	rig.requireNoSince(t, "no verdict yet")

	rig.answer(qbt.TrackerStatusNotWorking, "Unregistered torrent")
	rig.readTrackers(t, flagsHashDown)
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashDown])
	rig.requireSince(t, rig.now, "a read of the trackers of one torrent starts the time with the verdict")

	rig.now = rig.now.Add(time.Hour)
	rig.set(func() {
		down := rig.torrents[flagsHashDown]
		down.trackers = append(down.trackers, qbt.TorrentTracker{Url: "** [DHT] **", Status: qbt.TrackerStatusOK})
	})
	rig.readTrackers(t, flagsHashDown)
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashDown], "a DHT row is not a tracker")
	rig.requireSince(t, rig.now.Add(-time.Hour), "a working DHT row removes no time")
}

func fetched(requests [][]string, hash string) bool {
	return slices.ContainsFunc(requests, func(hashes []string) bool { return slices.Contains(hashes, hash) })
}

func TestUnregisteredSinceEndsWhenTheFreshReadFindsAWorkingTracker(t *testing.T) {
	t.Parallel()

	for _, webAPI := range []string{"2.15.1", "2.11.4"} {
		t.Run(webAPI, func(t *testing.T) {
			t.Parallel()
			rig := newSinceRig(t, webAPI)
			rig.pass(t)
			rig.requireSince(t, rig.now, "the verdict starts the time")

			rig.now = rig.now.Add(time.Hour)
			rig.answer(qbt.TrackerStatusOK, "")
			still, err := rig.sm.StillUnregistered(t.Context(), 1, []string{flagsHashDown})
			require.NoError(t, err)
			require.Zero(t, still.Len(), "the fresh read keeps the torrent")
			rig.requireNoSince(t, "a working tracker in the fresh read restarts the wait before the next pass")
			require.NotContains(t, rig.health(t), flagsHashDown, "the snapshot no longer says unregistered")
		})
	}
}
