// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"encoding/json"
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

// newExportDryRunService boots a Service with a source holding one torrent in /data, in state, and an empty target.
func newExportDryRunService(t *testing.T, state string) (svc *Service, sourceID, targetID int) {
	t.Helper()

	stub := func(torrents string) *httptest.Server {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch r.URL.Path {
			case "/api/v2/auth/login":
				_, _ = w.Write([]byte("Ok."))
			case "/api/v2/app/webapiVersion":
				_, _ = w.Write([]byte("2.10.0"))
			case "/api/v2/sync/maindata":
				_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"server_state":{"free_space_on_disk":1000},"torrents":{` + torrents + `}}`))
			case "/api/v2/torrents/files":
				_, _ = w.Write([]byte(`[]`))
			default:
				http.NotFound(w, r)
			}
		}))
		t.Cleanup(server.Close)
		return server
	}
	source := stub(`"` + parityHash + `":{"name":"parity","ratio":2,"progress":1,"size":10,"state":"` + state + `","save_path":"/data","content_path":"/data/parity"}`)
	target := stub("")

	db := testdb.NewMigratedSQLite(t, t.Name())
	instanceStore, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	sourceInstance, err := instanceStore.Create(t.Context(), "source", source.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)
	targetInstance, err := instanceStore.Create(t.Context(), "target", target.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)

	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })
	syncManager := qbittorrent.NewSyncManager(clientPool, nil)

	svc = NewService(Config{}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, syncManager, nil, nil, nil, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	return svc, sourceInstance.ID, targetInstance.ID
}

func TestApplyRuleDryRun_ExportCurrentSavePath(t *testing.T) {
	tests := []struct {
		name       string
		state      string
		movePath   string
		exportPath string
		wantExport bool
	}{
		{name: "export only", exportPath: "{{ .CurrentSavePath }}/exported", wantExport: true},
		{name: "export waits for a move in the same run", movePath: "/data/moved", exportPath: "{{ .CurrentSavePath }}"},
		{name: "export waits while the torrent is still moving", state: "moving", exportPath: "{{ .CurrentSavePath }}"},
		{name: "a literal export path does not wait", movePath: "/data/moved", exportPath: "/exported", wantExport: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := tt.state
			if state == "" {
				state = "stalledUP"
			}
			svc, sourceID, targetID := newExportDryRunService(t, state)
			conditions := &models.ActionConditions{
				ExportToInstance: &models.ExportToInstanceAction{Enabled: true, TargetInstanceID: targetID, SavePath: tt.exportPath},
			}
			if tt.movePath != "" {
				conditions.Move = &models.MoveAction{Enabled: true, Path: tt.movePath}
			}
			rule := &models.Automation{Name: "export", TrackerPattern: "*", Enabled: true, Conditions: conditions}

			activities, err := svc.ApplyRuleDryRun(t.Context(), sourceID, rule)
			require.NoError(t, err)

			exported := false
			for _, activity := range activities {
				if activity.Action != models.ActivityActionExportedToInstance {
					continue
				}
				var details map[string]any
				require.NoError(t, json.Unmarshal(activity.Details, &details))
				require.Nil(t, details["preflightFailed"], "export save path did not resolve")
				exported = true
			}
			require.Equal(t, tt.wantExport, exported)
		})
	}
}

func TestExportsAfterMoves_KeepsPreflightFailures(t *testing.T) {
	action := &models.ExportToInstanceAction{SavePath: "{{ .CurrentSavePath }}"}
	exports := []pendingExportToInstance{
		{hash: "moved", action: action},
		{hash: "moved-failed", action: action, failureReason: "Save path template resolution failed"},
	}
	ready, waiting := exportsAfterMoves(exports, map[string]struct{}{"moved": {}, "moved-failed": {}})
	require.Len(t, waiting, 1)
	require.Equal(t, "moved", waiting[0].hash)
	require.Len(t, ready, 1)
	require.Equal(t, "moved-failed", ready[0].hash, "a failed export is still reported this run")
}
