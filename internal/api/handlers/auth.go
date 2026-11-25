// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
	"golang.org/x/time/rate"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type AuthHandler struct {
	authService    *auth.Service
	sessionManager *scs.SessionManager
	oidcHandler    *OIDCHandler
	config         *domain.Config
	instanceStore  *models.InstanceStore
	clientPool     *qbittorrent.ClientPool
	syncManager    *qbittorrent.SyncManager
	loginLimiter   *rate.Limiter
	loginMu        sync.Mutex
}

func newLoginLimiter() *rate.Limiter {
	return rate.NewLimiter(rate.Every(time.Minute/5), 5)
}

func requireJSON(w http.ResponseWriter, r *http.Request) bool {
	mediaType, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		RespondError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	return true
}

func renewSessionToken(ctx context.Context, sessionManager *scs.SessionManager) error {
	if err := sessionManager.RenewToken(ctx); err != nil {
		return errors.Join(err, sessionManager.Clear(ctx))
	}
	return nil
}

func NewAuthHandler(
	authService *auth.Service,
	sessionManager *scs.SessionManager, config *domain.Config,
	instanceStore *models.InstanceStore,
	clientPool *qbittorrent.ClientPool,
	syncManager *qbittorrent.SyncManager,
) (*AuthHandler, error) {
	h := &AuthHandler{
		authService:    authService,
		sessionManager: sessionManager,
		instanceStore:  instanceStore,
		clientPool:     clientPool,
		syncManager:    syncManager,
		config:         config,
		loginLimiter:   newLoginLimiter(),
	}

	if config.OIDCEnabled {
		oidcHandler, err := NewOIDCHandler(config, sessionManager)
		if err != nil {
			return nil, fmt.Errorf("init OIDC handler: %w", err)
		}
		h.oidcHandler = oidcHandler
	}

	return h, nil
}

func (h *AuthHandler) GetOIDCHandler() *OIDCHandler {
	return h.oidcHandler
}

func (h *AuthHandler) rejectIfAuthDisabled(w http.ResponseWriter) bool {
	if h.config != nil && h.config.Live().IsAuthDisabled() {
		RespondError(w, http.StatusForbidden, "Endpoint disabled when authentication is disabled")
		return true
	}
	return false
}

type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type LoginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

type sessionUser struct {
	ID       int    `json:"id"`
	Username string `json:"username"`
}

type sessionStartedReply struct {
	Message string      `json:"message"`
	User    sessionUser `json:"user"`
}

type currentUserReply struct {
	ID         int    `json:"id,omitzero"`
	Username   string `json:"username"`
	AuthMethod string `json:"auth_method,omitempty"`
}

type validateReply struct {
	Username       string `json:"username"`
	AuthMethod     string `json:"auth_method"`
	ProfilePicture string `json:"profile_picture"`
}

type setupRequiredReply struct {
	SetupRequired bool `json:"setupRequired"`
}

type createdAPIKeyReply struct {
	ID          int                 `json:"id"`
	Name        string              `json:"name"`
	Key         string              `json:"key"`
	CreatedAt   time.Time           `json:"createdAt"`
	Permissions []models.Permission `json:"permissions"`
	InstanceIDs []int               `json:"instanceIds"`
	Message     string              `json:"message"`
}

