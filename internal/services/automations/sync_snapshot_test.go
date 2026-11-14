// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	lookupHashTwoTrackers = "aaaa000000000000000000000000000000000001"
	lookupHashOneTracker  = "aaaa000000000000000000000000000000000002"
)

// newTrackerStub serves two torrents. The first announces to alpha and beta,
// and its tracker field names only alpha, so a rule sees beta only through the
// tracker lookup.
func newTrackerStub(t *testing.T, webAPI string) *stubQbt {
	t.Helper()

	stub := newStubQbt(t, fmt.Sprintf(`{%q:{"name":"Quiet.Harbor.2024.1080p.WEB-DL-GRP","tracker":"https://alpha.example.invalid/announce","state":"stalledUP","progress":1,"save_path":"/data","has_other_announce_error":true},`+
		`%q:{"name":"Night.Ferry.2023.720p.BluRay-GRP","tracker":"https://gamma.example.invalid/announce","state":"stalledUP","progress":1,"save_path":"/data"}}`,
		lookupHashTwoTrackers, lookupHashOneTracker))
	stub.webAPIVersion = webAPI
	stub.trackers = fmt.Sprintf(`{"https://alpha.example.invalid/announce":[%q],"https://beta.example.invalid/announce":[%[1]q],"https://gamma.example.invalid/announce":[%q]}`,
		lookupHashTwoTrackers, lookupHashOneTracker)
	stub.trackerInfo = fmt.Sprintf(`[{"hash":%q,"trackers":[{"url":"https://alpha.example.invalid/announce","status":4,"msg":"timed out"},{"url":"https://beta.example.invalid/announce","status":2,"msg":""}]},`+
		`{"hash":%q,"trackers":[{"url":"https://gamma.example.invalid/announce","status":2,"msg":""}]}]`,
		lookupHashTwoTrackers, lookupHashOneTracker)
	return stub
}

func tagRuleWhen(pattern string, cond *models.RuleCondition) *models.Automation {
	return &models.Automation{
		Name:           "tracker tag",
		TrackerPattern: pattern,
		Enabled:        true,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{
			Enabled:   true,
			Tags:      []string{"managed"},
			Condition: cond,
		}}},
	}
}

// The torrents of the Sync snapshot carry no trackers. The rules that test
// trackers read them from the lookup by hash, and must match the torrents
// that they matched when qui wrote the trackers into the torrents.
func TestTrackerRulesMatchTheSameTorrentsFromTheLookup(t *testing.T) {
	trackersHaveBeta := &models.RuleCondition{Field: FieldTrackers, Operator: models.OperatorContains, Value: "beta.example.invalid"}
	rules := []struct {
		name string
		rule *models.Automation
		want []string
	}{
		{name: "tracker list", rule: tagRuleWhen("*", trackersHaveBeta), want: []string{lookupHashTwoTrackers}},
		{name: "negated tracker list", rule: tagRuleWhen("*", &models.RuleCondition{Field: FieldTrackers, Operator: models.OperatorNotContains, Value: "beta.example.invalid"}), want: []string{lookupHashOneTracker}},
		{name: "tracker pattern", rule: tagRuleWhen("beta.example.invalid", &models.RuleCondition{Field: FieldTrackers, Operator: models.OperatorContains, Value: "example.invalid"}), want: []string{lookupHashTwoTrackers}},
		{name: "tracker status", rule: tagRuleWhen("*", &models.RuleCondition{Field: FieldTrackerStatus, Operator: models.OperatorEqual, Value: "not_working"}), want: []string{lookupHashTwoTrackers}},
		{name: "tracker message", rule: tagRuleWhen("*", &models.RuleCondition{Field: FieldTrackerMessage, Operator: models.OperatorContains, Value: "timed out"}), want: []string{lookupHashTwoTrackers}},
	}
	versions := []struct {
		name   string
		webAPI string
		// noTrackers: below qBittorrent 5.1, qui loads no tracker list.
		noTrackers bool
	}{
		{name: "below qBittorrent 5.1", webAPI: "2.10.0", noTrackers: true},
		{name: "qBittorrent 5.1", webAPI: "2.11.4"},
		{name: "qBittorrent 5.2", webAPI: webAPIQbit52},
	}

	for _, version := range versions {
		for _, tc := range rules {
			t.Run(version.name+"/"+tc.name, func(t *testing.T) {
				stub := newTrackerStub(t, version.webAPI)
				svc, _, instanceIDs := newStubService(t, nil, stub)
				instanceID := instanceIDs[0]

				want := tc.want
				if version.noTrackers {
					want = nil
					if tc.rule.Conditions.Tags[0].Condition.Operator == models.OperatorNotContains {
						want = []string{lookupHashTwoTrackers, lookupHashOneTracker}
					}
				}

				preview, err := svc.PreviewTagRule(t.Context(), instanceID, tc.rule, 100, 0)
				require.NoError(t, err)
				previewHashes := make([]string, 0, len(preview.Examples))
				for _, example := range preview.Examples {
					previewHashes = append(previewHashes, example.Hash)
				}
				require.ElementsMatch(t, want, previewHashes, "preview")

				activities, err := svc.ApplyRuleDryRun(t.Context(), instanceID, tc.rule)
				require.NoError(t, err)
				require.ElementsMatch(t, want, dryRunActivityHashes(t, svc, instanceID, activities)["tag"], "dry run")
				syncSealed(t, svc, instanceID)
			})
		}
	}
}

