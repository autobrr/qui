// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	parityHash         = "cccccccccccccccccccccccccccccccccccccccc"
	parityCheckingHash = "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee"
)

// parityFixture is the qBittorrent state that the stub serves.
type parityFixture struct {
	torrents map[string]map[string]any
	files    map[string]qbt.TorrentFiles
	// sameContent is the cross-seed snapshot: the torrents that share files with each torrent.
	sameContent map[string][]string
	localFS     bool
}

// defaultParityFixture holds one seeded torrent and one torrent in a check, which matches
// the condition but must not be deleted.
func defaultParityFixture() parityFixture {
	return parityFixture{torrents: map[string]map[string]any{
		parityHash:         {"name": "parity", "ratio": 2, "progress": 1, "size": 10, "state": "stalledUP", "save_path": "/data", "content_path": "/data/parity"},
		parityCheckingHash: {"name": "checking", "ratio": 2, "progress": 0, "size": 10, "state": "checkingUP", "save_path": "/data", "content_path": "/data/checking"},
	}}
}

type parityCrossMatcher struct{ sameContent map[string][]string }

func (m parityCrossMatcher) BuildCrossMatchSets(context.Context, int, CrossMatchNeeds) *CrossMatchResult {
	return &CrossMatchResult{SameInstanceContentByHash: m.sameContent}
}

// newDryRunParityService boots a Service against a stub qBittorrent that serves the fixture.
func newDryRunParityService(t *testing.T, fixture parityFixture) (*Service, int) {
	t.Helper()

	maindata, err := json.Marshal(map[string]any{
		"rid": 1, "full_update": true, "server_state": map[string]any{"free_space_on_disk": 1000}, "torrents": fixture.torrents,
	})
	require.NoError(t, err)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			_, _ = w.Write(maindata)
		case "/api/v2/torrents/files":
			files := fixture.files[r.URL.Query().Get("hash")]
			if files == nil {
				files = qbt.TorrentFiles{}
			}
			_ = json.NewEncoder(w).Encode(files)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "parity", server.URL, "", "", nil, nil, false, new(fixture.localFS))
	require.NoError(t, err)
	clearHardlinkIndex := func() {
		globalHardlinkIndexCache.mu.Lock()
		delete(globalHardlinkIndexCache.indices, instance.ID)
		globalHardlinkIndexCache.mu.Unlock()
	}
	clearHardlinkIndex()
	t.Cleanup(clearHardlinkIndex)

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)

	svc := NewService(Config{}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, syncManager, nil, nil,
		parityCrossMatcher{sameContent: fixture.sameContent}, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, instance.ID
}

func parityDeleteRule(mode string, cond *models.RuleCondition) *models.Automation {
	return &models.Automation{
		Name:           "parity",
		TrackerPattern: "*",
		Enabled:        true,
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled:   true,
				Mode:      mode,
				Condition: cond,
			},
		},
	}
}

var parityDeleteTag = &models.RuleCondition{Field: models.FieldTags, Operator: models.OperatorContains, Value: "delete"}

func parityCategoryRule(action models.CategoryAction) *models.Automation {
	action.Enabled = true
	action.Category = "archive"
	action.Condition = &models.RuleCondition{Field: models.FieldTags, Operator: models.OperatorContains, Value: "archive"}
	return &models.Automation{
		Name:           "parity",
		TrackerPattern: "*",
		Enabled:        true,
		Conditions:     &models.ActionConditions{Category: &action},
	}
}

func parityTagRule(action models.TagAction) *models.Automation {
	action.Enabled = true
	action.Tags = []string{"managed"}
	action.Condition = &models.RuleCondition{Field: models.FieldTags, Operator: models.OperatorContains, Value: "archive"}
	return &models.Automation{
		Name:           "parity",
		TrackerPattern: "*",
		Enabled:        true,
		Conditions:     &models.ActionConditions{Tags: []*models.TagAction{&action}},
	}
}

