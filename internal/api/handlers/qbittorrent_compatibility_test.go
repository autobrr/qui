// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"bytes"
	"encoding/json/v2"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	quiqbt "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func compatibilityHandlerManager(t *testing.T, host string) (*quiqbt.SyncManager, int) {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "qbittorrent-compatibility")
	store, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	pool, err := quiqbt.NewClientPool(store, models.NewInstanceErrorStore(db), time.Minute)
	require.NoError(t, err)
	t.Cleanup(func() { _ = pool.Close() })
	instance, err := store.Create(t.Context(), "Synthetic", host, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	setUnexportedField(t, pool, "clients", map[int]*quiqbt.Client{
		instance.ID: newStaleCachedClient(t, host, nil),
	})
	return quiqbt.NewSyncManager(pool, nil), instance.ID
}

func TestRSSRuleEditPreservesSeedAndShareLimitsMode(t *testing.T) {
	for _, field := range []string{"skip_checking", "seed_mode"} {
		for _, seedMode := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s=%t", field, seedMode), func(t *testing.T) {
				posted := make(chan string, 1)
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					switch r.URL.Path {
					case "/api/v2/rss/rules":
						_, _ = fmt.Fprintf(w, `{"Synthetic":{"mustContain":"Before","torrentParams":{"%s":%t,"share_limits_mode":"MatchAll"}}}`, field, seedMode)
					case "/api/v2/rss/setRule":
						if err := r.ParseForm(); err != nil {
							http.Error(w, err.Error(), http.StatusBadRequest)
							return
						}
						posted <- r.Form.Get("ruleDef")
					default:
						http.NotFound(w, r)
					}
				}))
				t.Cleanup(srv.Close)
				sm, id := compatibilityHandlerManager(t, srv.URL)
				router := chi.NewRouter()
				router.Route("/api/instances/{instanceID}/rss", NewRSSHandler(sm).Routes)
				path := fmt.Sprintf("/api/instances/%d/rss/rules", id)
				rec := httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil))
				require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				require.Contains(t, rec.Body.String(), fmt.Sprintf(`"seed_mode":%t`, seedMode))
				require.Contains(t, rec.Body.String(), `"share_limits_mode":"MatchAll"`)

				var rules qbt.RSSRules
				require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &rules))
				rule := rules["Synthetic"]
				rule.MustContain = "After"
				body, err := json.Marshal(SetRuleRequest{Name: "Synthetic", Rule: rule})
				require.NoError(t, err)
				rec = httptest.NewRecorder()
				router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, bytes.NewReader(body)))
				require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
				select {
				case raw := <-posted:
					require.Contains(t, raw, `"mustContain":"After"`)
					require.Contains(t, raw, fmt.Sprintf(`"seed_mode":%t`, seedMode))
					require.Contains(t, raw, fmt.Sprintf(`"skip_checking":%t`, seedMode))
					require.Contains(t, raw, `"share_limits_mode":"MatchAll"`)
				default:
					t.Fatal("RSS edit did not reach qBittorrent")
				}
			})
		}
	}
}

func TestTorrentCreationHandlerPreservesDateStrings(t *testing.T) {
	const date = "2026-01-01T00:00:00Z"
	for _, upstreamDate := range []string{`"` + date + `"`, "1767225600"} {
		t.Run(upstreamDate, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/api/v2/app/webapiVersion":
					_, _ = w.Write([]byte("2.16.0"))
				case "/api/v2/torrentcreator/status":
					_, _ = fmt.Fprintf(w, `[{"taskID":"synthetic","status":"Finished","timeAdded":%s,"timeStarted":%s,"timeFinished":%s}]`, upstreamDate, upstreamDate, upstreamDate)
				default:
					http.NotFound(w, r)
				}
			}))
			t.Cleanup(srv.Close)
			sm, id := compatibilityHandlerManager(t, srv.URL)
			router := chi.NewRouter()
			router.Get("/api/instances/{instanceID}/torrent-creator/status", NewTorrentsHandler(sm, nil, nil).GetTorrentCreationStatus)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/instances/%d/torrent-creator/status", id), nil))
			require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
			for _, field := range []string{"timeAdded", "timeStarted", "timeFinished"} {
				require.Contains(t, rec.Body.String(), fmt.Sprintf(`"%s":%q`, field, date))
			}
		})
	}
}

func TestPreferencesHandlerCarriesCompatibilityFields(t *testing.T) {
	const preferences = `{"mail_notification_encryption_type":"STARTTLS","remove_torrent_file_backup":true,"torrent_files_backup_enabled":true,"torrent_files_backup_dir":"backup","torrent_files_finished_backup_dir_enabled":true,"torrent_files_finished_backup_dir":"finished","mail_notification_ssl_enabled":true,"export_dir":"legacy","export_dir_fin":"legacy-finished"}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v2/app/preferences" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(preferences))
	}))
	t.Cleanup(srv.Close)
	sm, id := compatibilityHandlerManager(t, srv.URL)
	router := chi.NewRouter()
	router.Get("/api/instances/{instanceID}/preferences", NewPreferencesHandler(sm).GetPreferences)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequestWithContext(t.Context(), http.MethodGet, fmt.Sprintf("/api/instances/%d/preferences", id), nil))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
	var want, got qbt.AppPreferences
	require.NoError(t, json.Unmarshal([]byte(preferences), &want))
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &got))
	require.Equal(t, want, got)
	require.Equal(t, "STARTTLS", got.MailNotificationEncryptionType)
	require.True(t, got.TorrentFilesBackupEnabled)
	require.True(t, got.RemoveTorrentFileBackup)
	require.Equal(t, "backup", got.TorrentFilesBackupDir)
	require.True(t, got.TorrentFilesFinishedBackupDirEnabled)
	require.Equal(t, "finished", got.TorrentFilesFinishedBackupDir)
}
