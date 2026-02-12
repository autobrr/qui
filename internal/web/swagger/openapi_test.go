// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package swagger

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
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

// TestAddTorrentFormFieldsDocumented verifies every r.FormValue() parameter in the
// add-torrent handler is documented in the OpenAPI spec's multipart schema, and vice versa.
// This catches mismatches like savePath vs savepath that cause silent API failures.
func TestAddTorrentFormFieldsDocumented(t *testing.T) {
	// Locate torrents.go relative to this test file so the test works
	// regardless of the working directory used by `go test`.
	_, thisFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	handlerPath := filepath.Join(filepath.Dir(thisFile), "..", "..", "api", "handlers", "torrents.go")

	// Parse the handler source with go/parser so we only inspect the
	// AddTorrent method and extract FormValue string arguments from the AST.
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, handlerPath, nil, 0)
	if err != nil {
		t.Fatalf("Failed to parse torrents handler: %v", err)
	}

	handlerFields := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "AddTorrent" {
			continue
		}
		// Walk the AST of AddTorrent looking for r.FormValue("...") calls.
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok || len(call.Args) != 1 {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || sel.Sel.Name != "FormValue" {
				return true
			}
			arg, ok := call.Args[0].(*ast.BasicLit)
			if !ok || arg.Kind != token.STRING {
				return true
			}
			// Strip quotes from the string literal.
			field := arg.Value[1 : len(arg.Value)-1]
			handlerFields[field] = true
			return true
		})
	}
	if len(handlerFields) == 0 {
		t.Fatal("No FormValue calls found in AddTorrent handler")
	}

	// Parse the OpenAPI spec and extract properties from the add-torrent endpoint.
	var spec map[string]any
	if err := yaml.Unmarshal(openapiYAML, &spec); err != nil {
		t.Fatalf("Failed to parse OpenAPI spec: %v", err)
	}

	// Navigate: paths -> /api/instances/{instanceID}/torrents -> post -> requestBody
	//   -> content -> multipart/form-data -> schema -> properties
	asMap := func(v any, path string) map[string]any {
		t.Helper()
		m, ok := v.(map[string]any)
		if !ok {
			t.Fatalf("Expected map at %s, got %T", path, v)
		}
		return m
	}
	key := func(m map[string]any, k, path string) any {
		t.Helper()
		v, ok := m[k]
		if !ok {
			t.Fatalf("Missing key %q at %s", k, path)
		}
		return v
	}

	paths := asMap(spec["paths"], "spec.paths")
	torrentsPath := asMap(key(paths, "/api/instances/{instanceID}/torrents", "paths"), "paths[torrents]")
	post := asMap(key(torrentsPath, "post", "torrents"), "torrents.post")
	reqBody := asMap(key(post, "requestBody", "post"), "post.requestBody")
	content := asMap(key(reqBody, "content", "requestBody"), "requestBody.content")
	formData := asMap(key(content, "multipart/form-data", "content"), "content[multipart/form-data]")
	schema := asMap(key(formData, "schema", "formData"), "formData.schema")
	properties := asMap(key(schema, "properties", "schema"), "schema.properties")

	specFields := make(map[string]bool)
	for name := range properties {
		specFields[name] = true
	}

	// torrentFile is a file upload field, not read via FormValue — exclude it.
	// urls is read via FormValue but also present in spec.
	skipHandler := map[string]bool{
		"torrentFile": true,
	}

	// Check: every handler field must be in the spec.
	for field := range handlerFields {
		if skipHandler[field] {
			continue
		}
		if !specFields[field] {
			t.Errorf("Handler reads r.FormValue(%q) but OpenAPI spec does not document it", field)
		}
	}

	// Check: every spec field must be in the handler (or be torrentFile).
	for field := range specFields {
		if skipHandler[field] {
			continue
		}
		if !handlerFields[field] {
			t.Errorf("OpenAPI spec documents %q but handler does not read it via r.FormValue", field)
		}
	}

	t.Logf("Handler fields: %d, Spec fields: %d", len(handlerFields), len(specFields))
}
