// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: MIT

package polar

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"
)

const (
	polarAPIBaseURL  = "https://api.polar.sh/v1"
	validateEndpoint = "/customer-portal/license-keys/validate"
	activateEndpoint = "/customer-portal/license-keys/activate"
)

// Client wraps the Polar API for theme license management
type Client struct {
	organizationID string
}

// LicenseInfo contains license validation information
type LicenseInfo struct {
	Key          string     `json:"key"`
	ThemeName    string     `json:"themeName"`
	CustomerID   string     `json:"customerId"`
	ProductID    string     `json:"productId"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Valid        bool       `json:"valid"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
}

// NewClient creates a new Polar API client
func NewClient() *Client {
	return &Client{}
}

// SetOrganizationID sets the organization ID required for license operations
func (c *Client) SetOrganizationID(orgID string) {
	c.organizationID = orgID
}

// ValidateLicense validates a license key against Polar API
func (c *Client) ValidateLicense(ctx context.Context, licenseKey string) (*LicenseInfo, error) {
	if c.organizationID == "" {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Organization ID not configured",
		}, nil
	}

	log.Debug().
		Str("organizationId", c.organizationID).
		Msg("Validating license key with Polar API")

	// Prepare request body
	requestBody := map[string]string{
		"key":             licenseKey,
		"organization_id": c.organizationID,
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		log.Error().Err(err).Msg("Failed to prepare validation request")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to validate license",
		}, err
	}

	// Make HTTP request to Polar API
	req, err := http.NewRequestWithContext(ctx, "POST", polarAPIBaseURL+validateEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create validation request")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to validate license",
		}, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Str("orgId", c.organizationID).
			Msg("Failed to validate license key with Polar API")

		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to validate license",
		}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read validation response")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to validate license",
		}, err
	}

	// Check if request was successful
	if resp.StatusCode != http.StatusOK {
		log.Error().
			Int("status", resp.StatusCode).
			Str("response", string(body)).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Msg("License validation failed")

		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Invalid license key",
		}, nil
	}

	// Parse response
	var response struct {
		ID               string     `json:"id"`
		BenefitID        string     `json:"benefit_id"`
		CustomerID       string     `json:"customer_id"`
		Key              string     `json:"key"`
		Status           string     `json:"status"`
		ExpiresAt        *time.Time `json:"expires_at"`
		LimitActivations int        `json:"limit_activations"`
		Usage            int        `json:"usage"`
		Validations      int        `json:"validations"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		log.Error().Err(err).Msg("Failed to parse validation response")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Invalid license response",
		}, err
	}

	// Map benefit ID to theme name for our premium access model
	themeName := "unknown"
	if response.BenefitID != "" {
		// For our one-time premium access model, any valid benefit should grant "premium-access"
		// This unlocks ALL current and future premium themes
		themeName = "premium-access"

		log.Debug().
			Str("benefitId", response.BenefitID).
			Str("mappedTheme", themeName).
			Msg("Mapped benefit ID to premium access")
	}

	log.Info().
		Str("themeName", themeName).
		Str("customerID", maskID(response.CustomerID)).
		Str("productID", maskID(response.BenefitID)).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License key validated successfully")

	return &LicenseInfo{
		Key:        licenseKey,
		ThemeName:  themeName,
		CustomerID: response.CustomerID,
		ProductID:  response.BenefitID,
		ExpiresAt:  response.ExpiresAt,
		Valid:      true,
	}, nil
}

// ActivateLicense activates a license key
func (c *Client) ActivateLicense(ctx context.Context, licenseKey string) (*LicenseInfo, error) {
	if c.organizationID == "" {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Organization ID not configured",
		}, nil
	}

	log.Debug().
		Str("organizationId", c.organizationID).
		Msg("Activating license key with Polar API")

	// Prepare request body
	requestBody := map[string]string{
		"key":             licenseKey,
		"organization_id": c.organizationID,
		"label":           "qui Theme License",
	}

	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		log.Error().Err(err).Msg("Failed to prepare validation request")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to validate license",
		}, err
	}

	// Make HTTP request to Polar API (no authentication needed)
	req, err := http.NewRequestWithContext(ctx, "POST", polarAPIBaseURL+activateEndpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: fmt.Sprintf("Failed to create request: %v", err),
		}, err
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Msg("Failed to activate license key with Polar API")

		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: fmt.Sprintf("Failed to activate license: %v", err),
		}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read activation response")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to activate license",
		}, err
	}

	// Check if request was successful
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		log.Error().
			Int("status", resp.StatusCode).
			Str("response", string(body)).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Msg("License activation failed")

		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Failed to activate license",
		}, nil
	}

	// Parse response
	var response struct {
		LicenseKey struct {
			ID               string     `json:"id"`
			BenefitID        string     `json:"benefit_id"`
			CustomerID       string     `json:"customer_id"`
			Key              string     `json:"key"`
			Status           string     `json:"status"`
			ExpiresAt        *time.Time `json:"expires_at"`
			LimitActivations int        `json:"limit_activations"`
			Usage            int        `json:"usage"`
		} `json:"license_key"`
	}

	if err := json.Unmarshal(body, &response); err != nil {
		log.Error().Err(err).Msg("Failed to parse validation response")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Invalid license response",
		}, err
	}

	// Map benefit ID to theme name for our premium access model
	themeName := "unknown"
	if response.LicenseKey.BenefitID != "" {
		// For our one-time premium access model, any valid benefit should grant "premium-access"
		// This unlocks ALL current and future premium themes
		themeName = "premium-access"

		log.Debug().
			Str("benefitId", response.LicenseKey.BenefitID).
			Str("mappedTheme", themeName).
			Msg("Mapped benefit ID to premium access (activation)")
	}

	log.Info().
		Str("themeName", themeName).
		Str("customerID", maskID(response.LicenseKey.CustomerID)).
		Str("productID", maskID(response.LicenseKey.BenefitID)).
		Msg("License key activated successfully")

	return &LicenseInfo{
		Key:        licenseKey,
		ThemeName:  themeName,
		CustomerID: response.LicenseKey.CustomerID,
		ProductID:  response.LicenseKey.BenefitID,
		ExpiresAt:  response.LicenseKey.ExpiresAt,
		Valid:      true,
	}, nil
}

// Helper functions

// maskLicenseKey masks a license key for logging (shows first 8 chars + ***)
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}

// maskID masks an ID for logging (shows first 8 chars + ***)
func maskID(id string) string {
	if len(id) <= 8 {
		return "***"
	}
	return id[:8] + "***"
}

// IsClientConfigured checks if the Polar client is properly configured
func (c *Client) IsClientConfigured() bool {
	return c.organizationID != ""
}

// ValidateConfiguration validates the client configuration
func (c *Client) ValidateConfiguration(ctx context.Context) error {
	if c.organizationID == "" {
		return fmt.Errorf("organization ID not configured")
	}

	// No authentication needed, so no connection test required
	return nil
}
