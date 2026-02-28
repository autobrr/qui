// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/backups"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/internal/update"
	"github.com/autobrr/qui/internal/web"
	"github.com/autobrr/qui/internal/web/swagger"
)

type routeKey struct {
	Method string
	Path   string
}

var undocumentedRoutes = map[routeKey]struct{}{
	{Method: http.MethodGet, Path: "/api/auth/validate"}:                                            {},
	{Method: http.MethodPost, Path: "/api/instances/{instanceId}/backups/run"}:                      {},
	{Method: http.MethodGet, Path: "/api/instances/{instanceId}/backups/runs"}:                      {},
	{Method: http.MethodDelete, Path: "/api/instances/{instanceId}/backups/runs"}:                   {},
	{Method: http.MethodDelete, Path: "/api/instances/{instanceId}/backups/runs/{runId}"}:           {},
	{Method: http.MethodGet, Path: "/api/instances/{instanceId}/backups/runs/{runId}/manifest"}:     {},
	{Method: http.MethodGet, Path: "/api/instances/{instanceId}/backups/settings"}:                  {},
	{Method: http.MethodPut, Path: "/api/instances/{instanceId}/backups/settings"}:                  {},
	{Method: http.MethodGet, Path: "/api/instances/{instanceId}/automations"}:                       {},
	{Method: http.MethodPost, Path: "/api/instances/{instanceId}/automations"}:                      {},
	{Method: http.MethodPost, Path: "/api/instances/{instanceId}/automations/apply"}:                {},
	{Method: http.MethodPost, Path: "/api/instances/{instanceId}/automations/dry-run"}:              {},
	{Method: http.MethodPost, Path: "/api/instances/{instanceId}/automations/preview"}:              {},
	{Method: http.MethodPost, Path: "/api/instances/{instanceId}/automations/validate-regex"}:       {},
	{Method: http.MethodPut, Path: "/api/instances/{instanceId}/automations/order"}:                 {},
	{Method: http.MethodGet, Path: "/api/instances/{instanceId}/automations/activity"}:              {},
	{Method: http.MethodGet, Path: "/api/instances/{instanceId}/automations/activity/{activityId}"}: {},
	{Method: http.MethodDelete, Path: "/api/instances/{instanceId}/automations/activity"}:           {},
	{Method: http.MethodDelete, Path: "/api/instances/{instanceId}/automations/{ruleID}"}:           {},
	{Method: http.MethodPut, Path: "/api/instances/{instanceId}/automations/{ruleID}"}:              {},
	{Method: http.MethodGet, Path: "/api/tracker-customizations"}:                                   {},
	{Method: http.MethodPost, Path: "/api/tracker-customizations"}:                                  {},
	{Method: http.MethodPut, Path: "/api/tracker-customizations/{id}"}:                              {},
	{Method: http.MethodDelete, Path: "/api/tracker-customizations/{id}"}:                           {},
	{Method: http.MethodGet, Path: "/api/dashboard-settings"}:                                       {},
	{Method: http.MethodPut, Path: "/api/dashboard-settings"}:                                       {},
}

func TestAllEndpointsDocumented(t *testing.T) {
	server := NewServer(newTestDependencies(t))
	router, err := server.Handler()
	require.NoError(t, err)

	actualRoutes := collectRouterRoutes(t, router)
	documentedRoutes := loadDocumentedRoutes(t)

	undocumented := diffRoutes(actualRoutes, documentedRoutes)
	if len(undocumented) > 0 {
		t.Fatalf("found %d undocumented API endpoints:\n%s", len(undocumented), formatRoutes(undocumented))
	}

	missingHandlers := diffRoutes(documentedRoutes, actualRoutes)
	if len(missingHandlers) > 0 {
		t.Fatalf("found %d documented endpoints without handlers:\n%s", len(missingHandlers), formatRoutes(missingHandlers))
	}

	t.Logf("checked %d API routes registered in chi", len(actualRoutes))
	t.Logf("OpenAPI spec documents %d API routes", len(documentedRoutes))
}

