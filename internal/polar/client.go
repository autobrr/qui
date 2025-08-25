// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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
	polarAPIBaseURL   = "https://api.polar.sh/v1"
	validateEndpoint  = "/customer-portal/license-keys/validate"
	activateEndpoint  = "/customer-portal/license-keys/activate"
	premiumThemeName  = "premium-access"
	defaultLabel      = "qui Theme License"
	unknownThemeName  = "unknown"
	requestTimeout    = 30 * time.Second
	contentTypeJSON   = "application/json"
	orgIDNotConfigMsg = "Organization ID not configured"
	licenseFailedMsg  = "Failed to validate license"
	activateFailedMsg = "Failed to activate license"
	invalidRespMsg    = "Invalid license response"
)

// ValidationResponse represents the response from the validate endpoint
type ValidationResponse struct {
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

// ActivationResponse represents the response from the activate endpoint
type ActivationResponse struct {
	LicenseKey LicenseKeyData `json:"license_key"`
}

// LicenseKeyData represents the nested license key data in activation response
type LicenseKeyData struct {
	ID               string     `json:"id"`
	BenefitID        string     `json:"benefit_id"`
	CustomerID       string     `json:"customer_id"`
	Key              string     `json:"key"`
	Status           string     `json:"status"`
	ExpiresAt        *time.Time `json:"expires_at"`
	LimitActivations int        `json:"limit_activations"`
	Usage            int        `json:"usage"`
}

// Client wraps the Polar API for theme license management
type Client struct {
	organizationID string
	httpClient     *http.Client
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

// NewClient creates a new Polar API client with configured HTTP client
func NewClient() *Client {
	return &Client{
		httpClient: &http.Client{
			Timeout: requestTimeout,
			Transport: &http.Transport{
				MaxIdleConns:        10,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     30 * time.Second,
			},
		},
	}
}

// ValidateLicense validates a license key against Polar API
func (c *Client) ValidateLicense(ctx context.Context, licenseKey string) (*LicenseInfo, error) {
	if c.organizationID == "" {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: orgIDNotConfigMsg,
		}, nil
	}

	log.Debug().
		Str("organizationId", c.organizationID).
		Msg("Validating license key with Polar API")

	requestBody := map[string]string{
		"key":             licenseKey,
		"organization_id": c.organizationID,
	}

	body, err := c.makeHTTPRequest(ctx, validateEndpoint, requestBody, false)
	if err != nil {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: licenseFailedMsg,
		}, err
	}

	var response ValidationResponse
	if err := json.Unmarshal(body, &response); err != nil {
		log.Error().Err(err).Msg("Failed to parse validation response")
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: invalidRespMsg,
		}, err
	}

	themeName := c.mapBenefitToTheme(response.BenefitID, "validation")

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

// makeHTTPRequest handles common HTTP request logic for both endpoints
func (c *Client) makeHTTPRequest(ctx context.Context, endpoint string, requestBody map[string]string, isActivation bool) ([]byte, error) {
	jsonData, err := json.Marshal(requestBody)
	if err != nil {
		log.Error().Err(err).Msg("Failed to marshal request body")
		return nil, fmt.Errorf("failed to prepare request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", polarAPIBaseURL+endpoint, bytes.NewBuffer(jsonData))
	if err != nil {
		log.Error().Err(err).Msg("Failed to create HTTP request")
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", contentTypeJSON)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(requestBody["key"])).
			Str("orgId", c.organizationID).
			Msg("HTTP request to Polar API failed")
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Error().Err(err).Msg("Failed to read response body")
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	// Check for successful status codes
	var isSuccess bool
	if isActivation {
		isSuccess = resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusCreated
	} else {
		isSuccess = resp.StatusCode == http.StatusOK
	}

	if !isSuccess {
		log.Error().
			Int("status", resp.StatusCode).
			Str("response", string(body)).
			Str("licenseKey", maskLicenseKey(requestBody["key"])).
			Msg("API request failed")
		return nil, fmt.Errorf("API returned status %d", resp.StatusCode)
	}

	return body, nil
}

// mapBenefitToTheme maps a benefit ID to theme name
func (c *Client) mapBenefitToTheme(benefitID, operation string) string {
	if benefitID == "" {
		return unknownThemeName
	}

	// For our one-time premium access model, any valid benefit should grant premium access
	// This unlocks ALL current and future premium themes
	themeName := premiumThemeName

	log.Debug().
		Str("benefitId", benefitID).
		Str("mappedTheme", themeName).
		Str("operation", operation).
		Msg("Mapped benefit ID to premium access")

	return themeName
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

// SetOrganizationID sets the organization ID required for license operations
func (c *Client) SetOrganizationID(orgID string) {
	c.organizationID = orgID
}

// IsClientConfigured checks if the Polar client is properly configured
func (c *Client) IsClientConfigured() bool {
	return c.organizationID != ""
}
