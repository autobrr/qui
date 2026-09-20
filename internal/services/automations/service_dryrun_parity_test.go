// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"net/http"
	"net/http/httptest"
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

// newDryRunParityService boots a Service against a stub qBittorrent holding one seeded torrent.
func newDryRunParityService(t *testing.T) (*Service, int) {
	t.Helper()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"server_state":{"free_space_on_disk":1000},"torrents":{"` + parityHash + `":{"name":"parity","ratio":2,"progress":1,"size":10,"state":"stalledUP","save_path":"/data","content_path":"/data/parity"}}}`))
		case "/api/v2/torrents/files":
			_, _ = w.Write([]byte(`[]`))
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

	svc := NewService(Config{}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, syncManager, nil, nil, nil, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, instance.ID
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
