// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/autobrr/pkg/sharedhttp"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/reannounce"
	"github.com/autobrr/qui/pkg/httphelpers"
	"github.com/autobrr/qui/pkg/redact"
)

// Handler manages reverse proxy requests to qBittorrent instances
type Handler struct {
	basePath          string
	clientPool        *qbittorrent.ClientPool
	clientAPIKeyStore *models.ClientAPIKeyStore
	instanceStore     *models.InstanceStore
	syncManager       *qbittorrent.SyncManager
	reannounceCache   *reannounce.SettingsCache
	reannounceService *reannounce.Service
	bufferPool        *BufferPool
	proxy             *httputil.ReverseProxy
}

const (
	proxyContextKey       contextKey = "proxy_request_context"
	clientHTTPSContextKey contextKey = "client_https"
	proxyErrorPayload     string     = `{"error":"Failed to connect to qBittorrent instance"}`
	proxyLoginCookieName  string     = "SID"
	proxyLoginSuccessBody string     = "Ok."
)

// missingProxyContextSampler throttles repeated missing-context warnings to avoid log floods.
var missingProxyContextSampler = &zerolog.BasicSampler{N: 100}

type basicAuthCredentials struct {
	username string
	password string
}

type proxyContext struct {
	instanceID  int
	instanceURL *url.URL
	httpClient  *http.Client
	basicAuth   *basicAuthCredentials
}

// NewHandler creates a new proxy handler
func NewHandler(clientPool *qbittorrent.ClientPool, clientAPIKeyStore *models.ClientAPIKeyStore, instanceStore *models.InstanceStore, syncManager *qbittorrent.SyncManager, cache *reannounce.SettingsCache, svc *reannounce.Service, baseURL string) *Handler {
	bufferPool := NewBufferPool()
	basePath := httphelpers.NormalizeBasePath(baseURL)

	h := &Handler{
		basePath:          basePath,
		clientPool:        clientPool,
		clientAPIKeyStore: clientAPIKeyStore,
		instanceStore:     instanceStore,
		syncManager:       syncManager,
		reannounceCache:   cache,
		reannounceService: svc,
		bufferPool:        bufferPool,
	}

	// Configure the reverse proxy with retry logic for transient network errors
	retryTransport := NewRetryTransportWithSelector(sharedhttp.Transport, func(req *http.Request) http.RoundTripper {
		proxyCtx, ok := getProxyContext(req.Context())
		if !ok || proxyCtx == nil || proxyCtx.httpClient == nil || proxyCtx.httpClient.Transport == nil {
			return nil
		}
		return proxyCtx.httpClient.Transport
	})
	h.proxy = &httputil.ReverseProxy{
		Rewrite:      h.rewriteRequest,
		BufferPool:   bufferPool,
		ErrorHandler: h.errorHandler,
		Transport:    retryTransport,
	}

	return h
}

// ServeHTTP handles the reverse proxy request (fallback for non-intercepted routes)
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	log.Debug().Msg("Forwarding to qBittorrent via reverse proxy")
	h.proxy.ServeHTTP(w, r)
}

// rewriteRequest modifies the outbound request to target the correct qBittorrent instance
func (h *Handler) rewriteRequest(pr *httputil.ProxyRequest) {
	ctx := pr.In.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)
	proxyCtx, ok := getProxyContext(ctx)

	if instanceID == 0 || clientAPIKey == nil || !ok || proxyCtx == nil {
		log.Error().Msg("Missing instance ID or client API key in proxy request context")
		return
	}

	instanceURL := proxyCtx.instanceURL
	if instanceURL == nil {
		log.Error().Int("instanceId", instanceID).Msg("Proxy context missing target URL")
		return
	}

	if proxyCtx.httpClient != nil && proxyCtx.httpClient.Jar != nil {
		// Get cookies for the target URL from the cookie jar
		cookies := proxyCtx.httpClient.Jar.Cookies(instanceURL)
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

	if proxyCtx.basicAuth != nil {
		pr.Out.SetBasicAuth(proxyCtx.basicAuth.username, proxyCtx.basicAuth.password)
	} else {
		pr.Out.Header.Del("Authorization")
	}

	// Strip the proxy prefix from the path
	apiKey := chi.URLParam(pr.In, "api-key")
	originalPath := pr.In.URL.Path
	strippedPath := h.stripProxyPrefix(originalPath, apiKey)

	log.Debug().
		Str("client", clientAPIKey.ClientName).
		Int("instanceId", instanceID).
		Str("strippedPath", redact.ProxyPath(strippedPath)).
		Str("targetHost", instanceURL.Host).
		Msg("Rewriting proxy request")

	// Set the target URL
	pr.SetURL(instanceURL)

	// Update the path, preserving any base path on the instance host
	targetPath := combineInstanceAndRequestPath(instanceURL.Path, strippedPath)
	pr.Out.URL.Path = targetPath
	pr.Out.URL.RawPath = targetPath

	// Preserve query parameters
	pr.Out.URL.RawQuery = pr.In.URL.RawQuery

	// Set proper host header (important for qBittorrent)
	pr.Out.Host = instanceURL.Host

	// Add headers to identify the proxy
	if prior := pr.In.Header["X-Forwarded-For"]; len(prior) > 0 {
		pr.Out.Header["X-Forwarded-For"] = append([]string(nil), prior...)
	}
	forwardedProto := pr.In.Header.Get("X-Forwarded-Proto")
	forwardedHost := pr.In.Header.Get("X-Forwarded-Host")
	if forwardedHost != "" {
		pr.Out.Header.Set("X-Forwarded-Host", forwardedHost)
	}
	pr.SetXForwarded()
	if forwardedProto != "" {
		pr.Out.Header.Set("X-Forwarded-Proto", forwardedProto)
	}
	if forwardedHost != "" {
		pr.Out.Header.Set("X-Forwarded-Host", forwardedHost)
	}
	pr.Out.Header.Set("X-Qui-Client", clientAPIKey.ClientName)
}

