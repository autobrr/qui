// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"bytes"
	"encoding/json/v2"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/logbudget"
)

const (
	factsAnnounceKey = "QUIFACTSANNOUNCEKEY"
	factsMessageKey  = "QUIFACTSMESSAGEKEY"
)

func factsSnapshot() *qbittorrent.TrackerHealthSnapshot {
	announce := "https://tracker.example.invalid/" + factsAnnounceKey + "/announce"
	return qbittorrent.NewTrackerHealthSnapshotWithTrackers(
		map[string]qbittorrent.TrackerHealth{
			waitFreshHash: qbittorrent.TrackerHealthUnregistered,
			waitOldHash:   qbittorrent.TrackerHealthUnregistered,
		},
		map[string]time.Time{waitOldHash: waitTestNow.Add(-8 * 24 * time.Hour)},
		map[string][]qbt.TorrentTracker{waitOldHash: {
			{Url: "** [DHT] **", Status: qbt.TrackerStatusOK},
			{Url: announce, Status: qbt.TrackerStatusNotWorking,
				Message: "Unregistered torrent " + factsAnnounceKey + " see https://mirror.example.invalid/t?key=" + factsMessageKey},
		}},
	)
}

func deleteRowFacts(t *testing.T, svc *Service, instanceID int) map[string]*unregisteredFacts {
	t.Helper()
	activities, err := svc.activityStore.ListByInstance(t.Context(), instanceID, 100)
	require.NoError(t, err)
	facts := make(map[string]*unregisteredFacts)
	for _, activity := range activities {
		if activity.Action != models.ActivityActionDeletedCondition {
			continue
		}
		require.NotContains(t, string(activity.Details), factsAnnounceKey)
		require.NotContains(t, string(activity.Details), factsMessageKey)
		var details struct {
			Unregistered *unregisteredFacts `json:"unregistered"`
		}
		require.NoError(t, json.Unmarshal(activity.Details, &details))
		facts[activity.Hash] = details.Unregistered
	}
	return facts
}

func TestDeleteRowRecordsWhatTheTrackerSaidOnlyForAnUnregisteredTriggerOfARuleThatReadsTheVerdict(t *testing.T) {
	tests := []struct {
		name string
		rule *models.Automation
		want map[string]*unregisteredFacts
	}{
		{
			name: "unregistered with a wait of 0",
			rule: waitDeleteRule(1, new(0), unregisteredIs("true")),
			want: map[string]*unregisteredFacts{
				waitOldHash: {
					Since:   waitTestNow.Add(-8 * 24 * time.Hour),
					Host:    "tracker.example.invalid",
					Status:  qbt.TrackerStatusNotWorking,
					Message: "Unregistered torrent see",
				},
				waitFreshHash: nil,
			},
		},
		{
			name: "a rule on the ratio",
			rule: waitDeleteRule(1, new(0), ratioBelow("1")),
			want: map[string]*unregisteredFacts{waitOldHash: nil, waitFreshHash: nil, waitDownHash: nil},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, _ := newWaitRunService(t, &fakeTrackerHealth{snapshot: factsSnapshot()})
			var err error
			lines := logbudget.Count(t, func() {
				_, err = svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{tt.rule}, false)
			})
			require.NoError(t, err)
			require.Equal(t, tt.want, deleteRowFacts(t, svc, instanceID))
			require.Zero(t, lines.Message("automations: could not encode the delete details"))
		})
	}
}

