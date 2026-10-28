// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
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

	torrents, err := json.Marshal(fixture.torrents)
	require.NoError(t, err)
	stub := newStubQbt(t, string(torrents))
	stub.files = make(map[string]string, len(fixture.files))
	for hash, files := range fixture.files {
		encoded, err := json.Marshal(files)
		require.NoError(t, err)
		stub.files[hash] = string(encoded)
	}
	stub.localFS = fixture.localFS
	svc, _, instanceIDs := newStubService(t, parityCrossMatcher{sameContent: fixture.sameContent}, stub)
	return svc, instanceIDs[0]
}

// stubWrite is one write request a stubQbt received.
type stubWrite struct {
	path string
	form url.Values
}

// stubQbt is a stub qBittorrent that serves a fixed torrent list and records every write in arrival order.
type stubQbt struct {
	url string

	mu       sync.Mutex
	torrents string            // JSON object keyed by hash, as in sync/maindata
	files    map[string]string // torrents/files answer by hash; other hashes get no files
	writes   []stubWrite
	localFS  bool // the instance has local filesystem access
	// torrentsAfterAdd replaces torrents on torrents/add, so an export's verification finds the torrent.
	torrentsAfterAdd string
}

func newStubQbt(t *testing.T, torrents string) *stubQbt {
	t.Helper()

	stub := &stubQbt{torrents: torrents}
	server := httptest.NewServer(http.HandlerFunc(stub.serveHTTP))
	t.Cleanup(server.Close)
	stub.url = server.URL
	return stub
}

func (s *stubQbt) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/api/v2/auth/login":
		_, _ = w.Write([]byte("Ok."))
	case "/api/v2/app/webapiVersion":
		_, _ = w.Write([]byte("2.10.0"))
	case "/api/v2/sync/maindata":
		s.mu.Lock()
		torrents := s.torrents
		s.mu.Unlock()
		_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"server_state":{"free_space_on_disk":1000},"torrents":` + torrents + `}`))
	case "/api/v2/torrents/files":
		s.mu.Lock()
		files, ok := s.files[r.URL.Query().Get("hash")]
		s.mu.Unlock()
		if !ok {
			files = `[]`
		}
		_, _ = w.Write([]byte(files))
	case "/api/v2/torrents/export":
		_, _ = w.Write([]byte("d4:infod4:name6:paritye"))
	default:
		if r.Method != http.MethodPost {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			_ = r.ParseForm()
		}
		path := strings.TrimPrefix(r.URL.Path, "/api/v2/")
		s.mu.Lock()
		s.writes = append(s.writes, stubWrite{path: path, form: r.Form})
		if path == "torrents/add" && s.torrentsAfterAdd != "" {
			s.torrents = s.torrentsAfterAdd
		}
		s.mu.Unlock()
		_, _ = w.Write([]byte("Ok."))
	}
}

// recordedWrites returns the writes received so far, in arrival order.
func (s *stubQbt) recordedWrites() []stubWrite {
	s.mu.Lock()
	defer s.mu.Unlock()
	return slices.Clone(s.writes)
}

// newStubService boots a Service with one instance per stub, in order, and returns the activity database.
func newStubService(t *testing.T, crossMatcher CrossMatcher, stubs ...*stubQbt) (*Service, *database.DB, []int) {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instanceIDs := make([]int, 0, len(stubs))
	for i, stub := range stubs {
		instance, err := instanceStore.Create(t.Context(), fmt.Sprintf("stub-%d", i), stub.url, "", "", nil, nil, false, new(stub.localFS))
		require.NoError(t, err)
		instanceIDs = append(instanceIDs, instance.ID)
		clearHardlinkIndex := func() {
			globalHardlinkIndexCache.mu.Lock()
			delete(globalHardlinkIndexCache.indices, instance.ID)
			globalHardlinkIndexCache.mu.Unlock()
		}
		clearHardlinkIndex()
		t.Cleanup(clearHardlinkIndex)
	}

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)

	svc := NewService(Config{ApplyTimeout: DefaultConfig().ApplyTimeout}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, syncManager, nil, nil, crossMatcher, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, db, instanceIDs
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

// A manual dry run must report the same torrents the live impact preview shows.
// Both take the same payload; only service-side state can split them.
func TestApplyRuleDryRun_MatchesPreview(t *testing.T) {
	tests := []struct {
		name    string
		fixture func(t *testing.T) parityFixture
		rule    *models.Automation
		state   func(svc *Service, instanceID int)
		want    []string
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
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fixture := defaultParityFixture()
			if tt.fixture != nil {
				fixture = tt.fixture(t)
			}
			svc, instanceID := newDryRunParityService(t, fixture)

			preview, err := svc.PreviewDeleteRule(t.Context(), instanceID, tt.rule, 100, 0, "needed")
			require.NoError(t, err)
			previewHashes := make([]string, 0, len(preview.Examples))
			for _, example := range preview.Examples {
				previewHashes = append(previewHashes, example.Hash)
			}
			require.ElementsMatch(t, tt.want, previewHashes)
			require.Zero(t, preview.CrossSeedCount, "only an include-cross-seeds expansion counts cross-seeds")

			if tt.state != nil {
				tt.state(svc, instanceID)
			}

			activities, err := svc.ApplyRuleDryRun(t.Context(), instanceID, tt.rule)
			require.NoError(t, err)
			require.Len(t, activities, 1)
			require.Equal(t, models.ActivityActionDeletedCondition, activities[0].Action)
			run, err := svc.GetActivityRun(instanceID, activities[0].ID, 100, 0)
			require.NoError(t, err)
			dryRunHashes := make([]string, 0, len(run.Items))
			for _, item := range run.Items {
				dryRunHashes = append(dryRunHashes, item.Hash)
			}
			require.ElementsMatch(t, previewHashes, dryRunHashes, "the dry run must select the torrents that the preview shows")
		})
	}
}