// stripProxyPrefix removes the proxy prefix from the URL path
func (h *Handler) stripProxyPrefix(path, apiKey string) string {
	prefix := httphelpers.JoinBasePath(h.basePath, "/proxy/"+apiKey)
	if after, found := strings.CutPrefix(path, prefix); found {
		return after
	}
	return path
}

func combineInstanceAndRequestPath(instanceBasePath, strippedPath string) string {
	base := strings.TrimSuffix(instanceBasePath, "/")
	request := strings.TrimPrefix(strippedPath, "/")

	switch {
	case base == "" && request == "":
		return "/"
	case base == "":
		return "/" + request
	case request == "":
		return base + "/"
	default:
		return base + "/" + request
	}
}

// bufferRequestBody reads the request body, restores it for ParseForm, and returns the buffered bytes.
// This allows both parsing form data and forwarding the request body to the proxy.
//
// IMPORTANT: Limits body size to 10MB to prevent OOM attacks. Larger requests will be rejected.
// This limit is appropriate for torrent operations which use small form data payloads.
func bufferRequestBody(r *http.Request) ([]byte, error) {
	const maxBodySize = 10 * 1024 * 1024 // 10MB limit

	// Use LimitReader to prevent reading more than maxBodySize
	// Reading exactly maxBodySize bytes is sufficient - we don't need to read extra
	limitedReader := io.LimitReader(r.Body, maxBodySize)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		r.Body.Close()
		return nil, err
	}

	// If we read exactly maxBodySize bytes, check if there's more data
	// This avoids reading unnecessary bytes for overflow detection
	if len(bodyBytes) == maxBodySize {
		// Try to read one more byte to see if body exceeds limit
		oneByte := make([]byte, 1)
		n, _ := r.Body.Read(oneByte)
		r.Body.Close()
		if n > 0 {
			// Body exceeds limit - use generic error message
			return nil, fmt.Errorf("request body exceeds maximum allowed size")
		}
	} else {
		r.Body.Close()
	}

	// Restore body for ParseForm or other consumers
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return bodyBytes, nil
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
		Str("error", redact.String(err.Error())).
		Str("client", clientName).
		Int("instanceId", instanceID).
		Str("method", r.Method).
		Msg("Proxy request failed")

	h.writeProxyError(w)
}

// handleSyncMainData proxies sync/maindata requests and updates local state from the response
func (h *Handler) handleSyncMainData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Msg("Proxying sync/maindata request")

	// Use a custom response writer to capture the response
	buf := h.bufferPool.Get()
	crwBody := buf[:0]
	crw := &capturingResponseWriter{
		ResponseWriter: w,
		body:           crwBody,
		statusCode:     http.StatusOK,
	}
	defer func() {
		if buf != nil {
			h.bufferPool.Put(buf[:cap(buf)])
		}
	}()

	// Proxy the request
	h.proxy.ServeHTTP(crw, r)

	// Only update local state for successful responses with body
	if crw.statusCode == http.StatusOK && len(crw.body) > 0 {
		var mainData qbt.MainData
		if err := json.Unmarshal(crw.body, &mainData); err != nil {
			log.Error().
				Err(err).
				Int("instanceId", instanceID).
				Msg("Failed to parse sync/maindata response")
			return
		}

		// Check if this is a full update by examining the response
		// A full update contains the FullUpdate field set to true, or has complete torrent data
		isFullUpdate := mainData.FullUpdate || (mainData.Rid == 0 && len(mainData.Torrents) > 0)

		if isFullUpdate {
			client, err := h.clientPool.GetClient(ctx, instanceID)
			if err != nil {
				log.Error().
					Err(err).
					Int("instanceId", instanceID).
					Msg("Failed to get client for maindata update")
				return
			}

			client.UpdateWithMainData(&mainData)
			log.Debug().
				Int("instanceId", instanceID).
				Int64("rid", mainData.Rid).
				Int("torrentCount", len(mainData.Torrents)).
				Bool("hasServerState", mainData.ServerState != (qbt.ServerState{})).
				Int("categoryCount", len(mainData.Categories)).
				Int("tagCount", len(mainData.Tags)).
				Msg("Updated local maindata from full sync/maindata response")
		} else {
			log.Debug().
				Int("instanceId", instanceID).
				Int64("rid", mainData.Rid).
				Msg("Skipping incremental sync/maindata update")
		}
	}
}

// capturingResponseWriter captures the response body while writing it to the client
type capturingResponseWriter struct {
	http.ResponseWriter
	body       []byte
	statusCode int
}

func (crw *capturingResponseWriter) WriteHeader(statusCode int) {
	crw.statusCode = statusCode
	crw.ResponseWriter.WriteHeader(statusCode)
}

