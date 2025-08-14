// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/autobrr/qui/internal/services"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"
)

// ThemeLicenseHandler handles theme license related HTTP requests
type ThemeLicenseHandler struct {
	themeLicenseService *services.ThemeLicenseService
}

// NewThemeLicenseHandler creates a new theme license handler
func NewThemeLicenseHandler(themeLicenseService *services.ThemeLicenseService) *ThemeLicenseHandler {
	return &ThemeLicenseHandler{
		themeLicenseService: themeLicenseService,
	}
}

// ValidateLicenseRequest represents the request body for license validation
type ValidateLicenseRequest struct {
	LicenseKey string `json:"licenseKey"`
}

// ValidateLicenseResponse represents the response for license validation
type ValidateLicenseResponse struct {
	Valid     bool       `json:"valid"`
	ThemeName string     `json:"themeName,omitempty"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
	Message   string     `json:"message,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// PremiumAccessResponse represents the response for premium access status
type PremiumAccessResponse struct {
	HasPremiumAccess bool `json:"hasPremiumAccess"`
}

// LicenseInfo represents basic license information for UI display
type LicenseInfo struct {
	LicenseKey string    `json:"licenseKey"`
	ThemeName  string    `json:"themeName"`
	Status     string    `json:"status"`
	CreatedAt  time.Time `json:"createdAt"`
}

// RegisterRoutes registers theme license routes
func (h *ThemeLicenseHandler) RegisterRoutes(r chi.Router) {
	r.Route("/themes", func(r chi.Router) {
		r.Post("/license/validate", h.ValidateLicense)
		r.Get("/licensed", h.GetLicensedThemes)
		r.Get("/licenses", h.GetAllLicenses)
		r.Delete("/license/{licenseKey}", h.DeleteLicense)
		r.Post("/license/refresh", h.RefreshLicenses)
	})
}

// ValidateLicense validates and activates a theme license
func (h *ThemeLicenseHandler) ValidateLicense(w http.ResponseWriter, r *http.Request) {
	var req ValidateLicenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode validate license request")
		RespondJSON(w, http.StatusBadRequest, ValidateLicenseResponse{
			Valid: false,
			Error: "Invalid request body",
		})
		return
	}

	if req.LicenseKey == "" {
		RespondJSON(w, http.StatusBadRequest, ValidateLicenseResponse{
			Valid: false,
			Error: "License key is required",
		})
		return
	}

	// Validate and store license
	license, err := h.themeLicenseService.ValidateAndStoreLicense(r.Context(), req.LicenseKey)
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(req.LicenseKey)).
			Msg("Failed to validate license")

		RespondJSON(w, http.StatusUnauthorized, ValidateLicenseResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	log.Info().
		Str("themeName", license.ThemeName).
		Str("licenseKey", maskLicenseKey(req.LicenseKey)).
		Msg("License validated successfully")

	RespondJSON(w, http.StatusOK, ValidateLicenseResponse{
		Valid:     true,
		ThemeName: license.ThemeName,
		ExpiresAt: license.ExpiresAt,
		Message:   "License validated and activated successfully",
	})
}

// GetLicensedThemes returns premium access status
func (h *ThemeLicenseHandler) GetLicensedThemes(w http.ResponseWriter, r *http.Request) {
	hasPremium, err := h.themeLicenseService.HasPremiumAccess(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to check premium access")
		RespondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to check premium access",
		})
		return
	}

	RespondJSON(w, http.StatusOK, PremiumAccessResponse{
		HasPremiumAccess: hasPremium,
	})
}

// GetAllLicenses returns all licenses for the current user
func (h *ThemeLicenseHandler) GetAllLicenses(w http.ResponseWriter, r *http.Request) {
	licenses, err := h.themeLicenseService.GetAllLicenses(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get licenses")
		RespondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to retrieve licenses",
		})
		return
	}

	// Convert to API response format
	var licenseInfos []LicenseInfo
	for _, license := range licenses {
		licenseInfos = append(licenseInfos, LicenseInfo{
			LicenseKey: license.LicenseKey,
			ThemeName:  license.ThemeName,
			Status:     license.Status,
			CreatedAt:  license.CreatedAt,
		})
	}

	RespondJSON(w, http.StatusOK, licenseInfos)
}

// DeleteLicense removes a license from the system
func (h *ThemeLicenseHandler) DeleteLicense(w http.ResponseWriter, r *http.Request) {
	licenseKey := chi.URLParam(r, "licenseKey")
	if licenseKey == "" {
		RespondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "License key is required",
		})
		return
	}

	err := h.themeLicenseService.DeleteLicense(r.Context(), licenseKey)
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Msg("Failed to delete license")
		RespondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to delete license",
		})
		return
	}

	log.Info().
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License deleted successfully")

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "License deleted successfully",
	})
}

// RefreshLicenses manually triggers a refresh of all licenses
func (h *ThemeLicenseHandler) RefreshLicenses(w http.ResponseWriter, r *http.Request) {
	err := h.themeLicenseService.RefreshAllLicenses(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to refresh licenses")
		RespondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to refresh licenses",
		})
		return
	}

	log.Info().Msg("All licenses refreshed successfully")

	RespondJSON(w, http.StatusOK, map[string]string{
		"message": "All licenses refreshed successfully",
	})
}

// Helper function to mask license keys in logs
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}
