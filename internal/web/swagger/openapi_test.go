// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package swagger

import (
	"testing"

	"gopkg.in/yaml.v3"
)

func TestOpenAPISpec(t *testing.T) {
	// Check if the embedded OpenAPI spec is valid
	if len(openapiYAML) == 0 {
		t.Fatal("OpenAPI spec is empty")
	}

	var spec map[string]any
	if err := yaml.Unmarshal(openapiYAML, &spec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	if spec["openapi"] == nil {
		t.Error("Missing 'openapi' field")
	}

	if spec["info"] == nil {
		t.Error("Missing 'info' field")
	}

	if spec["paths"] == nil {
		t.Error("Missing 'paths' field")
	}

	paths, ok := spec["paths"].(map[string]any)
	if !ok {
		t.Fatal("'paths' is not a map")
	}

	totalEndpoints := 0
	for _, pathItem := range paths {
		if methods, ok := pathItem.(map[string]any); ok {
			for method := range methods {
				// Skip non-HTTP methods like "parameters"
				if method == "get" || method == "post" || method == "put" || method == "delete" || method == "patch" {
					totalEndpoints++
				}
			}
		}
	}

	t.Logf("OpenAPI spec documents %d endpoints", totalEndpoints)

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("Missing or invalid 'components' section")
	}

	schemas, ok := components["schemas"].(map[string]any)
	if !ok {
		t.Fatal("Missing or invalid 'schemas' section")
	}

	// Check for required schemas
	requiredSchemas := []string{
		"User",
		"ApiKey",
		"Instance",
		"InstanceCapabilities",
		"Torrent",
		"TorrentProperties",
		"Tracker",
		"TorrentFile",
		"Category",
	}

	for _, schema := range requiredSchemas {
		if schemas[schema] == nil {
			t.Errorf("Missing schema: %s", schema)
		}
	}
}

// TestOpenAPISecuritySchemes validates that security schemes are properly defined
func TestOpenAPISecuritySchemes(t *testing.T) {
	var spec map[string]any
	if err := yaml.Unmarshal(openapiYAML, &spec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	components, ok := spec["components"].(map[string]any)
	if !ok {
		t.Fatal("Missing or invalid 'components' section")
	}

	securitySchemes, ok := components["securitySchemes"].(map[string]any)
	if !ok {
		t.Fatal("Missing or invalid 'securitySchemes' section")
	}

	requiredSchemes := []string{"ApiKeyAuth", "SessionAuth"}
	for _, scheme := range requiredSchemes {
		if securitySchemes[scheme] == nil {
			t.Errorf("Missing security scheme: %s", scheme)
		}
	}
}
