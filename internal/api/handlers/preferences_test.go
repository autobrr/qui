// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"maps"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

// fakePreferencesQbit is a qBittorrent WebUI that serves a captured
// app/preferences response and records each setPreferences body and test email.
type fakePreferencesQbit struct {
	mu          sync.Mutex
	webAPI      string
	buildInfo   map[string]any
	prefs       map[string]any
	sets        []map[string]any
	prefsGets   int
	failSetting bool
	testEmails  int
}

func newFakePreferencesQbit(t *testing.T, capture, webAPI string, buildInfo map[string]any) *fakePreferencesQbit {
	t.Helper()
	data, err := os.ReadFile("testdata/preferences/" + capture + ".json")
	require.NoError(t, err)
	fake := &fakePreferencesQbit{webAPI: webAPI, buildInfo: buildInfo}
	require.NoError(t, json.Unmarshal(data, &fake.prefs))
	return fake
}

func (f *fakePreferencesQbit) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	switch strings.TrimPrefix(r.URL.Path, "/api/v2/") {
	case "app/webapiVersion":
		_, _ = io.WriteString(w, f.webAPI)
	case "app/version":
		_, _ = io.WriteString(w, "v-test")
	case "app/buildInfo":
		_ = json.NewEncoder(w).Encode(f.buildInfo)
	case "app/preferences":
		f.prefsGets++
		_ = json.NewEncoder(w).Encode(f.prefs)
	case "app/setPreferences":
		if f.failSetting {
			http.Error(w, "failed", http.StatusInternalServerError)
			return
		}
		var patch map[string]any
		if err := json.Unmarshal([]byte(r.FormValue("json")), &patch); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		f.sets = append(f.sets, patch)
		maps.Copy(f.prefs, patch)
	case "app/sendTestEmail":
		f.testEmails++
	default:
		_, _ = io.WriteString(w, "{}")
	}
}

func (f *fakePreferencesQbit) recordedSets() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.sets...)
}

type preferencesRig struct {
	router     chi.Router
	instanceID int
}

func newPreferencesRig(t *testing.T, fake *fakePreferencesQbit) *preferencesRig {
	t.Helper()
	server := httptest.NewServer(fake)
	t.Cleanup(server.Close)

	db := testdb.NewMigratedSQLite(t, "preferences-route")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	clientPool, err := internalqbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	instance, err := instanceStore.Create(context.Background(), "fake", server.URL, "", "", nil, nil, false, nil)
	require.NoError(t, err)

	syncManager := internalqbittorrent.NewSyncManager(clientPool, nil)
	preferences := NewPreferencesHandler(syncManager)
	rss := NewRSSHandler(syncManager)
	instances := NewInstancesHandler(instanceStore, nil, nil, clientPool, nil, nil, nil, nil)
	router := chi.NewRouter()
	router.Get("/api/instances/{instanceID}/capabilities", instances.GetInstanceCapabilities)
	router.Get("/api/instances/{instanceID}/preferences", preferences.GetPreferences)
	router.Patch("/api/instances/{instanceID}/preferences", preferences.UpdatePreferences)
	router.Post("/api/instances/{instanceID}/preferences/test-email", preferences.SendTestEmail)
	router.Post("/api/instances/{instanceID}/rss/reprocess", rss.ReprocessRules)
	return &preferencesRig{router: router, instanceID: instance.ID}
}

func (rig *preferencesRig) do(t *testing.T, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var reader io.Reader
	if body != "" {
		reader = strings.NewReader(body)
	}
	recorder := httptest.NewRecorder()
	request := httptest.NewRequestWithContext(t.Context(), method, fmt.Sprintf("/api/instances/%d%s", rig.instanceID, path), reader)
	rig.router.ServeHTTP(recorder, request)
	return recorder
}

