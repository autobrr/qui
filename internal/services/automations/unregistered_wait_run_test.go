// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json/v2"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/pkg/infohash"
)

const (
	waitFreshHash = "f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1f1"
	waitOldHash   = "0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d"
	waitDownHash  = "d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0d0"
	waitFineHash  = "9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a9a"
)

const waitRunTorrents = `{
	"` + waitFreshHash + `": {"name": "Some.Show.S01E01.1080p.WEB-GRP", "ratio": 0.5, "progress": 1, "added_on": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/fresh"},
	"` + waitOldHash + `": {"name": "Other.Film.2024.2160p.BluRay-GRP", "ratio": 0.5, "progress": 1, "added_on": 2, "state": "stalledUP", "save_path": "/data", "content_path": "/data/old"},
	"` + waitDownHash + `": {"name": "Third.Show.S02E03.720p.HDTV-GRP", "ratio": 0.5, "progress": 1, "added_on": 3, "state": "stalledUP", "save_path": "/data", "content_path": "/data/down"},
	"` + waitFineHash + `": {"name": "Fourth.Film.2023.1080p.WEB-GRP", "ratio": 3, "progress": 1, "added_on": 4, "state": "stalledUP", "save_path": "/data", "content_path": "/data/fine"}
}`

type fakeTrackerHealth struct {
	snapshot  *qbittorrent.TrackerHealthSnapshot
	afterRead *qbittorrent.TrackerHealthSnapshot
	still     map[string]bool
	err       error

	mu    sync.Mutex
	asked [][]string
}

func (f *fakeTrackerHealth) GetTrackerHealthSnapshot(int) *qbittorrent.TrackerHealthSnapshot {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.snapshot
}

func (f *fakeTrackerHealth) StillUnregistered(_ context.Context, _ int, hashes []string) (*infohash.Set, error) {
	f.mu.Lock()
	f.asked = append(f.asked, slices.Sorted(slices.Values(hashes)))
	if f.afterRead != nil {
		f.snapshot = f.afterRead
	}
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	still := infohash.NewSet()
	for _, hash := range hashes {
		parsed, err := infohash.Parse(hash)
		if err != nil {
			return nil, err
		}
		if f.still == nil || f.still[hash] {
			still.Add(parsed)
		}
	}
	return still, nil
}

func newWaitRunService(t *testing.T, health *fakeTrackerHealth) (*Service, int, *stubQbt) {
	t.Helper()
	stub := newStubQbt(t, waitRunTorrents)
	stub.webAPIVersion = webAPIQbit52
	svc, _, instanceIDs := newStubService(t, nil, stub)
	if health.snapshot == nil {
		health.snapshot = qbittorrent.NewTrackerHealthSnapshotWithSince(
			map[string]qbittorrent.TrackerHealth{
				waitFreshHash: qbittorrent.TrackerHealthUnregistered,
				waitOldHash:   qbittorrent.TrackerHealthUnregistered,
				waitDownHash:  qbittorrent.TrackerHealthDown,
			},
			map[string]time.Time{
				waitFreshHash: waitTestNow.Add(-24 * time.Hour),
				waitOldHash:   waitTestNow.Add(-8 * 24 * time.Hour),
				waitDownHash:  waitTestNow.Add(-8 * 24 * time.Hour),
			},
		)
	}
	svc.trackerHealth = health
	svc.clock = func() time.Time { return waitTestNow }
	return svc, instanceIDs[0], stub
}

func runDeletedHashes(stub *stubQbt) []string {
	batches := deletedHashes(stub)
	hashes := make([]string, 0, len(batches))
	for _, batch := range batches {
		hashes = append(hashes, strings.Split(batch, "|")...)
	}
	return hashes
}

func TestDeleteWaitLiveRunDeletesOnlyTheTorrentWhoseWaitIsOver(t *testing.T) {
	health := &fakeTrackerHealth{}
	svc, instanceID, stub := newWaitRunService(t, health)
	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{waitDeleteRule(1, nil, unregisteredIs("true"))}, false)
	require.NoError(t, err)
	require.Equal(t, []string{waitOldHash}, runDeletedHashes(stub))
	require.Equal(t, [][]string{{waitOldHash}}, health.asked)
}