func (crw *capturingResponseWriter) Write(b []byte) (int, error) {
	crw.body = append(crw.body, b...)
	return crw.ResponseWriter.Write(b)
}

// prepareProxyContextMiddleware adds proxy context to the request
func (h *Handler) prepareProxyContextMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		proxyCtx, err := h.prepareProxyContext(r)
		if err != nil {
			h.writeProxyError(w)
			return
		}

		ctx := context.WithValue(r.Context(), proxyContextKey, proxyCtx)
		if isHTTPSRequest(r) {
			ctx = context.WithValue(ctx, clientHTTPSContextKey, true)
		}

		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Routes sets up the proxy routes
func (h *Handler) Routes(r chi.Router) {
	// Proxy route with API key parameter
	proxyRoute := httphelpers.JoinBasePath(h.basePath, "/proxy/{api-key}")
	proxyRouter := r.With(ClientAPIKeyMiddleware(h.clientAPIKeyStore))

	// Scoped proxy routes retain API key middleware and prepare proxy context
	proxyRouter.Route(proxyRoute, func(pr chi.Router) {
		// Apply proxy context middleware (adds instance info to context)
		pr.Use(h.prepareProxyContextMiddleware)

		pr.Post("/api/v2/torrents/reannounce", h.handleReannounce)

		// Register intercepted endpoints (these use qui's sync manager or special handling)
		pr.Post("/api/v2/auth/login", h.handleAuthLogin)
		pr.Get("/api/v2/sync/maindata", h.handleSyncMainData)
		pr.Get("/api/v2/sync/torrentPeers", h.handleTorrentPeers)
		pr.Get("/api/v2/torrents/info", h.handleTorrentsInfo)
		pr.Get("/api/v2/torrents/categories", h.handleCategories)
		pr.Get("/api/v2/torrents/tags", h.handleTags)
		pr.Get("/api/v2/torrents/properties", h.handleTorrentProperties)
		pr.Get("/api/v2/torrents/trackers", h.handleTorrentTrackers)
		pr.Get("/api/v2/torrents/files", h.handleTorrentFiles)
		pr.Get("/api/v2/torrents/search", h.handleTorrentSearch)

		// Intercepted write operations for cache invalidation
		pr.Post("/api/v2/torrents/setLocation", h.handleSetLocation)
		pr.Post("/api/v2/torrents/renameFile", h.handleRenameFile)
		pr.Post("/api/v2/torrents/renameFolder", h.handleRenameFolder)
		pr.Post("/api/v2/torrents/delete", h.handleDeleteTorrents)

		// Handle the base proxy path and any nested paths requested through the proxy
		pr.HandleFunc("/", h.ServeHTTP)
		pr.HandleFunc("/*", h.ServeHTTP)
	})
}

func (h *Handler) prepareProxyContext(r *http.Request) (*proxyContext, error) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	logger := log.With().
		Int("instanceId", instanceID).
		Str("method", r.Method).
		Logger()

	if clientAPIKey != nil {
		logger = logger.With().Str("client", clientAPIKey.ClientName).Logger()
	}

	if instanceID == 0 || clientAPIKey == nil {
		sampled := logger.Sample(missingProxyContextSampler)
		sampled.Warn().Msg("Proxy request missing instance ID or client API key")
		return nil, fmt.Errorf("missing proxy context")
	}

	instance, err := h.instanceStore.Get(ctx, instanceID)
	if err != nil {
		if err == models.ErrInstanceNotFound {
			logger.Warn().Msg("Instance not found for proxy request")
		} else {
			logger.Error().Err(err).Msg("Failed to load instance for proxy request")
		}
		return nil, err
	}

	if !instance.IsActive {
		logger.Warn().Msg("Instance disabled, rejecting proxy request")
		return nil, qbittorrent.ErrInstanceDisabled
	}

	instanceURL, err := url.Parse(instance.Host)
	if err != nil {
		logger.Error().Err(err).Str("host", instance.Host).Msg("Failed to parse instance host for proxy request")
		return nil, err
	}

	client, err := h.clientPool.GetClient(ctx, instanceID)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to get qBittorrent client from pool for proxy request")
		return nil, err
	}

	var basicAuth *basicAuthCredentials
	if instance.BasicUsername != nil && *instance.BasicUsername != "" {
		password, err := h.instanceStore.GetDecryptedBasicPassword(instance)
		if err != nil {
			logger.Error().Err(err).Msg("Failed to decrypt basic auth password for proxy request")
			return nil, err
		}
		if password != nil {
			basicAuth = &basicAuthCredentials{
				username: *instance.BasicUsername,
				password: *password,
			}
		}
	}

	proxyCtx := &proxyContext{
		instanceID:  instanceID,
		instanceURL: instanceURL,
		httpClient:  client.GetHTTPClient(),
		basicAuth:   basicAuth,
	}

	return proxyCtx, nil
}

func getProxyContext(ctx context.Context) (*proxyContext, bool) {
	if ctx == nil {
		return nil, false
	}
	proxyCtx, ok := ctx.Value(proxyContextKey).(*proxyContext)
	return proxyCtx, ok
}

func (h *Handler) writeProxyError(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadGateway)
	_, _ = w.Write([]byte(proxyErrorPayload))
}

func (h *Handler) monitoringEnabled(instanceID int) bool {
	if h.reannounceCache == nil {
		return false
	}
	settings := h.reannounceCache.Get(instanceID)
	return settings != nil && settings.Enabled
}

