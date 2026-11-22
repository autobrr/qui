// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package proxy

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	mediainfo "github.com/autobrr/go-mediainfo"
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

type Handler struct {
	basePath          string
	clientPool        *qbittorrent.ClientPool
	clientAPIKeyStore *models.ClientAPIKeyStore
	instanceStore     *models.InstanceStore
	syncManager       *qbittorrent.SyncManager
	writeNoter        writeNoter
	deleteNoter       deleteNoter
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

var missingProxyContextSampler = &zerolog.BasicSampler{N: 100}

type basicAuthCredentials struct {
	username string
	password string
}

type writeNoter interface {
	NoteWrite(instanceID int)
}

// deleteNoter learns who deletes which torrents through the proxy, before
// qBittorrent gets the delete.
type deleteNoter interface {
	NoteProxiedDelete(ctx context.Context, instanceID int, hashes []string, keyName string)
}

type sessionRefresher interface {
	LoginCtx(context.Context) error
}

type proxyContext struct {
	instanceID  int
	instanceURL *url.URL
	httpClient  *http.Client
	basicAuth   *basicAuthCredentials
	apiKey      string
	session     sessionRefresher
}

type proxyContentPathMediaInfoRequest struct {
	ContentPath string `json:"contentPath"`
}

func (r *proxyContentPathMediaInfoRequest) UnmarshalJSON(data []byte) error {
	type proxyContentPathMediaInfoRequestAlias struct {
		ContentPath       string `json:"contentPath"`
		LegacyContentPath string `json:"content_path"`
	}

	var decoded proxyContentPathMediaInfoRequestAlias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}

	r.ContentPath = decoded.ContentPath
	if strings.TrimSpace(r.ContentPath) == "" {
		r.ContentPath = decoded.LegacyContentPath
	}

	return nil
}

type proxyContentPathMediaInfoResponse struct {
	ContentPath   string          `json:"contentPath"`
	SummaryTxt    string          `json:"summaryTxt"`
	MediaInfoJSON json.RawMessage `json:"mediaInfoJson"`
}

