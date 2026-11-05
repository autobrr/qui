// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"encoding/json"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/pkg/stringutils"
)

// What each reader makes of one result.
const (
	rssAdded    = "added"
	rssExisting = "existing"
	rssSkipped  = "skipped"
	rssFailed   = "failed"

	hookAdded  = "added"
	hookFailed = "failed"
	hookNone   = ""
)

// TestReadersSortEachStatus sends each status through the real readers.
func TestReadersSortEachStatus(t *testing.T) {
	t.Parallel()

	searchAdded := models.CrossSeedSearchResultStatusAdded
	searchSkipped := models.CrossSeedSearchResultStatusSkipped
	searchFailed := models.CrossSeedSearchResultStatusFailed

	tests := []struct {
		status    string
		loopStops bool
		rss       string
		search    models.CrossSeedSearchResultStatus
		webhook   string
	}{
		{"added", true, rssAdded, searchAdded, hookAdded},
		{"added_hardlink", true, rssAdded, searchAdded, hookAdded},
		{"added_reflink", true, rssAdded, searchAdded, hookAdded},
		{"exists", true, rssExisting, searchSkipped, hookNone},
		{"no_match", false, rssSkipped, searchSkipped, hookNone},
		{"rejected", false, rssSkipped, searchSkipped, hookNone},
		{"blocked", false, rssSkipped, searchSkipped, hookNone},
		{"requires_hardlink_reflink", false, rssSkipped, searchSkipped, hookNone},
		{"below_threshold", false, rssSkipped, searchSkipped, hookNone},
		{"skipped_recheck", false, rssSkipped, searchSkipped, hookNone},
		{"skipped_unsafe_pieces", false, rssSkipped, searchSkipped, hookNone},
		{"content_mismatch", false, rssSkipped, searchSkipped, hookNone},
		{"alignment_failed", true, rssFailed, searchFailed, hookFailed},
		{"pause_failed", true, rssFailed, searchFailed, hookFailed},
		{"partial_pool_registration_error", true, rssFailed, searchFailed, hookFailed},
		{"error", false, rssFailed, searchFailed, hookFailed},
		{"no_save_path", false, rssFailed, searchFailed, hookFailed},
		{"invalid_content_path", false, rssFailed, searchFailed, hookFailed},
		{"hardlink_error", false, rssFailed, searchFailed, hookFailed},
		{"reflink_error", false, rssFailed, searchFailed, hookFailed},
		{"size_mismatch", false, rssFailed, searchFailed, hookFailed},
		{"crossseed_instance_unavailable", false, rssFailed, searchFailed, hookFailed},
		{"crossseed_instance_cannot_see_data", false, rssFailed, searchFailed, hookFailed},
		{"synthetic_unknown_status", false, rssFailed, searchFailed, hookFailed},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			t.Parallel()
			// Only an add sets Success, and its response then succeeds too.
			success := tt.rss == rssAdded
			respond := func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
				return &CrossSeedResponse{Success: success, Results: []InstanceCrossSeedResult{
					{InstanceID: 1, InstanceName: "main", Success: success, Status: tt.status},
				}}, nil
			}

			require.Equal(t, tt.loopStops, readSourceLoop(t, respond), "source loop")
			require.Equal(t, tt.rss, readRSS(t, respond), "RSS")
			require.Equal(t, tt.search, readSearchRun(t, respond), "search run")
			require.Equal(t, tt.webhook, readWebhook(t, respond), "webhook notification")
		})
	}
}

type crossSeedResponder func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error)

// readSourceLoop reports whether the source loop stops after the first source.
func readSourceLoop(t *testing.T, respond crossSeedResponder) bool {
	t.Helper()
	calls := 0
	service := &Service{crossSeedInvoker: func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		calls++
		return respond(ctx, req)
	}}
	_, err := service.invokeBoundAnnouncementMatches(t.Context(), []boundAnnouncementMatch{
		{instanceID: 1, instanceName: "main", torrent: qbt.Torrent{Hash: "source-one"}},
		{instanceID: 1, instanceName: "main", torrent: qbt.Torrent{Hash: "source-two"}},
	}, func(boundAnnouncementMatch) *CrossSeedRequest { return &CrossSeedRequest{} })
	require.NoError(t, err)
	return calls == 1
}