func restoreBody(r *http.Request, body []byte) {
	if r == nil {
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	r.ContentLength = int64(len(body))
}

func normalizeHashes(hashes []string) []string {
	result := make([]string, 0, len(hashes))
	seen := make(map[string]struct{}, len(hashes))
	for _, hash := range hashes {
		trimmed := strings.ToUpper(strings.TrimSpace(hash))
		if trimmed == "" {
			continue
		}
		if _, ok := seen[trimmed]; ok {
			continue
		}
		seen[trimmed] = struct{}{}
		result = append(result, trimmed)
	}
	return result
}

func difference(all, subset []string) []string {
	if len(subset) == 0 {
		return append([]string{}, all...)
	}
	remaining := make([]string, 0, len(all))
	set := make(map[string]int, len(subset))
	for _, val := range subset {
		set[val]++
	}
	for _, val := range all {
		if count, ok := set[val]; ok && count > 0 {
			set[val] = count - 1
			continue
		}
		remaining = append(remaining, val)
	}
	return remaining
}

func generateLoginCookieValue() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func clientRequestIsHTTPS(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	if v, ok := ctx.Value(clientHTTPSContextKey).(bool); ok {
		return v
	}
	return false
}

func isHTTPSRequest(r *http.Request) bool {
	if r == nil {
		return false
	}

	if proto := strings.ToLower(strings.TrimSpace(r.Header.Get("X-Forwarded-Proto"))); proto != "" {
		if strings.HasPrefix(proto, "https") {
			return true
		}
	}

	if forwarded := strings.ToLower(r.Header.Get("Forwarded")); strings.Contains(forwarded, "proto=https") {
		return true
	}

	if r.TLS != nil {
		return true
	}

	if r.URL != nil && strings.EqualFold(r.URL.Scheme, "https") {
		return true
	}

	return false
}

// validateQueryParams checks if all query parameters are in the allowed list
// Returns true if validation passes, false if validation fails (and proxies upstream)
func (h *Handler) validateQueryParams(w http.ResponseWriter, r *http.Request, allowedParams map[string]struct{}, endpoint string) bool {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)
	queryParams := r.URL.Query()

	for key := range queryParams {
		if _, ok := allowedParams[strings.ToLower(key)]; !ok {
			log.Trace().
				Int("instanceId", instanceID).
				Str("client", clientAPIKey.ClientName).
				Str("endpoint", endpoint).
				Str("param", key).
				Str("value", queryParams.Get(key)).
				Msg("Unsupported query parameter, proxying upstream")
			h.ServeHTTP(w, r)
			return false
		}
	}
	return true
}

func parseCSVQueryValues(queryParams url.Values, keys ...string) []string {
	if len(keys) == 0 {
		return nil
	}

	seen := make(map[string]struct{})
	parsed := make([]string, 0)

	for _, key := range keys {
		values, exists := queryParams[key]
		if !exists {
			continue
		}

		for _, value := range values {
			for entry := range strings.SplitSeq(value, ",") {
				trimmed := strings.TrimSpace(entry)
				if trimmed == "" {
					continue
				}

				if _, ok := seen[trimmed]; ok {
					continue
				}

				seen[trimmed] = struct{}{}
				parsed = append(parsed, trimmed)
			}
		}
	}

	if len(parsed) == 0 {
		return nil
	}

	return parsed
}

func parseHashesQueryValues(queryParams url.Values) []string {
	hashValues, exists := queryParams["hashes"]
	if !exists {
		return nil
	}

	rawHashes := make([]string, 0, len(hashValues))
	for _, hashValue := range hashValues {
		for segment := range strings.SplitSeq(hashValue, "|") {
			for hashEntry := range strings.SplitSeq(segment, ",") {
				trimmed := strings.TrimSpace(hashEntry)
				if trimmed == "" || strings.EqualFold(trimmed, "all") {
					continue
				}

				rawHashes = append(rawHashes, trimmed)
			}
		}
	}

	if len(rawHashes) == 0 {
		return nil
	}

	return normalizeHashes(rawHashes)
}

func mergeUniqueStringSlices(valueSets ...[]string) []string {
	merged := make([]string, 0)
	seen := make(map[string]struct{})

	for _, values := range valueSets {
		for _, value := range values {
			if value == "" {
				continue
			}

			if _, ok := seen[value]; ok {
				continue
			}

			seen[value] = struct{}{}
			merged = append(merged, value)
		}
	}

	if len(merged) == 0 {
		return nil
	}

	return merged
}

func normalizeStatusFilters(statusValues []string) []string {
	normalized := make([]string, 0, len(statusValues))
	seen := make(map[string]struct{})

	for _, status := range statusValues {
		trimmed := strings.TrimSpace(status)
		if trimmed == "" {
			continue
		}

		lowered := strings.ToLower(trimmed)
		if _, ok := seen[lowered]; ok {
			continue
		}

		seen[lowered] = struct{}{}
		normalized = append(normalized, lowered)
	}

	if len(normalized) == 0 {
		return nil
	}

	return normalized
}

