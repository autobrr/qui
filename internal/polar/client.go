// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package polar

import (
	"context"
	"fmt"
	"time"

	polargo "github.com/polarsource/polar-go"
	"github.com/polarsource/polar-go/models/components"
	"github.com/rs/zerolog/log"
)

// Client wraps the Polar SDK for theme license management
type Client struct {
	polar          *polargo.Polar
	accessToken    string
	environment    string
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
func NewClient(accessToken, environment string) *Client {
	// Always use production environment
	client := polargo.New(
		polargo.WithSecurity(accessToken),
	)

	return &Client{
		polar:       client,
		accessToken: accessToken,
		environment: environment,
	}
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
		Str("environment", c.environment).
		Str("organizationId", c.organizationID).
		Msg("Validating license key with Polar API")

	// Use the validate endpoint for license keys
	validateRes, err := c.polar.CustomerPortal.LicenseKeys.Validate(ctx, components.LicenseKeyValidate{
		Key:            licenseKey,
		OrganizationID: c.organizationID,
	})

	if err != nil {
		log.Error().
			Err(err).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Str("orgId", c.organizationID).
			Msg("Failed to validate license key with Polar API")

		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: fmt.Sprintf("Failed to validate license: %v", err),
		}, err
	}

	if validateRes.ValidatedLicenseKey == nil {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Invalid response from Polar API",
		}, nil
	}

	// Extract license info from validation response
	licenseData := validateRes.ValidatedLicenseKey

	// Map benefit ID to theme name for our premium access model
	themeName := "unknown"
	if licenseData.BenefitID != "" {
		// For our one-time premium access model, any valid benefit should grant "premium-access"
		// This unlocks ALL current and future premium themes
		themeName = "premium-access"

		log.Debug().
			Str("benefitId", licenseData.BenefitID).
			Str("mappedTheme", themeName).
			Msg("Mapped benefit ID to premium access")
	}

	// Extract customer ID
	customerID := ""
	if licenseData.CustomerID != "" {
		customerID = licenseData.CustomerID
	}

	// Extract product ID from benefit ID
	productID := ""
	if licenseData.BenefitID != "" {
		productID = licenseData.BenefitID
	}

	// Extract expiration date if available
	var expiresAt *time.Time
	if licenseData.ExpiresAt != nil {
		expiresAt = licenseData.ExpiresAt
	}

	log.Info().
		Str("themeName", themeName).
		Str("customerID", maskID(customerID)).
		Str("productID", maskID(productID)).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License key validated successfully")

	return &LicenseInfo{
		Key:        licenseKey,
		ThemeName:  themeName,
		CustomerID: customerID,
		ProductID:  productID,
		ExpiresAt:  expiresAt,
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
		Str("environment", c.environment).
		Str("organizationId", c.organizationID).
		Msg("Activating license key with Polar API")

	// Use the license key activation endpoint
	res, err := c.polar.CustomerPortal.LicenseKeys.Activate(ctx, components.LicenseKeyActivate{
		Key:            licenseKey,
		OrganizationID: c.organizationID,
		Label:          "qui Theme License",
	})

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

	if res.LicenseKeyActivationRead == nil {
		return &LicenseInfo{
			Key:          licenseKey,
			Valid:        false,
			ErrorMessage: "Invalid response from Polar API",
		}, nil
	}

	licenseData := res.LicenseKeyActivationRead

	// Extract information from the nested license key
	var themeName, customerID, productID string
	var expiresAt *time.Time

	// Map benefit ID to theme name for our premium access model
	themeName = "unknown"
	if licenseData.LicenseKey.BenefitID != "" {
		// For our one-time premium access model, any valid benefit should grant "premium-access"
		// This unlocks ALL current and future premium themes
		themeName = "premium-access"

		log.Debug().
			Str("benefitId", licenseData.LicenseKey.BenefitID).
			Str("mappedTheme", themeName).
			Msg("Mapped benefit ID to premium access (activation)")
	}

	// Extract customer ID
	if licenseData.LicenseKey.CustomerID != "" {
		customerID = licenseData.LicenseKey.CustomerID
	}

	// Extract product ID from benefit ID (for now use benefit ID)
	// TODO: Query products API using benefit ID to get actual product details
	if licenseData.LicenseKey.BenefitID != "" {
		productID = licenseData.LicenseKey.BenefitID
	}

	// Extract expiration date if available
	if licenseData.LicenseKey.ExpiresAt != nil {
		expiresAt = licenseData.LicenseKey.ExpiresAt
	}

	log.Info().
		Str("themeName", themeName).
		Str("customerID", maskID(customerID)).
		Str("productID", maskID(productID)).
		Msg("License key activated successfully")

	return &LicenseInfo{
		Key:        licenseKey,
		ThemeName:  themeName,
		CustomerID: customerID,
		ProductID:  productID,
		ExpiresAt:  expiresAt,
		Valid:      true,
	}, nil
}

// GetLicenseInfo retrieves license information without activating
func (c *Client) GetLicenseInfo(ctx context.Context, licenseKey string) (*LicenseInfo, error) {
	return c.ValidateLicense(ctx, licenseKey)
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
	return c.accessToken != "" && c.polar != nil && c.organizationID != ""
}

// GetEnvironment returns the current environment
func (c *Client) GetEnvironment() string {
	return c.environment
}

// ValidateConfiguration validates the client configuration
func (c *Client) ValidateConfiguration(ctx context.Context) error {
	if c.accessToken == "" || c.polar == nil {
		return fmt.Errorf("polar client not configured")
	}

	if c.organizationID == "" {
		return fmt.Errorf("organization ID not configured")
	}

	// Test the connection with a simple API call (if needed)
	// Note: We don't test here to avoid unnecessary API calls during initialization

	return nil
}
