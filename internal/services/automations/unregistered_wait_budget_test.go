// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"encoding/json/v2"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/logbudget"
)

type waitBudgetTorrent struct {
	Name        string  `json:"name"`
	Ratio       float64 `json:"ratio"`
	Progress    float64 `json:"progress"`
	AddedOn     int64   `json:"added_on"`
	State       string  `json:"state"`
	SavePath    string  `json:"save_path"`
	ContentPath string  `json:"content_path"`
}

const waitBudgetDeletes = 2

func newWaitBudgetService(t *testing.T, waiting, noLongerUnregistered int) (*Service, int, *stubQbt) {
	t.Helper()
	count := waiting + noLongerUnregistered + waitBudgetDeletes
	torrents := make(map[string]waitBudgetTorrent, count)
	health := make(map[string]qbittorrent.TrackerHealth, count)
	since := make(map[string]time.Time, count)
	still := make(map[string]bool, waitBudgetDeletes)
	for i := range count {
		hash := fmt.Sprintf("%040x", 0xb000+i)
		torrents[hash] = waitBudgetTorrent{Name: fmt.Sprintf("Some.Show.S01E%02d.1080p.WEB-GRP", i+1), Ratio: 0.5, Progress: 1,
			AddedOn: int64(i + 1), State: "stalledUP", SavePath: "/data", ContentPath: fmt.Sprintf("/data/e%d", i)}
		health[hash] = qbittorrent.TrackerHealthUnregistered
		switch {
		case i < waiting:
			since[hash] = waitTestNow.Add(-time.Hour)
		case i < waiting+noLongerUnregistered:
			since[hash] = waitTestNow.Add(-8 * 24 * time.Hour)
		default:
			since[hash] = waitTestNow.Add(-8 * 24 * time.Hour)
			still[hash] = true
		}
	}
	raw, err := json.Marshal(torrents)
	require.NoError(t, err)
	stub := newStubQbt(t, string(raw))
	stub.webAPIVersion = webAPIQbit52
	svc, _, instanceIDs := newStubService(t, nil, stub)
	svc.trackerHealth = &fakeTrackerHealth{snapshot: qbittorrent.NewTrackerHealthSnapshotWithSince(health, since), still: still}
	svc.clock = func() time.Time { return waitTestNow }
	return svc, instanceIDs[0], stub
}

func TestDeleteWaitLogsDoNotGrowWithTheTorrents(t *testing.T) {
	for _, dryRun := range []bool{false, true} {
		t.Run(fmt.Sprintf("dry run %t", dryRun), func(t *testing.T) {
			run := func(waiting int) logbudget.Lines {
				noLongerUnregistered := waiting
				if dryRun {
					noLongerUnregistered = 0
				}
				svc, instanceID, stub := newWaitBudgetService(t, waiting, noLongerUnregistered)
				rules := []*models.Automation{waitDeleteRule(1, nil, unregisteredIs("true"))}
				lines := logbudget.Count(t, func() {
					_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, rules, dryRun)
					require.NoError(t, err)
					if !dryRun {
						require.Len(t, runDeletedHashes(stub), waitBudgetDeletes, "only the due torrents whose tracker still says unregistered go")
					}
				})
				if !dryRun {
					require.Eventually(t, func() bool {
						stub.mu.Lock()
						defer stub.mu.Unlock()
						return stub.syncsSinceWrite >= 2
					}, 5*time.Second, 10*time.Millisecond, "the two syncs that follow a delete end before the stub stops")
				}
				return lines
			}
			logbudget.AssertFlat(t, run(2), run(30))
		})
	}
}