func readRSS(t *testing.T, respond crossSeedResponder) string {
	t.Helper()
	const name = "Quiet.Meridian.2025.1080p.WEB-DL.H.264-LUMA"
	instance := &models.Instance{ID: 1, Name: "main", IsActive: true}
	local := qbt.Torrent{Hash: "local", Name: name, Size: 1024, TotalSize: 1024, Progress: 1}
	service := &Service{
		instanceStore: &fakeInstanceStore{instances: map[int]*models.Instance{instance.ID: instance}},
		syncManager: newFakeSyncManager(instance, []qbt.Torrent{local}, map[string]qbt.TorrentFiles{
			local.Hash: {{Name: name + ".mkv", Size: 1024}},
		}),
		releaseCache:     NewReleaseCache(),
		stringNormalizer: stringutils.NewDefaultNormalizer(),
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: respond,
	}
	run := &models.CrossSeedRun{}
	status, _, err := service.processAutomationCandidate(t.Context(), run,
		&models.CrossSeedAutomationSettings{TargetInstanceIDs: []int{instance.ID}}, nil,
		jackett.SearchResult{Indexer: "Indexer", IndexerID: 10, Title: name, DownloadURL: "https://example.invalid/item.torrent", GUID: "guid", Size: 1024},
		AutomationRunOptions{}, map[int]jackett.EnabledIndexerInfo{})
	require.NoError(t, err)

	switch {
	case status == models.CrossSeedFeedItemStatusProcessed && run.CrossSeedsAdded == 1:
		return rssAdded
	case status == models.CrossSeedFeedItemStatusProcessed && run.CandidatesSkipped == 1:
		return rssExisting
	case status == models.CrossSeedFeedItemStatusSkipped && run.CandidatesSkipped == 1:
		return rssSkipped
	case status == models.CrossSeedFeedItemStatusFailed && run.CandidatesFailed == 1:
		return rssFailed
	}
	t.Fatalf("unexpected RSS outcome: item %s, run %+v", status, run)
	return ""
}

func readSearchRun(t *testing.T, respond crossSeedResponder) models.CrossSeedSearchResultStatus {
	t.Helper()
	service := &Service{
		torrentDownloadFunc: func(context.Context, jackett.TorrentDownloadRequest) ([]byte, error) {
			return []byte("torrent"), nil
		},
		crossSeedInvoker: respond,
	}
	result, err := service.executeCrossSeedSearchAttempt(t.Context(), 1, triggerDefaults{},
		&qbt.Torrent{Hash: "source", Name: "source"},
		TorrentSearchResult{Indexer: "Indexer", DownloadURL: "https://example.invalid/item.torrent"},
		time.Now())
	require.NoError(t, err)
	return result.Status
}

func readWebhook(t *testing.T, respond crossSeedResponder) string {
	t.Helper()
	notifier := &recordingNotifier{}
	service := &Service{notifier: notifier, crossSeedInvoker: respond}
	_, err := service.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentData: "ZGF0YQ=="})
	require.NoError(t, err)

	events := notifier.Events()
	switch {
	case len(events) == 0:
		return hookNone
	case len(events) == 1 && events[0].Type == notifications.EventCrossSeedWebhookSucceeded && events[0].CrossSeed.Added == 1:
		return hookAdded
	case len(events) == 1 && events[0].Type == notifications.EventCrossSeedWebhookFailed && events[0].CrossSeed.Failed == 1:
		return hookFailed
	}
	t.Fatalf("unexpected webhook events: %+v", events)
	return ""
}

