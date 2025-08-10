// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package swagger

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

//go:embed openapi.yaml
var openapiYAML []byte

//go:embed index.html
var swaggerHTML string

type Handler struct {
	spec    map[string]interface{}
	baseURL string
}

func NewHandler(baseURL string) (*Handler, error) {
	if len(openapiYAML) == 0 {
		return nil, nil // Return nil handler if no spec embedded
	}

	var spec map[string]interface{}
	if err := yaml.Unmarshal(openapiYAML, &spec); err != nil {
		return nil, err
	}

	// Ensure baseURL doesn't have trailing slash
	baseURL = strings.TrimSuffix(baseURL, "/")

	return &Handler{
		spec:    spec,
		baseURL: baseURL,
	}, nil
}

func (h *Handler) RegisterRoutes(r chi.Router) {
	r.Get("/api/docs", h.ServeSwaggerUI)
	r.Get("/api/openapi.json", h.ServeOpenAPISpec)
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

func GetOpenAPISpec() ([]byte, error) {
	if len(openapiYAML) == 0 {
		return nil, nil
	}
	return openapiYAML, nil
}

func (h *Handler) ServeOpenAPISpec(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	// Create a copy of the spec to modify
	spec := make(map[string]interface{})
	for k, v := range h.spec {
		spec[k] = v
	}

	if h.baseURL != "" {
		scheme := "http"
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			scheme = "https"
		}
		host := r.Host

		servers := []map[string]interface{}{
			{
				"url":         scheme + "://" + host + h.baseURL,
				"description": "Current server with base URL",
			},
		}

		// Keep existing servers as fallback
		if existingServers, ok := spec["servers"].([]interface{}); ok {
			for _, s := range existingServers {
				if server, ok := s.(map[string]interface{}); ok {
					servers = append(servers, server)
				}
			}
		}

		spec["servers"] = servers
	}

	json.NewEncoder(w).Encode(spec)
}
