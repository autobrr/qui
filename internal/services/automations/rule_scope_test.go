// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// noSkipWindow opens the Rule scope of the rules as a dry run and a preview do.
func noSkipWindow(rules ...*models.Automation) *ruleScope {
	return newRuleScope(rules, nil, nil)
}

// scopeTestTorrent returns a torrent with one tracker for each domain. The first is the tracker in use.
func scopeTestTorrent(hash string, hasMetadata bool, domains ...string) qbt.Torrent {
	torrent := qbt.Torrent{Hash: hash, HasMetadata: new(hasMetadata)}
	for _, domain := range domains {
		torrent.Trackers = append(torrent.Trackers, qbt.TorrentTracker{Url: "https://" + domain + "/announce"})
	}
	if len(torrent.Trackers) > 0 {
		torrent.Tracker = torrent.Trackers[0].Url
	}
	return torrent
}

func TestRuleScope(t *testing.T) {
	const skipWithin = 2 * time.Minute
	byName := &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "a"}
	pauseReadsPrivate := &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: privateIs("false", false)}}
	tests := []struct {
		name    string
		pattern string
		// domains are the trackers of the torrent.
		domains    []string
		conditions *models.ActionConditions
		sorting    *models.SortingConfig
		noMetadata bool
		// stampAge is the time since a live run selected the torrent for a delete. Zero means no stamp.
		stampAge time.Duration
		// noWindow opens the Rule scope as a dry run and a preview do.
		noWindow bool
		want     scopeAnswer
	}{
		{name: "wildcard matches all", pattern: "*", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "wildcard matches empty domains", pattern: "*", domains: nil, want: scopeIn},
		{name: "empty pattern matches nothing", pattern: "", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "exact match", pattern: "tracker.example.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "exact match case insensitive", pattern: "Tracker.Example.COM", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "exact match no match", pattern: "other.tracker.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "suffix pattern matches", pattern: ".example.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "suffix pattern case insensitive", pattern: ".Example.COM", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "suffix pattern no match different domain", pattern: ".other.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "comma separated first matches", pattern: "tracker.example.com, other.tracker.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "comma separated second matches", pattern: "other.tracker.com, tracker.example.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "comma separated none match", pattern: "foo.com, bar.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "semicolon separated matches", pattern: "foo.com; tracker.example.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "pipe separated matches", pattern: "foo.com|tracker.example.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "glob wildcard prefix", pattern: "*.example.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "glob wildcard middle", pattern: "tracker.*.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "glob question mark", pattern: "tracker.exampl?.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "glob no match", pattern: "*.other.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "exclude single tracker match", pattern: "!tracker.example.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "exclude single tracker non-match", pattern: "!tracker.example.com", domains: []string{"other.tracker.com"}, want: scopeIn},
		{name: "include and exclude where exclude wins", pattern: "tracker.example.com,!tracker.example.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "include and exclude where include matches", pattern: "tracker.example.com,!other.tracker.com", domains: []string{"tracker.example.com"}, want: scopeIn},
		{name: "exclude supports glob", pattern: "!*.example.com", domains: []string{"tracker.example.com"}, want: scopeOtherTracker},
		{name: "multiple domains first matches", pattern: "tracker.example.com", domains: []string{"tracker.example.com", "other.tracker.com"}, want: scopeIn},
		{name: "multiple domains second matches", pattern: "other.tracker.com", domains: []string{"tracker.example.com", "other.tracker.com"}, want: scopeIn},
		{name: "empty domains with non-wildcard pattern", pattern: "tracker.example.com", domains: nil, want: scopeOtherTracker},
		{name: "whitespace in pattern is trimmed", pattern: "  tracker.example.com  ", domains: []string{"tracker.example.com"}, want: scopeIn},

		{name: "no value for a field in the condition of an Action", pattern: "*", conditions: pauseReadsPrivate, noMetadata: true, want: scopeNoValue},
		{name: "no value for a field in another Action", pattern: "*", noMetadata: true, want: scopeNoValue, conditions: &models.ActionConditions{
			Pause:  &models.PauseAction{Enabled: true, Condition: byName},
			Resume: &models.ResumeAction{Enabled: true, Condition: privateIs("true", true)},
		}},
		{name: "no value for the field of the sort", pattern: "*", noMetadata: true, want: scopeNoValue,
			conditions: &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: byName}},
			sorting:    &models.SortingConfig{Type: models.SortingTypeSimple, Field: models.FieldPrivate}},
		{name: "no value for a field that the rule does not read", pattern: "*", noMetadata: true, want: scopeIn,
			conditions: &models.ActionConditions{Pause: &models.PauseAction{Enabled: true, Condition: byName}}},
		{name: "no value for a field that only a disabled Action reads", pattern: "*", noMetadata: true, want: scopeIn,
			conditions: &models.ActionConditions{Pause: &models.PauseAction{Enabled: false, Condition: privateIs("false", false)}}},
		{name: "a value for a field that the rule reads", pattern: "*", conditions: pauseReadsPrivate, want: scopeIn},

		{name: "in the Skip window", pattern: "*", stampAge: time.Minute, want: scopeSkipWindow},
		{name: "the stamp is older than the Skip window", pattern: "*", stampAge: skipWithin, want: scopeIn},
		{name: "no Skip window", pattern: "*", stampAge: time.Minute, noWindow: true, want: scopeIn},

		{name: "the Skip window comes before the tracker pattern", pattern: "other.tracker.com", domains: []string{"tracker.example.com"}, stampAge: time.Minute, want: scopeSkipWindow},
		{name: "the Skip window comes before the value", pattern: "*", conditions: pauseReadsPrivate, noMetadata: true, stampAge: time.Minute, want: scopeSkipWindow},
		{name: "the tracker pattern comes before the value", pattern: "other.tracker.com", domains: []string{"tracker.example.com"}, conditions: pauseReadsPrivate, noMetadata: true, want: scopeOtherTracker},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			const instanceID = 1
			now := time.Now()
			svc := &Service{cfg: Config{SkipWithin: skipWithin}}
			torrent := scopeTestTorrent("t", !tt.noMetadata, tt.domains...)
			if tt.stampAge > 0 {
				svc.startSkipWindow(instanceID, []string{torrent.Hash}, now.Add(-tt.stampAge))
			}
			var skipWindow map[string]struct{}
			if !tt.noWindow {
				skipWindow = svc.skipWindow(instanceID, now)
			}
			rule := &models.Automation{ID: 1, TrackerPattern: tt.pattern, Conditions: tt.conditions, SortingConfig: tt.sorting}

			got := newRuleScope([]*models.Automation{rule}, skipWindow, trackerLookupOf(torrent)).answer(rule, &torrent)

			require.Equal(t, tt.want, got)
		})
	}
}