// TestTroubleshootingPageMatchesClasses reads the status table on the
// troubleshooting page.
func TestTroubleshootingPageMatchesClasses(t *testing.T) {
	t.Parallel()

	page, err := os.ReadFile("../../../documentation/docs/features/cross-seed/troubleshooting.md")
	require.NoError(t, err)
	_, table, found := strings.Cut(string(page), "| Status or message | Outcome |")
	require.True(t, found, "the troubleshooting page has no status table")
	_, table, _ = strings.Cut(table, "\n")

	allowed := map[string][]resultClass{
		"Skipped": {classExists, classSkipped},
		"Failed":  {classStranded, classFailed},
	}
	read := 0
	for line := range strings.Lines(table) {
		if !strings.HasPrefix(line, "|") {
			break
		}
		cells := strings.Split(line, "|")
		statuses := regexp.MustCompile("`([a-z_]+)`").FindAllStringSubmatch(cells[1], -1)
		outcome := strings.TrimSpace(cells[2])
		for _, match := range statuses {
			status := match[1]
			class, ok := resultClasses[status]
			require.True(t, ok, "status %s on the page has no entry in resultClasses", status)
			require.Contains(t, allowed[outcome], class, "status %s has the outcome %q on the page and the class %s", status, outcome, class)
			read++
		}
	}
	require.NotZero(t, read)
}

// TestWebhookNotificationCountsOnlyFailures sends a stranded result beside
// harmless results in one response.
func TestWebhookNotificationCountsOnlyFailures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		statuses []string
		want     []notifications.EventType
	}{
		{name: "stranded beside exists and no match", statuses: []string{"alignment_failed", "exists", "no_match"}, want: []notifications.EventType{notifications.EventCrossSeedWebhookFailed}},
		{name: "exists and skipped only", statuses: []string{"exists", "no_match", "blocked"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results := make([]InstanceCrossSeedResult, 0, len(tt.statuses))
			for i, status := range tt.statuses {
				results = append(results, InstanceCrossSeedResult{InstanceID: i + 1, InstanceName: status, Status: status})
			}
			notifier := &recordingNotifier{}
			service := &Service{notifier: notifier, crossSeedInvoker: func(context.Context, *CrossSeedRequest) (*CrossSeedResponse, error) {
				return &CrossSeedResponse{Results: results}, nil
			}}
			_, err := service.AutobrrApply(t.Context(), &AutobrrApplyRequest{TorrentData: "ZGF0YQ=="})
			require.NoError(t, err)

			events := notifier.Events()
			types := make([]notifications.EventType, 0, len(events))
			for _, event := range events {
				types = append(types, event.Type)
			}
			require.ElementsMatch(t, tt.want, types)
			if len(events) == 1 {
				require.Equal(t, 1, events[0].CrossSeed.Failed)
				require.Equal(t, []string{"alignment_failed"}, events[0].CrossSeed.Samples)
			}
		})
	}
}

// regression: #3826
func TestApplyReplyEncodesClassBesideUnchangedStatus(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(ApplyTorrentSearchResponse{Results: []TorrentSearchAddResult{{
		Title:   "Cedar.Harbor.S01E01",
		Indexer: "alpha",
		InstanceResults: []InstanceCrossSeedResult{
			{InstanceID: 1, InstanceName: "lib", Status: "alignment_failed", Message: "rename failed"},
			{InstanceID: 2, InstanceName: "seeds", Success: true, Status: "added_hardlink", CrossSeedInstanceID: 3, CrossSeedInstanceName: "routed"},
			{InstanceID: 4, InstanceName: "new", Status: "a_status_with_no_table_entry"},
		},
	}}})
	require.NoError(t, err)

	require.JSONEq(t, `{"results":[{"title":"Cedar.Harbor.S01E01","indexer":"alpha","success":false,"instance_results":[
		{"instance_id":1,"instance_name":"lib","success":false,"status":"alignment_failed","message":"rename failed","class":"stranded"},
		{"instance_id":2,"instance_name":"seeds","success":true,"status":"added_hardlink","cross_seed_instance_id":3,"cross_seed_instance_name":"routed","class":"added"},
		{"instance_id":4,"instance_name":"new","success":false,"status":"a_status_with_no_table_entry","class":"failed"}
	]}]}`, string(encoded))
}
