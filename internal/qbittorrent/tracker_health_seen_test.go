// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func (rig *flagsRig) seenQueue() chan seenTrackers {
	seen := make(chan seenTrackers, 1)
	rig.sm.trackerHealthMu.Lock()
	rig.sm.trackerHealthSeen[1] = seen
	rig.sm.trackerHealthMu.Unlock()
	return seen
}

func (rig *flagsRig) sync(t *testing.T) {
	t.Helper()
	client, err := rig.sm.clientPool.GetClient(t.Context(), 1)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
}

func (rig *flagsRig) changeErrorTorrentToDown() {
	rig.set(func() {
		errored := rig.torrents[flagsHashError]
		errored.flags = [3]bool{false, false, true}
		errored.trackers = []qbt.TorrentTracker{{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusUnreachable}}
	})
}

func (rig *flagsRig) queueTrackersRead(t *testing.T, hash string) seenTrackers {
	t.Helper()
	seen := rig.seenQueue()
	_, err := rig.sm.GetTorrentTrackers(t.Context(), 1, hash)
	require.NoError(t, err)
	return takeRead(t, seen)
}

func takeRead(t *testing.T, seen chan seenTrackers) seenTrackers {
	t.Helper()
	select {
	case read := <-seen:
		return read
	default:
		t.Fatal("the trackers read queued nothing")
		return seenTrackers{}
	}
}

func (rig *flagsRig) readTrackers(t *testing.T, hash string) {
	t.Helper()
	rig.sm.applySeenTrackers(t.Context(), 1, rig.queueTrackersRead(t, hash))
}

func TestTrackersReadUpdatesLabelBeforeDetailsExpire(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)
	require.Equal(t, TrackerHealthError, rig.health(t)[flagsHashError])

	rig.set(func() {
		rig.torrents[flagsHashError].trackers = []qbt.TorrentTracker{
			{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"},
		}
	})
	require.Empty(t, rig.pass(t), "the flags did not change, so the pass reuses the old details")
	require.Equal(t, TrackerHealthError, rig.health(t)[flagsHashError])

	rig.readTrackers(t, flagsHashError)
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashError], "the Trackers tab read sets the label")

	require.Empty(t, rig.pass(t))
	require.Equal(t, TrackerHealthUnregistered, rig.health(t)[flagsHashError], "the next pass keeps the label of the read")
}

func TestTrackersReadGivesNoLabelWithoutAnnounceFlags(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)
	rig.set(func() {
		rig.torrents[flagsHashHealthy].trackers = []qbt.TorrentTracker{
			{Url: "https://ok.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"},
		}
	})

	rig.readTrackers(t, flagsHashHealthy)
	require.NotContains(t, rig.health(t), flagsHashHealthy, "qBittorrent reports no announce problem, as the pass reads it")
}

func TestTrackersReadIsDroppedWhenFlagsChangeBeforeItApplies(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)
	rig.set(func() {
		rig.torrents[flagsHashError].trackers = []qbt.TorrentTracker{
			{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"},
		}
	})
	read := rig.queueTrackersRead(t, flagsHashError)

	rig.changeErrorTorrentToDown()
	rig.sync(t)
	rig.sm.applySeenTrackers(t.Context(), 1, read)
	require.Equal(t, TrackerHealthError, rig.health(t)[flagsHashError], "a read from before the flag change sets no label")

	require.Equal(t, [][]string{{flagsHashError}}, rig.pass(t), "the pass fetches the torrent whose flags changed")
	require.Equal(t, TrackerHealthDown, rig.health(t)[flagsHashError])
}

func TestTrackersReadKeepsTheFlagsFromBeforeTheRequest(t *testing.T) {
	t.Parallel()

	rig := newFlagsRig(t)
	rig.pass(t)
	seen := rig.seenQueue()
	client, owner, err := rig.sm.getClientAndSyncManager(t.Context(), 1)
	require.NoError(t, err)
	noteRead := rig.sm.beginTrackersRead(1, owner, flagsHashError)
	stale := []qbt.TorrentTracker{{Url: "https://error.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}

	rig.changeErrorTorrentToDown()
	rig.sync(t)
	require.True(t, client.Supports(CapAnnounceFlags))
	noteRead(stale)
	rig.sm.applySeenTrackers(t.Context(), 1, takeRead(t, seen))
	require.Equal(t, TrackerHealthError, rig.health(t)[flagsHashError], "a response that started before the flag change sets no label")
}
