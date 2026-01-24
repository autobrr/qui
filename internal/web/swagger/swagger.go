// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package swagger

import (
	_ "embed"
	"encoding/json"
	"maps"
	"net/http"
	"strings"

	"github.com/autobrr/qui/pkg/httphelpers"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var openapiYAML []byte

//go:embed index.html
var swaggerHTML string

type Handler struct {
	spec    map[string]any
	baseURL string
}

func NewHandler(baseURL string) (*Handler, error) {
	if len(openapiYAML) == 0 {
		return nil, nil // Return nil handler if no spec embedded
	}

	var spec map[string]any
	if err := yaml.Unmarshal(openapiYAML, &spec); err != nil {
		return nil, err
	}

	// Normalize baseURL: ensures leading slash, no trailing slash, "" for root
	baseURL = httphelpers.NormalizeBasePath(baseURL)

	return &Handler{
		spec:    spec,
		baseURL: baseURL,
	}, nil
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get(h.baseURL+"/api/docs", h.ServeSwaggerUI)
	r.Get(h.baseURL+"/api/openapi.json", h.ServeOpenAPISpec)
}

func (h *Handler) ServeSwaggerUI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	// Replace URLs in the HTML with base URL aware paths
	openAPIPath := h.baseURL + "/api/openapi.json"
	faviconPath := h.baseURL + "/qui.png"

	html := strings.ReplaceAll(swaggerHTML, "{{OPENAPI_URL}}", openAPIPath)
	html = strings.ReplaceAll(html, "{{FAVICON_URL}}", faviconPath)

	w.Write([]byte(html))
}

// GetOpenAPISpec returns the embedded OpenAPI spec for testing
func GetOpenAPISpec() ([]byte, error) {
	if len(openapiYAML) == 0 {
		return nil, nil
	}
	return openapiYAML, nil
}

func (h *Handler) ServeOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Create a copy of the spec to modify
	spec := make(map[string]any)
	maps.Copy(spec, h.spec)

	if h.baseURL != "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := r.Host

		servers := []map[string]any{
			{
				"url":         scheme + "://" + host + h.baseURL,
				"description": "Current server with base URL",
			},
		}

		// Keep existing servers as fallback
		if existingServers, ok := spec["servers"].([]any); ok {
			for _, s := range existingServers {
				if server, ok := s.(map[string]any); ok {
					servers = append(servers, server)
				}
			}
		}

		spec["servers"] = servers
	}

	json.NewEncoder(w).Encode(spec)
}
