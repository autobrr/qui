// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// writeStub is a stub qBittorrent that serves fixed torrents and per-hash file lists, and
// records the hashes of every write by action.
type writeStub struct {
	torrents string
	files    map[string]string

	mu     sync.Mutex
	hashes map[string][]string
}

// writeStubActions maps the write endpoints these tests read to the action they belong to.
var writeStubActions = map[string]string{
	"torrents/setLocation": "move",
	"torrents/setCategory": "category",
	"torrents/addTags":     "tag",
	"torrents/removeTags":  "tag",
	"torrents/setTags":     "tag",
	"torrents/delete":      "delete",
}

func (s *writeStub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v2/auth/login":
		_, _ = w.Write([]byte("Ok."))
	case "/api/v2/app/webapiVersion":
		_, _ = w.Write([]byte("2.10.0"))
	case "/api/v2/sync/maindata":
		_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"server_state":{"free_space_on_disk":1000},"torrents":` + s.torrents + `}`))
	case "/api/v2/torrents/files":
		files, ok := s.files[r.URL.Query().Get("hash")]
		if !ok {
			files = `[]`
		}
		_, _ = w.Write([]byte(files))
	default:
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			_ = r.ParseForm()
		}
		if action, ok := writeStubActions[strings.TrimPrefix(r.URL.Path, "/api/v2/")]; ok {
			s.mu.Lock()
			s.hashes[action] = append(s.hashes[action], strings.Split(r.Form.Get("hashes"), "|")...)
			s.mu.Unlock()
		}
		_, _ = w.Write([]byte("Ok."))
	}
}

func (s *writeStub) writes() map[string][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string][]string, len(s.hashes))
	for action, hashes := range s.hashes {
		out[action] = slices.Sorted(slices.Values(hashes))
	}
	return out
}

type stubTorrent struct {
	hash, name, tags, tracker string
}

// newWriteStubService serves the torrents from one save path, each with its own single file.
// Torrents that share a name share a content path and a file list, so they are cross-seeds.
func newWriteStubService(t *testing.T, torrents []stubTorrent) (*Service, int, *writeStub) {
	t.Helper()

	entries := make([]string, 0, len(torrents))
	files := make(map[string]string, len(torrents))
	for _, tor := range torrents {
		entries = append(entries, fmt.Sprintf(`"%s":{"name":"%s","ratio":2,"progress":1,"size":1000,"state":"stoppedUP","save_path":"/data/old","content_path":"/data/old/%s.mkv","tags":"%s","tracker":"%s"}`,
			tor.hash, tor.name, tor.name, tor.tags, tor.tracker))
		files[tor.hash] = fmt.Sprintf(`[{"index":0,"name":"%s.mkv","size":1000,"progress":1,"priority":1}]`, tor.name)
	}
	stub := &writeStub{torrents: "{" + strings.Join(entries, ",") + "}", files: files, hashes: make(map[string][]string)}
	server := httptest.NewServer(stub)
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "stub", server.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)

	svc := NewService(Config{ApplyTimeout: DefaultConfig().ApplyTimeout}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, syncManager, nil, nil, nil, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, instance.ID, stub
}

// dryRunActivityHashes returns the torrents each dry-run activity row lists, keyed like writeStubActions.
func dryRunActivityHashes(t *testing.T, svc *Service, instanceID int, activities []*models.AutomationActivity) map[string][]string {
	t.Helper()

	actions := map[string]string{
		models.ActivityActionMoved:            "move",
		models.ActivityActionCategoryChanged:  "category",
		models.ActivityActionTagsChanged:      "tag",
		models.ActivityActionDeletedCondition: "delete",
	}
	out := make(map[string][]string)
	for _, activity := range activities {
		action, ok := actions[activity.Action]
		if !ok {
			continue
		}
		page, err := svc.GetActivityRun(instanceID, activity.ID, 100, 0)
		require.NoError(t, err)
		for _, item := range page.Items {
			out[action] = append(out[action], item.Hash)
		}
		slices.Sort(out[action])
	}
	return out
}

func tagsContain(tag string) *models.RuleCondition {
	return &models.RuleCondition{Field: models.FieldTags, Operator: models.OperatorContains, Value: tag}
}

func deleteRule(mode string, cond *models.RuleCondition) *models.Automation {
	return &models.Automation{
		ID: 1, Name: "delete", TrackerPattern: "*", Enabled: true, SortOrder: 1,
		Conditions: &models.ActionConditions{Delete: &models.DeleteAction{Enabled: true, Mode: mode, Condition: cond}},
	}
}

func moveRule(groupID string) *models.Automation {
	return &models.Automation{
		ID: 2, Name: "move", TrackerPattern: "*", Enabled: true, SortOrder: 2,
		Conditions: &models.ActionConditions{Move: &models.MoveAction{Enabled: true, Path: "/data/new", GroupID: groupID, Condition: tagsContain("mv")}},
	}
}

func categoryRule(includeCrossSeeds bool, groupID string) *models.Automation {
	return &models.Automation{
		ID: 2, Name: "category", TrackerPattern: "*", Enabled: true, SortOrder: 2,
		Conditions: &models.ActionConditions{Category: &models.CategoryAction{
			Enabled: true, Category: "newcat", IncludeCrossSeeds: includeCrossSeeds, GroupID: groupID, Condition: tagsContain("mv"),
		}},
	}
}

// A torrent the run deletes gets no other action in that run, whichever rule or expansion chose it.
func TestApplyRules_TorrentTheRunDeletesGetsNoOtherAction(t *testing.T) {
	const (
		x = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
		y = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
		z = "dddddddddddddddddddddddddddddddddddddddd"
		w = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
		v = "cccccccccccccccccccccccccccccccccccccccc"
	)
	// Same name, different hash: X and Y are cross-seeds. Z and W are one release in two resolutions.
	crossSeeds := func(xTags string) []stubTorrent {
		return []stubTorrent{{x, "Example.Show.S01E01.1080p.WEB-DL-GRP", xTags, ""}, {y, "Example.Show.S01E01.1080p.WEB-DL-GRP", "mv", ""}}
	}
	release := func(zTags string) []stubTorrent {
		return []stubTorrent{{z, "Example.Movie.2021.1080p.BluRay.x264-GRP", zTags, ""}, {w, "Example.Movie.2021.2160p.BluRay.x265-GRP", "mv", ""}}
	}
	moveTagAndCategory := moveRule("")
	moveTagAndCategory.Conditions.Tags = []*models.TagAction{{Enabled: true, Tags: []string{"moved"}, Mode: "add", Condition: tagsContain("mv")}}
	moveTagAndCategory.Conditions.Category = &models.CategoryAction{Enabled: true, Category: "newcat", Condition: tagsContain("mv")}
	// The tag decides the delete; the hardlink scope term makes it a rule the hardlink re-check reads.
	tagOrUnlinked := &models.RuleCondition{Operator: models.OperatorOr, Conditions: []*models.RuleCondition{
		tagsContain("del"),
		{Field: models.FieldHardlinkScope, Operator: models.OperatorEqual, Value: HardlinkScopeNone},
	}}
	groupDeleteOnTrackerA := deleteRule(DeleteModeKeepFiles, tagsContain("del"))
	groupDeleteOnTrackerA.TrackerPattern = "a.example"
	groupDeleteOnTrackerA.Conditions.Delete.GroupID = GroupCrossSeedContentSavePath
	// A rule ahead of the delete rule gives the delete trigger an action of its own.
	tagBeforeDelete := &models.Automation{
		ID: 3, Name: "tag", TrackerPattern: "*", Enabled: true,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"seen"}, Mode: "add", Condition: tagsContain("del")}}},
	}

	tests := []struct {
		name     string
		torrents []stubTorrent
		rules    []*models.Automation
		want     map[string][]string
		liveOnly bool // the dry run skips the hardlink re-check
	}{
		{
			name:     "legacy cross-seed move skips the deleted cross-seed",
			torrents: crossSeeds("del"),
			rules:    []*models.Automation{deleteRule(DeleteModeKeepFiles, tagsContain("del")), moveRule("")},
			want:     map[string][]string{"move": {y}, "delete": {x}},
		},
		{
			name:     "group move skips the deleted member",
			torrents: release("del,mv"),
			rules:    []*models.Automation{deleteRule(DeleteModeWithFiles, tagsContain("del")), moveRule(GroupReleaseItem)},
			want:     map[string][]string{"move": {w}, "delete": {z}},
		},
		{
			name:     "a deleted member that fails the move condition still makes the group ineligible",
			torrents: release("del"),
			rules:    []*models.Automation{deleteRule(DeleteModeWithFiles, tagsContain("del")), moveRule(GroupReleaseItem)},
			want:     map[string][]string{"delete": {z}},
		},
		{
			name:     "category cross-seed expansion skips the deleted cross-seed",
			torrents: crossSeeds("del"),
			rules:    []*models.Automation{deleteRule(DeleteModeKeepFiles, tagsContain("del")), categoryRule(true, "")},
			want:     map[string][]string{"category": {y}, "delete": {x}},
		},
		{
			name:     "category group expansion skips the deleted member",
			torrents: crossSeeds("del,mv"),
			rules:    []*models.Automation{deleteRule(DeleteModeKeepFiles, tagsContain("del")), categoryRule(false, GroupCrossSeedContentSavePath)},
			want:     map[string][]string{"category": {y}, "delete": {x}},
		},
		{
			name:     "a cross-seed pulled into the delete gets none of its own actions",
			torrents: append(crossSeeds("del"), stubTorrent{v, "Example.Show.S01E02.1080p.WEB-DL-GRP", "mv", ""}),
			rules:    []*models.Automation{deleteRule(DeleteModeWithFilesIncludeCrossSeeds, tagsContain("del")), moveTagAndCategory},
			want:     map[string][]string{"move": {v}, "category": {v}, "tag": {v}, "delete": {x, y}},
		},
		{
			// Group strictness checks the delete condition, not the rule's tracker pattern, so Y joins X's delete.
			name: "a member pulled into a keep-files group delete gets none of its own actions",
			torrents: []stubTorrent{
				{x, "Example.Show.S01E01.1080p.WEB-DL-GRP", "del", "https://a.example/announce"},
				{y, "Example.Show.S01E01.1080p.WEB-DL-GRP", "del,mv", "https://b.example/announce"},
			},
			rules: []*models.Automation{groupDeleteOnTrackerA, moveRule("")},
			want:  map[string][]string{"delete": {x, y}},
		},
		{
			name:     "a delete the hardlink re-check blocks leaves the torrent to expansion but not to its own actions",
			torrents: crossSeeds("del"),
			rules:    []*models.Automation{tagBeforeDelete, deleteRule(DeleteModeWithFiles, tagOrUnlinked), moveRule("")},
			want:     map[string][]string{"move": {x, y}},
			liveOnly: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID, stub := newWriteStubService(t, tt.torrents)
			_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, tt.rules, false)
			require.NoError(t, err)
			require.Equal(t, tt.want, stub.writes())
		})
		if tt.liveOnly {
			continue
		}
		t.Run(tt.name+" (dry run)", func(t *testing.T) {
			svc, instanceID, stub := newWriteStubService(t, tt.torrents)
			rules := make([]*models.Automation, 0, len(tt.rules))
			for _, rule := range tt.rules {
				dryRunRule := *rule
				dryRunRule.DryRun = true
				rules = append(rules, &dryRunRule)
			}
			activities, err := svc.applyRulesForInstance(t.Context(), instanceID, true, rules, true)
			require.NoError(t, err)
			require.Equal(t, tt.want, dryRunActivityHashes(t, svc, instanceID, activities))
			require.Empty(t, stub.writes())
		})
	}
}
