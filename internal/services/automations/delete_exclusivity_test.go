// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// writeStubActions maps the write endpoints these tests read to the action they belong to.
var writeStubActions = map[string]string{
	"torrents/setLocation": "move",
	"torrents/setCategory": "category",
	"torrents/addTags":     "tag",
	"torrents/removeTags":  "tag",
	"torrents/setTags":     "tag",
	"torrents/delete":      "delete",
}

// writtenHashes returns the hashes of every write to a writeStubActions endpoint, by action.
func (s *stubQbt) writtenHashes() map[string][]string {
	out := make(map[string][]string)
	for _, write := range s.recordedWrites() {
		if action, ok := writeStubActions[write.path]; ok {
			out[action] = append(out[action], strings.Split(write.form.Get("hashes"), "|")...)
		}
	}
	for action := range out {
		slices.Sort(out[action])
	}
	return out
}

// filesDeleted returns the hashes of each delete that also removed the files.
func (s *stubQbt) filesDeleted() []string {
	var out []string
	for _, write := range s.recordedWrites() {
		if write.path == "torrents/delete" && write.form.Get("deleteFiles") == "true" {
			out = append(out, strings.Split(write.form.Get("hashes"), "|")...)
		}
	}
	return out
}

type stubTorrent struct {
	hash, name, tags, tracker string
}

// newWriteStubService serves the torrents from one save path, each with its own single file.
// Torrents that share a name share a content path and a file list, so they are cross-seeds.
func newWriteStubService(t *testing.T, torrents []stubTorrent) (*Service, int, *stubQbt) {
	t.Helper()

	entries := make([]string, 0, len(torrents))
	files := make(map[string]string, len(torrents))
	for _, tor := range torrents {
		entries = append(entries, fmt.Sprintf(`"%s":{"name":"%s","ratio":2,"progress":1,"size":1000,"state":"stoppedUP","save_path":"/data/old","content_path":"/data/old/%s.mkv","tags":"%s","tracker":"%s"}`,
			tor.hash, tor.name, tor.name, tor.tags, tor.tracker))
		files[tor.hash] = fmt.Sprintf(`[{"index":0,"name":"%s.mkv","size":1000,"progress":1,"priority":1}]`, tor.name)
	}
	stub := newStubQbt(t, "{"+strings.Join(entries, ",")+"}")
	stub.mu.Lock()
	stub.files = files
	stub.mu.Unlock()

	// Cross-seed expansion pairs torrents through the cross matcher; here a shared name means shared files.
	sharing := make(map[string][]string)
	for _, a := range torrents {
		for _, b := range torrents {
			if a.hash != b.hash && a.name == b.name {
				sharing[a.hash] = append(sharing[a.hash], b.hash)
			}
		}
	}
	svc, _, instanceIDs := newStubService(t, sharedFilesMatcher{sharing: sharing}, stub)
	return svc, instanceIDs[0], stub
}

// sharedFilesMatcher reports which stub torrents share files, as the cross-seed service does.
// otherShared, otherUnknown, and offline are the result of the check on other instances.
type sharedFilesMatcher struct {
	sharing                   map[string][]string
	otherShared, otherUnknown map[string]struct{}
	offline                   []string
}

func (m sharedFilesMatcher) BuildCrossMatchSets(context.Context, int, CrossMatchNeeds) *CrossMatchResult {
	return &CrossMatchResult{
		SameInstanceContentByHash:  m.sharing,
		OtherInstanceShared:        m.otherShared,
		OtherInstanceSharedUnknown: m.otherUnknown,
		OfflineLocalInstances:      m.offline,
	}
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

	tagCrossSeedsRule := &models.Automation{
		ID: 2, Name: "tag", TrackerPattern: "*", Enabled: true, SortOrder: 2,
		Conditions: &models.ActionConditions{Tags: []*models.TagAction{{Enabled: true, Tags: []string{"seen"}, Mode: "add", IncludeCrossSeeds: true, Condition: tagsContain("del")}}},
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
			name:     "a deleted torrent does not pass its tag to its cross-seed",
			torrents: crossSeeds("del"),
			rules:    []*models.Automation{deleteRule(DeleteModeKeepFiles, tagsContain("del")), tagCrossSeedsRule},
			want:     map[string][]string{"delete": {x}},
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
			require.Equal(t, tt.want, stub.writtenHashes())
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
			require.Empty(t, stub.writtenHashes())
		})
	}
}

// deadlineReader records whether each file read carries a deadline.
type deadlineReader struct {
	filesReader

	mu        sync.Mutex
	reads     int
	unbounded int
}

func (r *deadlineReader) GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	r.mu.Lock()
	r.reads++
	if _, ok := ctx.Deadline(); !ok {
		r.unbounded++
	}
	r.mu.Unlock()
	return r.filesReader.GetTorrentFilesBatch(ctx, instanceID, hashes)
}

// The file reads of the overlap check in a live run stop at the apply timeout.
func TestApplyRules_OverlapCheckReadsStopAtTheApplyTimeout(t *testing.T) {
	const name = "Example.Show.S01E01.1080p.WEB-DL-GRP"
	svc, instanceID, stub := newWriteStubService(t, []stubTorrent{
		{"aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", name, "mv", ""},
		{"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", name, "mv", ""},
	})
	// A content path that is the save path makes the group ambiguous, so the plan reads file lists.
	stub.torrents = strings.ReplaceAll(stub.torrents, `"content_path":"/data/old/`+name+`.mkv"`, `"content_path":"/data/old"`)
	reader := &deadlineReader{filesReader: svc.filesReader}
	svc.filesReader = reader

	_, err := svc.applyRulesForInstance(t.Context(), instanceID, true, []*models.Automation{moveRule(GroupCrossSeedContentPath)}, false)
	require.NoError(t, err)
	require.Positive(t, reader.reads)
	require.Zero(t, reader.unbounded)
}
