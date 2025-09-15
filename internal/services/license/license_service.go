// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"fmt"
	"time"

	"github.com/keygen-sh/machineid"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/polar"
)

var (
	ErrLicenseNotFound = errors.New("license not found")
)

// Service handles license operations
type Service struct {
	db          *database.DB
	licenseRepo *database.LicenseRepo
	polarClient *polar.Client
}

// NewLicenseService creates a new license service
func NewLicenseService(repo *database.LicenseRepo, polarClient *polar.Client) *Service {
	return &Service{
		licenseRepo: repo,
		polarClient: polarClient,
	}
}

// ActivateAndStoreLicense activates a license key and stores it if valid
func (s *Service) ActivateAndStoreLicense(ctx context.Context, licenseKey string) (*models.ProductLicense, error) {
	// Validate with Polar API
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		return nil, fmt.Errorf("polar client not configured")
	}

	fingerprint, err := machineid.ProtectedID("qui-premium")
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	log.Debug().Msgf("attempting license activation..")

	activateReq := polar.ActivateRequest{Key: licenseKey, Label: defaultLabel}
	activateReq.SetCondition("fingerprint", fingerprint)
	activateReq.SetMeta("product", defaultLabel)

	activateResp, err := s.polarClient.Activate(ctx, activateReq)
	switch {
	case errors.Is(err, polar.ErrActivationLimitExceeded):
		return nil, fmt.Errorf("activation limit exceeded")
	case err != nil:
		return nil, errors.Wrapf(err, "failed to activate license key: %s", licenseKey)
	}

	log.Info().Msgf("license successfully activated!")

	validationReq := polar.ValidateRequest{Key: licenseKey, ActivationID: activateResp.Id}
	validationReq.SetCondition("fingerprint", fingerprint)

	validationResp, err := s.polarClient.Validate(ctx, validationReq)
	if err != nil {
		return nil, fmt.Errorf("failed to validate license: %w", err)
	}

	if validationResp.Status != "granted" {
		return nil, fmt.Errorf("validation error: %s", validationResp.Status)
	}

	log.Debug().Msgf("license successfully validated!")

	productName := mapBenefitToProduct(activateResp.LicenseKey.BenefitID, "validation")

	// Create a license record
	license := &models.ProductLicense{
		LicenseKey:        licenseKey,
		ProductName:       productName,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       time.Now(),
		ExpiresAt:         activateResp.LicenseKey.ExpiresAt,
		LastValidated:     activateResp.CreatedAt,
		PolarCustomerID:   &activateResp.LicenseKey.CustomerID,
		PolarProductID:    &activateResp.LicenseKey.BenefitID,
		PolarActivationID: activateResp.Id,
		CreatedAt:         activateResp.CreatedAt,
		UpdatedAt:         activateResp.ModifiedAt,
	}

	// Store in the database
	if err := s.licenseRepo.StoreLicense(ctx, license); err != nil {
		return nil, fmt.Errorf("failed to store license: %w", err)
	}

	log.Info().
		Str("productName", license.ProductName).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License validated and stored successfully")

	return license, nil
}

// ValidateAndStoreLicense validates a license key and stores it if valid
func (s *Service) ValidateAndStoreLicense(ctx context.Context, licenseKey string) (*models.ProductLicense, error) {
	// Validate with Polar API
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		return nil, fmt.Errorf("polar client not configured")
	}

	// Check if license already exists
	existingLicense, err := s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
	if err != nil {
		return nil, err
	}

	fingerprint, err := machineid.ProtectedID("qui-premium")
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	validationReq := polar.ValidateRequest{Key: licenseKey, ActivationID: existingLicense.PolarActivationID}
	validationReq.SetCondition("fingerprint", fingerprint)

	validationResp, err := s.polarClient.Validate(ctx, validationReq)
	if err != nil {
		return nil, fmt.Errorf("failed to validate license: %w", err)
	}

	if validationResp.Status != "granted" {
		return nil, fmt.Errorf("validation error: %s", validationResp.Status)
	}

	// License already exists, update validation time and return
	existingLicense.LastValidated = time.Now()
	if err := s.licenseRepo.UpdateLicenseValidation(ctx, existingLicense); err != nil {
		log.Error().Err(err).Msg("Failed to update license validation time")
	}

	log.Info().
		Str("productName", existingLicense.ProductName).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License validated and updated successfully")

	return existingLicense, nil
}

// HasPremiumAccess checks if the user has premium access
func (s *Service) HasPremiumAccess(ctx context.Context) (bool, error) {
	return s.licenseRepo.HasPremiumAccess(ctx)
}

// RefreshAllLicenses validates all stored licenses against Polar API
func (s *Service) RefreshAllLicenses(ctx context.Context) error {
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		log.Warn().Msg("Polar client not configured, skipping license refresh")
		return nil
	}

	licenses, err := s.licenseRepo.GetAllLicenses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get licenses: %w", err)
	}

	log.Debug().Int("count", len(licenses)).Msg("Refreshing licenses")

	if len(licenses) == 0 {
		return nil
	}

	fingerprint, err := machineid.ProtectedID("qui-premium")
	if err != nil {
		return fmt.Errorf("failed to get machine ID: %w", err)
	}

	log.Debug().Str("fingerprint", fingerprint).Msg("Refreshing licenses")

	for _, license := range licenses {
		// Skip recently validated licenses (within 1 hour)
		if time.Since(license.LastValidated) < time.Hour {
			continue
		}

		log.Info().Msgf("checking license validation...")

		validationRequest := polar.ValidateRequest{Key: license.LicenseKey, ActivationID: license.PolarActivationID}
		validationRequest.SetCondition("fingerprint", fingerprint)

		// Validate with Polar
		licenseInfo, err := s.polarClient.Validate(ctx, validationRequest)
		if err != nil {
			log.Error().
				Err(err).
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Failed to validate license during refresh")
			continue
		}

		// Update status
		newStatus := models.LicenseStatusActive
		if !licenseInfo.ValidLicense() {
			newStatus = models.LicenseStatusInvalid
		}

		if err := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, newStatus); err != nil {
			log.Error().
				Err(err).
				Int("licenseId", license.ID).
				Msg("Failed to update license status")
		}
	}

	return nil
}

func (s *Service) GetLicenseByKey(ctx context.Context, licenseKey string) (*models.ProductLicense, error) {
	return s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
}

func (s *Service) GetAllLicenses(ctx context.Context) ([]*models.ProductLicense, error) {
	return s.licenseRepo.GetAllLicenses(ctx)
}

func (s *Service) DeleteLicense(ctx context.Context, licenseKey string) error {
	return s.licenseRepo.DeleteLicense(ctx, licenseKey)
}

// Helper function to mask license keys in logs
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}

const (
	ProductNamePremium = "premium-access"
	ProductNameUnknown = "unknown"
	defaultLabel       = "qui Premium License"
)

// mapBenefitToProduct maps a benefit ID to product name
func mapBenefitToProduct(benefitID, operation string) string {
	if benefitID == "" {
		return ProductNameUnknown
	}

	// For our one-time premium access model, any valid benefit should grant premium access
	// This unlocks ALL current and future premium themes
	name := ProductNamePremium

	log.Trace().
		Str("benefitId", benefitID).
		Str("mappedProduct", name).
		Str("operation", operation).
		Msg("Mapped benefit ID to premium access")

	return name
}
