// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
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
// After the recheck the target first reports the stale stopped state, then
// checks, then reports the torrent complete; nothing starts or resumes it.
// regression: #3015
func TestExecuteExportToInstance_PausedRecheck(t *testing.T) {
	result := runPausedExport(t, pausedExportCase{paused: true, recheckStatus: http.StatusOK, afterRecheck: recheckFindsPayload})

	require.Len(t, result.activities, 1)
	require.Equal(t, models.ActivityOutcomeSuccess, result.activities[0].Outcome, result.activities[0].Reason)
	require.Equal(t, int32(1), result.rechecks.Load())
	require.Zero(t, result.ignoredRechecks.Load())
	require.Zero(t, result.starts.Load())
	require.Empty(t, result.deleteFiles)
	require.GreaterOrEqual(t, len(result.statesAfterRecheck), 3)
	require.Equal(t, []string{"stoppedDL", "checkingDL", "stoppedUP"}, result.statesAfterRecheck[:3])
}

// A failed recheck request fails the export and removes the torrent from the
// target, keeping its files, so it does not block the next export.
// regression: #3015
func TestExecuteExportToInstance_PausedRecheckFailure(t *testing.T) {
	result := runPausedExport(t, pausedExportCase{paused: true, recheckStatus: http.StatusInternalServerError, afterRecheck: recheckFindsPayload})

	require.Len(t, result.activities, 1)
	require.Equal(t, models.ActivityOutcomeFailed, result.activities[0].Outcome)
	require.True(t, strings.HasPrefix(result.activities[0].Reason, "Recheck on target failed:"), result.activities[0].Reason)
	require.Equal(t, []string{"false"}, result.deleteFiles)
	require.Zero(t, result.starts.Load())
}

// When the target has no data for the torrent, the recheck leaves it stopped at
// progress 0. The recheck alone is not a success: the export fails and the
// torrent is removed from the target with its files kept.
// regression: #3015
func TestExecuteExportToInstance_PausedRecheckPayloadMissing(t *testing.T) {
	result := runPausedExport(t, pausedExportCase{paused: true, recheckStatus: http.StatusOK, afterRecheck: recheckFindsNoPayload})

	require.Len(t, result.activities, 1)
	require.Equal(t, models.ActivityOutcomeFailed, result.activities[0].Outcome)
	require.Equal(t, int32(1), result.rechecks.Load())
	require.Zero(t, result.ignoredRechecks.Load())
	require.Zero(t, result.starts.Load())
	require.Equal(t, []string{"false"}, result.deleteFiles)
}

// A large torrent can still be checking when verification ends. That counts as
// success: the torrent stays on the target and nothing deletes it.
// regression: #3015
func TestExecuteExportToInstance_PausedRecheckStillChecking(t *testing.T) {
	result := runPausedExport(t, pausedExportCase{paused: true, recheckStatus: http.StatusOK, afterRecheck: recheckKeepsChecking})

	require.Len(t, result.activities, 1)
	require.Equal(t, models.ActivityOutcomeSuccess, result.activities[0].Outcome, result.activities[0].Reason)
	require.Equal(t, int32(1), result.rechecks.Load())
	require.Zero(t, result.ignoredRechecks.Load())
	require.Zero(t, result.starts.Load())
	require.Empty(t, result.deleteFiles)
}

// Only a paused add with skip checking off needs the recheck; every other
// combination exports without asking the target to recheck.
func TestExecuteExportToInstance_NoRecheckWhenNotNeeded(t *testing.T) {
	for _, tc := range []struct {
		name         string
		paused       bool
		skipChecking bool
	}{
		{"paused, skip checking", true, true},
		{"started, skip checking", false, true},
		{"started, checking", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			result := runPausedExport(t, pausedExportCase{paused: tc.paused, skipChecking: tc.skipChecking, recheckStatus: http.StatusOK, afterRecheck: recheckFindsPayload})

			require.Len(t, result.activities, 1)
			require.Equal(t, models.ActivityOutcomeSuccess, result.activities[0].Outcome, result.activities[0].Reason)
			require.Equal(t, int32(0), result.rechecks.Load())
			require.Empty(t, result.deleteFiles)
		})
	}
}

// recheckOutcome is what the stub target reports once it accepts a recheck.
type recheckOutcome int

const (
	// recheckFindsPayload: one stale stoppedDL poll at progress 0, one
	// checkingDL poll, then stoppedUP at progress 1.
	recheckFindsPayload recheckOutcome = iota
	// recheckFindsNoPayload: stoppedDL at progress 0 on every poll.
	recheckFindsNoPayload
	// recheckKeepsChecking: checkingDL on every poll.
	recheckKeepsChecking
)

