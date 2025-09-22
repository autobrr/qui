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

<<<<<<< HEAD:internal/api/router_test.go
type routeKey struct {
=======
// TestAllEndpointsDocumented ensures every API route in router.go is documented in OpenAPI spec
func TestAllEndpointsDocumented(t *testing.T) {
	// Create minimal dependencies just to build the router structure
	// The handlers won't be executed during chi.Walk, so we just need non-nil pointers
	deps := &Dependencies{
		Config: &config.AppConfig{
			Config: &domain.Config{
				BaseURL: "/",
			},
		},
		AuthService:    &auth.Service{},
		InstanceStore:  &models.InstanceStore{},
		ClientPool:     &qbittorrent.ClientPool{},
		SyncManager:    &qbittorrent.SyncManager{},
		WebHandler:     &web.Handler{},
		LicenseService: &license.Service{}, // Include license service to get all routes
	}

	// Create the actual router from router.go
	server := NewServer(deps)
	router := server.Handler()

	// Extract all routes from the actual router
	var actualRoutes []Route
	walkFunc := func(method string, path string, handler http.Handler, middlewares ...func(http.Handler) http.Handler) error {
		actualRoutes = append(actualRoutes, Route{
			Method: method,
			Path:   path,
		})
		return nil
	}
	chi.Walk(router, walkFunc)

	// Load and parse OpenAPI spec
	spec, err := swagger.GetOpenAPISpec()
	if err != nil {
		t.Fatalf("Failed to get OpenAPI spec: %v", err)
	}

	var openapiSpec map[string]any
	if err := yaml.Unmarshal(spec, &openapiSpec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	// Get all documented paths from OpenAPI
	documentedPaths := make(map[string]map[string]bool)
	if paths, ok := openapiSpec["paths"].(map[string]any); ok {
		for path, pathItem := range paths {
			documentedPaths[path] = make(map[string]bool)
			if methods, ok := pathItem.(map[string]any); ok {
				for method := range methods {
					if method == "get" || method == "post" || method == "put" || method == "delete" || method == "patch" {
						documentedPaths[path][strings.ToUpper(method)] = true
					}
				}
			}
		}
	}

	// Check for undocumented routes
	var undocumented []string
	var nonAPIRoutes []string

	for _, route := range actualRoutes {
		// Skip non-API routes (these are handled elsewhere)
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/health") {
			if route.Path != "/" && route.Path != "/*" {
				nonAPIRoutes = append(nonAPIRoutes, route.Method+" "+route.Path)
			}
			continue
		}

		// Skip special routes that shouldn't be documented
		if route.Path == "/api/docs" || route.Path == "/api/openapi.json" {
			continue
		}

		// Convert Chi path params to OpenAPI format and normalize
		openapiPath := route.Path
		// Remove trailing slash (Chi adds them but OpenAPI doesn't use them)
		openapiPath = strings.TrimSuffix(openapiPath, "/")
		// Convert parameter names to match OpenAPI spec
		openapiPath = strings.ReplaceAll(openapiPath, "{instanceID}", "{instanceId}")
		openapiPath = strings.ReplaceAll(openapiPath, "{licenseKey}", "{licenseKey}")

		// Check if route is documented
		found := false
		if methods, exists := documentedPaths[openapiPath]; exists {
			if methods[route.Method] {
				found = true
			}
		}

		if !found {
			undocumented = append(undocumented, route.Method+" "+route.Path)
		}
	}

	// Report any undocumented routes
	if len(undocumented) > 0 {
		t.Errorf("Found %d undocumented API endpoints:", len(undocumented))
		for _, route := range undocumented {
			t.Errorf("  - %s", route)
		}
		t.Error("Please add these endpoints to internal/web/swagger/openapi.yaml")
	}

	// Check for documented routes that don't exist in code
	var phantom []string
	actualRouteSet := make(map[string]bool)

	for _, route := range actualRoutes {
		// Skip non-API routes
		if !strings.HasPrefix(route.Path, "/api/") && !strings.HasPrefix(route.Path, "/health") {
			continue
		}

		// Skip special routes that shouldn't be documented
		if route.Path == "/api/docs" || route.Path == "/api/openapi.json" {
			continue
		}

		// Normalize path for comparison
		normalizedPath := route.Path
		normalizedPath = strings.TrimSuffix(normalizedPath, "/")
		normalizedPath = strings.ReplaceAll(normalizedPath, "{instanceID}", "{instanceId}")
		normalizedPath = strings.ReplaceAll(normalizedPath, "{licenseKey}", "{licenseKey}")

		actualRouteSet[route.Method+" "+normalizedPath] = true
	}

	// Check each documented endpoint
	for path, methods := range documentedPaths {
		for method := range methods {
			routeKey := strings.ToUpper(method) + " " + path
			if !actualRouteSet[routeKey] {
				phantom = append(phantom, routeKey)
			}
		}
	}

	// Report any phantom routes (documented but not implemented)
	if len(phantom) > 0 {
		t.Errorf("Found %d documented endpoints that don't exist in code:", len(phantom))
		for _, route := range phantom {
			t.Errorf("  - %s", route)
		}
		t.Error("Please remove these endpoints from internal/web/swagger/openapi.yaml or implement them")
	}

	// Log summary
	t.Logf("Checked %d routes from router.go", len(actualRoutes))
	t.Logf("Found %d API routes", len(actualRoutes)-len(nonAPIRoutes))
	t.Logf("Found %d documented endpoints in OpenAPI spec", countDocumentedEndpoints(documentedPaths))
}

// Route represents a single route
type Route struct {
>>>>>>> main:internal/api/server_test.go
	Method string
	Path   string
}

func TestAllEndpointsDocumented(t *testing.T) {
	router := NewRouter(newTestDependencies(t))

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
			Config: &domain.Config{},
		},
		AuthService:         &auth.Service{},
		SessionManager:      sessionManager,
		InstanceStore:       &models.InstanceStore{},
		ClientAPIKeyStore:   &models.ClientAPIKeyStore{},
		ClientPool:          &qbittorrent.ClientPool{},
		SyncManager:         &qbittorrent.SyncManager{},
		WebHandler:          &web.Handler{},
		ThemeLicenseService: &services.ThemeLicenseService{},
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

	if !strings.HasPrefix(path, "/api") && path != "/health" {
		return "", false
	}

	path = strings.ReplaceAll(path, "{instanceID}", "{instanceId}")

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