func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	if h.config != nil && h.config.Live().OIDCEnabled {
		RespondError(w, http.StatusForbidden, "Setup is disabled when OIDC is enabled")
		return
	}

	if !requireJSON(w, r) {
		return
	}

	complete, err := h.authService.IsSetupComplete(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to check setup status")
		RespondError(w, http.StatusInternalServerError, "Failed to check setup status")
		return
	}

	if complete {
		RespondError(w, http.StatusBadRequest, "Setup already completed")
		return
	}

	var req SetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Username == "" || req.Password == "" {
		RespondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	user, err := h.authService.SetupUser(r.Context(), req.Username, req.Password)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create user")
		RespondError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	if err := renewSessionToken(r.Context(), h.sessionManager); err != nil {
		log.Error().Err(err).Msg("Failed to renew session token")
		RespondError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	h.sessionManager.Put(r.Context(), "authenticated", true)
	h.sessionManager.Put(r.Context(), "user_id", user.ID)
	h.sessionManager.Put(r.Context(), "username", user.Username)

	RespondJSON(w, http.StatusCreated, sessionStartedReply{
		Message: "Setup completed successfully",
		User:    sessionUser{ID: user.ID, Username: user.Username},
	})
}

func (h *AuthHandler) warmSession(ctx context.Context) {
	instances, err := h.instanceStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list instances for session warming")
		return
	}

	var activeInstances []*models.Instance
	for _, instance := range instances {
		if !instance.IsActive {
			log.Debug().
				Int("instance_id", instance.ID).
				Str("instance_name", instance.Name).
				Msg("Skipping session warmup for disabled instance")
			continue
		}
		activeInstances = append(activeInstances, instance)
	}

	if len(activeInstances) == 0 {
		log.Debug().Msg("Skipping session warmup: no active instances")
		return
	}

	for _, instance := range activeInstances {
		inst := instance
		go func(inst *models.Instance) {
			warmCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			_, err := h.clientPool.GetClientWithTimeout(warmCtx, inst.ID, 3*time.Second)
			if err != nil {
				log.Error().
					Int("instance_id", inst.ID).
					Str("instance_name", inst.Name).
					Err(err).
					Msg("Failed to warm instance connection")
				return
			}

			log.Debug().
				Int("instance_id", inst.ID).
				Str("instance_name", inst.Name).
				Msg("Successfully warmed instance connection")
		}(inst)
	}

	targetInstance := activeInstances[0]

	go func() {
		warmCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()

		_, err := h.syncManager.ListTorrents(warmCtx, qbittorrent.TorrentListView{
			InstanceID:    targetInstance.ID,
			Limit:         1,
			Sort:          "added_on",
			Order:         "desc",
			SidebarCounts: true,
		})
		if err != nil {
			log.Error().
				Int("instance_id", targetInstance.ID).
				Err(err).
				Msg("Failed to prefetch torrents during session warming")
			return
		}

		log.Debug().
			Int("instance_id", targetInstance.ID).
			Msg("Successfully prefetched torrents during session warming")
	}()
}

var errTooManyLogins = errors.New("too many failed login attempts")