func splitStatusFilters(filterValues []string) (status []string, excludeStatus []string) {
	statusSeen := make(map[string]struct{})
	excludeSeen := make(map[string]struct{})

	for _, normalized := range normalizeStatusFilters(filterValues) {

		switch normalized {
		case "unregistered", "tracker_down":
			if _, ok := excludeSeen[normalized]; ok {
				continue
			}
			excludeSeen[normalized] = struct{}{}
			excludeStatus = append(excludeStatus, normalized)
		default:
			if _, ok := statusSeen[normalized]; ok {
				continue
			}
			statusSeen[normalized] = struct{}{}
			status = append(status, normalized)
		}
	}

	return status, excludeStatus
}

func buildTorrentSearchFilters(queryParams url.Values) qbittorrent.FilterOptions {
	legacyFilterValues := parseCSVQueryValues(queryParams, "filter")
	legacyStatusFilters, legacyExcludeStatusFilters := splitStatusFilters(legacyFilterValues)

	statusFilters := mergeUniqueStringSlices(
		normalizeStatusFilters(parseCSVQueryValues(queryParams, "status")),
		legacyStatusFilters,
	)
	excludeStatusFilters := mergeUniqueStringSlices(
		normalizeStatusFilters(parseCSVQueryValues(queryParams, "excludeStatus")),
		legacyExcludeStatusFilters,
	)

	filters := qbittorrent.FilterOptions{
		Hashes:            parseHashesQueryValues(queryParams),
		Status:            statusFilters,
		ExcludeStatus:     excludeStatusFilters,
		Categories:        parseCSVQueryValues(queryParams, "category", "categories"),
		ExcludeCategories: parseCSVQueryValues(queryParams, "excludeCategories"),
		Tags:              parseCSVQueryValues(queryParams, "tag", "tags"),
		ExcludeTags:       parseCSVQueryValues(queryParams, "excludeTags"),
		Trackers:          parseCSVQueryValues(queryParams, "trackers"),
		ExcludeTrackers:   parseCSVQueryValues(queryParams, "excludeTrackers"),
		Expr:              strings.TrimSpace(queryParams.Get("expr")),
	}

	return filters
}

// handleTorrentsInfo handles /api/v2/torrents/info using qui's sync manager
func (h *Handler) handleTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	ctx := qbittorrent.WithSkipTrackerHydration(r.Context())
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Extract standard qBittorrent API parameters (no advanced filtering)
	allowedParams := map[string]struct{}{
		"filter":   {},
		"category": {},
		"tag":      {},
		"sort":     {},
		"reverse":  {},
		"limit":    {},
		"offset":   {},
		"hashes":   {},
	}
	if !h.validateQueryParams(w, r, allowedParams, "torrents/info") {
		return
	}

	queryParams := r.URL.Query()
	filterValues := parseCSVQueryValues(queryParams, "filter")
	categoryValues := parseCSVQueryValues(queryParams, "category")
	tagValues := parseCSVQueryValues(queryParams, "tag")
	statusFilters, excludeStatusFilters := splitStatusFilters(filterValues)
	sort := queryParams.Get("sort")
	reverse := queryParams.Get("reverse") == "true"
	limit := 0
	offset := 0

	if l := queryParams.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := queryParams.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	hashesParam := queryParams.Get("hashes")
	var hashes []string
	var uniqueHashCount int
	if hashesParam != "" && !strings.EqualFold(hashesParam, "all") {
		hashSet := make(map[string]struct{})
		for rawHash := range strings.SplitSeq(hashesParam, "|") {
			trimmed := strings.TrimSpace(rawHash)
			if trimmed == "" {
				continue
			}

			normalized := strings.ToUpper(trimmed)
			if _, exists := hashSet[normalized]; exists {
				continue
			}

			hashSet[normalized] = struct{}{}
			hashes = append(hashes, normalized)
		}
		uniqueHashCount = len(hashSet)
	}

	// Build basic filter options (standard qBittorrent parameters only)
	filters := qbittorrent.FilterOptions{}

	if len(hashes) > 0 {
		filters.Hashes = hashes
	}

	if len(statusFilters) > 0 {
		filters.Status = statusFilters
	}

	if len(excludeStatusFilters) > 0 {
		filters.ExcludeStatus = excludeStatusFilters
	}

	if len(categoryValues) > 0 {
		filters.Categories = categoryValues
	}

	if len(tagValues) > 0 {
		filters.Tags = tagValues
	}

	// Default sort order
	if sort == "" {
		sort = "added_on"
	}
	order := "asc"
	if reverse {
		order = "desc"
	}

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Strs("filter", filterValues).
		Strs("category", categoryValues).
		Strs("tag", tagValues).
		Str("sort", sort).
		Str("order", order).
		Int("limit", limit).
		Int("offset", offset).
		Int("hashCount", uniqueHashCount).
		Msg("Handling torrents/info request via qui sync manager")

	// Use qui's sync manager
	response, err := h.syncManager.GetTorrentsWithFilters(ctx, instanceID, limit, offset, sort, order, "", filters)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to get torrents from sync manager")
		h.writeProxyError(w)
		return
	}

	// Convert qui's TorrentView format back to qBittorrent's Torrent format
	// The TorrentView embeds qbt.Torrent, so we can extract it
	torrents := make([]any, len(response.Torrents))
	for i, tv := range response.Torrents {
		torrents[i] = tv.Torrent
	}

	// Return as JSON array (qBittorrent API format)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(torrents); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode torrents response")
	}
}