// A grouped Action is all or none. Only a member without a value stops it.
func TestRuleScope_GroupDropsMember(t *testing.T) {
	const tracker = "tracker.example.com"
	torrentByHash := map[string]qbt.Torrent{
		"withmeta":  scopeTestTorrent("withmeta", true, tracker),
		"magnet":    scopeTestTorrent("magnet", false, tracker),
		"elsewhere": scopeTestTorrent("elsewhere", true, "other.tracker.com"),
		"deleting":  scopeTestTorrent("deleting", true, tracker),
	}
	readsPrivate := &models.Automation{ID: 1, TrackerPattern: tracker, Conditions: &models.ActionConditions{
		Pause: &models.PauseAction{Enabled: true, Condition: privateIs("false", false)},
	}}
	readsName := &models.Automation{ID: 2, TrackerPattern: tracker, Conditions: &models.ActionConditions{
		Pause: &models.PauseAction{Enabled: true, Condition: &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorContains, Value: "a"}},
	}}
	scope := newRuleScope([]*models.Automation{readsPrivate, readsName}, map[string]struct{}{"deleting": {}}, nil)
	tests := []struct {
		name   string
		rule   *models.Automation
		member string
		want   bool
	}{
		{"a member with no value in a rule that reads the field", readsPrivate, "magnet", true},
		{"the same member in a rule that does not read the field", readsName, "magnet", false},
		{"a member on another tracker", readsPrivate, "elsewhere", false},
		{"a member in the Skip window", readsPrivate, "deleting", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := scope.groupDropsMember(tt.rule, []string{"withmeta", tt.member}, torrentRefMap(torrentByHash))

			require.Equal(t, tt.want, got)
		})
	}
}

// The stub lists the torrent after the delete, as qBittorrent does while it removes the torrent.
func TestRun_SkipWindowStopsASecondDelete(t *testing.T) {
	svc, instanceID, stub := newSkipTestService(t, webAPIQbit52)
	rule := parityDeleteRule(DeleteModeKeepFiles, &models.RuleCondition{Field: models.FieldName, Operator: models.OperatorEqual, Value: "withmeta"})
	rule.ID = 1
	run := func() {
		t.Helper()
		_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{rule}, false)
		require.NoError(t, err)
	}

	run()
	require.Equal(t, []string{"withmeta"}, deletedHashes(stub))

	run()
	require.Equal(t, []string{"withmeta"}, deletedHashes(stub), "the run inside the Skip window sent a second delete")

	svc.startSkipWindow(instanceID, []string{"withmeta"}, time.Now().Add(-svc.cfg.SkipWithin))
	run()
	require.Equal(t, []string{"withmeta", "withmeta"}, deletedHashes(stub))
}

// A run adds the managed tag back to a torrent that dropped out of the rule. The tag preview
// lists only the torrents that the rule tags.
func TestPreview_TorrentWithoutMetadataThatKeepsAManagedTagIsNoTagMatch(t *testing.T) {
	const torrents = `{
		"withmeta": {"name": "withmeta", "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/withmeta", "has_metadata": true, "private": false},
		"magnet":   {"name": "magnet", "progress": 0, "state": "metaDL", "save_path": "/data", "content_path": "/data/magnet", "has_metadata": false, "tags": "managed"}
	}`
	stub := newStubQbt(t, torrents)
	stub.webAPIVersion = webAPIQbit52
	svc, _, instanceIDs := newStubService(t, nil, stub)
	rule := &models.Automation{ID: 1, Name: "tags", TrackerPattern: "*", Enabled: true, Conditions: &models.ActionConditions{
		Tags: []*models.TagAction{{Enabled: true, Tags: []string{"managed"}, Mode: models.TagModeFull, DeleteFromClient: true, Condition: privateIs("false", false)}},
	}}

	preview, err := svc.PreviewTagRule(t.Context(), instanceIDs[0], rule, 10, 0)

	require.NoError(t, err)
	require.Equal(t, 1, preview.TotalMatches)
	require.Equal(t, "withmeta", preview.Examples[0].Hash)
}
