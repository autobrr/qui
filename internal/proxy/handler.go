// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/autobrr/autobrr/pkg/sharedhttp"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog"
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
		Rewrite:        h.rewriteRequest,
		ModifyResponse: h.modifyResponse,
		BufferPool:     bufferPool,
		ErrorHandler:   h.errorHandler,
		Transport:      sharedhttp.Transport,
	}

	return h
}

// ServeHTTP handles the reverse proxy request
func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	proxyCtx, err := h.prepareProxyContext(r)
	if err != nil {
		h.writeProxyError(w)
		return
	}

	ctx := context.WithValue(r.Context(), proxyContextKey, proxyCtx)
	if isHTTPSRequest(r) {
		ctx = context.WithValue(ctx, clientHTTPSContextKey, true)
	}

	h.proxy.ServeHTTP(w, r.WithContext(ctx))
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
		Str("originalPath", originalPath).
		Str("strippedPath", strippedPath).
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

func (h *Handler) modifyResponse(resp *http.Response) error {
	if resp == nil || resp.Request == nil || resp.Request.URL == nil {
		return nil
	}

	if resp.StatusCode != http.StatusOK {
		return nil
	}

	if !strings.HasSuffix(resp.Request.URL.Path, "/auth/login") {
		return nil
	}

	ctx := resp.Request.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)
	if clientAPIKey == nil {
		return nil
	}

	if len(resp.Header.Values("Set-Cookie")) > 0 {
		return nil
	}

	bodyBytes, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	resp.ContentLength = int64(len(bodyBytes))
	if len(bodyBytes) > 0 {
		resp.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
	} else {
		resp.Header.Del("Content-Length")
	}
	if readErr != nil {
		log.Error().
			Err(readErr).
			Str("client", clientAPIKey.ClientName).
			Int("instanceId", instanceID).
			Msg("Failed to read complete proxy login response body")
		return nil
	}

	if strings.TrimSpace(string(bodyBytes)) != proxyLoginSuccessBody {
		return nil
	}

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

	resp.Header.Add("Set-Cookie", cookie.String())

	log.Debug().
		Str("client", clientAPIKey.ClientName).
		Int("instanceId", instanceID).
		Str("cookieName", cookie.Name).
		Msg("Injected proxy login cookie for client request")

	return nil
}

// stripProxyPrefix removes the proxy prefix from the URL path
func (h *Handler) stripProxyPrefix(path, apiKey string) string {
	prefix := "/proxy/" + apiKey
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

	h.writeProxyError(w)
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

func (h *Handler) prepareProxyContext(r *http.Request) (*proxyContext, error) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	logger := log.With().
		Int("instanceId", instanceID).
		Str("method", r.Method).
		Str("path", r.URL.Path).
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