// handleTorrentSearch handles torrents/search requests using qui's sync manager with advanced filtering
func (h *Handler) handleTorrentSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Extract qBittorrent API parameters
	allowedParams := map[string]struct{}{
		"search":            {},
		"filter":            {},
		"status":            {},
		"excludestatus":     {},
		"category":          {},
		"categories":        {},
		"excludecategories": {},
		"tag":               {},
		"tags":              {},
		"excludetags":       {},
		"trackers":          {},
		"excludetrackers":   {},
		"expr":              {},
		"hashes":            {},
		"sort":              {},
		"reverse":           {},
		"limit":             {},
		"offset":            {},
	}
	if !h.validateQueryParams(w, r, allowedParams, "torrents/search") {
		return
	}

	queryParams := r.URL.Query()
	search := queryParams.Get("search")
	filters := buildTorrentSearchFilters(queryParams)
	sort := queryParams.Get("sort")
	reverse := queryParams.Get("reverse") == "true"
	limit := 0
	offset := 0

	if l := queryParams.Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 {
			limit = parsed
		}
	}

	if o := queryParams.Get("offset"); o != "" {
		if parsed, err := strconv.Atoi(o); err == nil && parsed >= 0 {
			offset = parsed
		}
	}

	// Default sort order
	if sort == "" {
		sort = "added_on"
	}
	order := "asc"
	if reverse {
		order = "desc"
	}

	// If no limit specified, use a reasonable default
	if limit == 0 {
		limit = 100000 // Large limit to get all results
	}

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("search", search).
		Strs("status", filters.Status).
		Strs("excludeStatus", filters.ExcludeStatus).
		Strs("categories", filters.Categories).
		Strs("excludeCategories", filters.ExcludeCategories).
		Strs("tags", filters.Tags).
		Strs("excludeTags", filters.ExcludeTags).
		Strs("trackers", filters.Trackers).
		Strs("excludeTrackers", filters.ExcludeTrackers).
		Str("expr", filters.Expr).
		Str("sort", sort).
		Str("order", order).
		Int("limit", limit).
		Int("offset", offset).
		Msg("Handling torrents/qui request with qui sync manager")

	// Use qui's sync manager to get filtered torrents
	response, err := h.syncManager.GetTorrentsWithFilters(ctx, instanceID, limit, offset, sort, order, search, filters)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Str("search", search).
			Msg("Failed to get torrents with qui filters")
		h.writeProxyError(w)
		return
	}

	// Return full qui response with metadata
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(response); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode qui response")
	}
}

// handleCategories handles /api/v2/torrents/categories requests
func (h *Handler) handleCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Categories endpoint doesn't accept query parameters
	if !h.validateQueryParams(w, r, map[string]struct{}{}, "torrents/categories") {
		return
	}

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Msg("Handling categories request via qui sync manager")

	categories, err := h.syncManager.GetCategories(ctx, instanceID)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to get categories")
		h.writeProxyError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(categories); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode categories response")
	}
}

// handleTags handles /api/v2/torrents/tags requests
func (h *Handler) handleTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Tags endpoint doesn't accept query parameters
	if !h.validateQueryParams(w, r, map[string]struct{}{}, "torrents/tags") {
		return
	}

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Msg("Handling tags request via qui sync manager")

	tags, err := h.syncManager.GetTags(ctx, instanceID)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to get tags")
		h.writeProxyError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(tags); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode tags response")
	}
}

// handleTorrentProperties handles /api/v2/torrents/properties requests
func (h *Handler) handleTorrentProperties(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	allowedParams := map[string]struct{}{
		"hash": {},
	}
	if !h.validateQueryParams(w, r, allowedParams, "torrents/properties") {
		return
	}

	hash := r.URL.Query().Get("hash")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hash", hash).
		Msg("Handling torrent properties request via qui sync manager")

	properties, err := h.syncManager.GetTorrentProperties(ctx, instanceID, hash)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Str("hash", hash).
			Msg("Failed to get torrent properties")
		h.writeProxyError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(properties); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode properties response")
	}
}

// handleTorrentTrackers handles /api/v2/torrents/trackers requests
func (h *Handler) handleTorrentTrackers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	allowedParams := map[string]struct{}{
		"hash": {},
	}
	if !h.validateQueryParams(w, r, allowedParams, "torrents/trackers") {
		return
	}

	hash := r.URL.Query().Get("hash")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hash", hash).
		Msg("Handling torrent trackers request via qui sync manager")

	trackers, err := h.syncManager.GetTorrentTrackers(ctx, instanceID, hash)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Str("hash", hash).
			Msg("Failed to get torrent trackers")
		h.writeProxyError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(trackers); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode trackers response")
	}
}

