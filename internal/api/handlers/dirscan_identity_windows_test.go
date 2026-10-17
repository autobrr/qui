// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	localbackend "github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestWebhookTriggerScan_WindowsDirectoryIdentity(t *testing.T) {
	ctx := t.Context()
	root := t.TempDir()
	path := filepath.Join(root, "Media Library")
	alias := filepath.Join(root, "MEDIA LIBRARY")
	require.NoError(t, os.Mkdir(path, 0o750))
	info, err := os.Stat(path)
	require.NoError(t, err)
	aliasInfo, err := os.Stat(alias)
	require.NoError(t, err)
	require.True(t, os.SameFile(info, aliasInfo))

	db := testdb.NewMigratedSQLite(t, "windows-directory-identity")
	instances, err := models.NewInstanceStore(db, []byte("0123456789abcdef0123456789abcdef"))
	require.NoError(t, err)
	instance, err := instances.Create(ctx, "synthetic", "http://localhost:8080", "", "", nil, nil, false, new(true))
	require.NoError(t, err)
	service := dirscan.NewService(dirscan.DefaultConfig(), models.NewDirScanStore(db), nil, instances,
		nil, nil, nil, nil, nil, fsops.NewPool(instances, localbackend.NewBackend()), nil)
	created, err := service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path: path, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
	})
	require.NoError(t, err)
	_, err = service.CreateDirectory(ctx, &models.DirScanDirectory{
		Path: alias, Enabled: true, TargetInstanceID: instance.ID, ScanIntervalMinutes: 60,
	})
	if !errors.Is(err, models.ErrDuplicateDirScanDirectoryPath) {
		t.Errorf("case-twin configuration must be rejected: got error %v", err)
	}

	handler := NewDirScanHandler(service, instances)
	req := httptest.NewRequestWithContext(ctx, http.MethodPost, "/api/dir-scan/webhook/scan",
		dirScanWebhookJSONBody(t, webhookPayloadSimple{Path: alias}))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.WebhookTriggerScan(rec, req)
	assert.Equal(t, http.StatusAccepted, rec.Code, "normal configuration must not produce an ambiguous webhook: %s", rec.Body)
	if rec.Code != http.StatusAccepted {
		return
	}
	var response dirScanTriggerResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &response))
	assert.Equal(t, created.ID, response.DirectoryID)
	assert.Equal(t, path, response.DirectoryPath)
	require.Eventually(t, func() bool {
		run, err := service.GetActiveRun(ctx, created.ID)
		return err == nil && run == nil
	}, 5*time.Second, 10*time.Millisecond)
}
