// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/autobrr/qui/internal/auth"
	"github.com/autobrr/qui/internal/models"
	"github.com/go-chi/chi/v5"
	"github.com/gorilla/sessions"
	"github.com/rs/zerolog/log"
)

type AuthHandler struct {
	authService *auth.Service
}

func NewAuthHandler(authService *auth.Service) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// SetupRequest represents the initial setup request
type SetupRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginRequest represents a login request
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// ChangePasswordRequest represents a password change request
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// Setup handles initial user setup
func (h *AuthHandler) Setup(w http.ResponseWriter, r *http.Request) {
	// Check if setup is already complete
	complete, err := h.authService.IsSetupComplete()
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
	user, err := h.authService.SetupUser(req.Username, req.Password)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create user")
		RespondError(w, http.StatusInternalServerError, "Failed to create user")
		return
	}

	// Create session
	session, _ := h.authService.GetSessionStore().Get(r, auth.SessionName)
	session.Values["authenticated"] = true
	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username

	// Configure cookie security
	session.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	// If behind reverse proxy with HTTPS, upgrade security
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		session.Options.Secure = true
		session.Options.SameSite = http.SameSiteStrictMode
	}

	if err := session.Save(r, w); err != nil {
		log.Error().Err(err).Msg("Failed to save session")
		RespondError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"message": "Setup completed successfully",
		"user": map[string]interface{}{
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
	user, err := h.authService.Login(req.Username, req.Password)
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

	// Create session
	session, _ := h.authService.GetSessionStore().Get(r, auth.SessionName)
	session.Values["authenticated"] = true
	session.Values["user_id"] = user.ID
	session.Values["username"] = user.Username

	// Configure cookie security
	session.Options = &sessions.Options{
		Path:     "/",
		MaxAge:   86400 * 7, // 7 days
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	// If behind reverse proxy with HTTPS, upgrade security
	if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
		session.Options.Secure = true
		session.Options.SameSite = http.SameSiteStrictMode
	}

	if err := session.Save(r, w); err != nil {
		log.Error().Err(err).Msg("Failed to save session")
		RespondError(w, http.StatusInternalServerError, "Failed to create session")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"message": "Login successful",
		"user": map[string]interface{}{
			"id":       user.ID,
			"username": user.Username,
		},
	})
}

// Logout handles user logout
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	session, _ := h.authService.GetSessionStore().Get(r, auth.SessionName)

	// Clear session values
	session.Values["authenticated"] = false
	session.Options.MaxAge = -1

	if err := session.Save(r, w); err != nil {
		log.Error().Err(err).Msg("Failed to clear session")
		RespondError(w, http.StatusInternalServerError, "Failed to logout")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "Logged out successfully",
	})
}

// GetCurrentUser returns the current user information
func (h *AuthHandler) GetCurrentUser(w http.ResponseWriter, r *http.Request) {
	session, _ := h.authService.GetSessionStore().Get(r, auth.SessionName)

	userID, ok := session.Values["user_id"].(int)
	if !ok {
		RespondError(w, http.StatusUnauthorized, "Not authenticated")
		return
	}

	username, ok := session.Values["username"].(string)
	if !ok {
		RespondError(w, http.StatusInternalServerError, "Invalid session data")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
		"id":       userID,
		"username": username,
	})
}

// CheckSetupRequired checks if initial setup is required
func (h *AuthHandler) CheckSetupRequired(w http.ResponseWriter, r *http.Request) {
	complete, err := h.authService.IsSetupComplete()
	if err != nil {
		log.Error().Err(err).Msg("Failed to check setup status")
		RespondError(w, http.StatusInternalServerError, "Failed to check setup status")
		return
	}

	RespondJSON(w, http.StatusOK, map[string]interface{}{
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
	if err := h.authService.ChangePassword(req.CurrentPassword, req.NewPassword); err != nil {
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
	rawKey, apiKey, err := h.authService.CreateAPIKey(req.Name)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create API key")
		RespondError(w, http.StatusInternalServerError, "Failed to create API key")
		return
	}

	RespondJSON(w, http.StatusCreated, map[string]interface{}{
		"id":        apiKey.ID,
		"name":      apiKey.Name,
		"key":       rawKey, // Only shown once
		"createdAt": apiKey.CreatedAt,
		"message":   "Save this key securely - it will not be shown again",
	})
}

// ListAPIKeys returns all API keys
func (h *AuthHandler) ListAPIKeys(w http.ResponseWriter, r *http.Request) {
	keys, err := h.authService.ListAPIKeys()
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

	if err := h.authService.DeleteAPIKey(id); err != nil {
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
