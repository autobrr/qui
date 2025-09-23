// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/config"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/license"
	"github.com/autobrr/qui/internal/web"
	"github.com/autobrr/qui/internal/web/swagger"
)

type routeKey struct {
	Method string
	Path   string
}

func TestAllEndpointsDocumented(t *testing.T) {
	server := NewServer(newTestDependencies(t))
	router := server.Handler()

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

	return &Dependencies{
		Config: &config.AppConfig{
			Config: &domain.Config{
				BaseURL: "/",
			},
		},
		Version:           "test",
		AuthService:       &auth.Service{},
		SessionManager:    sessionManager,
		InstanceStore:     &models.InstanceStore{},
		ClientAPIKeyStore: &models.ClientAPIKeyStore{},
		ClientPool:        &qbittorrent.ClientPool{},
		SyncManager:       &qbittorrent.SyncManager{},
		WebHandler:        &web.Handler{},
		LicenseService:    &license.Service{},
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

		routes[routeKey{Method: method, Path: normalizedPath}] = struct{}{}
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
