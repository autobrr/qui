// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/notifications"
)

const waitNotifyTorrents = `{
	"` + waitFreshHash + `": {"name": "Some.Show.S01E01.1080p.WEB-GRP", "tracker": "https://tracker-a.test/announce", "ratio": 0.5, "progress": 1, "added_on": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/fresh"},
	"` + waitOldHash + `": {"name": "Other.Film.2024.2160p.BluRay-GRP", "tracker": "https://tracker-a.test/announce", "ratio": 0.5, "progress": 1, "added_on": 2, "state": "stalledUP", "save_path": "/data", "content_path": "/data/old"},
	"` + waitDownHash + `": {"name": "Third.Show.S02E03.720p.HDTV-GRP", "tracker": "https://tracker-b.test/announce", "ratio": 0.5, "progress": 1, "added_on": 3, "state": "stalledUP", "save_path": "/data", "content_path": "/data/down"},
	"` + waitFineHash + `": {"name": "Fourth.Film.2023.1080p.WEB-GRP", "tracker": "https://tracker-b.test/announce", "ratio": 3, "progress": 1, "added_on": 4, "state": "stalledUP", "save_path": "/data", "content_path": "/data/fine"}
}`

func TestARunNotifiesOnceWhenTorrentsStartToWait(t *testing.T) {
	rig := newPendingRig(t, waitNotifyTorrents)
	notifier := &automationRecordingNotifier{}
	rig.svc.notifier = notifier
	rule := waitDeleteRule(0, new(2*24*3600), unregisteredIs("true"))
	rule.Name = "Clean unregistered"
	rule.Notify = true
	rule = rig.createRule(rule)

	rig.run(rule)
	events := notifier.Events()
	require.Len(t, events, 1)
	event := events[0]
	require.Equal(t, notifications.EventAutomationsActionsApplied, event.Type)
	require.Equal(t, 1, event.Automations.Applied, "the torrent past its wait is deleted")
	require.Equal(t, 2, event.Automations.Waiting, "the deleted torrent does not wait")
	lines := strings.Split(event.Message, "\n")
	require.Contains(t, lines, "Applied: 1")
	require.Contains(t, lines, "Waiting to delete: 2")
	require.Contains(t, lines, "Waiting rules: Clean unregistered=2 (wait 2d)")
	require.Contains(t, lines, "Waiting trackers: tracker-a.test=1; tracker-b.test=1")
	require.Contains(t, lines, "Waiting samples: Some.Show.S01E01.1080p.WEB-GRP; Third.Show.S02E03.720p.HDTV-GRP")

	rig.run(rule)
	require.Len(t, notifier.Events(), 1, "the second run sends nothing for torrents that already wait")

	rig.setHealth(map[string]qbittorrent.TrackerHealth{
		waitFreshHash: qbittorrent.TrackerHealthDown,
		waitDownHash:  qbittorrent.TrackerHealthDown,
	}, map[string]time.Time{
		waitFreshHash: waitTestNow.Add(-24 * time.Hour),
		waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
	})
	rig.run(rule)
	require.Len(t, notifier.Events(), 1, "a tracker that is down sends no second message")
}

func TestARunSendsNoWaitNotificationForARuleWithNotifyOff(t *testing.T) {
	rig := newPendingRig(t, waitNotifyTorrents)
	notifier := &automationRecordingNotifier{}
	rig.svc.notifier = notifier
	rule := rig.createRule(waitDeleteRule(0, new(pendingYear), unregisteredIs("true")))

	lines := rig.run(rule)
	require.Len(t, lines, 1)
	requireStartedLine(t, lines[0], rule, 3)
	require.Empty(t, notifier.Events())
}
