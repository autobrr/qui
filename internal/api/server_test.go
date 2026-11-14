// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/synctest"
	"time"
	"unsafe"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
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
	"github.com/autobrr/qui/internal/services/arr"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/services/dbshrink"
	"github.com/autobrr/qui/internal/services/dirscan"
	"github.com/autobrr/qui/internal/services/discscan"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/services/notifications"
	"github.com/autobrr/qui/internal/services/peerclienticons"
	"github.com/autobrr/qui/internal/services/systemnotice"
	"github.com/autobrr/qui/internal/services/trackericons"
	"github.com/autobrr/qui/internal/services/transferhistory"
	"github.com/autobrr/qui/internal/testutil/testdb"
	"github.com/autobrr/qui/internal/update"
	"github.com/autobrr/qui/internal/web"
	"github.com/autobrr/qui/internal/web/swagger"
)

type routeKey struct {
	Method string
	Path   string
}

var undocumentedRoutes = map[routeKey]struct{}{
	{Method: http.MethodGet, Path: "/api/torznab/activity"}:                                         {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers"}:                                         {},
	{Method: http.MethodPost, Path: "/api/torznab/indexers"}:                                        {},
	{Method: http.MethodPost, Path: "/api/torznab/indexers/discover"}:                               {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers/health"}:                                  {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers/tracker-domains"}:                         {},
	{Method: http.MethodDelete, Path: "/api/torznab/indexers/{indexerID}"}:                          {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers/{indexerID}"}:                             {},
	{Method: http.MethodPut, Path: "/api/torznab/indexers/{indexerID}"}:                             {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers/{indexerID}/errors"}:                      {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers/{indexerID}/health"}:                      {},
	{Method: http.MethodGet, Path: "/api/torznab/indexers/{indexerID}/stats"}:                       {},
	{Method: http.MethodGet, Path: "/api/torznab/search/cache"}:                                     {},
	{Method: http.MethodPut, Path: "/api/torznab/search/cache/settings"}:                            {},
	{Method: http.MethodGet, Path: "/api/torznab/search/history"}:                                   {},
	{Method: http.MethodGet, Path: "/api/torznab/search/recent"}:                                    {},
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
	{Method: http.MethodGet, Path: "/api/application/info"}:                                         {},
	{Method: http.MethodGet, Path: "/api/tracker-customizations"}:                                   {},
	{Method: http.MethodPost, Path: "/api/tracker-customizations"}:                                  {},
	{Method: http.MethodPut, Path: "/api/tracker-customizations/{id}"}:                              {},
	{Method: http.MethodDelete, Path: "/api/tracker-customizations/{id}"}:                           {},
	{Method: http.MethodGet, Path: "/api/dashboard-settings"}:                                       {},
	{Method: http.MethodPut, Path: "/api/dashboard-settings"}:                                       {},
}

func TestNewServerRegistersStreamManagerAsSyncSink(t *testing.T) {
	clientPool := &qbittorrent.ClientPool{}

	server := NewServer(&Dependencies{
		Config:     &config.AppConfig{Config: &domain.Config{BaseURL: "/"}},
		ClientPool: clientPool,
	})

	require.NotNil(t, server.streamManager, "expected stream manager to be initialized")

	sinks := getClientPoolSyncEventSinks(t, clientPool)
	require.Len(t, sinks, 1, "expected client pool to have one sync sink registered")
	require.Same(t, server.streamManager, sinks[0], "stream manager should be registered as sync sink")
}

func TestServerShutdownTwiceEndsStreamsWithoutPanic(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		server := NewServer(&Dependencies{
			Config: &config.AppConfig{Config: &domain.Config{BaseURL: "/"}},
		})

		// http.Server.Shutdown starts the registered callbacks on every call.
		require.NoError(t, server.Shutdown(t.Context()))
		require.NoError(t, server.Shutdown(t.Context()))

		// Each callback runs in its own goroutine; wait for both so a double close panics here.
		synctest.Wait()
		select {
		case <-server.shuttingDown:
		default:
			t.Fatal("shutdown signal was not sent")
		}
	})
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

	db := testdb.NewMigratedSQLite(t, "api-server")
	encryptionKey := []byte("01234567890123456789012345678901")
	dataDir := t.TempDir()

	authService := auth.NewService(db)
	_, err := authService.SetupUser(context.Background(), "test-user", "password123")
	if err != nil && !errors.Is(err, models.ErrUserAlreadyExists) {
		require.NoError(t, err)
	}

	instanceStore, err := models.NewInstanceStore(db, encryptionKey)
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instanceStore, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, clientPool.Close()) })

	trackerIconService, err := trackericons.NewService(dataDir, "qui-test")
	require.NoError(t, err)
	peerClientIconService, err := peerclienticons.NewService(dataDir, "qui-test", nil)
	require.NoError(t, err)

	trackerCustomizationStore := models.NewTrackerCustomizationStore(db)
	syncManager := qbittorrent.NewSyncManager(clientPool, trackerCustomizationStore)
	notificationTargetStore := models.NewNotificationTargetStore(db)
	notificationService := notifications.NewService(notificationTargetStore, instanceStore, log.Logger)
	torznabIndexerStore, err := models.NewTorznabIndexerStore(db, encryptionKey)
	require.NoError(t, err)
	arrInstanceStore, err := models.NewArrInstanceStore(db, encryptionKey)
	require.NoError(t, err)
	discScanStore := models.NewDiscScanStore(db)
	systemNotices := systemnotice.New(nil)
	databaseShrink := dbshrink.New(db, systemNotices, nil)
	transferHistory := transferhistory.New(db, trackerCustomizationStore, systemNotices)
	t.Cleanup(func() { require.NoError(t, transferHistory.Shutdown(context.Background())) })
	jackettService := jackett.NewService(torznabIndexerStore)
	crossSeedStore, err := models.NewCrossSeedStore(db, encryptionKey)
	require.NoError(t, err)
	crossSeedService := crossseed.NewService(instanceStore, syncManager, nil, crossSeedStore, models.NewCrossSeedBlocklistStore(db), jackettService, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil, nil)
	dirScanService := dirscan.NewService(
		dirscan.DefaultConfig(),
		models.NewDirScanStore(db),
		nil,
		syncManager,
		nil,
		nil,
		trackerCustomizationStore,
		nil,
		nil,
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
		InstanceStore:             instanceStore,
		ClientAPIKeyStore:         models.NewClientAPIKeyStore(db),
		ExternalProgramStore:      models.NewExternalProgramStore(db),
		ClientPool:                clientPool,
		SyncManager:               syncManager,
		WebHandler:                &web.Handler{},
		LicenseService:            license.NewLicenseService(database.NewLicenseRepo(db), nil, dataDir),
		UpdateService:             &update.Service{},
		TrackerIconService:        trackerIconService,
		PeerClientIconService:     peerClientIconService,
		BackupService:             backups.NewService(models.NewBackupStore(db), syncManager, backups.Config{}, notificationService),
		AutomationStore:           models.NewAutomationStore(db),
		TrackerCustomizationStore: trackerCustomizationStore,
		DashboardSettingsStore:    models.NewDashboardSettingsStore(db),
		ClientSettingsStore:       models.NewClientSettingsStore(db),
		ThemeSettingsStore:        models.NewThemeSettingsStore(db),
		FilterViewStore:           models.NewFilterViewStore(db),
		LogExclusionsStore:        models.NewLogExclusionsStore(db),
		NotificationTargetStore:   notificationTargetStore,
		NotificationService:       notificationService,
		DiscScanStore:             discScanStore,
		DiscScanService:           discscan.NewService(discScanStore),
		TransferHistory:           transferHistory,
		SystemNotices:             systemNotices,
		DatabaseShrink:            databaseShrink,
		DirScanService:            dirScanService,
		CrossSeedService:          crossSeedService,
		CrossSeedRoutes:           crossseedroute.NewService(models.NewCrossSeedRouteStore(db), instanceStore, clientPool, nil),
		JackettService:            jackettService,
		TorznabIndexerStore:       torznabIndexerStore,
		ArrInstanceStore:          arrInstanceStore,
		ArrService:                arr.NewService(arrInstanceStore, models.NewArrIDCacheStore(db)),
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

func getClientPoolSyncEventSinks(t *testing.T, pool *qbittorrent.ClientPool) []qbittorrent.SyncEventSink {
	t.Helper()

	value := reflect.ValueOf(pool).Elem().FieldByName("syncEventSinks")
	if !value.IsValid() {
		t.Fatalf("client pool does not expose syncEventSinks field")
	}

	exposed := reflect.NewAt(value.Type(), unsafe.Pointer(value.UnsafeAddr())).Elem()
	sinks := make([]qbittorrent.SyncEventSink, exposed.Len())
	for i := range sinks {
		sink, ok := exposed.Index(i).Interface().(qbittorrent.SyncEventSink)
		require.True(t, ok, "unexpected sink type stored on client pool")
		sinks[i] = sink
	}
	return sinks
}

func TestOpenStopsWhenPortIsInUse(t *testing.T) {
	held, err := net.Listen("tcp4", "127.0.0.1:0")
	require.NoError(t, err)
	defer held.Close()

	s := &Server{
		logger: zerolog.Nop(),
		config: &config.AppConfig{Config: &domain.Config{
			Host: "localhost",
			Port: held.Addr().(*net.TCPAddr).Port,
		}},
	}
	err = s.open(nil)
	require.ErrorIs(t, err, errAddrInUse)
}
