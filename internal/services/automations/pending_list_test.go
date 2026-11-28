// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	listGoneHash = "6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e6e"
	listPasskey  = "c0ffee1234567890deadbeef"
	listTorrents = `{
	"` + waitFreshHash + `": {"name": "Some.Show.S01E01.1080p.WEB-GRP", "added_on": 1, "state": "stalledUP", "tracker": "https://fresh.example.invalid/` + listPasskey + `/announce"},
	"` + waitDownHash + `": {"name": "Third.Show.S02E03.720p.HDTV-GRP", "added_on": 3, "state": "stalledUP", "tracker": "https://down.example.invalid/` + listPasskey + `/announce"},
	"` + waitOldHash + `": {"name": "Other.Film.2024.2160p.BluRay-GRP", "added_on": 2, "state": "stalledUP"}
}`
)

func TestPendingDeleteListGivesEachWaitingTorrentWithItsTrackerAndTimeOfDelete(t *testing.T) {
	rig := newPendingRig(t, listTorrents)
	named := waitDeleteRule(0, nil, unregisteredIs("true"))
	named.Name = "Clean unregistered"
	rule := rig.createRule(named)
	rig.health.snapshot = qbittorrent.NewTrackerHealthSnapshotWithTrackers(
		map[string]qbittorrent.TrackerHealth{
			waitFreshHash: qbittorrent.TrackerHealthUnregistered,
			waitDownHash:  qbittorrent.TrackerHealthDown,
			waitOldHash:   qbittorrent.TrackerHealthUnregistered,
		},
		map[string]time.Time{
			waitFreshHash: waitTestNow.Add(-24 * time.Hour),
			waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
			waitOldHash:   waitTestNow.Add(-9 * 24 * time.Hour),
		},
		map[string][]qbt.TorrentTracker{waitFreshHash: {{
			Url:     "https://fresh.example.invalid/" + listPasskey + "/announce",
			Status:  qbt.TrackerStatusNotWorking,
			Message: "Unregistered torrent " + listPasskey,
		}}},
	)
	rig.seed(rule.ID, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
		waitOldHash:   waitTestNow.Add(-9 * 24 * time.Hour),
		listGoneHash:  waitTestNow.Add(-time.Hour),
	})

	list, err := rig.svc.ListPendingDeletes(t.Context(), rig.instanceID)
	require.NoError(t, err)

	require.Len(t, list.Rules, 1)
	group := list.Rules[0]
	require.Equal(t, rule.ID, group.RuleID)
	require.Equal(t, "Clean unregistered", group.RuleName)
	require.Len(t, group.Torrents, 3)
	for _, torrent := range group.Torrents {
		require.NotContains(t, torrent.Message, listPasskey)
	}
	fresh := group.Torrents[2]
	fresh.Message = ""
	require.Equal(t, []PendingDeleteTorrent{
		{Hash: waitOldHash, Name: "Other.Film.2024.2160p.BluRay-GRP",
			UnregisteredSince: waitTestNow.Add(-9 * 24 * time.Hour).Unix(), DeletesAt: waitTestNow.Add(-7 * 24 * time.Hour).Unix()},
		{Hash: waitDownHash, Name: "Third.Show.S02E03.720p.HDTV-GRP", Tracker: "down.example.invalid",
			UnregisteredSince: waitTestNow.Add(-8 * 24 * time.Hour).Unix(), DeletesAt: waitTestNow.Add(-6 * 24 * time.Hour).Unix(), AwaitsTracker: true},
		{Hash: waitFreshHash, Name: "Some.Show.S01E01.1080p.WEB-GRP", Tracker: "fresh.example.invalid",
			UnregisteredSince: waitTestNow.Add(-24 * time.Hour).Unix(), DeletesAt: waitTestNow.Add(24 * time.Hour).Unix()},
	}, []PendingDeleteTorrent{group.Torrents[0], group.Torrents[1], fresh})
	require.Contains(t, group.Torrents[2].Message, "Unregistered torrent")
}

func TestPendingDeleteListIsEmptyWithoutStoredRows(t *testing.T) {
	rig := newPendingRig(t, listTorrents)
	rig.createRule(waitDeleteRule(0, nil, unregisteredIs("true")))

	list, err := rig.svc.ListPendingDeletes(t.Context(), rig.instanceID)
	require.NoError(t, err)
	require.NotNil(t, list.Rules)
	require.Empty(t, list.Rules)
}

func TestPendingDeleteListReadsTheWaitFromTheLiveVerdictBeforeTheNextRun(t *testing.T) {
	rig := newPendingRig(t, listTorrents)
	rule := rig.createRule(waitDeleteRule(0, nil, unregisteredIs("true")))
	rig.setHealth(map[string]qbittorrent.TrackerHealth{
		waitFreshHash: qbittorrent.TrackerHealthUnregistered,
	}, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-time.Hour),
	})
	rig.seed(rule.ID, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-72 * time.Hour),
		waitDownHash:  waitTestNow.Add(-72 * time.Hour),
	})

	list, err := rig.svc.ListPendingDeletes(t.Context(), rig.instanceID)
	require.NoError(t, err)

	require.Len(t, list.Rules, 1)
	require.Len(t, list.Rules[0].Torrents, 1)
	restarted := list.Rules[0].Torrents[0]
	require.Equal(t, waitFreshHash, restarted.Hash)
	require.Equal(t, waitTestNow.Add(-time.Hour).Unix(), restarted.UnregisteredSince)
	require.Equal(t, waitTestNow.Add(47*time.Hour).Unix(), restarted.DeletesAt)
	require.False(t, restarted.AwaitsTracker)
}