func (h *AuthHandler) checkPassword(ctx context.Context, username, password string) (*models.User, error) {
	h.loginMu.Lock()
	defer h.loginMu.Unlock()

	if h.loginLimiter.Tokens() < 1 {
		return nil, errTooManyLogins
	}
	user, err := h.authService.Login(ctx, username, password)
	if errors.Is(err, auth.ErrInvalidCredentials) {
		h.loginLimiter.Allow()
	}
	return user, err
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	if !requireJSON(w, r) {
		return
	}

	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	user, err := h.checkPassword(r.Context(), req.Username, req.Password)
	if err != nil {
		if errors.Is(err, errTooManyLogins) {
			RespondError(w, http.StatusTooManyRequests, "Too many failed login attempts, try again later")
			return
		}
		if errors.Is(err, auth.ErrInvalidCredentials) {
			RespondError(w, http.StatusUnauthorized, "Invalid credentials")
			return
		}
		if errors.Is(err, auth.ErrNotSetup) {
			RespondError(w, http.StatusPreconditionRequired, "Initial setup required")
			return
		}
		log.Error().Err(err).Msg("Login failed")
		RespondError(w, http.StatusInternalServerError, "Login failed")
		return
	}

	if err := renewSessionToken(r.Context(), h.sessionManager); err != nil {
		log.Error().Err(err).Msg("Failed to renew session token")
		RespondError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	h.sessionManager.Put(r.Context(), "authenticated", true)
	h.sessionManager.Put(r.Context(), "user_id", user.ID)
	h.sessionManager.Put(r.Context(), "username", user.Username)
	h.sessionManager.Put(r.Context(), "auth_method", "password")

	h.sessionManager.RememberMe(r.Context(), req.RememberMe)

	go h.warmSession(context.Background()) //nolint:gosec // G118: session warm-up must outlive the login request

	RespondJSON(w, http.StatusOK, sessionStartedReply{
		Message: "Login successful",
		User:    sessionUser{ID: user.ID, Username: user.Username},
	})
}

func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	if err := h.sessionManager.Destroy(r.Context()); err != nil {
		log.Error().Err(err).Msg("Failed to destroy session")
		RespondError(w, http.StatusInternalServerError, "Failed to logout")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

func syntheticAdminResponse() currentUserReply {
	return currentUserReply{Username: "admin", AuthMethod: "none"}
}

func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	if h.config != nil && h.config.Live().IsAuthDisabled() {
		RespondJSON(w, http.StatusOK, syntheticAdminResponse())
		return
	}

	authenticated := h.sessionManager.GetBool(r.Context(), "authenticated")
	if !authenticated {
		RespondError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	username := h.sessionManager.GetString(r.Context(), "username")
	if username == "" {
		RespondError(w, http.StatusInternalServerError, "Invalid session data")
		return
	}

	userID := h.sessionManager.GetInt(r.Context(), "user_id")
	authMethod := h.sessionManager.GetString(r.Context(), "auth_method")

	RespondJSON(w, http.StatusOK, currentUserReply{
		ID:         userID,
		Username:   username,
		AuthMethod: authMethod,
	})
}

func (h *AuthHandler) Validate(w http.ResponseWriter, r *http.Request) {
	if h.config != nil && h.config.Live().IsAuthDisabled() {
		RespondJSON(w, http.StatusOK, syntheticAdminResponse())
		return
	}

	authenticated := h.sessionManager.GetBool(r.Context(), "authenticated")
	if !authenticated {
		RespondError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	username := h.sessionManager.GetString(r.Context(), "username")
	authMethod := h.sessionManager.GetString(r.Context(), "auth_method")
	profilePicture := h.sessionManager.GetString(r.Context(), "profile_picture")

	RespondJSON(w, http.StatusOK, validateReply{
		Username:       username,
		AuthMethod:     authMethod,
		ProfilePicture: profilePicture,
	})
}

func (h *AuthHandler) CheckSetupRequired(w http.ResponseWriter, r *http.Request) {
	if h.config != nil && (h.config.Live().IsAuthDisabled() || h.config.Live().OIDCEnabled) {
		RespondJSON(w, http.StatusOK, setupRequiredReply{SetupRequired: false})
		return
	}

	complete, err := h.authService.IsSetupComplete(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to check setup status")
		RespondError(w, http.StatusInternalServerError, "Failed to check setup status")
		return
	}

	RespondJSON(w, http.StatusOK, setupRequiredReply{SetupRequired: !complete})
}

func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if err := h.authService.ChangePassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			RespondError(w, http.StatusUnauthorized, "Invalid current password")
			return
		}
		log.Error().Err(err).Msg("Failed to change password")
		RespondError(w, http.StatusInternalServerError, "Failed to change password")
		return
	}

	err := h.sessionManager.Iterate(r.Context(), func(ctx context.Context) error {
		return h.sessionManager.Destroy(ctx)
	})
	if err == nil {
		err = h.sessionManager.Destroy(r.Context())
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to end sessions after password change")
		RespondError(w, http.StatusInternalServerError, "Password changed, but ending sessions failed")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Password changed successfully",
	})
}

type APIKeyRequest struct {
	Name        string              `json:"name"`
	Permissions []models.Permission `json:"permissions"`
	InstanceIDs []int               `json:"instanceIds"`
}