// A manual dry run must report the same torrents the live impact preview shows.
// Both take the same payload; only service-side state can split them.
func TestApplyRuleDryRun_MatchesPreview(t *testing.T) {
	tests := []struct {
		name           string
		fixture        func(t *testing.T) parityFixture
		rule           *models.Automation
		state          func(svc *Service, instanceID int)
		want           []string
		wantCrossSeeds int
	}{
		{
			name: "torrent touched by a live rule within SkipWithin",
			rule: parityDeleteRule(DeleteModeWithFilesIncludeCrossSeeds,
				&models.RuleCondition{Field: models.FieldRatio, Operator: models.OperatorGreaterThanOrEqual, Value: "1"}),
			state: func(svc *Service, instanceID int) {
				svc.lastApplied[instanceID] = map[string]time.Time{parityHash: time.Now()}
			},
			want: []string{parityHash},
		},
		{
			name: "FREE_SPACE rule inside the live delete cooldown",
			rule: parityDeleteRule(DeleteModeWithFilesIncludeCrossSeeds,
				&models.RuleCondition{Field: models.FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1000000"}),
			state: func(svc *Service, instanceID int) {
				svc.lastFreeSpaceDeleteAt[instanceID] = time.Now()
			},
			want: []string{parityHash},
		},
		{
			name: "FREE_SPACE rule stops when satisfied",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{torrents: map[string]map[string]any{
					"a": {"name": "a", "size": 10, "added_on": 1, "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/a"},
					"b": {"name": "b", "size": 10, "added_on": 2, "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/b"},
					"c": {"name": "c", "size": 10, "added_on": 3, "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/c"},
				}}
			},
			rule: parityDeleteRule(DeleteModeWithFiles,
				&models.RuleCondition{Field: models.FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1015"}),
			want: []string{"a", "b"},
		},
		{
			name: "include cross-seeds with hardlinks",
			fixture: func(t *testing.T) parityFixture {
				dir := filepath.ToSlash(t.TempDir())
				for _, sub := range []string{"a", "b", "c"} {
					require.NoError(t, os.MkdirAll(filepath.Join(dir, sub), 0o700))
				}
				require.NoError(t, os.WriteFile(filepath.Join(dir, "a", "x.mkv"), []byte("hardlinked"), 0o600))
				require.NoError(t, os.Link(filepath.Join(dir, "a", "x.mkv"), filepath.Join(dir, "b", "x.mkv")))
				require.NoError(t, os.WriteFile(filepath.Join(dir, "c", "y.mkv"), []byte("unrelated"), 0o600))
				return parityFixture{
					torrents: map[string]map[string]any{
						"source":    {"name": "source", "tags": "delete", "size": 10, "progress": 1, "state": "stalledUP", "save_path": dir + "/a", "content_path": dir + "/a/x.mkv"},
						"copy":      {"name": "copy", "size": 10, "progress": 1, "state": "stalledUP", "save_path": dir + "/b", "content_path": dir + "/b/x.mkv"},
						"unrelated": {"name": "unrelated", "size": 9, "progress": 1, "state": "stalledUP", "save_path": dir + "/c", "content_path": dir + "/c/y.mkv"},
					},
					files: map[string]qbt.TorrentFiles{
						"source":    {{Name: "x.mkv", Size: 10, Progress: 1, Priority: 1}},
						"copy":      {{Name: "x.mkv", Size: 10, Progress: 1, Priority: 1}},
						"unrelated": {{Name: "y.mkv", Size: 9, Progress: 1, Priority: 1}},
					},
					localFS: true,
				}
			},
			rule: func() *models.Automation {
				rule := parityDeleteRule(DeleteModeWithFilesIncludeCrossSeeds, parityDeleteTag)
				rule.Conditions.Delete.IncludeHardlinks = true
				return rule
			}(),
			want: []string{"source", "copy"},
		},
		{
			name: "keep-files group with an ambiguous content path",
			fixture: func(*testing.T) parityFixture {
				files := qbt.TorrentFiles{{Name: "pack/01.mkv", Size: 10}}
				return parityFixture{
					torrents: map[string]map[string]any{
						"first":  {"name": "first", "tags": "delete", "size": 10, "progress": 1, "state": "stalledUP", "save_path": "/data/pack", "content_path": "/data/pack"},
						"second": {"name": "second", "tags": "delete", "size": 10, "progress": 1, "state": "stalledUP", "save_path": "/data/pack", "content_path": "/data/pack"},
					},
					files: map[string]qbt.TorrentFiles{"first": files, "second": files},
				}
			},
			rule: func() *models.Automation {
				rule := parityDeleteRule(DeleteModeKeepFiles, parityDeleteTag)
				rule.Conditions.Delete.GroupID = GroupCrossSeedContentPath
				return rule
			}(),
			want: []string{"first", "second"},
		},
		{
			name: "preserve cross-seeds",
			fixture: func(*testing.T) parityFixture {
				files := qbt.TorrentFiles{{Name: "movie/movie.mkv", Size: 10}}
				return parityFixture{
					torrents: map[string]map[string]any{
						"a": {"name": "a", "tags": "delete", "size": 10, "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/movie"},
						"b": {"name": "b", "size": 10, "progress": 1, "state": "stalledUP", "save_path": "/data", "content_path": "/data/movie"},
					},
					files:       map[string]qbt.TorrentFiles{"a": files, "b": files},
					sameContent: map[string][]string{"a": {"b"}, "b": {"a"}},
				}
			},
			rule: parityDeleteRule(DeleteModeWithFilesPreserveCrossSeeds, parityDeleteTag),
			want: []string{"a"},
		},
		{
			name: "category with include cross-seeds",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{
					torrents: map[string]map[string]any{
						"a":       {"name": "a", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
						"b":       {"name": "b", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
						"done":    {"name": "done", "category": "archive", "save_path": "/data", "content_path": "/data/show"},
						"outside": {"name": "outside", "category": "tv", "save_path": "/data", "content_path": "/data/other"},
					},
					sameContent: map[string][]string{"a": {"b", "done"}, "b": {"a", "done"}, "done": {"a", "b"}},
				}
			},
			rule:           parityCategoryRule(models.CategoryAction{IncludeCrossSeeds: true}),
			want:           []string{"a", "b"},
			wantCrossSeeds: 1,
		},
		{
			name: "category of a rule that is turned off",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{torrents: map[string]map[string]any{
					"a": {"name": "a", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
				}}
			},
			rule: func() *models.Automation {
				rule := parityCategoryRule(models.CategoryAction{})
				rule.Enabled = false
				return rule
			}(),
			want: []string{"a"},
		},
		{
			name: "category for a group that every member matches",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{torrents: map[string]map[string]any{
					"a":       {"name": "a", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
					"b":       {"name": "b", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
					"outside": {"name": "outside", "category": "tv", "save_path": "/data", "content_path": "/data/other"},
				}}
			},
			rule: parityCategoryRule(models.CategoryAction{GroupID: GroupCrossSeedContentPath}),
			want: []string{"a", "b"},
		},
		{
			name: "category for a group that one member blocks",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{torrents: map[string]map[string]any{
					"a": {"name": "a", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
					"b": {"name": "b", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
				}}
			},
			rule: parityCategoryRule(models.CategoryAction{GroupID: GroupCrossSeedContentPath}),
			want: []string{},
		},
		{
			name: "category that a cross-seed in a protected category blocks",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{
					torrents: map[string]map[string]any{
						"a":    {"name": "a", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/show"},
						"kept": {"name": "kept", "category": "keep", "save_path": "/data", "content_path": "/data/show"},
						"solo": {"name": "solo", "tags": "archive", "category": "tv", "save_path": "/data", "content_path": "/data/solo"},
					},
					sameContent: map[string][]string{"a": {"kept"}, "kept": {"a"}},
				}
			},
			rule: parityCategoryRule(models.CategoryAction{BlockIfCrossSeedInCategories: []string{"keep"}}),
			want: []string{"solo"},
		},
		{
			name: "tag with include cross-seeds",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{
					torrents: map[string]map[string]any{
						"a":       {"name": "a", "tags": "archive", "save_path": "/data", "content_path": "/data/show"},
						"b":       {"name": "b", "save_path": "/data", "content_path": "/data/show"},
						"done":    {"name": "done", "tags": "managed", "save_path": "/data", "content_path": "/data/show"},
						"outside": {"name": "outside", "save_path": "/data", "content_path": "/data/other"},
					},
					sameContent: map[string][]string{"a": {"b", "done"}, "b": {"a", "done"}, "done": {"a", "b"}},
				}
			},
			rule:           parityTagRule(models.TagAction{Mode: models.TagModeAdd, IncludeCrossSeeds: true}),
			want:           []string{"a", "b"},
			wantCrossSeeds: 1,
		},
		{
			name: "tag with a managed reset re-add",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{
					torrents: map[string]map[string]any{
						"a":       {"name": "a", "tags": "archive, managed", "save_path": "/data", "content_path": "/data/show"},
						"b":       {"name": "b", "tags": "managed", "save_path": "/data", "content_path": "/data/show"},
						"outside": {"name": "outside", "tags": "managed", "save_path": "/data", "content_path": "/data/other"},
					},
					sameContent: map[string][]string{"a": {"b"}, "b": {"a"}},
				}
			},
			rule:           parityTagRule(models.TagAction{Mode: models.TagModeAdd, IncludeCrossSeeds: true, DeleteFromClient: true}),
			want:           []string{"a", "b"},
			wantCrossSeeds: 1,
		},
		{
			name: "tag where a cross-seed cancels the own removal of a torrent",
			fixture: func(*testing.T) parityFixture {
				return parityFixture{
					torrents: map[string]map[string]any{
						"a":       {"name": "a", "tags": "archive", "save_path": "/data", "content_path": "/data/show"},
						"b":       {"name": "b", "tags": "managed", "save_path": "/data", "content_path": "/data/show"},
						"outside": {"name": "outside", "tags": "managed", "save_path": "/data", "content_path": "/data/other"},
					},
					sameContent: map[string][]string{"a": {"b"}, "b": {"a"}},
				}
			},
			rule: parityTagRule(models.TagAction{Mode: models.TagModeFull, IncludeCrossSeeds: true}),
			want: []string{"a", "outside"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := defaultParityFixture()
			if tt.fixture != nil {
				fixture = tt.fixture(t)
			}
			svc, instanceID := newDryRunParityService(t, fixture)

			wantAction := models.ActivityActionDeletedCondition
			var preview *PreviewResult
			var err error
			switch {
			case tt.rule.Conditions.Category != nil:
				wantAction = models.ActivityActionCategoryChanged
				preview, err = svc.PreviewCategoryRule(t.Context(), instanceID, tt.rule, 100, 0)
			case len(tt.rule.Conditions.TagActions()) > 0:
				wantAction = models.ActivityActionTagsChanged
				preview, err = svc.PreviewTagRule(t.Context(), instanceID, tt.rule, 100, 0)
			default:
				preview, err = svc.PreviewDeleteRule(t.Context(), instanceID, tt.rule, 100, 0, "needed")
			}
			require.NoError(t, err)
			previewHashes := make([]string, 0, len(preview.Examples))
			for _, example := range preview.Examples {
				previewHashes = append(previewHashes, example.Hash)
			}
			require.ElementsMatch(t, tt.want, previewHashes)
			require.Equal(t, tt.wantCrossSeeds, preview.CrossSeedCount)

			if tt.state != nil {
				tt.state(svc, instanceID)
			}

			activities, err := svc.ApplyRuleDryRun(t.Context(), instanceID, tt.rule)
			require.NoError(t, err)
			require.Len(t, activities, 1)
			dryRunHashes := make([]string, 0)
			if len(tt.want) == 0 {
				require.Equal(t, models.ActivityActionDryRunNoMatch, activities[0].Action)
			} else {
				require.Equal(t, wantAction, activities[0].Action)
				run, err := svc.GetActivityRun(instanceID, activities[0].ID, 100, 0)
				require.NoError(t, err)
				for _, item := range run.Items {
					dryRunHashes = append(dryRunHashes, item.Hash)
				}
			}
			require.ElementsMatch(t, previewHashes, dryRunHashes, "the dry run must select the torrents that the preview shows")
		})
	}
}