func NewHandler(clientPool *qbittorrent.ClientPool, clientAPIKeyStore *models.ClientAPIKeyStore, instanceStore *models.InstanceStore, syncManager *qbittorrent.SyncManager, cache *reannounce.SettingsCache, svc *reannounce.Service, baseURL string) *Handler {
	bufferPool := NewBufferPool()
	basePath := httphelpers.NormalizeBasePath(baseURL)

	h := &Handler{
		basePath:          basePath,
		clientPool:        clientPool,
		clientAPIKeyStore: clientAPIKeyStore,
		instanceStore:     instanceStore,
		syncManager:       syncManager,
		writeNoter:        syncManager,
		deleteNoter:       syncManager,
		reannounceCache:   cache,
		reannounceService: svc,
		bufferPool:        bufferPool,
	}

	// Fallback when an instance has no client transport. Tuned from autobrr's sharedhttp.Transport.
	baseTransport := http.DefaultTransport.(*http.Transport).Clone()
	baseTransport.MaxIdleConnsPerHost = 10
	baseTransport.ResponseHeaderTimeout = 120 * time.Second
	baseTransport.ReadBufferSize = 64 << 10
	baseTransport.WriteBufferSize = 64 << 10

	retryTransport := NewRetryTransportWithSelector(baseTransport, func(req *http.Request) http.RoundTripper {
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

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.proxy.ServeHTTP(w, r)
}

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

	proxyCtx.applyAuthHeaders(pr.Out)

	// qBittorrent's CSRF check rejects a Referer or Origin that does not match its own host
	// with 401. The values come from the calling app, not the instance, so drop them.
	pr.Out.Header.Del("Referer")
	pr.Out.Header.Del("Origin")

	apiKey := chi.URLParam(pr.In, "api-key")
	originalPath := pr.In.URL.Path
	strippedPath := h.stripProxyPrefix(originalPath, apiKey)

	log.Trace().
		Str("client", clientAPIKey.ClientName).
		Int("instanceId", instanceID).
		Str("strippedPath", redact.ProxyPath(strippedPath)).
		Str("targetHost", instanceURL.Host).
		Msg("Rewriting proxy request")

	pr.SetURL(instanceURL)

	targetPath := combineInstanceAndRequestPath(instanceURL.Path, strippedPath)
	pr.Out.URL.Path = targetPath
	pr.Out.URL.RawPath = targetPath

	pr.Out.URL.RawQuery = pr.In.URL.RawQuery

	pr.Out.Host = instanceURL.Host

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
	const maxBodySize = 10 * 1024 * 1024

	limitedReader := io.LimitReader(r.Body, maxBodySize)
	bodyBytes, err := io.ReadAll(limitedReader)
	if err != nil {
		r.Body.Close()
		return nil, err
	}

	if len(bodyBytes) == maxBodySize {
		oneByte := make([]byte, 1)
		n, _ := r.Body.Read(oneByte)
		r.Body.Close()
		if n > 0 {
			return nil, errors.New("request body exceeds maximum allowed size")
		}
	} else {
		r.Body.Close()
	}

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	return bodyBytes, nil
}

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

func (h *Handler) handleSyncMainData(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	log.Trace().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Msg("Proxying sync/maindata request")

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

	h.proxy.ServeHTTP(crw, r)

	if crw.statusCode == http.StatusOK && len(crw.body) > 0 {
		var mainData qbt.MainData
		if err := json.Unmarshal(crw.body, &mainData); err != nil {
			log.Error().
				Err(err).
				Int("instanceId", instanceID).
				Msg("Failed to parse sync/maindata response")
			return
		}

		isFullUpdate := mainData.FullUpdate || (mainData.Rid == 0 && len(mainData.Torrents) > 0)

		if isFullUpdate {
			h.syncManager.HintMainDataRefresh(instanceID, "proxy_sync_maindata")
			log.Trace().
				Int("instanceId", instanceID).
				Int64("rid", mainData.Rid).
				Int("torrentCount", len(mainData.Torrents)).
				Bool("hasServerState", mainData.ServerState != (qbt.ServerState{})).
				Int("categoryCount", len(mainData.Categories)).
				Int("tagCount", len(mainData.Tags)).
				Msg("Queued maindata refresh hint from full sync/maindata response")
		} else {
			log.Trace().
				Int("instanceId", instanceID).
				Int64("rid", mainData.Rid).
				Msg("Skipping incremental sync/maindata update")
		}
	}
}

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

func (h *Handler) noteTorrentWrites(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
		path := h.stripProxyPrefix(r.URL.Path, chi.URLParam(r, "api-key"))
		if r.Method == http.MethodPost && strings.HasPrefix(path, "/api/v2/torrents/") {
			h.writeNoter.NoteWrite(GetInstanceIDFromContext(r.Context()))
		}
	})
}