// handleTorrentPeers handles /api/v2/sync/torrentPeers requests
func (h *Handler) handleTorrentPeers(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	hash := r.URL.Query().Get("hash")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hash", hash).
		Msg("Proxying sync/torrentPeers request")

	// Use a custom response writer to capture the response
	buf := h.bufferPool.Get()
	crwBody := buf[:0]
	crw := &capturingResponseWriter{
		ResponseWriter: w,
		body:           crwBody,
		statusCode:     http.StatusOK,
	}
	defer func() {
		if buf != nil {
			h.bufferPool.Put(buf[:cap(buf)])
		}
	}()

	// Proxy the request
	h.proxy.ServeHTTP(crw, r)

	// Update local peer sync manager state for successful responses
	if crw.statusCode == http.StatusOK && len(crw.body) > 0 {
		var peersData qbt.TorrentPeersResponse
		if err := json.Unmarshal(crw.body, &peersData); err != nil {
			log.Error().
				Err(err).
				Int("instanceId", instanceID).
				Str("hash", hash).
				Msg("Failed to parse sync/torrentPeers response")
			return
		}

		// Check if this is a full update (FullUpdate field or rid == 0)
		isFullUpdate := peersData.FullUpdate || peersData.Rid == 0

		if isFullUpdate {
			client, err := h.clientPool.GetClient(ctx, instanceID)
			if err != nil {
				log.Error().
					Err(err).
					Int("instanceId", instanceID).
					Msg("Failed to get client for peer state update")
				return
			}

			// Update peer state using the same pattern as maindata
			client.UpdateWithPeersData(hash, &peersData)

			log.Debug().
				Int("instanceId", instanceID).
				Str("hash", hash).
				Int64("rid", peersData.Rid).
				Int("peerCount", len(peersData.Peers)).
				Msg("Updated local peer state from full sync/torrentPeers response")
		} else {
			log.Debug().
				Int("instanceId", instanceID).
				Str("hash", hash).
				Int64("rid", peersData.Rid).
				Msg("Skipping incremental sync/torrentPeers update")
		}
	}
}

// handleTorrentFiles handles /api/v2/torrents/files requests
func (h *Handler) handleTorrentFiles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	allowedParams := map[string]struct{}{
		"hash":    {},
		"indexes": {},
	}
	if !h.validateQueryParams(w, r, allowedParams, "torrents/files") {
		return
	}

	hash := r.URL.Query().Get("hash")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hash", hash).
		Msg("Handling torrent files request via qui sync manager")

	files, err := h.syncManager.GetTorrentFiles(ctx, instanceID, hash)
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Str("hash", hash).
			Msg("Failed to get torrent files")
		h.writeProxyError(w)
		return
	}
	if files == nil {
		log.Warn().
			Int("instanceId", instanceID).
			Str("hash", hash).
			Msg("Torrent files not found")
		h.writeProxyError(w)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	encoder := json.NewEncoder(w)
	if err := encoder.Encode(files); err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to encode files response")
	}
}

// handleAuthLogin handles /api/v2/auth/login requests (ceremonial - proxy already authenticated)
func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Msg("Handling ceremonial auth/login request")

	// Check if instance is healthy using the client pool's health tracking
	if !h.clientPool.IsHealthy(instanceID) {
		log.Warn().
			Int("instanceId", instanceID).
			Msg("Instance unhealthy for auth/login")
		h.writeProxyError(w)
		return
	}

	// Instance is healthy, generate and set cookie
	cookieValue, err := generateLoginCookieValue()
	if err != nil {
		log.Warn().
			Err(err).
			Str("client", clientAPIKey.ClientName).
			Int("instanceId", instanceID).
			Msg("Falling back to timestamp-based proxy login cookie value")
		cookieValue = fmt.Sprintf("fallback-%d", time.Now().UnixNano())
	}

	cookie := &http.Cookie{
		Name:     proxyLoginCookieName,
		Value:    cookieValue,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	if clientRequestIsHTTPS(ctx) {
		cookie.Secure = true
	}

	http.SetCookie(w, cookie)

	log.Debug().
		Str("client", clientAPIKey.ClientName).
		Int("instanceId", instanceID).
		Str("cookieName", cookie.Name).
		Msg("Set ceremonial login cookie for client")

	// Return success response
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(proxyLoginSuccessBody))
}

// handleSetLocation handles /api/v2/torrents/setLocation and invalidates file cache
func (h *Handler) handleSetLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Buffer body so we can parse form and still forward to proxy
	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read setLocation body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to parse setLocation form")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	hashes := r.Form.Get("hashes")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hashes", hashes).
		Msg("Intercepting setLocation request for cache invalidation")

	// Defer cache invalidation to ensure it happens even if proxy panics
	// Use background context with timeout to avoid cancellation issues
	defer func() {
		// Capture panic value if any
		panicValue := recover()
		if panicValue != nil {
			log.Error().Interface("panic", panicValue).Int("instanceId", instanceID).
				Msg("Recovered from panic during setLocation cache invalidation")
		}

		if hashes != "" {
			invalidateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			hashList := strings.Split(hashes, "|")
			failedInvalidations := 0
			for _, hash := range hashList {
				hash = strings.TrimSpace(hash)
				if hash == "" {
					continue
				}
				if err := h.syncManager.InvalidateFileCache(invalidateCtx, instanceID, hash); err != nil {
					failedInvalidations++
					log.Warn().Err(err).Int("instanceId", instanceID).Str("hash", hash).
						Msg("Failed to invalidate file cache after setLocation")
				}
			}
			// Log summary if any invalidations failed
			if failedInvalidations > 0 {
				log.Warn().Int("instanceId", instanceID).Int("failed", failedInvalidations).Int("total", len(hashList)).
					Msg("Some file cache invalidations failed after setLocation - cache may be stale")
			}
		}

		// Re-panic with captured value to preserve stack trace
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	// Restore body for proxy
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Forward to qBittorrent
	h.proxy.ServeHTTP(w, r)
}