func (req *APIKeyRequest) validate() string {
	if req.Name == "" {
		return "API key name is required"
	}
	if err := models.ValidatePermissions(req.Permissions); err != nil {
		return err.Error()
	}
	if slices.ContainsFunc(req.InstanceIDs, func(id int) bool { return id < 1 }) {
		return "API key instance limit holds an invalid instance id"
	}
	return ""
}

func (h *AuthHandler) refuseUnknownLimitInstance(w http.ResponseWriter, r *http.Request, keyID int, limit []int) bool {
	if len(limit) == 0 {
		return false
	}
	instances, err := h.instanceStore.List(r.Context())
	var keys []*models.APIKey
	if err == nil && keyID > 0 {
		keys, err = h.authService.ListAPIKeys(r.Context())
	}
	if err != nil {
		log.Error().Err(err).Msg("Failed to check API key instance limit")
		RespondError(w, http.StatusInternalServerError, "Failed to check API key instance limit")
		return true
	}
	known := make(map[int]bool, len(instances))
	for _, inst := range instances {
		known[inst.ID] = true
	}
	if i := slices.IndexFunc(keys, func(key *models.APIKey) bool { return key.ID == keyID }); i >= 0 {
		for _, id := range keys[i].InstanceIDs {
			known[id] = true
		}
	}
	if slices.ContainsFunc(limit, func(id int) bool { return !known[id] }) {
		RespondError(w, http.StatusBadRequest, "API key instance limit holds an id that is not an instance")
		return true
	}
	return false
}

func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	var req APIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if msg := req.validate(); msg != "" {
		RespondError(w, http.StatusBadRequest, msg)
		return
	}
	if h.refuseUnknownLimitInstance(w, r, 0, req.InstanceIDs) {
		return
	}

	rawKey, apiKey, err := h.authService.CreateAPIKey(r.Context(), req.Name, req.Permissions, req.InstanceIDs)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create API key")
		RespondError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	RespondJSON(w, http.StatusCreated, createdAPIKeyReply{
		ID:          apiKey.ID,
		Name:        apiKey.Name,
		Key:         rawKey,
		CreatedAt:   apiKey.CreatedAt,
		Permissions: apiKey.Permissions,
		InstanceIDs: apiKey.InstanceIDs,
		Message:     "Save this key securely - it will not be shown again",
	})
}

func (h *AuthHandler) UpdateAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	id, err := strconv.Atoi(chi.URLParam(r, "id"))
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	var req APIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}
	if msg := req.validate(); msg != "" {
		RespondError(w, http.StatusBadRequest, msg)
		return
	}
	if h.refuseUnknownLimitInstance(w, r, id, req.InstanceIDs) {
		return
	}

	apiKey, err := h.authService.UpdateAPIKey(r.Context(), id, req.Name, req.Permissions, req.InstanceIDs)
	if errors.Is(err, models.ErrAPIKeyNotFound) {
		RespondError(w, http.StatusNotFound, "API key not found")
		return
	}
	if err != nil {
		log.Error().Err(err).Int("id", id).Msg("Failed to update API key")
		RespondError(w, http.StatusInternalServerError, "Failed to update API key")
		return
	}

	RespondJSON(w, http.StatusOK, apiKey)
}

func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	keys, err := h.authService.ListAPIKeys(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list API keys")
		RespondError(w, http.StatusInternalServerError, "Failed to list API keys")
		return
	}

	RespondJSON(w, http.StatusOK, keys)
}

func (h *AuthHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	if h.rejectIfAuthDisabled(w) {
		return
	}

	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		RespondError(w, http.StatusBadRequest, "API key ID is required")
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid API key ID")
		return
	}

	if err := h.authService.DeleteAPIKey(r.Context(), id); err != nil {
		if errors.Is(err, models.ErrAPIKeyNotFound) {
			RespondError(w, http.StatusNotFound, "API key not found")
			return
		}
		log.Error().Err(err).Msg("Failed to delete API key")
		RespondError(w, http.StatusInternalServerError, "Failed to delete API key")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "API key deleted successfully",
	})
}