func TestDeleteRowOfAGroupMemberRecordsNoTrackerFacts(t *testing.T) {
	since := waitTestNow.Add(-8 * 24 * time.Hour)
	tracker := []qbt.TorrentTracker{{Url: "https://tracker.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}
	snapshot := qbittorrent.NewTrackerHealthSnapshotWithTrackers(
		map[string]qbittorrent.TrackerHealth{"a": qbittorrent.TrackerHealthUnregistered, "b": qbittorrent.TrackerHealthUnregistered},
		map[string]time.Time{"a": since, "b": since},
		map[string][]qbt.TorrentTracker{"a": tracker, "b": tracker},
	)
	torrents := []qbt.Torrent{
		{Hash: "a", AddedOn: 1, SavePath: "/data", ContentPath: "/data/movie", State: qbt.TorrentStateStalledUp},
		{Hash: "b", AddedOn: 2, SavePath: "/data", ContentPath: "/data/movie", State: qbt.TorrentStateStalledUp},
	}
	rule := waitDeleteRule(1, new(0), unregisteredIs("true"))
	rule.Conditions.Delete.GroupID = GroupCrossSeedContentPath

	sel := selectTestDeletions(t, []*models.Automation{rule}, torrents, waitTestContext(snapshot))

	require.Equal(t, &unregisteredFacts{Since: since, Host: "tracker.example.invalid", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}, sel.byHash["a"].unregistered)
	require.Contains(t, sel.byHash, "b")
	require.Nil(t, sel.byHash["b"].unregistered)
}

func TestDeleteRowOfACrossSeedRecordsNoTrackerFactsWhenAnotherRuleClaimsItAsUnregistered(t *testing.T) {
	tracker := []qbt.TorrentTracker{{Url: "https://tracker.example.invalid/announce", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"}}
	snapshot := qbittorrent.NewTrackerHealthSnapshotWithTrackers(
		map[string]qbittorrent.TrackerHealth{"b": qbittorrent.TrackerHealthUnregistered},
		map[string]time.Time{"b": waitTestNow.Add(-8 * 24 * time.Hour)},
		map[string][]qbt.TorrentTracker{"b": tracker},
	)
	torrents := []qbt.Torrent{
		{Hash: "a", Tags: "first", AddedOn: 1, SavePath: "/data", ContentPath: "/data/movie", State: qbt.TorrentStateStalledUp},
		{Hash: "b", AddedOn: 2, SavePath: "/data", ContentPath: "/data/movie", State: qbt.TorrentStateStalledUp},
	}
	files := qbt.TorrentFiles{{Name: "movie/movie.mkv", Size: 10}}
	evalCtx := waitTestContext(snapshot)
	evalCtx.SameInstanceContentByHash = sharingByContentPath(torrents)
	evalCtx.CrossSeedFilesByHash = map[string]qbt.TorrentFiles{"a": files, "b": files}
	byTag := &models.Automation{ID: 1, Enabled: true, TrackerPattern: "*", Conditions: &models.ActionConditions{
		Delete: &models.DeleteAction{Enabled: true, Mode: DeleteModeWithFilesIncludeCrossSeeds, Condition: tagsContain("first")},
	}}

	sel := selectTestDeletions(t, []*models.Automation{byTag, waitDeleteRule(2, new(0), unregisteredIs("true"))}, torrents, evalCtx)

	require.Equal(t, 1, sel.byHash["b"].rule.id)
	require.Nil(t, sel.byHash["b"].unregistered)
}

func TestDeleteLogLineCarriesTheTrackerFieldsOnlyWithATracker(t *testing.T) {
	since := waitTestNow.Add(-8 * 24 * time.Hour)
	tests := []struct {
		name  string
		facts *unregisteredFacts
		want  map[string]any
	}{
		{name: "no facts", facts: nil, want: map[string]any{}},
		{name: "only the time", facts: &unregisteredFacts{Since: since}, want: map[string]any{"unregisteredSince": since.Format(time.RFC3339)}},
		{
			name:  "a tracker",
			facts: &unregisteredFacts{Since: since, Host: "tracker.example.invalid", Status: qbt.TrackerStatusNotWorking, Message: "Unregistered torrent"},
			want: map[string]any{
				"unregisteredSince": since.Format(time.RFC3339), "trackerHost": "tracker.example.invalid",
				"trackerStatus": float64(qbt.TrackerStatusNotWorking), "trackerMessage": "Unregistered torrent",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			logger := zerolog.New(&out)
			logger.Info().Func(selectedDeletion{unregistered: tt.facts}.logUnregistered).Send()
			got := map[string]any{}
			require.NoError(t, json.Unmarshal(out.Bytes(), &got))
			delete(got, "level")
			require.Equal(t, tt.want, got)
		})
	}
}