type viewField struct {
	Key         string          `json:"key"`
	Value       json.RawMessage `json:"value"`
	Tab         string          `json:"tab"`
	Section     string          `json:"section"`
	Control     string          `json:"control"`
	WireUnit    string          `json:"wireUnit"`
	DisplayUnit string          `json:"displayUnit"`
	Options     []any           `json:"options"`
	DependsOn   string          `json:"dependsOn"`
	ReadOnly    bool            `json:"readOnly"`
	Reason      string          `json:"reason"`
}

func (rig *preferencesRig) view(t *testing.T) (map[string]viewField, string) {
	t.Helper()
	recorder := rig.do(t, http.MethodGet, "/preferences", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	return decodeView(t, recorder.Body.Bytes()), recorder.Body.String()
}

func decodeView(t *testing.T, body []byte) map[string]viewField {
	t.Helper()
	var fields []viewField
	require.NoError(t, json.Unmarshal(body, &fields))
	view := make(map[string]viewField, len(fields))
	for _, field := range fields {
		require.NotContains(t, view, field.Key, "duplicate key in view")
		view[field.Key] = field
	}
	return view
}

func TestPreferencesViewQBittorrent5Linux(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	fake.prefs["web_ui_api_key"] = "secret-api-key"
	fake.prefs["dl_limit"] = 1024000
	rig := newPreferencesRig(t, fake)

	view, body := rig.view(t)

	assert.NotContains(t, body, "web_ui_api_key")
	assert.NotContains(t, body, "secret-api-key")

	dl := view["dl_limit"]
	assert.JSONEq(t, "1024000", string(dl.Value))
	assert.Equal(t, "speed", dl.Tab)
	assert.Equal(t, "number", dl.Control)
	assert.Equal(t, "B/s", dl.WireUnit)
	assert.Equal(t, "KiB/s", dl.DisplayUnit)
	assert.False(t, dl.ReadOnly)

	// Present only when qBittorrent sent the key.
	assert.Contains(t, view, "add_stopped_enabled")
	assert.NotContains(t, view, "start_paused_enabled")
	assert.NotContains(t, view, "use_subcategories")

	// libtorrent 2 hides the libtorrent 1 disk cache and shows hashing threads.
	assert.NotContains(t, view, "disk_cache")
	assert.NotContains(t, view, "enable_coalesce_read_write")
	assert.Contains(t, view, "hashing_threads")
	assert.Contains(t, view["disk_io_write_mode"].Options, float64(2))

	// The Linux platform hides the memory working set limit.
	assert.NotContains(t, view, "memory_working_set_limit")

	// A key that the dialog does not show yet is not in the view.
	assert.NotContains(t, view, "locale")

	network := view["current_network_interface"]
	assert.True(t, network.ReadOnly)
	assert.NotEmpty(t, network.Reason)
	assert.Equal(t, "scheduler_enabled", view["schedule_from_hour"].DependsOn)
}

func TestPreferencesViewUnknownPlatformShowsPlatformFields(t *testing.T) {
	// qBittorrent before 5.0 does not report its platform.
	fake := newFakePreferencesQbit(t, "4.6.7", "2.9.3", map[string]any{"libtorrent": "2.0.9.0"})
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	assert.Contains(t, view, "memory_working_set_limit")
	assert.Contains(t, view, "start_paused_enabled")
	assert.Contains(t, view, "use_subcategories")
}

func TestPreferencesViewLibtorrent1(t *testing.T) {
	fake := newFakePreferencesQbit(t, "4.6.7", "2.9.3", map[string]any{"libtorrent": "1.2.19.0", "platform": "windows"})
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	assert.Contains(t, view, "disk_cache")
	assert.Contains(t, view, "enable_coalesce_read_write")
	assert.NotContains(t, view, "hashing_threads")
	assert.NotContains(t, view, "disk_io_type")
	assert.NotContains(t, view, "memory_working_set_limit")
	assert.Equal(t, []any{float64(0), float64(1)}, view["disk_io_write_mode"].Options)
}

func TestPreferencesViewBehaviorShowsTheLogFile(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	// qBittorrent 5.3 adds the search keys to the Behavior tab.
	fake.prefs["store_search_jobs"] = true
	fake.prefs["store_search_job_results"] = false
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	var behavior []string
	for key, field := range view {
		if field.Tab == "behavior" {
			assert.Equal(t, "logFile", field.Section, key)
			behavior = append(behavior, key)
		}
	}
	assert.ElementsMatch(t, []string{
		"file_log_enabled", "file_log_path", "file_log_backup_enabled", "file_log_max_size",
		"file_log_delete_old", "file_log_age", "file_log_age_type",
	}, behavior)

	assert.Equal(t, "path", view["file_log_path"].Control)
	assert.Equal(t, "file_log_enabled", view["file_log_path"].DependsOn)
	assert.Equal(t, "file_log_enabled", view["file_log_backup_enabled"].DependsOn)
	assert.Equal(t, "file_log_enabled", view["file_log_delete_old"].DependsOn)
	maxSize := view["file_log_max_size"]
	assert.Equal(t, "file_log_backup_enabled", maxSize.DependsOn)
	assert.Equal(t, "KiB", maxSize.WireUnit)
	assert.Equal(t, "KiB", maxSize.DisplayUnit)
	assert.Equal(t, "file_log_delete_old", view["file_log_age"].DependsOn)
	ageType := view["file_log_age_type"]
	assert.Equal(t, "select", ageType.Control)
	assert.Equal(t, []any{float64(0), float64(1), float64(2)}, ageType.Options)
	assert.Equal(t, "file_log_delete_old", ageType.DependsOn)
}

func TestPreferencesViewBitTorrentSeedingLimitsAndTrackers(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	// qBittorrent 5.3 adds share_limits_mode.
	fake.prefs["share_limits_mode"] = "MatchAny"
	fake.prefs["add_trackers_url_list"] = "udp://tracker.example.invalid:1337/announce"
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	inactive := view["max_inactive_seeding_time"]
	assert.Equal(t, "seedingLimits", inactive.Section)
	assert.Equal(t, "max_inactive_seeding_time_enabled", inactive.DependsOn)
	assert.Equal(t, "min", inactive.WireUnit)
	assert.Equal(t, "switch", view["max_inactive_seeding_time_enabled"].Control)
	assert.Equal(t, []any{float64(0), float64(1), float64(3), float64(2)}, view["max_ratio_act"].Options)
	assert.Equal(t, []any{"MatchAny", "MatchAll"}, view["share_limits_mode"].Options)

	assert.False(t, view["add_trackers_enabled"].ReadOnly)
	assert.False(t, view["add_trackers"].ReadOnly)
	assert.Equal(t, "add_trackers_enabled", view["add_trackers"].DependsOn)
	assert.Equal(t, "add_trackers_from_url_enabled", view["add_trackers_url"].DependsOn)
	assert.Equal(t, "text", view["add_trackers_url"].Control)

	fetched := view["add_trackers_url_list"]
	assert.Equal(t, "list", fetched.Control)
	assert.True(t, fetched.ReadOnly)
	assert.JSONEq(t, `"udp://tracker.example.invalid:1337/announce"`, string(fetched.Value))
}

func TestPreferencesViewRSSFetchDelayAndEpisodeFilters(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	delay := view["rss_fetch_delay"]
	assert.Equal(t, "reader", delay.Section)
	assert.Equal(t, "number", delay.Control)
	assert.Equal(t, "s", delay.DisplayUnit)
	assert.Equal(t, "rss_processing_enabled", delay.DependsOn)
	filters := view["rss_smart_episode_filters"]
	assert.Equal(t, "smartEpisodeFilter", filters.Section)
	assert.Equal(t, "list", filters.Control)

	recorder := rig.do(t, http.MethodPatch, "/preferences", `{"rss_fetch_delay": 5, "rss_smart_episode_filters": "s(\\d+)e(\\d+)"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, []map[string]any{{"rss_fetch_delay": float64(5), "rss_smart_episode_filters": `s(\d+)e(\d+)`}}, fake.recordedSets())
}

func TestPreferencesViewDownloadsOnQBittorrent53(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.3.0rc1", "2.16.2", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	sections := map[string]string{}
	for key, field := range view {
		if field.Tab == "downloads" {
			assert.False(t, field.ReadOnly, key)
			sections[key] = field.Section
		}
	}
	assert.Equal(t, map[string]string{
		"torrent_content_layout": "whenAdding", "add_to_top_of_queue": "whenAdding", "add_stopped_enabled": "whenAdding",
		"torrent_stop_condition": "whenAdding", "auto_delete_mode": "whenAdding",
		"merge_trackers":               "whenAddingDuplicate",
		"torrent_files_backup_enabled": "torrentFileBackup", "torrent_files_backup_dir": "torrentFileBackup",
		"torrent_files_finished_backup_dir_enabled": "torrentFileBackup", "torrent_files_finished_backup_dir": "torrentFileBackup",
		"remove_torrent_file_backup": "torrentFileBackup",
		"preallocate_all":            "files", "incomplete_files_ext": "files", "use_unwanted_folder": "files",
		"auto_tmm_enabled": "savingManagement", "torrent_changed_tmm_enabled": "savingManagement",
		"save_path_changed_tmm_enabled": "savingManagement", "category_changed_tmm_enabled": "savingManagement",
		"use_category_paths_in_manual_mode": "savingManagement", "save_path": "savingManagement",
		"temp_path_enabled": "savingManagement", "temp_path": "savingManagement", "scan_dirs": "savingManagement",
		"excluded_file_names_enabled": "excludedFileNames", "excluded_file_names": "excludedFileNames",
		"mail_notification_enabled": "emailNotification", "mail_notification_sender": "emailNotification",
		"mail_notification_email": "emailNotification", "mail_notification_smtp": "emailNotification",
		"mail_notification_encryption_type": "emailNotification", "mail_notification_auth_enabled": "emailNotification",
		"mail_notification_username": "emailNotification", "mail_notification_password": "emailNotification",
		"autorun_on_torrent_added_enabled": "runExternalProgram", "autorun_on_torrent_added_program": "runExternalProgram",
		"autorun_enabled": "runExternalProgram", "autorun_program": "runExternalProgram",
	}, sections)

	assert.Equal(t, []any{"None", "MetadataReceived", "FilesChecked"}, view["torrent_stop_condition"].Options)
	assert.Equal(t, []any{float64(0), float64(1), float64(2)}, view["auto_delete_mode"].Options)
	backupDir := view["torrent_files_backup_dir"]
	assert.Equal(t, "path", backupDir.Control)
	assert.Equal(t, "torrent_files_backup_enabled", backupDir.DependsOn)
	assert.Equal(t, "torrent_files_finished_backup_dir_enabled", view["torrent_files_finished_backup_dir"].DependsOn)
	assert.Equal(t, "excluded_file_names_enabled", view["excluded_file_names"].DependsOn)

	recorder := rig.do(t, http.MethodPatch, "/preferences", `{"torrent_stop_condition": "FilesChecked", "auto_delete_mode": 2, "torrent_files_backup_enabled": true, "torrent_files_backup_dir": "/backup", "excluded_file_names": "*.nfo"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, []map[string]any{{
		"torrent_stop_condition": "FilesChecked", "auto_delete_mode": float64(2), "torrent_files_backup_enabled": true,
		"torrent_files_backup_dir": "/backup", "excluded_file_names": "*.nfo",
	}}, fake.recordedSets())
}

func TestPreferencesViewDownloadsOnQBittorrent4(t *testing.T) {
	fake := newFakePreferencesQbit(t, "4.3.9", "2.8.2", map[string]any{"libtorrent": "1.2.14.0"})
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)

	for _, key := range []string{
		"add_to_top_of_queue", "torrent_stop_condition", "merge_trackers", "use_unwanted_folder",
		"use_category_paths_in_manual_mode", "excluded_file_names", "torrent_files_backup_dir", "remove_torrent_file_backup",
	} {
		assert.NotContains(t, view, key)
	}
	for _, key := range []string{"auto_delete_mode", "preallocate_all", "incomplete_files_ext"} {
		assert.Contains(t, view, key)
	}
	// Before qBittorrent 5.3, an empty folder turns the copy off.
	for _, key := range []string{"export_dir", "export_dir_fin"} {
		assert.Equal(t, "torrentFileBackup", view[key].Section, key)
		assert.Equal(t, "path", view[key].Control, key)
		assert.Empty(t, view[key].DependsOn, key)
	}
}

func TestPreferencesViewEmailNotification(t *testing.T) {
	tests := []struct {
		capture       string
		webAPI        string
		encryptionKey string // the key that sets the encryption on this version
		missingKey    string // the encryption key of the other versions, which qBittorrent does not send
	}{
		{capture: "5.2.4", webAPI: "2.15.1", encryptionKey: "mail_notification_ssl_enabled", missingKey: "mail_notification_encryption_type"},
		{capture: "5.3.0rc1", webAPI: "2.16.2", encryptionKey: "mail_notification_encryption_type", missingKey: "mail_notification_ssl_enabled"},
	}
	for _, tt := range tests {
		t.Run(tt.capture, func(t *testing.T) {
			fake := newFakePreferencesQbit(t, tt.capture, tt.webAPI, map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
			rig := newPreferencesRig(t, fake)

			view, _ := rig.view(t)

			assert.NotContains(t, view, tt.missingKey)
			assert.Equal(t, "mail_notification_enabled", view[tt.encryptionKey].DependsOn)
			password := view["mail_notification_password"]
			assert.Equal(t, "password", password.Control)
			assert.Equal(t, "mail_notification_auth_enabled", password.DependsOn)
			assert.Equal(t, "mail_notification_enabled", view["mail_notification_auth_enabled"].DependsOn)
			for _, key := range []string{"mail_notification_sender", "mail_notification_email", "mail_notification_smtp", "mail_notification_username"} {
				assert.Equal(t, "text", view[key].Control, key)
			}
		})
	}

	fake := newFakePreferencesQbit(t, "5.3.0rc1", "2.16.2", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	rig := newPreferencesRig(t, fake)
	view, _ := rig.view(t)
	encryption := view["mail_notification_encryption_type"]
	assert.Equal(t, "select", encryption.Control)
	assert.Equal(t, []any{"None", "STARTTLS", "SMTPS"}, encryption.Options)

	recorder := rig.do(t, http.MethodPatch, "/preferences", `{"mail_notification_enabled": true, "mail_notification_encryption_type": "STARTTLS", "mail_notification_password": "hunter2"}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	assert.Equal(t, []map[string]any{{
		"mail_notification_enabled": true, "mail_notification_encryption_type": "STARTTLS", "mail_notification_password": "hunter2",
	}}, fake.recordedSets())
}

func TestPreferencesTestEmail(t *testing.T) {
	tests := []struct {
		webAPI            string
		supportsTestEmail bool
		wantStatus        int
		wantSent          int
	}{
		{webAPI: "2.9.3", supportsTestEmail: false, wantStatus: http.StatusBadRequest, wantSent: 0},
		{webAPI: "2.11.2", supportsTestEmail: true, wantStatus: http.StatusNoContent, wantSent: 1},
	}
	for _, tt := range tests {
		t.Run(tt.webAPI, func(t *testing.T) {
			fake := newFakePreferencesQbit(t, "5.2.4", tt.webAPI, map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
			rig := newPreferencesRig(t, fake)

			recorder := rig.do(t, http.MethodGet, "/capabilities", "")
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			var capabilities InstanceCapabilitiesResponse
			require.NoError(t, json.Unmarshal(recorder.Body.Bytes(), &capabilities))
			assert.Equal(t, tt.supportsTestEmail, capabilities.SupportsTestEmail)

			recorder = rig.do(t, http.MethodPost, "/preferences/test-email", "")
			assert.Equal(t, tt.wantStatus, recorder.Code, recorder.Body.String())

			fake.mu.Lock()
			defer fake.mu.Unlock()
			assert.Equal(t, tt.wantSent, fake.testEmails)
		})
	}
}

func TestPreferencesPatchSendsOnlyThePatch(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	rig := newPreferencesRig(t, fake)

	recorder := rig.do(t, http.MethodPatch, "/preferences", `{"dl_limit": 2048, "scan_dirs": {"/watch": 0}}`)
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	assert.Equal(t, []map[string]any{{"dl_limit": float64(2048), "scan_dirs": map[string]any{"/watch": float64(0)}}}, fake.recordedSets())
	view := decodeView(t, recorder.Body.Bytes())
	assert.JSONEq(t, "2048", string(view["dl_limit"].Value))
}

func TestPreferencesPatchRejections(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		cause string
	}{
		{name: "unknown key", body: `{"dl_limit": 1, "no_such_key": true}`, cause: "no_such_key"},
		{name: "read-only key", body: `{"current_network_interface": "eth0"}`, cause: "current_network_interface"},
		{name: "fetched trackers", body: `{"add_trackers_url_list": "udp://a.invalid"}`, cause: `"add_trackers_url_list" is read-only`},
		{name: "not shown connection key", body: `{"web_ui_port": 9090}`, cause: "web_ui_port"},
		{name: "not shown key", body: `{"locale": "de"}`, cause: "locale"},
		{name: "api key", body: `{"web_ui_api_key": "x"}`, cause: "web_ui_api_key"},
		{name: "string for number", body: `{"dl_limit": "fast"}`, cause: "dl_limit"},
		{name: "fraction for integer", body: `{"dl_limit": 1.5}`, cause: "dl_limit"},
		{name: "number for switch", body: `{"dht": 1}`, cause: "dht"},
		{name: "null value", body: `{"save_path": null}`, cause: "save_path"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
			rig := newPreferencesRig(t, fake)

			recorder := rig.do(t, http.MethodPatch, "/preferences", tt.body)

			require.Equal(t, http.StatusBadRequest, recorder.Code)
			assert.Contains(t, recorder.Body.String(), tt.cause)
			assert.Empty(t, fake.recordedSets())
		})
	}
}

func TestPreferencesPatchReportsAFailedSave(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	fake.failSetting = true
	rig := newPreferencesRig(t, fake)

	recorder := rig.do(t, http.MethodPatch, "/preferences", `{"dl_limit": 2048}`)

	require.Equal(t, http.StatusInternalServerError, recorder.Code)
	assert.Contains(t, recorder.Body.String(), "save failed")
}

func TestRSSReprocessWritesThroughTheCatalogAndClearsTheCache(t *testing.T) {
	fake := newFakePreferencesQbit(t, "5.2.4", "2.15.1", map[string]any{"libtorrent": "2.0.11.0", "platform": "linux"})
	fake.prefs["rss_auto_downloading_enabled"] = false
	rig := newPreferencesRig(t, fake)

	view, _ := rig.view(t)
	assert.JSONEq(t, "false", string(view["rss_auto_downloading_enabled"].Value))

	// Another tool turns auto downloading on. The cached view does not see it yet.
	fake.mu.Lock()
	fake.prefs["rss_auto_downloading_enabled"] = true
	fake.mu.Unlock()

	recorder := rig.do(t, http.MethodPost, "/rss/reprocess", "")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())

	assert.Equal(t, []map[string]any{
		{"rss_auto_downloading_enabled": false},
		{"rss_auto_downloading_enabled": true},
	}, fake.recordedSets())

	view, _ = rig.view(t)
	assert.JSONEq(t, "true", string(view["rss_auto_downloading_enabled"].Value))
}