func TestDeleteWaitTagActsOnTheFirstMatch(t *testing.T) {
	tag := &models.Automation{ID: 2, Name: "tag", Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Tags: []*models.TagAction{{Enabled: true, Tags: []string{"unreg"}, Mode: "add", Condition: unregisteredIs("true")}},
	}}
	tests := []struct {
		name   string
		rules  []*models.Automation
		health *fakeTrackerHealth
	}{
		{name: "a tag rule alone", rules: []*models.Automation{tag}, health: &fakeTrackerHealth{}},
		{name: "after a delete rule whose torrents wait", rules: []*models.Automation{waitDeleteRule(1, new(365*24*3600), unregisteredIs("true")), tag}, health: &fakeTrackerHealth{}},
		{name: "after a delete rule whose fresh read fails", rules: []*models.Automation{waitDeleteRule(1, new(0), unregisteredIs("true")), tag},
			health: &fakeTrackerHealth{err: errors.New("qBittorrent did not answer")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newWaitRunService(t, tt.health)
			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, tt.rules, false)
			require.NoError(t, err)
			require.Empty(t, runDeletedHashes(stub))
			var tagged []string
			for _, write := range stub.recordedWrites() {
				if write.path == "torrents/addTags" || write.path == "torrents/setTags" {
					tagged = append(tagged, strings.Split(write.form.Get("hashes"), "|")...)
				}
			}
			require.ElementsMatch(t, []string{waitFreshHash, waitOldHash}, tagged, "the tag reads the verdict of today")
		})
	}
}

func TestDeleteWaitDryRunWritesTheWaitingLine(t *testing.T) {
	svc, instanceID, stub := newWaitRunService(t, &fakeTrackerHealth{})
	activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{waitDeleteRule(1, new(365*24*3600), unregisteredIs("true"))}, true)
	require.NoError(t, err)
	require.Empty(t, stub.recordedWrites())

	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityActionDeleteWaiting, activities[0].Action)
	var details struct {
		Count int `json:"count"`
	}
	require.NoError(t, json.Unmarshal(activities[0].Details, &details))
	require.Equal(t, 3, details.Count)
}

func TestDeleteWaitPreviewListsDeletesThenWaitingTorrents(t *testing.T) {
	svc, instanceID, _ := newWaitRunService(t, &fakeTrackerHealth{})
	rule := waitDeleteRule(1, nil, unregisteredIs("true"))

	result, err := svc.PreviewDeleteRule(t.Context(), instanceID, rule, 10, 0, "")
	require.NoError(t, err)
	require.Equal(t, 1, result.TotalMatches)
	require.Equal(t, 2, result.WaitingCount)
	rows := make([]string, 0, len(result.Examples))
	for _, row := range result.Examples {
		rows = append(rows, row.Hash)
	}
	require.Equal(t, waitOldHash, rows[0])
	require.ElementsMatch(t, []string{waitFreshHash, waitDownHash}, rows[1:])
	require.Zero(t, result.Examples[0].WaitsUntil)
	for _, row := range result.Examples[1:] {
		since := map[string]time.Time{waitFreshHash: waitTestNow.Add(-24 * time.Hour), waitDownHash: waitTestNow.Add(-8 * 24 * time.Hour)}[row.Hash]
		require.Equal(t, since.Add(7*24*time.Hour).Unix(), row.WaitsUntil)
	}

	page, err := svc.PreviewDeleteRule(t.Context(), instanceID, rule, 1, 2, "")
	require.NoError(t, err)
	require.Len(t, page.Examples, 1)
	require.Equal(t, rows[2], page.Examples[0].Hash, "the waiting rows page after the deletes")

	full, err := svc.PreviewDeleteRule(t.Context(), instanceID, rule, 1, 1, "")
	require.NoError(t, err)
	require.Len(t, full.Examples, 1, "a page that the first waiting row fills takes no second one")
	require.Equal(t, rows[1], full.Examples[0].Hash)
}

func TestDeleteWaitPreviewAsksAgainUntilTheTimesLoad(t *testing.T) {
	svc, instanceID, _ := newWaitRunService(t, &fakeTrackerHealth{snapshot: qbittorrent.NewTrackerHealthSnapshot(nil)})
	svc.trackerHealth = &fakeTrackerHealth{snapshot: nil}

	_, err := svc.PreviewDeleteRule(t.Context(), instanceID, waitDeleteRule(1, nil, unregisteredIs("true")), 10, 0, "")
	require.ErrorIs(t, err, ErrUnregisteredSinceNotLoaded)
}
