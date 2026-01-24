// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/services/license"
)

// LicenseHandler handles license related HTTP requests
type LicenseHandler struct {
	licenseService *license.Service
}

// NewLicenseHandler creates a new license handler
func NewLicenseHandler(licenseService *license.Service) *LicenseHandler {
	return &LicenseHandler{
		licenseService: licenseService,
	}
}

// ActivateLicenseRequest represents the request body for license activation
type ActivateLicenseRequest struct {
	LicenseKey string `json:"licenseKey"`
}

// ActivateLicenseResponse represents the response for license activation
type ActivateLicenseResponse struct {
	Valid       bool       `json:"valid"`
	ProductName string     `json:"productName,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Message     string     `json:"message,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// ValidateLicenseRequest represents the request body for license validation
type ValidateLicenseRequest struct {
	LicenseKey string `json:"licenseKey"`
}

// ValidateLicenseResponse represents the response for license validation
type ValidateLicenseResponse struct {
	Valid       bool       `json:"valid"`
	ProductName string     `json:"productName,omitempty"`
	ExpiresAt   *time.Time `json:"expiresAt,omitempty"`
	Message     string     `json:"message,omitempty"`
	Error       string     `json:"error,omitempty"`
}

// PremiumAccessResponse represents the response for premium access status
type PremiumAccessResponse struct {
	HasPremiumAccess bool `json:"hasPremiumAccess"`
}

// LicenseInfo represents basic license information for UI display
type LicenseInfo struct {
	LicenseKey  string    `json:"licenseKey"`
	ProductName string    `json:"productName"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"createdAt"`
}

func (h *LicenseHandler) Routes(r chi.Router) {
	r.Get("/licensed", h.GetLicensedThemes)
	r.Get("/licenses", h.GetAllLicenses)
	r.Post("/activate", h.ActivateLicense)
	r.Post("/validate", h.ValidateLicense)
	r.Post("/refresh", h.RefreshLicenses)
	r.Delete("/{licenseKey}", h.DeleteLicense)
}

// ActivateLicense activates a license
func (h *LicenseHandler) ActivateLicense(w http.ResponseWriter, r *http.Request) {
	var req ActivateLicenseRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode activate license request")
		RespondJSON(w, http.StatusBadRequest, ActivateLicenseResponse{
			Valid: false,
			Error: "Invalid request body",
		})
		return
	}

	if req.LicenseKey == "" {
		RespondJSON(w, http.StatusBadRequest, ActivateLicenseResponse{
			Valid: false,
			Error: "License key is required",
		})
		return
	}

	username := r.Context().Value("username")
	if username == "" || username == nil {
		RespondJSON(w, http.StatusBadRequest, ActivateLicenseResponse{
			Valid: false,
			Error: "Username not found in context",
		})
		return
	}

	// Activate and store license
	licenseResp, err := h.licenseService.ActivateAndStoreLicense(r.Context(), req.LicenseKey, username.(string))
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(req.LicenseKey)).
			Msg("Failed to activate license")

		RespondJSON(w, http.StatusForbidden, ActivateLicenseResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	log.Info().
		Str("productName", licenseResp.ProductName).
		Str("licenseKey", maskLicenseKey(req.LicenseKey)).
		Msg("License activated successfully")

	RespondJSON(w, http.StatusOK, ActivateLicenseResponse{
		Valid:       true,
		ProductName: licenseResp.ProductName,
		ExpiresAt:   licenseResp.ExpiresAt,
		Message:     "License activated successfully",
	})
}

// ValidateLicense validates a license
func (h *LicenseHandler) ValidateLicense(w http.ResponseWriter, r *http.Request) {
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

	username := r.Context().Value("username")
	if username == "" || username == nil {
		RespondJSON(w, http.StatusBadRequest, ActivateLicenseResponse{
			Valid: false,
			Error: "Username not found in context",
		})
		return
	}

	// Validate and store license
	licenseResp, err := h.licenseService.ValidateAndStoreLicense(r.Context(), req.LicenseKey, username.(string))
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(req.LicenseKey)).
			Msg("Failed to validate license")

		if errors.Is(err, license.ErrLicenseNotFound) {
			RespondJSON(w, http.StatusNotFound, ValidateLicenseResponse{
				Valid: false,
				Error: err.Error(),
			})
			return
		}

		RespondJSON(w, http.StatusForbidden, ValidateLicenseResponse{
			Valid: false,
			Error: err.Error(),
		})
		return
	}

	log.Info().
		Str("productName", licenseResp.ProductName).
		Str("licenseKey", maskLicenseKey(req.LicenseKey)).
		Msg("License validated successfully")

	RespondJSON(w, http.StatusOK, ValidateLicenseResponse{
		Valid:       true,
		ProductName: licenseResp.ProductName,
		ExpiresAt:   licenseResp.ExpiresAt,
		Message:     "License validated and activated successfully",
	})
}

// GetLicensedThemes returns premium access status
func (h *LicenseHandler) GetLicensedThemes(w http.ResponseWriter, r *http.Request) {
	hasPremium, err := h.licenseService.HasPremiumAccess(r.Context())
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
func (h *LicenseHandler) GetAllLicenses(w http.ResponseWriter, r *http.Request) {
	licenses, err := h.licenseService.GetAllLicenses(r.Context())
	if err != nil {
		log.Error().Err(err).Msg("Failed to get licenses")
		RespondJSON(w, http.StatusInternalServerError, map[string]string{
			"error": "Failed to retrieve licenses",
		})
		return
	}

	// Convert to API response format
	licenseInfos := make([]LicenseInfo, 0)
	for _, lic := range licenses {
		licenseInfos = append(licenseInfos, LicenseInfo{
			LicenseKey:  lic.LicenseKey,
			ProductName: lic.ProductName,
			Status:      lic.Status,
			CreatedAt:   lic.CreatedAt,
		})
	}

	RespondJSON(w, http.StatusOK, licenseInfos)
}

// DeleteLicense removes a license from the system
func (h *LicenseHandler) DeleteLicense(w http.ResponseWriter, r *http.Request) {
	licenseKey := chi.URLParam(r, "licenseKey")
	if licenseKey == "" {
		RespondJSON(w, http.StatusBadRequest, map[string]string{
			"error": "License key is required",
		})
		return
	}

	err := h.licenseService.DeleteLicense(r.Context(), licenseKey)
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
func (h *LicenseHandler) RefreshLicenses(w http.ResponseWriter, r *http.Request) {
	err := h.licenseService.RefreshAllLicenses(r.Context())
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
