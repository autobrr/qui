// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/alexedwards/scs/v2"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

type AuthHandler struct {
	authService    *auth.Service
	sessionManager *scs.SessionManager
	oidcHandler    *OIDCHandler
	config         *domain.Config
}

func NewAuthHandler(authService *auth.Service, sessionManager *scs.SessionManager, config *domain.Config) *AuthHandler {
	h := &AuthHandler{
		authService:    authService,
		sessionManager: sessionManager,
		config:         config,
	}

	// Initialize OIDC handler if enabled
	if config.OIDCEnabled {
		oidcHandler, err := NewOIDCHandler(config, sessionManager)
		if err != nil {
			log.Error().Err(err).Msg("failed to initialize OIDC handler")
		} else {
			h.oidcHandler = oidcHandler
		}
	}

	return h
}

// GetOIDCHandler returns the OIDC handler if configured
func (h *AuthHandler) GetOIDCHandler() *OIDCHandler {
	return h.oidcHandler
}

// SetupRequest represents the initial setup request
type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	RememberMe bool   `json:"remember_me"`
}

// ChangePasswordRequest represents a password change request
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// Setup handles initial user setup
func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request) {
	// Check if setup is already complete
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

	// Validate input
	if req.Username == "" || req.Password == "" {
		RespondError(w, http.StatusBadRequest, "Username and password are required")
		return
	}

	// Create user
	user, err := h.authService.SetupUser(r.Context(), req.Username, req.Password)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create user")
		RespondError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Create session using SCS
	// Renew token to prevent session fixation attacks
	if err := h.sessionManager.RenewToken(r.Context()); err != nil {
		log.Error().Err(err).Msg("Failed to renew session token")
	}

	h.sessionManager.Put(r.Context(), "authenticated", true)
	h.sessionManager.Put(r.Context(), "user_id", user.ID)
	h.sessionManager.Put(r.Context(), "username", user.Username)

	RespondJSON(w, http.StatusCreated, map[string]any{
		"message": "Setup completed successfully",
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// Login handles user login
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Validate credentials
	user, err := h.authService.Login(r.Context(), req.Username, req.Password)
	if err != nil {
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

	// Create session using SCS
	// Renew token to prevent session fixation attacks
	if err := h.sessionManager.RenewToken(r.Context()); err != nil {
		log.Error().Err(err).Msg("Failed to renew session token")
	}

	h.sessionManager.Put(r.Context(), "authenticated", true)
	h.sessionManager.Put(r.Context(), "user_id", user.ID)
	h.sessionManager.Put(r.Context(), "username", user.Username)
	h.sessionManager.Put(r.Context(), "auth_method", "password")

	// Handle remember_me functionality
	h.sessionManager.RememberMe(r.Context(), req.RememberMe)

	RespondJSON(w, http.StatusOK, map[string]any{
		"message": "Login successful",
		"user": map[string]any{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// Logout handles user logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	// Destroy the session
	if err := h.sessionManager.Destroy(r.Context()); err != nil {
		log.Error().Err(err).Msg("Failed to destroy session")
		RespondError(w, http.StatusInternalServerError, "Failed to logout")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

// GetCurrentUser returns the current user information
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	// Check if the session is authenticated (works for both regular and OIDC auth)
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

	// For OIDC users, we might not have a user_id
	userID := h.sessionManager.GetInt(r.Context(), "user_id")
	authMethod := h.sessionManager.GetString(r.Context(), "auth_method")

	response := map[string]any{
		"username": username,
	}

	// Only include ID if it exists (for built-in auth users)
	if userID != 0 {
		response["id"] = userID
	}

	// Include auth method if available
	if authMethod != "" {
		response["auth_method"] = authMethod
	}

	RespondJSON(w, http.StatusOK, response)
}

// Validate checks if the user has a valid session (used for OIDC callback)
func (h *AuthHandler) Validate(w http.ResponseWriter, r *http.Request) {
	authenticated := h.sessionManager.GetBool(r.Context(), "authenticated")
	if !authenticated {
		RespondError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	username := h.sessionManager.GetString(r.Context(), "username")
	authMethod := h.sessionManager.GetString(r.Context(), "auth_method")
	profilePicture := h.sessionManager.GetString(r.Context(), "profile_picture")

	RespondJSON(w, http.StatusOK, map[string]any{
		"username":        username,
		"auth_method":     authMethod,
		"profile_picture": profilePicture,
	})
}

// CheckSetupRequired checks if initial setup is required
func (h *AuthHandler) CheckSetupRequired(w http.ResponseWriter, r *http.Request) {
	complete, err := h.authService.IsSetupComplete(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to check setup status")
		RespondError(w, http.StatusInternalServerError, "Failed to check setup status")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]any{
		"setupRequired": !complete,
	})
}

// ChangePassword handles password change requests
func (h *AuthHandler) ChangePassword(w http.ResponseWriter, r *http.Request) {
	var req ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	// Change password
	if err := h.authService.ChangePassword(r.Context(), req.CurrentPassword, req.NewPassword); err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			RespondError(w, http.StatusUnauthorized, "Invalid current password")
			return
		}
		log.Error().Err(err).Msg("Failed to change password")
		RespondError(w, http.StatusInternalServerError, "Failed to change password")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Password changed successfully",
	})
}

// API Key Management

// CreateAPIKeyRequest represents a request to create an API key
type CreateAPIKeyRequest struct {
	Name string `json:"name"`
}

// CreateAPIKey creates a new API key
func (h *AuthHandler) CreateAPIKey(w http.ResponseWriter, r *http.Request) {
	var req CreateAPIKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		RespondError(w, http.StatusBadRequest, "Invalid request body")
		return
	}

	if req.Name == "" {
		RespondError(w, http.StatusBadRequest, "API key name is required")
		return
	}

	// Create API key
	rawKey, apiKey, err := h.authService.CreateAPIKey(r.Context(), req.Name)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create API key")
		RespondError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]any{
		"id":        apiKey.ID,
		"name":      apiKey.Name,
		"key":       rawKey, // Only shown once
		"createdAt": apiKey.CreatedAt,
		"message":   "Save this key securely - it will not be shown again",
	})
}

// ListAPIKeys returns all API keys
func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.authService.ListAPIKeys(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to list API keys")
		RespondError(w, http.StatusInternalServerError, "Failed to list API keys")
		return
	}

	RespondJSON(w, http.StatusOK, keys)
}

// DeleteAPIKey deletes an API key
func (h *AuthHandler) DeleteAPIKey(w http.ResponseWriter, r *http.Request) {
	// Get API key ID from URL parameter
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
