// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
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

// A torrent added paused, with skip checking off, stays at progress 0 until the
// target is asked to recheck. Without that recheck, verification times out.
func TestExecuteExportToInstance_PausedRecheck(t *testing.T) {
	prevAttempts := exportVerifyMaxAttempts
	prevInterval := exportVerifyPollInterval
	exportVerifyMaxAttempts = 6
	exportVerifyPollInterval = 30 * time.Millisecond
	t.Cleanup(func() {
		exportVerifyMaxAttempts = prevAttempts
		exportVerifyPollInterval = prevInterval
	})

	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var rechecked atomic.Bool
	var rechecks, deletes atomic.Int32

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{"` + hash + `":{"name":"Export Sample","progress":1,"state":"stalledUP"}}}`))
		case "/api/v2/torrents/export":
			_, _ = w.Write([]byte("export-sample-torrent"))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	t.Cleanup(source.Close)

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			state, progress := "stoppedDL", "0"
			if rechecked.Load() {
				state, progress = "stoppedUP", "1"
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Export Sample","progress":%s,"state":"%s"}}}`, hash, progress, state)
		case "/api/v2/torrents/add":
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/recheck":
			rechecked.Store(true)
			rechecks.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/api/v2/torrents/delete":
			deletes.Add(1)
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok."))
		}
	}))
	t.Cleanup(target.Close)

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

	svc := NewService(Config{}, instanceStore, nil, models.NewAutomationActivityStore(db), nil, qbittorrent.NewSyncManager(clientPool, nil), nil, nil, nil, fsops.NewPool(instanceStore, localbackend.NewBackend()))
	skipChecking := false
	ch := svc.executeExportToInstance(t.Context(), sourceInstance.ID, []pendingExportToInstance{{
		hash: hash,
		torrent: qbt.Torrent{
			Hash: hash,
			Name: "Export Sample",
		},
		action: &models.ExportToInstanceAction{
			Enabled:          true,
			TargetInstanceID: targetInstance.ID,
			Paused:           true,
			SkipChecking:     &skipChecking,
		},
		ruleID:   1,
		ruleName: "export paused recheck",
	}})

	var activities []*models.AutomationActivity
	for activity := range ch {
		activities = append(activities, activity)
	}
	require.Len(t, activities, 1)
	require.Equal(t, models.ActivityOutcomeSuccess, activities[0].Outcome)
	require.Equal(t, int32(1), rechecks.Load())
	require.Zero(t, deletes.Load())
}
