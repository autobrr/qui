// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// Handler manages reverse proxy requests to qBittorrent instances
type Handler struct {
	clientPool        *qbittorrent.ClientPool
	clientAPIKeyStore *models.ClientAPIKeyStore
	instanceStore     *models.InstanceStore
	bufferPool        *BufferPool
	proxy             *httputil.ReverseProxy
}

// NewHandler creates a new proxy handler
func NewHandler(clientPool *qbittorrent.ClientPool, clientAPIKeyStore *models.ClientAPIKeyStore, instanceStore *models.InstanceStore) *Handler {
	bufferPool := NewBufferPool()

	h := &Handler{
		clientPool:        clientPool,
		clientAPIKeyStore: clientAPIKeyStore,
		instanceStore:     instanceStore,
		bufferPool:        bufferPool,
	}

	// Configure the reverse proxy
	h.proxy = &httputil.ReverseProxy{
		Rewrite:      h.rewriteRequest,
		BufferPool:   bufferPool,
		ErrorHandler: h.errorHandler,
	}

	return h
}

// ServeHTTP handles the reverse proxy request
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}

// rewriteRequest modifies the outbound request to target the correct qBittorrent instance
func (h *Handler) rewriteRequest(pr *httputil.ProxyRequest) {
	ctx := pr.In.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	if instanceID == 0 || clientAPIKey == nil {
		log.Error().Msg("Missing instance ID or client API key in proxy request context")
		return
	}

	// Get the instance details to get the host
	instance, err := h.instanceStore.Get(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceId", instanceID).Msg("Failed to get instance")
		return
	}

	// Parse the instance host to get the target URL
	instanceURL, err := url.Parse(instance.Host)
	if err != nil {
		log.Error().Err(err).Str("host", instance.Host).Msg("Failed to parse instance host")
		return
	}

	// Get the authenticated qBittorrent client from the pool
	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceId", instanceID).Msg("Failed to get qBittorrent client from pool")
		return
	}

	// Get the HTTP client with cookie jar from the authenticated qBittorrent client
	// TODO: When go-qbittorrent merges GetHTTPClient, change this to: client.Client.GetHTTPClient()
	httpClient := client.GetHTTPClient()
	if httpClient != nil && httpClient.Jar != nil {
		// Get cookies for the target URL from the cookie jar
		cookies := httpClient.Jar.Cookies(instanceURL)
		if len(cookies) > 0 {
			var cookiePairs []string
			for _, cookie := range cookies {
				cookiePairs = append(cookiePairs, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
			}
			pr.Out.Header.Set("Cookie", strings.Join(cookiePairs, "; "))
			log.Debug().Int("instanceId", instanceID).Int("cookieCount", len(cookies)).Msg("Added cookies from HTTP client jar to proxy request")
		} else {
			log.Debug().Int("instanceId", instanceID).Msg("No cookies found in HTTP client jar")
		}
	} else {
		log.Debug().Int("instanceId", instanceID).Msg("No HTTP client or cookie jar available")
	}

	// Strip the proxy prefix from the path
	apiKey := chi.URLParam(pr.In, "api-key")
	originalPath := pr.In.URL.Path
	strippedPath := h.stripProxyPrefix(originalPath, apiKey)

	log.Debug().
		Str("client", clientAPIKey.ClientName).
		Int("instanceId", instanceID).
		Str("originalPath", originalPath).
		Str("strippedPath", strippedPath).
		Str("targetHost", instanceURL.Host).
		Msg("Rewriting proxy request")

	// Set the target URL
	pr.SetURL(instanceURL)

	// Update the path to the stripped version
	pr.Out.URL.Path = strippedPath
	pr.Out.URL.RawPath = ""

	// Preserve query parameters
	pr.Out.URL.RawQuery = pr.In.URL.RawQuery

	// Set proper host header (important for qBittorrent)
	pr.Out.Host = instanceURL.Host

	// Add headers to identify the proxy
	pr.Out.Header.Set("X-Forwarded-For", pr.In.RemoteAddr)
	pr.Out.Header.Set("X-Forwarded-Proto", pr.In.URL.Scheme)
	pr.Out.Header.Set("X-Qui-Client", clientAPIKey.ClientName)
}

// stripProxyPrefix removes the proxy prefix from the URL path
func (h *Handler) stripProxyPrefix(path, apiKey string) string {
	prefix := "/proxy/" + apiKey
	if after, found := strings.CutPrefix(path, prefix); found {
		return after
	}
	return path
}

// errorHandler handles proxy errors
func (h *Handler) errorHandler(w http.ResponseWriter, r *http.Request, err error) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	clientName := "unknown"
	if clientAPIKey != nil {
		clientName = clientAPIKey.ClientName
	}

	log.Error().
		Err(err).
		Str("client", clientName).
		Int("instanceId", instanceID).
		Str("method", r.Method).
		Str("path", r.URL.Path).
		Msg("Proxy request failed")

	// Return a generic error to avoid leaking internal details
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)

	// Return qBittorrent-style error response
	errorResponse := `{"error":"Failed to connect to qBittorrent instance"}`
	w.Write([]byte(errorResponse))
}

// Routes sets up the proxy routes
func (h *Handler) Routes(r chi.Router) {
	// Proxy route with API key parameter
	r.Route("/proxy/{api-key}", func(r chi.Router) {
		// Apply client API key validation middleware
		r.Use(ClientAPIKeyMiddleware(h.clientAPIKeyStore))

		// Handle all requests under this prefix
		r.HandleFunc("/*", h.ServeHTTP)
	})
}