func (h *Handler) Routes(r chi.Router) {
	proxyRoute := httphelpers.JoinBasePath(h.basePath, "/proxy/{api-key}")
	proxyRouter := r.With(ClientAPIKeyMiddleware(h.clientAPIKeyStore))

	proxyRouter.Route(proxyRoute, func(pr chi.Router) {
		pr.Use(h.prepareProxyContextMiddleware, h.noteTorrentWrites)

		pr.Post("/api/v2/torrents/reannounce", h.handleReannounce)

		pr.Post("/api/v2/auth/login", h.handleAuthLogin)
		pr.Get("/api/v2/sync/maindata", h.handleSyncMainData)
		pr.Get("/api/v2/torrents/info", h.handleTorrentsInfo)
		pr.Get("/api/v2/torrents/categories", h.handleCategories)
		pr.Get("/api/v2/torrents/tags", h.handleTags)
		pr.Get("/api/v2/torrents/properties", h.handleTorrentProperties)
		pr.Get("/api/v2/torrents/trackers", h.handleTorrentTrackers)
		pr.Get("/api/v2/torrents/files", h.handleTorrentFiles)
		pr.Get("/api/v2/torrents/search", h.handleTorrentSearch)
		pr.Get("/api/v2/torrents/mediainfo", h.handleTorrentMediaInfo)

		pr.Post("/api/v2/torrents/setLocation", h.handleSetLocation)
		pr.Post("/api/v2/torrents/renameFile", h.handleRenameFile)
		pr.Post("/api/v2/torrents/renameFolder", h.handleRenameFolder)
		pr.Post("/api/v2/torrents/delete", h.handleDeleteTorrents)

		// Intercepted only to clean up the folders a move leaves behind
		pr.Post("/api/v2/torrents/setCategory", h.handleSetCategory)
		pr.Post("/api/v2/torrents/setAutoManagement", h.handleSetAutoManagement)
		pr.Post("/api/v2/torrents/editCategory", h.handleEditCategory)
		pr.Post("/api/v2/torrents/setSavePath", h.handleSetSavePath)
		pr.Post("/api/v2/torrents/setDownloadPath", h.handleSetDownloadPath)

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
		return nil, errors.New("missing proxy context")
	}

	instance, err := h.instanceStore.Get(ctx, instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
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

	apiKey, err := h.instanceStore.GetDecryptedAPIKey(instance)
	if err != nil {
		logger.Error().Err(err).Msg("Failed to decrypt API key for proxy request")
		return nil, err
	}

	proxyCtx := &proxyContext{
		instanceID:  instanceID,
		instanceURL: instanceURL,
		httpClient:  client.GetHTTPClient(),
		basicAuth:   basicAuth,
		apiKey:      apiKey,
		session:     client,
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

func (pc *proxyContext) applyAuthHeaders(req *http.Request) {
	if pc == nil || req == nil {
		return
	}

	if pc.httpClient != nil && pc.httpClient.Jar != nil && pc.instanceURL != nil {
		cookies := pc.httpClient.Jar.Cookies(pc.instanceURL)
		if len(cookies) > 0 {
			var cookiePairs []string
			for _, cookie := range cookies {
				cookiePairs = append(cookiePairs, fmt.Sprintf("%s=%s", cookie.Name, cookie.Value))
			}
			req.Header.Set("Cookie", strings.Join(cookiePairs, "; "))
		} else {
			req.Header.Del("Cookie")
		}
	}

	if pc.basicAuth != nil {
		req.SetBasicAuth(pc.basicAuth.username, pc.basicAuth.password)
	} else {
		req.Header.Del("Authorization")
	}

	if pc.apiKey != "" {
		req.Header.Set("Authorization", "Bearer "+pc.apiKey)
	}
}

func (pc *proxyContext) refreshSession(ctx context.Context) error {
	if pc == nil || pc.session == nil {
		return errors.New("proxy session refresh unavailable")
	}
	return pc.session.LoginCtx(ctx)
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
	return h.reannounceCache.Get(instanceID).CanMatchTorrents()
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

func writeJSONError(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	err := json.NewEncoder(w).Encode(map[string]string{"error": message})
	if err != nil {
		log.Error().Err(err).Int("status", status).Msg("Failed to encode JSON error response")
		if _, writeErr := io.WriteString(w, "error\n"); writeErr != nil {
			log.Error().Err(writeErr).Int("status", status).Msg("Failed to write JSON error fallback response")
		}
	}
}

type proxyMediaInfoRequestError struct {
	status  int
	message string
}

func (e *proxyMediaInfoRequestError) Error() string {
	return e.message
}

type proxyMediaInfoResponseWriteError struct {
	encodeErr   error
	fallbackErr error
}

func (e *proxyMediaInfoResponseWriteError) Error() string {
	if e.fallbackErr != nil {
		return e.fallbackErr.Error()
	}
	if e.encodeErr != nil {
		return e.encodeErr.Error()
	}
	return "proxy mediainfo response write failed"
}

func validateProxyMediainfoRequest(ctx context.Context, instanceStore *models.InstanceStore, syncManager *qbittorrent.SyncManager, instanceID int) (*models.Instance, qbt.AppPreferences, error) {
	if instanceStore == nil || syncManager == nil {
		log.Error().Int("instanceId", instanceID).Msg("Proxy mediainfo dependencies not configured")
		return nil, qbt.AppPreferences{}, &proxyMediaInfoRequestError{status: http.StatusInternalServerError, message: "MediaInfo service unavailable"}
	}

	instance, err := instanceStore.Get(ctx, instanceID)
	if err != nil {
		if errors.Is(err, models.ErrInstanceNotFound) {
			return nil, qbt.AppPreferences{}, &proxyMediaInfoRequestError{status: http.StatusNotFound, message: "Instance not found"}
		}

		log.Error().Err(err).Int("instanceId", instanceID).Msg("Failed to load instance for proxy mediainfo")
		return nil, qbt.AppPreferences{}, &proxyMediaInfoRequestError{status: http.StatusInternalServerError, message: "Failed to load instance"}
	}

	if instance == nil {
		return nil, qbt.AppPreferences{}, &proxyMediaInfoRequestError{status: http.StatusNotFound, message: "Instance not found"}
	}

	if !models.FilesystemCapabilitiesOf(instance).Content {
		return nil, qbt.AppPreferences{}, &proxyMediaInfoRequestError{status: http.StatusForbidden, message: "Instance does not have local filesystem access enabled"}
	}

	prefs, err := syncManager.GetAppPreferences(ctx, instanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceId", instanceID).Msg("Failed to get app preferences for proxy mediainfo")
		return nil, qbt.AppPreferences{}, &proxyMediaInfoRequestError{status: http.StatusInternalServerError, message: "Failed to get app preferences"}
	}

	return instance, prefs, nil
}

func resolveProxyContentPathCandidates(r *http.Request, prefs qbt.AppPreferences, contentPathCandidates func(qbt.AppPreferences, string) []string) (string, string, error) {
	contentPath, err := parseProxyMediaInfoContentPath(r)
	if err != nil {
		return "", "", &proxyMediaInfoRequestError{status: http.StatusBadRequest, message: "Invalid content path"}
	}

	candidates := contentPathCandidates(prefs, contentPath)
	if len(candidates) == 0 {
		return "", "", &proxyMediaInfoRequestError{status: http.StatusBadRequest, message: "No content roots configured for instance"}
	}

	resolvedPath, ok := findExistingProxyContentFile(candidates)
	if !ok {
		return "", "", &proxyMediaInfoRequestError{status: http.StatusNotFound, message: "File not found on disk"}
	}

	return contentPath, resolvedPath, nil
}

func analyzeMediaFile(resolvedPath, contentPath string, instanceID int) (string, string, error) {
	// #nosec G304 -- resolvedPath is constructed from validated base paths via resolveProxyContentPath.
	report, err := mediainfo.AnalyzeFile(resolvedPath, mediainfo.WithParseSpeed(0.5))
	if err != nil {
		log.Error().Err(err).Int("instanceId", instanceID).Str("contentPath", contentPath).Msg("Failed to analyze file with MediaInfo")
		return "", "", &proxyMediaInfoRequestError{status: http.StatusInternalServerError, message: "Failed to analyze file"}
	}

	summaryTxt, err := mediainfo.Render([]mediainfo.Report{report}, mediainfo.OutputText)
	if err != nil {
		log.Error().Err(err).Int("instanceId", instanceID).Str("contentPath", contentPath).Msg("Failed to render MediaInfo summary text")
		return "", "", &proxyMediaInfoRequestError{status: http.StatusInternalServerError, message: "Failed to render MediaInfo"}
	}

	rawJSON, err := mediainfo.Render([]mediainfo.Report{report}, mediainfo.OutputJSON)
	if err != nil {
		log.Error().Err(err).Int("instanceId", instanceID).Str("contentPath", contentPath).Msg("Failed to render MediaInfo JSON")
		return "", "", &proxyMediaInfoRequestError{status: http.StatusInternalServerError, message: "Failed to render MediaInfo"}
	}

	return summaryTxt, rawJSON, nil
}

func writeProxyMediaInfoResponse(w http.ResponseWriter, contentPath, summaryTxt, rawJSON string) error {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	err := json.NewEncoder(w).Encode(proxyContentPathMediaInfoResponse{
		ContentPath:   contentPath,
		SummaryTxt:    summaryTxt,
		MediaInfoJSON: json.RawMessage(rawJSON),
	})
	if err == nil {
		return nil
	}

	writeErr := error(nil)
	if _, fallbackErr := io.WriteString(w, "{}\n"); fallbackErr != nil {
		writeErr = fallbackErr
	}

	return &proxyMediaInfoResponseWriteError{encodeErr: err, fallbackErr: writeErr}
}

func normalizeContentPathRelativeInput(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("content path is required")
	}

	trimmed = strings.ReplaceAll(trimmed, "\\", "/")
	normalized := filepath.Clean(filepath.FromSlash(trimmed))
	if filepath.IsAbs(normalized) {
		return "", errors.New("absolute paths are not allowed")
	}
	if normalized == ".." || strings.HasPrefix(normalized, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	return normalized, nil
}

func resolveProxyContentPath(basePath, relativePath string) (string, error) {
	cleanBase := filepath.Clean(basePath)
	if !filepath.IsAbs(cleanBase) {
		return "", errors.New("base path must be absolute")
	}

	full := filepath.Join(cleanBase, filepath.FromSlash(relativePath))
	cleanFull := filepath.Clean(full)

	rel, err := filepath.Rel(cleanBase, cleanFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	// #nosec G703 -- cleanBase is validated as absolute and constrained by traversal checks above.
	if _, err := os.Lstat(cleanBase); err != nil {
		return "", fmt.Errorf("failed to access base path: %w", err)
	}

	evaluatedBase, err := filepath.EvalSymlinks(cleanBase)
	if err != nil {
		return "", fmt.Errorf("failed to resolve base path symlinks: %w", err)
	}

	// #nosec G703 -- cleanFull is derived from a validated base path and traversal-checked relative input.
	if _, err := os.Lstat(cleanFull); err != nil {
		if os.IsNotExist(err) {
			return cleanFull, nil
		}
		return "", fmt.Errorf("failed to access candidate path: %w", err)
	}

	evaluatedFull, err := filepath.EvalSymlinks(cleanFull)
	if err != nil {
		return "", fmt.Errorf("failed to resolve candidate path symlinks: %w", err)
	}

	rel, err = filepath.Rel(evaluatedBase, evaluatedFull)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("path traversal detected")
	}

	return evaluatedFull, nil
}

func appendUniqueProxyCandidate(candidates []string, seen map[string]struct{}, candidate string) []string {
	if candidate == "" {
		return candidates
	}

	cleanCandidate := filepath.Clean(candidate)
	if !filepath.IsAbs(cleanCandidate) {
		return candidates
	}
	if _, ok := seen[cleanCandidate]; ok {
		return candidates
	}

	seen[cleanCandidate] = struct{}{}
	return append(candidates, cleanCandidate)
}

func contentPathCandidatesFromPreferences(prefs qbt.AppPreferences, relativePath string) []string {
	candidates := make([]string, 0, 2)
	seen := make(map[string]struct{})

	if p, err := resolveProxyContentPath(prefs.SavePath, relativePath); err == nil {
		candidates = appendUniqueProxyCandidate(candidates, seen, p)
	}

	if prefs.TempPathEnabled {
		if p, err := resolveProxyContentPath(prefs.TempPath, relativePath); err == nil {
			candidates = appendUniqueProxyCandidate(candidates, seen, p)
		}
	}

	return candidates
}

func findExistingProxyContentFile(candidates []string) (string, bool) {
	for _, candidate := range candidates {
		// #nosec G703,G304 -- candidate is constructed from validated base paths via resolveProxyContentPath.
		f, err := os.Open(candidate)
		if err != nil {
			continue
		}

		stat, err := f.Stat()
		if err != nil {
			if cerr := f.Close(); cerr != nil {
				log.Warn().Err(cerr).Str("candidate", candidate).Msg("failed to close probed content file")
			}
			continue
		}
		if stat.IsDir() {
			if cerr := f.Close(); cerr != nil {
				log.Warn().Err(cerr).Str("candidate", candidate).Msg("failed to close probed content file")
			}
			continue
		}
		if cerr := f.Close(); cerr != nil {
			log.Warn().Err(cerr).Str("candidate", candidate).Msg("failed to close probed content file")
			continue
		}

		return candidate, true
	}

	return "", false
}

func parseProxyMediaInfoContentPath(r *http.Request) (string, error) {
	queryContentPath := strings.TrimSpace(r.URL.Query().Get("contentPath"))
	if queryContentPath == "" {
		queryContentPath = strings.TrimSpace(r.URL.Query().Get("content_path"))
	}
	if queryContentPath != "" {
		return normalizeContentPathRelativeInput(queryContentPath)
	}

	contentType := strings.TrimSpace(strings.ToLower(r.Header.Get("Content-Type")))
	if strings.HasPrefix(contentType, "application/json") {
		var req proxyContentPathMediaInfoRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			return "", errors.New("invalid request body")
		}
		return normalizeContentPathRelativeInput(req.ContentPath)
	}

	if err := r.ParseForm(); err != nil {
		return "", errors.New("invalid form data")
	}

	contentPath := r.FormValue("contentPath")
	if strings.TrimSpace(contentPath) == "" {
		contentPath = r.FormValue("content_path")
	}

	return normalizeContentPathRelativeInput(contentPath)
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
		normalizeStatusFilters(parseCSVQueryValues(queryParams, "excludeStatus", "excludestatus")),
		legacyExcludeStatusFilters,
	)

	filters := qbittorrent.FilterOptions{
		Hashes:            parseHashesQueryValues(queryParams),
		Status:            statusFilters,
		ExcludeStatus:     excludeStatusFilters,
		Categories:        parseCSVQueryValues(queryParams, "category", "categories"),
		ExcludeCategories: parseCSVQueryValues(queryParams, "excludeCategories", "excludecategories"),
		Tags:              parseCSVQueryValues(queryParams, "tag", "tags"),
		ExcludeTags:       parseCSVQueryValues(queryParams, "excludeTags", "excludetags"),
		Trackers:          parseCSVQueryValues(queryParams, "trackers"),
		ExcludeTrackers:   parseCSVQueryValues(queryParams, "excludeTrackers", "excludetrackers"),
		Expr:              strings.TrimSpace(queryParams.Get("expr")),
	}

	return filters
}

// parseInfoQueryValues reads a category or tag parameter of torrents/info. As
// in qBittorrent, a present parameter with no value (category=) asks for the
// torrents with no category or no tag, which the value "" selects.
func parseInfoQueryValues(queryParams url.Values, key string) []string {
	values := parseCSVQueryValues(queryParams, key)
	if _, present := queryParams[key]; present && len(values) == 0 {
		return []string{""}
	}
	return values
}

func (h *Handler) handleTorrentsInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

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
	categoryValues := parseInfoQueryValues(queryParams, "category")
	tagValues := parseInfoQueryValues(queryParams, "tag")
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

	hashes := parseHashesQueryValues(queryParams)
	uniqueHashCount := len(hashes)

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

	if sort == "" {
		sort = "added_on"
	}
	order := "asc"
	if reverse {
		order = "desc"
	}

	log.Trace().
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

	response, err := h.syncManager.ListTorrents(ctx, qbittorrent.TorrentListView{
		InstanceID:        instanceID,
		Limit:             limit,
		Offset:            offset,
		Sort:              sort,
		Order:             order,
		Filters:           filters,
		FullPasskeyAccess: true,
	})
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Msg("Failed to get torrents from sync manager")
		h.writeProxyError(w)
		return
	}

	torrents := make([]any, len(response.Torrents))
	for i, tv := range response.Torrents {
		torrents[i] = tv.Torrent
	}

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

func (h *Handler) handleTorrentSearch(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

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

	if sort == "" {
		sort = "added_on"
	}
	order := "asc"
	if reverse {
		order = "desc"
	}

	log.Trace().
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

	response, err := h.syncManager.ListTorrents(ctx, qbittorrent.TorrentListView{
		InstanceID:        instanceID,
		Limit:             limit,
		Offset:            offset,
		Sort:              sort,
		Order:             order,
		Search:            search,
		Filters:           filters,
		SidebarCounts:     true,
		FullPasskeyAccess: true,
	})
	if err != nil {
		log.Error().
			Err(err).
			Int("instanceId", instanceID).
			Str("search", search).
			Msg("Failed to get torrents with qui filters")
		h.writeProxyError(w)
		return
	}

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

func (h *Handler) handleCategories(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	if !h.validateQueryParams(w, r, map[string]struct{}{}, "torrents/categories") {
		return
	}

	log.Trace().
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

func (h *Handler) handleTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	if !h.validateQueryParams(w, r, map[string]struct{}{}, "torrents/tags") {
		return
	}

	log.Trace().
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

	log.Trace().
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

	log.Trace().
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

	log.Trace().
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

func (h *Handler) handleTorrentMediaInfo(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	instance, prefs, err := validateProxyMediainfoRequest(ctx, h.instanceStore, h.syncManager, instanceID)
	if err != nil {
		if reqErr, ok := errors.AsType[*proxyMediaInfoRequestError](err); ok {
			writeJSONError(w, reqErr.status, reqErr.message)
			return
		}

		writeJSONError(w, http.StatusInternalServerError, "Failed to load instance")
		return
	}
	_ = instance

	contentPath, resolvedPath, err := resolveProxyContentPathCandidates(r, prefs, contentPathCandidatesFromPreferences)
	if err != nil {
		if reqErr, ok := errors.AsType[*proxyMediaInfoRequestError](err); ok {
			writeJSONError(w, reqErr.status, reqErr.message)
			return
		}

		writeJSONError(w, http.StatusInternalServerError, "File not found on disk")
		return
	}

	clientName := "unknown"
	if clientAPIKey != nil {
		clientName = clientAPIKey.ClientName
	}

	log.Trace().
		Int("instanceId", instanceID).
		Str("client", clientName).
		Str("contentPath", contentPath).
		Msg("Handling torrent mediainfo request via proxy endpoint")

	summaryTxt, rawJSON, err := analyzeMediaFile(resolvedPath, contentPath, instanceID)
	if err != nil {
		if reqErr, ok := errors.AsType[*proxyMediaInfoRequestError](err); ok {
			writeJSONError(w, reqErr.status, reqErr.message)
			return
		}

		writeJSONError(w, http.StatusInternalServerError, "Failed to analyze file")
		return
	}

	if err = writeProxyMediaInfoResponse(w, contentPath, summaryTxt, rawJSON); err != nil {
		if writeErr, ok := errors.AsType[*proxyMediaInfoResponseWriteError](err); ok {
			if writeErr.encodeErr != nil {
				log.Error().Err(writeErr.encodeErr).Int("instanceId", instanceID).Str("contentPath", contentPath).Msg("Failed to encode proxy mediainfo response")
			}
			if writeErr.fallbackErr != nil {
				log.Error().Err(writeErr.fallbackErr).Int("instanceId", instanceID).Str("contentPath", contentPath).Msg("Failed to write proxy mediainfo fallback response")
			}
			return
		}

		log.Error().Err(err).Int("instanceId", instanceID).Str("contentPath", contentPath).Msg("Failed to encode proxy mediainfo response")
	}
}

// handleAuthLogin handles /api/v2/auth/login requests (ceremonial - proxy already authenticated)
func (h *Handler) handleAuthLogin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	log.Trace().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Msg("Handling ceremonial auth/login request")

	if !h.clientPool.IsHealthy(instanceID) {
		log.Warn().
			Int("instanceId", instanceID).
			Msg("Instance unhealthy for auth/login")
		h.writeProxyError(w)
		return
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

	http.SetCookie(w, cookie)

	log.Trace().
		Str("client", clientAPIKey.ClientName).
		Int("instanceId", instanceID).
		Str("cookieName", cookie.Name).
		Msg("Set ceremonial login cookie for client")

	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(proxyLoginSuccessBody))
}

func (h *Handler) handleSetLocation(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read setLocation body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

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
			if failedInvalidations > 0 {
				log.Warn().Int("instanceId", instanceID).Int("failed", failedInvalidations).Int("total", len(hashList)).
					Msg("Some file cache invalidations failed after setLocation - cache may be stale")
			}
		}

		if panicValue != nil {
			panic(panicValue)
		}
	}()

	h.forwardWithFolderCleanup(w, r, bodyBytes, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupSetLocation, Hashes: splitHashes(form.get("hashes")),
			Path: strings.TrimSpace(form.get("location"))}, true
	})
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

