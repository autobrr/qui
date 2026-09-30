// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"net/http"
	"net/http/httptest"
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

const parityHash = "cccccccccccccccccccccccccccccccccccccccc"

// qbitStub is a minimal qBittorrent WebUI that serves fixed torrents and records every POST.
type qbitStub struct {
	torrents        string // sync/maindata "torrents" object
	queueingEnabled bool

	mu    sync.Mutex
	posts []string // "<path> <hashes>"
}

func (q *qbitStub) postsTo(path string) []string {
	q.mu.Lock()
	defer q.mu.Unlock()
	var matched []string
	for _, p := range q.posts {
		if after, ok := strings.CutPrefix(p, path+" "); ok {
			matched = append(matched, after)
		}
	}
	return matched
}

// newStubQbitService boots a Service against stub.
func newStubQbitService(t *testing.T, stub *qbitStub) (*Service, int) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path != "/api/v2/auth/login" {
			_ = r.ParseForm()
			stub.mu.Lock()
			stub.posts = append(stub.posts, r.URL.Path+" "+r.PostForm.Get("hashes"))
			stub.mu.Unlock()
		}
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/app/preferences":
			_, _ = fmt.Fprintf(w, `{"queueing_enabled":%t}`, stub.queueingEnabled)
		case "/api/v2/sync/maindata":
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"server_state":{"free_space_on_disk":1000},"torrents":` + stub.torrents + `}`))
		case "/api/v2/torrents/files":
			_, _ = w.Write([]byte(`[]`))
		case "/api/v2/torrents/topPrio", "/api/v2/torrents/bottomPrio", "/api/v2/torrents/stop", "/api/v2/torrents/pause":
			w.WriteHeader(http.StatusOK)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "parity", server.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)

	svc := NewService(Config{ApplyTimeout: 10 * time.Second}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, syncManager, nil, nil, nil, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, instance.ID
}

// newDryRunParityService boots a Service against a stub qBittorrent holding one seeded torrent.
func newDryRunParityService(t *testing.T) (*Service, int) {
	t.Helper()
	return newStubQbitService(t, &qbitStub{
		torrents: `{"` + parityHash + `":{"name":"parity","ratio":2,"progress":1,"size":10,"state":"stalledUP","save_path":"/data","content_path":"/data/parity"}}`,
	})
}

func parityDeleteRule(cond *models.RuleCondition) *models.Automation {
	return &models.Automation{
		Name:           "parity",
		TrackerPattern: "*",
		Enabled:        true,
		Conditions: &models.ActionConditions{
			Delete: &models.DeleteAction{
				Enabled:   true,
				Mode:      DeleteModeWithFilesIncludeCrossSeeds,
				Condition: cond,
			},
		},
	}
}

// A manual dry run must report the same torrents the live impact preview shows.
// Both take the same payload; only service-side state can split them.
func TestApplyRuleDryRun_MatchesPreview(t *testing.T) {
	tests := []struct {
		name  string
		cond  *models.RuleCondition
		state func(svc *Service, instanceID int)
	}{
		{
			name: "torrent touched by a live rule within SkipWithin",
			cond: &models.RuleCondition{Field: models.FieldRatio, Operator: models.OperatorGreaterThanOrEqual, Value: "1"},
			state: func(svc *Service, instanceID int) {
				svc.lastApplied[instanceID] = map[string]time.Time{parityHash: time.Now()}
			},
		},
		{
			name: "FREE_SPACE rule inside the live delete cooldown",
			cond: &models.RuleCondition{Field: models.FieldFreeSpace, Operator: models.OperatorLessThan, Value: "1000000"},
			state: func(svc *Service, instanceID int) {
				svc.lastFreeSpaceDeleteAt[instanceID] = time.Now()
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc, instanceID := newDryRunParityService(t)
			rule := parityDeleteRule(tt.cond)

			preview, err := svc.PreviewDeleteRule(t.Context(), instanceID, rule, 10, 0, "needed")
			require.NoError(t, err)
			require.Equal(t, 1, preview.TotalMatches, "preview must see the torrent")

			tt.state(svc, instanceID)

			activities, err := svc.ApplyRuleDryRun(t.Context(), instanceID, rule)
			require.NoError(t, err)
			require.Len(t, activities, 1)
			require.Equal(t, models.ActivityActionDeletedCondition, activities[0].Action)
		})
	}
}
