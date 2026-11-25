// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/testutil/sshtest"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type webhookFixture struct {
	service       *dirscan.Service
	handler       *DirScanHandler
	scan          *singleDirScanRun
	remote, local *models.Instance
}

func newWebhookFixture(t *testing.T, host *webhookHost) *webhookFixture {
	t.Helper()
	ctx := t.Context()
	db := testdb.NewMigratedSQLite(t, "dirscan-remote-webhook")
	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	remote, err := instances.Create(ctx, "remote", "http://localhost:8080", "", "", nil, nil, false, new(false))
	require.NoError(t, err)
	require.NoError(t, instances.SetSSHCredentials(ctx, remote.ID, "box.example", 22, "qui", sshtest.PrivateKey("")))
	require.NoError(t, instances.SetHostKeyPin(ctx, remote.ID, "box.example", 22, sshtest.NewSigner().PublicKey().Marshal()))
	local, err := instances.Create(ctx, "local", "http://localhost:8080", "", "", nil, nil, false, new(true))
	require.NoError(t, err)

	pool := fsops.NewPoolWithRemote(instances, localbackend.NewBackend(), func(*models.Instance) fsops.Backend { return host })
	service, scan := newSingleDirScanRun(db, pool)
	return &webhookFixture{service: service, handler: NewDirScanHandler(service, instances), scan: scan, remote: remote, local: local}
}

func (f *webhookFixture) createDir(t *testing.T, path string, instance *models.Instance) (*models.DirScanDirectory, error) {
	t.Helper()
	return f.service.CreateDirectory(t.Context(), &models.DirScanDirectory{Path: path, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60})
}

func TestDirScanDirectoryPathIsUniquePerInstance(t *testing.T) {
	f := newWebhookFixture(t, &webhookHost{})
	onRemote, err := f.createDir(t, "/data/lib", f.remote)
	require.NoError(t, err)
	_, err = f.createDir(t, "/data/lib", f.local)
	require.NoError(t, err, "another instance can use the same path")
	_, err = f.createDir(t, "/data/Lib", f.remote)
	require.NoError(t, err, "a remote path keeps its letter case on every qui host")

	_, err = f.createDir(t, "/data/lib/", f.remote)
	require.ErrorIs(t, err, models.ErrDuplicateDirScanDirectoryPath)

	other, err := f.createDir(t, "/data/other", f.local)
	require.NoError(t, err)
	_, err = f.service.UpdateDirectory(t.Context(), other.ID, &models.DirScanDirectoryUpdateParams{Path: new("/data/lib")})
	require.ErrorIs(t, err, models.ErrDuplicateDirScanDirectoryPath)
	_, err = f.service.UpdateDirectory(t.Context(), onRemote.ID, &models.DirScanDirectoryUpdateParams{TargetInstanceID: new(f.local.ID)})
	require.ErrorIs(t, err, models.ErrDuplicateDirScanDirectoryPath)
	_, err = f.service.UpdateDirectory(t.Context(), onRemote.ID, &models.DirScanDirectoryUpdateParams{Category: new("tv")})
	require.NoError(t, err, "an update that keeps the path does not collide with itself")
}

func TestWebhookTriggerScan_InstanceID(t *testing.T) {
	const testEvent = "Test"
	for _, tt := range []struct {
		name       string
		instanceID func(f *webhookFixture) string
		eventType  string
		wantCode   int
		wantBody   string
	}{
		{name: "a shared path without instanceId is a conflict that names it", wantCode: http.StatusConflict, wantBody: "Add instanceId"},
		{name: "instanceId picks the directory of that instance", instanceID: func(f *webhookFixture) string { return strconv.Itoa(f.remote.ID) }, wantCode: http.StatusAccepted},
		{name: "an instance without a directory matches nothing", instanceID: func(*webhookFixture) string { return "9999" }, wantCode: http.StatusNotFound},
		{name: "a malformed instanceId is rejected", instanceID: func(*webhookFixture) string { return "abc" }, wantCode: http.StatusBadRequest},
		{name: "instanceId 0 is rejected", instanceID: func(*webhookFixture) string { return "0" }, wantCode: http.StatusBadRequest},
		{name: "a test event checks the instance", instanceID: func(*webhookFixture) string { return "9999" }, eventType: testEvent, wantCode: http.StatusNotFound},
		{name: "a test event passes for an instance with a directory", instanceID: func(f *webhookFixture) string { return strconv.Itoa(f.remote.ID) }, eventType: testEvent, wantCode: http.StatusNoContent},
	} {
		t.Run(tt.name, func(t *testing.T) {
			f := newWebhookFixture(t, &webhookHost{files: map[string]bool{"/data/lib/Show": true}})
			_, err := f.createDir(t, "/data/lib", f.local)
			require.NoError(t, err)
			onRemote, err := f.createDir(t, "/data/lib", f.remote)
			require.NoError(t, err)

			url := "/api/dir-scan/webhook/scan"
			if tt.instanceID != nil {
				url += "?instanceId=" + tt.instanceID(f)
			}
			payload := webhookPayloadArr{Series: webhookPayloadSeries{Path: "/data/lib/Show"}, EventType: tt.eventType}
			rec := httptest.NewRecorder()
			f.handler.WebhookTriggerScan(rec, httptest.NewRequestWithContext(t.Context(), http.MethodPost, url, dirScanWebhookJSONBody(t, payload)))

			require.Equal(t, tt.wantCode, rec.Code, rec.Body.String())
			require.Contains(t, rec.Body.String(), tt.wantBody)
			if tt.wantCode == http.StatusAccepted {
				require.Equal(t, onRemote.ID, f.scan.response(t, rec).DirectoryID)
			}
		})
	}
}