func (h *Handler) handleRenameFile(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read renameFile body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

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

	defer func() {
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

		if panicValue != nil {
			panic(panicValue)
		}
	}()

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	h.proxy.ServeHTTP(w, r)
}

func (h *Handler) handleRenameFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read renameFolder body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

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

	defer func() {
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

		if panicValue != nil {
			panic(panicValue)
		}
	}()

	r.Body = io.NopCloser(bytes.NewReader(bodyBytes))

	h.proxy.ServeHTTP(w, r)
}

// deleteHashes returns the torrents that a torrents/delete request names and
// whether it deletes their files, as qBittorrent reads them from a urlencoded
// or a multipart body (RequestParser::parsePostMessage): the last value of
// each field.
func deleteHashes(r *http.Request, body []byte) (hashes []string, deleteFiles bool, ok bool) {
	if form, ok := parseQBTForm(r, body); ok {
		return splitHashes(form.get("hashes")), strings.EqualFold(form.get("deleteFiles"), "true"), true
	}
	mediaType, params, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "multipart/form-data" {
		return nil, false, false
	}
	form, err := multipart.NewReader(bytes.NewReader(body), params["boundary"]).ReadForm(int64(len(body)))
	if err != nil {
		return nil, false, false
	}
	defer func() { _ = form.RemoveAll() }()
	last := func(field string) string {
		if values := form.Value[field]; len(values) > 0 {
			return values[len(values)-1]
		}
		return ""
	}
	if len(form.Value["hashes"]) == 0 {
		return nil, false, false
	}
	return splitHashes(last("hashes")), strings.EqualFold(last("deleteFiles"), "true"), true
}