func newTestDependencies(t *testing.T) *Dependencies {
	t.Helper()

	sessionManager := scs.New()

	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	authService := auth.NewService(db)
	_, err = authService.SetupUser(context.Background(), "test-user", "password123")
	if err != nil && !errors.Is(err, models.ErrUserAlreadyExists) {
		require.NoError(t, err)
	}

	trackerIconService, err := trackericons.NewService(t.TempDir(), "qui-test")
	require.NoError(t, err)

	trackerCustomizationStore := models.NewTrackerCustomizationStore(db)
	notificationTargetStore := models.NewNotificationTargetStore(db)
	notificationService := notifications.NewService(notificationTargetStore, &models.InstanceStore{}, log.Logger)
	dirScanService := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		&models.InstanceStore{},
		&qbittorrent.SyncManager{},
		nil,
		nil,
		trackerCustomizationStore,
		nil,
	)

	return &Dependencies{
		Config: &config.AppConfig{
			Config: &domain.Config{
				BaseURL: "/",
			},
		},
		Version:                   "test",
		AuthService:               authService,
		SessionManager:            sessionManager,
		InstanceStore:             &models.InstanceStore{},
		ClientAPIKeyStore:         &models.ClientAPIKeyStore{},
		ClientPool:                &qbittorrent.ClientPool{},
		SyncManager:               qbittorrent.NewSyncManager(nil, trackerCustomizationStore),
		WebHandler:                &web.Handler{},
		LicenseService:            &license.Service{},
		UpdateService:             &update.Service{},
		TrackerIconService:        trackerIconService,
		BackupService:             &backups.Service{},
		AutomationStore:           models.NewAutomationStore(db),
		TrackerCustomizationStore: trackerCustomizationStore,
		DashboardSettingsStore:    models.NewDashboardSettingsStore(db),
		NotificationTargetStore:   notificationTargetStore,
		NotificationService:       notificationService,
		DirScanService:            dirScanService,
	}
}

func collectRouterRoutes(t *testing.T, r chi.Routes) map[routeKey]struct{} {
	t.Helper()

	routes := make(map[routeKey]struct{})
	err := chi.Walk(r, func(method string, path string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		method = strings.ToUpper(method)
		if !isComparableMethod(method) {
			return nil
		}

		normalizedPath, ok := normalizeRoutePath(path)
		if !ok {
			return nil
		}

		route := routeKey{Method: method, Path: normalizedPath}
		if _, skip := undocumentedRoutes[route]; skip {
			return nil
		}

		routes[route] = struct{}{}
		return nil
	})
	require.NoError(t, err)

	return routes
}

func loadDocumentedRoutes(t *testing.T) map[routeKey]struct{} {
	t.Helper()

	specBytes, err := swagger.GetOpenAPISpec()
	require.NoError(t, err)
	require.NotEmpty(t, specBytes, "OpenAPI spec should be embedded")

	var spec map[string]any
	require.NoError(t, yaml.Unmarshal(specBytes, &spec))

	pathsNode, ok := spec["paths"].(map[string]any)
	require.True(t, ok, "OpenAPI spec missing paths section")

	routes := make(map[routeKey]struct{})

	for path, pathItem := range pathsNode {
		normalizedPath, ok := normalizeRoutePath(path)
		if !ok {
			continue
		}

		methods, ok := pathItem.(map[string]any)
		if !ok {
			continue
		}

		for method := range methods {
			upperMethod := strings.ToUpper(method)
			if !isComparableMethod(upperMethod) {
				continue
			}

			routes[routeKey{Method: upperMethod, Path: normalizedPath}] = struct{}{}
		}
	}

	return routes
}

func normalizeRoutePath(path string) (string, bool) {
	if path == "" {
		return "", false
	}

	if strings.Contains(path, "/*") {
		return "", false
	}

	if path != "/" {
		path = strings.TrimSuffix(path, "/")
	}

	if path == "/api/docs" || path == "/api/openapi.json" {
		return "", false
	}

	if !strings.HasPrefix(path, "/api") && !strings.HasPrefix(path, "/health") {
		return "", false
	}

	path = strings.ReplaceAll(path, "{instanceID}", "{instanceId}")
	path = strings.ReplaceAll(path, "{runID}", "{runId}")
	path = strings.ReplaceAll(path, "{licenseKey}", "{licenseKey}")

	return path, true
}

func isComparableMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func diffRoutes(left, right map[routeKey]struct{}) []routeKey {
	diff := make([]routeKey, 0)
	for route := range left {
		if _, exists := right[route]; !exists {
			diff = append(diff, route)
		}
	}

	sort.Slice(diff, func(i, j int) bool {
		if diff[i].Path == diff[j].Path {
			return diff[i].Method < diff[j].Method
		}
		return diff[i].Path < diff[j].Path
	})

	return diff
}

func formatRoutes(routes []routeKey) string {
	lines := make([]string, len(routes))
	for i, route := range routes {
		lines[i] = fmt.Sprintf("%s %s", route.Method, route.Path)
	}
	return strings.Join(lines, "\n")
}

func TestSkipCompressionForPath(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name string
		path string
		want bool
	}{
		{
			name: "content_download_route",
			path: "/api/instances/1/torrents/abcd/files/2/download",
			want: true,
		},
		{
			name: "content_download_route_with_baseurl_prefix",
			path: "/qui/api/instances/1/torrents/abcd/files/2/download",
			want: true,
		},
		{
			name: "backups_download_route_not_skipped",
			path: "/api/instances/1/backups/runs/2/download",
			want: false,
		},
		{
			name: "content_files_endpoint_not_skipped",
			path: "/api/instances/1/torrents/abcd/files",
			want: false,
		},
		{
			name: "download_with_trailing_segment_not_skipped",
			path: "/api/instances/1/torrents/abcd/files/2/download/extra",
			want: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, skipCompressionForPath(tc.path))
		})
	}
}
