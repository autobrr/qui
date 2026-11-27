// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func newTwoUnregisteredRig(t *testing.T) *sinceRig {
	t.Helper()
	rig := newSinceRig(t, "2.15.1")
	rig.set(func() {
		rig.torrents[flagsHashError].trackers = []qbt.TorrentTracker{{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}
	})
	rig.pass(t)
	require.Equal(t, map[string]TrackerHealth{flagsHashDown: TrackerHealthUnregistered, flagsHashError: TrackerHealthUnregistered}, rig.health(t))
	return rig
}

func requireUnregisteredBy(t *testing.T, rig *sinceRig, hash, host string) {
	t.Helper()
	tracker, ok := rig.sm.GetTrackerHealthSnapshot(1).UnregisteredBy(hash)
	require.True(t, ok, "%s keeps the tracker that said unregistered", hash)
	require.Equal(t, UnregisteredTracker{Host: host, Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}, tracker)
}

func TestTrackersReadNamesTheUnregisteredTrackerAndKeepsTheOthers(t *testing.T) {
	t.Parallel()

	rig := newTwoUnregisteredRig(t)
	rig.now = rig.now.Add(time.Hour)
	rig.answer(qbt.TrackerStatusOK, "")
	rig.readTrackers(t, flagsHashDown)
	_, ok := rig.sm.GetTrackerHealthSnapshot(1).UnregisteredBy(flagsHashDown)
	require.False(t, ok, "a working tracker names no unregistered tracker")
	requireUnregisteredBy(t, rig, flagsHashError, "error.example.invalid")

	rig.answer(qbt.TrackerStatusNotWorking, "Unregistered torrent")
	rig.readTrackers(t, flagsHashDown)
	requireUnregisteredBy(t, rig, flagsHashDown, "down.example.invalid")
	requireUnregisteredBy(t, rig, flagsHashError, "error.example.invalid")
}

func TestTrackersReadThatChangesNothingKeepsTheSnapshot(t *testing.T) {
	t.Parallel()

	rig := newTwoUnregisteredRig(t)
	before := rig.sm.GetTrackerHealthSnapshot(1)
	rig.readTrackers(t, flagsHashDown)
	require.Same(t, before, rig.sm.GetTrackerHealthSnapshot(1), "an unchanged read builds no new snapshot")
}

func TestTrackersReadKeepsTheTimesOfOtherTorrents(t *testing.T) {
	t.Parallel()

	rig := newTwoUnregisteredRig(t)
	started := rig.now
	rig.now = rig.now.Add(time.Hour)
	rig.answer(qbt.TrackerStatusOK, "")
	rig.readTrackers(t, flagsHashDown)
	rig.requireNoSince(t, "a working tracker removes the time")
	since, ok := rig.sm.GetTrackerHealthSnapshot(1).UnregisteredSince(flagsHashError)
	require.True(t, ok, "the read of one torrent keeps the time of another")
	require.True(t, started.Equal(since))
}

func TestUnregisteredSinceWritesOnlyWhatChanged(t *testing.T) {
	t.Parallel()

	rig := newSinceRig(t, "2.15.1")
	rig.pass(t)
	require.Equal(t, [2]int{1, 0}, [2]int{rig.store.puts, rig.store.deletes}, "a new time is a write and no removal")

	rig.now = rig.now.Add(time.Hour)
	rig.sm.trackerHealthDetailMaxAge = 0
	rig.answer(qbt.TrackerStatusOK, "")
	rig.pass(t)
	rig.requireNoSince(t, "a working tracker removes the time")
	require.Equal(t, [2]int{1, 1}, [2]int{rig.store.puts, rig.store.deletes}, "a removal is no write")

	rig.pass(t)
	require.Equal(t, [2]int{1, 1}, [2]int{rig.store.puts, rig.store.deletes}, "a removed time is not removed again")
}

func TestTrackerVerdictNamesOnlyTheUnregisteredTracker(t *testing.T) {
	t.Parallel()

	torrent := &qbt.Torrent{AddedOn: 1}
	row := func(status qbt.TrackerStatus, message string) qbt.TorrentTracker {
		return qbt.TorrentTracker{Url: "https://tracker.example.invalid/announce", Status: status, Message: message}
	}
	for _, tc := range []struct {
		name     string
		trackers []qbt.TorrentTracker
		health   TrackerHealth
		by       int
	}{
		{"no trackers", nil, "", -1},
		{"a working tracker", []qbt.TorrentTracker{row(qbt.TrackerStatusNotWorking, "Unregistered torrent"), row(qbt.TrackerStatusOK, "")}, "", -1},
		{"the second tracker", []qbt.TorrentTracker{row(qbt.TrackerStatusDisabled, ""), row(qbt.TrackerStatusNotWorking, "Unregistered torrent")}, TrackerHealthUnregistered, 1},
		{"down", []qbt.TorrentTracker{row(qbt.TrackerStatusUnreachable, "")}, TrackerHealthDown, -1},
		{"error", []qbt.TorrentTracker{row(qbt.TrackerStatusNotWorking, "torrent banned")}, TrackerHealthError, -1},
		{"updating", []qbt.TorrentTracker{row(qbt.TrackerStatusUpdating, "")}, "", -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			health, by := trackerVerdictOf(torrent, tc.trackers, time.Now())
			require.Equal(t, tc.health, health)
			require.Equal(t, tc.by, by)
		})
	}
}