func (h *Handler) handleDeleteTorrents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	instanceID := GetInstanceIDFromContext(ctx)
	clientAPIKey := GetClientAPIKeyFromContext(ctx)

	bodyBytes, err := bufferRequestBody(r)
	if err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to read delete body")
		http.Error(w, "Failed to read request body", http.StatusInternalServerError)
		return
	}

	if err := r.ParseForm(); err != nil {
		log.Warn().Err(err).Int("instanceId", instanceID).Msg("Failed to parse delete form")
		http.Error(w, "Invalid form data", http.StatusBadRequest)
		return
	}

	noted, deleteFiles, parsed := deleteHashes(r, bodyBytes)
	if !parsed {
		log.Warn().Int("instanceId", instanceID).Str("client", clientAPIKey.ClientName).
			Msg("Refused a proxied delete that qui cannot read")
		http.Error(w, "qui cannot read the torrents and the deleteFiles value of this delete", http.StatusBadRequest)
		return
	}
	if deleteFiles {
		if err := h.syncManager.RefuseHelperFiles(ctx, instanceID, noted); err != nil {
			log.Warn().Err(err).Int("instanceId", instanceID).Str("client", clientAPIKey.ClientName).
				Msg("Refused a proxied delete with files")
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}
	}

	hashes := r.Form.Get("hashes")

	log.Debug().
		Int("instanceId", instanceID).
		Str("client", clientAPIKey.ClientName).
		Str("hashes", hashes).
		Msg("Intercepting delete request for cache invalidation")

	defer func() {
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
			if failedInvalidations > 0 {
				log.Warn().Int("instanceId", instanceID).Int("failed", failedInvalidations).Int("total", len(hashList)).
					Msg("Some file cache invalidations failed after delete - cache may be stale")
			}
		}

		if panicValue != nil {
			panic(panicValue)
		}
	}()

	h.deleteNoter.NoteProxiedDelete(ctx, instanceID, noted, clientAPIKey.ClientName)
	h.forwardWithFolderCleanup(w, r, bodyBytes, func(form qbtForm) (qbittorrent.FolderCleanupOp, bool) {
		return qbittorrent.FolderCleanupOp{Kind: qbittorrent.FolderCleanupDelete, Hashes: splitHashes(form.get("hashes"))},
			strings.EqualFold(form.get("deleteFiles"), "true")
	})
}
