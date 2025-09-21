// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/http"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
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
	Method string
	Path   string
}

// countDocumentedEndpoints counts the total number of documented endpoints
func countDocumentedEndpoints(paths map[string]map[string]bool) int {
	count := 0
	for _, methods := range paths {
		count += len(methods)
	}
	return count
}
