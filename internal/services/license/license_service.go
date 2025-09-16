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

	// Check if license already exists in database
	existingLicense, err := s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
	if err != nil && !errors.Is(err, models.ErrLicenseNotFound) {
		return nil, fmt.Errorf("failed to check existing license: %w", err)
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

	// If license exists, update it; otherwise create new
	if existingLicense != nil {
		// Update existing license with new activation details
		existingLicense.ProductName = productName
		existingLicense.Status = models.LicenseStatusActive
		existingLicense.ActivatedAt = time.Now()
		existingLicense.ExpiresAt = activateResp.LicenseKey.ExpiresAt
		existingLicense.LastValidated = time.Now()
		existingLicense.PolarCustomerID = &activateResp.LicenseKey.CustomerID
		existingLicense.PolarProductID = &activateResp.LicenseKey.BenefitID
		existingLicense.PolarActivationID = activateResp.Id
		existingLicense.UpdatedAt = time.Now()

		if err := s.licenseRepo.UpdateLicenseActivation(ctx, existingLicense); err != nil {
			return nil, fmt.Errorf("failed to update license activation: %w", err)
		}

		log.Info().
			Str("productName", existingLicense.ProductName).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Msg("License re-activated and updated successfully")

		return existingLicense, nil
	}

	// Create a new license record
	license := &models.ProductLicense{
		LicenseKey:        licenseKey,
		ProductName:       productName,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       time.Now(),
		ExpiresAt:         activateResp.LicenseKey.ExpiresAt,
		LastValidated:     time.Now(),
		PolarCustomerID:   &activateResp.LicenseKey.CustomerID,
		PolarProductID:    &activateResp.LicenseKey.BenefitID,
		PolarActivationID: activateResp.Id,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
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

	log.Trace().Str("fingerprint", fingerprint).Msg("Refreshing licenses")

	for _, license := range licenses {
		// Skip recently validated licenses (within 1 hour)
		if time.Since(license.LastValidated) < time.Hour {
			continue
		}

		// Handle licenses without activation IDs (migrated from old system)
		if license.PolarActivationID == "" {
			log.Info().
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Found license without activation ID, attempting to activate")

			activateReq := polar.ActivateRequest{Key: license.LicenseKey, Label: defaultLabel}
			activateReq.SetCondition("fingerprint", fingerprint)
			activateReq.SetMeta("product", defaultLabel)

			activateResp, err := s.polarClient.Activate(ctx, activateReq)
			if err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg(polar.ActivateFailedMsg)

				// If activation limit is exceeded, mark the license as invalid
				if errors.Is(err, polar.ErrActivationLimitExceeded) {
					if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
						log.Error().
							Err(updateErr).
							Int("licenseId", license.ID).
							Msg("Failed to update license status to invalid")
					}
				}
				// Continue to next license instead of failing entire refresh
				continue
			}

			// Update the license with activation ID
			license.PolarActivationID = activateResp.Id
			license.PolarCustomerID = &activateResp.LicenseKey.CustomerID
			license.PolarProductID = &activateResp.LicenseKey.BenefitID
			license.ActivatedAt = time.Now()
			license.ExpiresAt = activateResp.LicenseKey.ExpiresAt

			// Update in database
			if err := s.licenseRepo.UpdateLicenseActivation(ctx, license); err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("Failed to update license with activation ID")
				continue
			}

			log.Info().
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Str("activationId", activateResp.Id).
				Msg("Successfully activated license and updated activation ID")
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
				Msg(polar.LicenseFailedMsg)
			switch {
			case errors.Is(err, polar.ErrActivationLimitExceeded):
				log.Error().Err(err).Msg("Activation limit exceeded")
				return err
			case errors.Is(err, polar.ErrInvalidLicenseKey):
				return err
			default:
				return err
			}
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

// ValidateLicenses validates all stored licenses against Polar API
func (s *Service) ValidateLicenses(ctx context.Context) (bool, error) {
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		log.Warn().Msg("Polar client not configured, skipping license refresh")
		return false, nil
	}

	licenses, err := s.licenseRepo.GetAllLicenses(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get licenses: %w", err)
	}

	log.Debug().Int("count", len(licenses)).Msg("Refreshing licenses")

	if len(licenses) == 0 {
		return true, nil
	}

	fingerprint, err := machineid.ProtectedID("qui-premium")
	if err != nil {
		return false, fmt.Errorf("failed to get machine ID: %w", err)
	}

	// TEST: Hardcode fingerprint to simulate copied database
	fingerprint = "fake-test-fingerprint-12345"
	log.Debug().Str("TEST_fingerprint", fingerprint).Msg("TESTING: Using fake fingerprint to simulate copied database")

	log.Trace().Str("fingerprint", fingerprint).Msg("Refreshing licenses")

	for _, license := range licenses {
		// Skip recently validated licenses (within 1 hour)
		//if time.Since(license.LastValidated) < time.Hour {
		//	continue
		//}

		// Handle licenses without activation IDs (migrated from old system)
		if license.PolarActivationID == "" {
			log.Info().
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Found license without activation ID, attempting to activate")

			activateReq := polar.ActivateRequest{Key: license.LicenseKey, Label: defaultLabel}
			activateReq.SetCondition("fingerprint", fingerprint)
			activateReq.SetMeta("product", defaultLabel)

			activateResp, err := s.polarClient.Activate(ctx, activateReq)
			if err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg(polar.ActivateFailedMsg)

				// If activation limit is exceeded, mark the license as invalid
				if errors.Is(err, polar.ErrActivationLimitExceeded) {
					if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
						log.Error().
							Err(updateErr).
							Int("licenseId", license.ID).
							Msg("Failed to update license status to invalid")
					}
				}
				// Continue to next license instead of failing entire validation
				continue
			}

			// Update the license with activation ID
			license.PolarActivationID = activateResp.Id
			license.PolarCustomerID = &activateResp.LicenseKey.CustomerID
			license.PolarProductID = &activateResp.LicenseKey.BenefitID
			license.ActivatedAt = time.Now()
			license.ExpiresAt = activateResp.LicenseKey.ExpiresAt

			// Update in database
			if err := s.licenseRepo.UpdateLicenseActivation(ctx, license); err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("Failed to update license with activation ID")
				continue
			}

			log.Info().
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Str("activationId", activateResp.Id).
				Msg("Successfully activated license and updated activation ID")
		}

		log.Info().Msgf("checking license validation...")

		validationRequest := polar.ValidateRequest{Key: license.LicenseKey, ActivationID: license.PolarActivationID}
		validationRequest.SetCondition("fingerprint", fingerprint)

		// Validate with Polar
		licenseInfo, err := s.polarClient.Validate(ctx, validationRequest)
		if err != nil {
			// Log specific error based on type
			switch {
			case errors.Is(err, polar.ErrConditionMismatch):
				log.Error().
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("License fingerprint mismatch - database appears to have been copied from another machine")
			case errors.Is(err, polar.ErrActivationLimitExceeded):
				log.Error().
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("License activation limit exceeded")
			case errors.Is(err, polar.ErrInvalidLicenseKey):
				log.Error().
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("Invalid license key - license does not exist")
			default:
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg(polar.LicenseFailedMsg)
			}

			// Mark license as invalid when validation fails
			if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
				log.Error().
					Err(updateErr).
					Int("licenseId", license.ID).
					Msg("Failed to update license status to invalid")
			}

			return false, err
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

	return true, nil
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