// A pass that runs after a pause through qui, and before the next sync shows
// the pause, sees the torrent as paused.
func TestPassSeesAPauseBeforeTheNextSync(t *testing.T) {
	svc, instanceID := newDryRunParityService(t, defaultParityFixture())
	require.NoError(t, svc.syncManager.BulkAction(t.Context(), instanceID, []string{parityHash}, "pause"))

	rule := tagRuleWhen("*", &models.RuleCondition{Field: FieldState, Operator: models.OperatorEqual, Value: "paused"})
	activities, err := svc.ApplyRuleDryRun(t.Context(), instanceID, rule)
	require.NoError(t, err)
	require.Equal(t, []string{parityHash}, dryRunActivityHashes(t, svc, instanceID, activities)["tag"])
	syncSealed(t, svc, instanceID)
}

// syncSealed runs one more sync. The seal of the Sync snapshot panics in it
// when the pass wrote to a shared torrent.
func syncSealed(t *testing.T, svc *Service, instanceID int) {
	t.Helper()
	client, err := svc.syncManager.GetClient(t.Context(), instanceID)
	require.NoError(t, err)
	require.NoError(t, client.SyncOwner().Sync(t.Context()))
}

// torrentRefMap gives each torrent of a test fixture as a reference, as the
// torrents of a Sync snapshot are.
func torrentRefMap(torrents map[string]qbt.Torrent) map[string]*qbt.Torrent {
	refs := make(map[string]*qbt.Torrent, len(torrents))
	for hash, torrent := range torrents {
		refs[hash] = &torrent
	}
	return refs
}

// trackerLookupOf gives the tracker lists of the fixture torrents as a tracker
// lookup, the form in which a run reads them.
func trackerLookupOf(torrents ...qbt.Torrent) qbittorrent.TrackerLookup {
	lookup := qbittorrent.TrackerLookup{}
	for _, torrent := range torrents {
		if len(torrent.Trackers) > 0 {
			lookup[torrent.Hash] = torrent.Trackers
		}
	}
	return lookup
}

// withTrackerLookup returns a copy of ctx that holds the tracker list of the
// fixture torrent in its tracker lookup. It returns ctx when the torrent has
// no tracker list.
func withTrackerLookup(ctx *EvalContext, torrent qbt.Torrent) *EvalContext {
	if len(torrent.Trackers) == 0 {
		return ctx
	}
	var out EvalContext
	if ctx != nil {
		out = *ctx
	}
	out.trackers = trackerLookupOf(torrent)
	return &out
}
