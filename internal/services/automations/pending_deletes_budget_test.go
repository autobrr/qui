// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"encoding/json/v2"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/logbudget"
)

func TestPendingDeleteLogsDoNotGrowWithTheWaitingTorrents(t *testing.T) {
	run := func(waiting int) logbudget.Lines {
		torrents := make(map[string]waitBudgetTorrent, waiting)
		health := make(map[string]qbittorrent.TrackerHealth, waiting)
		since := make(map[string]time.Time, waiting)
		for i := range waiting {
			hash := fmt.Sprintf("%040x", 0xc000+i)
			torrents[hash] = waitBudgetTorrent{Name: fmt.Sprintf("Some.Show.S01E%02d.1080p.WEB-GRP", i+1), Ratio: 0.5, Progress: 1,
				AddedOn: int64(i + 1), State: "stalledUP", SavePath: "/data", ContentPath: fmt.Sprintf("/data/e%d", i)}
			health[hash] = qbittorrent.TrackerHealthUnregistered
			since[hash] = waitTestNow.Add(-time.Hour)
		}
		raw, err := json.Marshal(torrents)
		require.NoError(t, err)
		rig := newPendingRig(t, string(raw))
		rig.setHealth(health, since)
		rule := rig.createRule(waitDeleteRule(0, nil, unregisteredIs("true")))
		return logbudget.CountWhere(t, func(text string) bool {
			if strings.Contains(text, "tracker health") {
				return false
			}
			return !strings.Contains(text, "http://127.0.0.1:") || strings.Contains(text, rig.stub.url)
		}, func() {
			require.Len(t, rig.run(rule), 1)
			require.Len(t, rig.rows(), waiting)
			require.Empty(t, rig.run(rule))
			rig.setHealth(nil, nil)
			lines := rig.run(rule)
			require.Len(t, lines, 1)
			require.Equal(t, models.ActivityActionUnregisteredWaitEnded, lines[0].Action)
		})
	}
	logbudget.AssertFlat(t, run(2), run(30))
}