func (h *Handler) handleReannounce(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	if h.reannounceService == nil || !h.monitoringEnabled(instanceID) {
		h.proxy.ServeHTTP(w, r)
		return
	}

	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read reannounce body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	if err := r.ParseForm(); err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to parse reannounce form")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	raw := r.Form.Get("hashes")
	normalized := normalizeHashes(strings.Split(raw, "|"))
	if len(normalized) == 0 {
		restoreBody(r, bodyBytes)
		h.proxy.ServeHTTP(w, r)
		return
	}

	handled := h.reannounceService.RequestReannounce(ctx, instanceID, normalized)
	if len(handled) == 0 {
		restoreBody(r, bodyBytes)
		h.proxy.ServeHTTP(w, r)
		return
	}

	remaining := difference(normalized, handled)
	if len(remaining) > 0 {
		form := url.Values{}
		form.Set("hashes", strings.Join(remaining, "|"))
		encoded := form.Encode()
		r.Body = io.NopCloser(strings.NewReader(encoded))
		r.ContentLength = int64(len(encoded))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		h.proxy.ServeHTTP(w, r)
		return
	}

	log.Debug().Int("instanceId", instanceID).Str("client", clientAPIKey.ClientName).Int("handled", len(handled)).Msg("Intercepted reannounce request")
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(proxyLoginSuccessBody))
}

// handleRenameFile handles /api/v2/torrents/renameFile and invalidates file cache
func (h *Handler) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Buffer body so we can parse form and still forward to proxy
	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read renameFile body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to parse renameFile form")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	hash := r.Form.Get("hash")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hash", hash).
		Msg("Intercepting renameFile request for cache invalidation")

	// Defer cache invalidation to ensure it happens even if proxy panics
	defer func() {
		// Capture panic value if any
		panicValue := recover()
		if panicValue != nil {
			log.Error().Interface("panic", panicValue).Int("instanceId", instanceID).
				Msg("Recovered from panic during renameFile cache invalidation")
		}

		if hash != "" {
			invalidateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := h.syncManager.InvalidateFileCache(invalidateCtx, instanceID, hash); err != nil {
				log.Error().Err(err).Int("instanceId", instanceID).Str("hash", hash).
					Msg("CRITICAL: Failed to invalidate file cache after renameFile - cache is now stale")
			}
		}

		// Re-panic with captured value to preserve stack trace
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	// Restore body for proxy
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Forward to qBittorrent
	h.proxy.ServeHTTP(w, r)
}

// handleRenameFolder handles /api/v2/torrents/renameFolder and invalidates file cache
func (h *Handler) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Buffer body so we can parse form and still forward to proxy
	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read renameFolder body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to parse renameFolder form")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	hash := r.Form.Get("hash")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hash", hash).
		Msg("Intercepting renameFolder request for cache invalidation")

	// Defer cache invalidation to ensure it happens even if proxy panics
	defer func() {
		// Capture panic value if any
		panicValue := recover()
		if panicValue != nil {
			log.Error().Interface("panic", panicValue).Int("instanceId", instanceID).
				Msg("Recovered from panic during renameFolder cache invalidation")
		}

		if hash != "" {
			invalidateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			if err := h.syncManager.InvalidateFileCache(invalidateCtx, instanceID, hash); err != nil {
				log.Error().Err(err).Int("instanceId", instanceID).Str("hash", hash).
					Msg("CRITICAL: Failed to invalidate file cache after renameFolder - cache is now stale")
			}
		}

		// Re-panic with captured value to preserve stack trace
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	// Restore body for proxy
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Forward to qBittorrent
	h.proxy.ServeHTTP(w, r)
}

// handleDeleteTorrents handles /api/v2/torrents/delete and invalidates file cache
func (h *Handler) handleDeleteTorrents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	// Buffer body so we can parse form and still forward to proxy
	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read delete body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	// Parse form data
	if err := r.ParseForm(); err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to parse delete form")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	hashes := r.Form.Get("hashes")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hashes", hashes).
		Msg("Intercepting delete request for cache invalidation")

	// Defer cache invalidation to ensure it happens even if proxy panics
	defer func() {
		// Capture panic value if any
		panicValue := recover()
		if panicValue != nil {
			log.Error().Interface("panic", panicValue).Int("instanceId", instanceID).
				Msg("Recovered from panic during delete cache invalidation")
		}

		if hashes != "" {
			invalidateCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()

			hashList := strings.Split(hashes, "|")
			failedInvalidations := 0
			for _, hash := range hashList {
				hash = strings.TrimSpace(hash)
				if hash == "" {
					continue
				}
				if err := h.syncManager.InvalidateFileCache(invalidateCtx, instanceID, hash); err != nil {
					failedInvalidations++
					log.Warn().Err(err).Int("instanceId", instanceID).Str("hash", hash).
						Msg("Failed to invalidate file cache after delete")
				}
			}
			// Log summary if any invalidations failed
			if failedInvalidations > 0 {
				log.Warn().Int("instanceId", instanceID).Int("failed", failedInvalidations).Int("total", len(hashList)).
					Msg("Some file cache invalidations failed after delete - cache may be stale")
			}
		}

		// Re-panic with captured value to preserve stack trace
		if panicValue != nil {
			panic(panicValue)
		}
	}()

	// Restore body for proxy
	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	// Forward to qBittorrent
	h.proxy.ServeHTTP(w, r)
}