type pausedExportCase struct {
	paused        bool
	skipChecking  bool
	recheckStatus int
	afterRecheck  recheckOutcome
}

type pausedExportResult struct {
	activities         []*models.AutomationActivity
	rechecks           atomic.Int32
	ignoredRechecks    atomic.Int32
	starts             atomic.Int32
	deleteFiles        []string
	statesAfterRecheck []string
}

// runPausedExport exports one torrent with the given settings to a stub target.
// The target lists the torrent only from the second poll after the add, and it
// ignores a recheck for a hash it does not list yet, as qBittorrent does. It
// reports the torrent complete once listed, except for a paused add with skip
// checking off, which stays stopped at progress 0 until a recheck is accepted
// and then follows tc.afterRecheck. Start, resume and force start requests are
// counted.
func runPausedExport(t *testing.T, tc pausedExportCase) *pausedExportResult {
	t.Helper()
	prevAttempts := exportVerifyMaxAttempts
	prevInterval := exportVerifyPollInterval
	exportVerifyMaxAttempts = 6
	exportVerifyPollInterval = 30 * time.Millisecond
	t.Cleanup(func() {
		exportVerifyMaxAttempts = prevAttempts
		exportVerifyPollInterval = prevInterval
	})

	const hash = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	var mu sync.Mutex
	var added, listed, rechecked bool
	var postAddPolls, postRecheckPolls int
	result := &pausedExportResult{}

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

	// targetState returns the state and progress the target lists the torrent
	// with on this poll. The caller holds mu.
	targetState := func() (string, string) {
		if !tc.paused || tc.skipChecking {
			return "stoppedUP", "1"
		}
		if !rechecked {
			return "stoppedDL", "0"
		}
		poll := postRecheckPolls
		postRecheckPolls++
		state, progress := "stoppedDL", "0"
		switch tc.afterRecheck {
		case recheckFindsPayload:
			switch poll {
			case 0:
			case 1:
				state, progress = "checkingDL", "0.5"
			default:
				state, progress = "stoppedUP", "1"
			}
		case recheckFindsNoPayload:
		case recheckKeepsChecking:
			state, progress = "checkingDL", "0.5"
		}
		result.statesAfterRecheck = append(result.statesAfterRecheck, state)
		return state, progress
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/auth/login":
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/app/webapiVersion":
			_, _ = w.Write([]byte("2.10.0"))
		case "/api/v2/sync/maindata":
			w.Header().Set("Content-Type", "application/json")
			mu.Lock()
			defer mu.Unlock()
			if added {
				postAddPolls++
				listed = listed || postAddPolls >= 2
			}
			if !listed {
				_, _ = w.Write([]byte(`{"rid":1,"full_update":true,"torrents":{}}`))
				return
			}
			state, progress := targetState()
			_, _ = fmt.Fprintf(w, `{"rid":1,"full_update":true,"torrents":{%q:{"name":"Export Sample","progress":%s,"state":"%s"}}}`, hash, progress, state)
		case "/api/v2/torrents/add":
			mu.Lock()
			added = true
			mu.Unlock()
			w.Header().Set("Content-Type", "text/plain")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("Ok."))
		case "/api/v2/torrents/recheck":
			result.rechecks.Add(1)
			mu.Lock()
			switch {
			case !listed:
				result.ignoredRechecks.Add(1)
			case tc.recheckStatus == http.StatusOK:
				rechecked = true
			}
			mu.Unlock()
			w.WriteHeader(tc.recheckStatus)
		case "/api/v2/torrents/start", "/api/v2/torrents/resume", "/api/v2/torrents/setForceStart":
			result.starts.Add(1)
			w.WriteHeader(http.StatusOK)
		case "/api/v2/torrents/delete":
			_ = r.ParseForm()
			mu.Lock()
			result.deleteFiles = append(result.deleteFiles, r.PostForm.Get("deleteFiles"))
			mu.Unlock()
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
	ch := svc.executeExportToInstance(t.Context(), sourceInstance.ID, []pendingExportToInstance{{
		hash: hash,
		torrent: qbt.Torrent{
			Hash: hash,
			Name: "Export Sample",
		},
		action: &models.ExportToInstanceAction{
			Enabled:          true,
			TargetInstanceID: targetInstance.ID,
			Paused:           tc.paused,
			SkipChecking:     &tc.skipChecking,
		},
		ruleID:   1,
		ruleName: "export paused recheck",
	}})

	for activity := range ch {
		result.activities = append(result.activities, activity)
	}
	target.Close()
	return result
}
