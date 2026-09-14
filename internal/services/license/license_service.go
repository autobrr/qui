// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/models"
)

const (
	offlineGracePeriod = 7 * 24 * time.Hour
	ProductNamePremium = "premium-access"
)

// Service handles license operations
type Service struct {
	licenseRepo *database.LicenseRepo
	dodoClient  *dodo.Client
	configDir   string
}

// NewLicenseService creates a new license service
func NewLicenseService(repo *database.LicenseRepo, dodoClient *dodo.Client, configDir string) *Service {
	return &Service{
		licenseRepo: repo,
		dodoClient:  dodoClient,
		configDir:   configDir,
	}
}

// ActivateAndStoreLicense activates a license key and stores it if valid
func (s *Service) ActivateAndStoreLicense(ctx context.Context, licenseKey string, username string) (*models.ProductLicense, error) {
	// Check if license already exists in database
	existingLicense, err := s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
	if err != nil && !errors.Is(err, models.ErrLicenseNotFound) {
		return nil, fmt.Errorf("failed to check existing license: %w", err)
	}
	if errors.Is(err, models.ErrLicenseNotFound) {
		existingLicense = nil
	}

	fingerprint, err := GetDeviceID("qui-premium", username, s.configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	return s.activateWithDodo(ctx, licenseKey, username, fingerprint, existingLicense)
}

// ValidateAndStoreLicense validates a license key and stores it if valid
func (s *Service) ValidateAndStoreLicense(ctx context.Context, licenseKey string, username string) (*models.ProductLicense, error) {
	// Check if license already exists
	existingLicense, err := s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
	if err != nil {
		return nil, err
	}

	if err := s.validateExistingDodoLicense(ctx, existingLicense); err != nil {
		return nil, err
	}

	log.Info().
		Str("productName", existingLicense.ProductName).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License validated and updated successfully")

	return existingLicense, nil
}

func (s *Service) activateWithDodo(ctx context.Context, licenseKey, username, fingerprint string, existingLicense *models.ProductLicense) (*models.ProductLicense, error) {
	if s.dodoClient == nil {
		return nil, ErrDodoClientNotConfigured
	}

	log.Debug().Msgf("attempting Dodo license activation..")

	activateResp, err := s.dodoClient.Activate(ctx, dodo.ActivateRequest{
		LicenseKey: licenseKey,
		Name:       fingerprint,
	})
	if err != nil {
		return nil, errors.Wrap(err, "failed to activate license")
	}

	instanceID := activateResp.InstanceID
	if instanceID == "" {
		instanceID = activateResp.ID
	}

	now := time.Now()

	if existingLicense != nil {
		existingLicense.ProductName = ProductNamePremium
		existingLicense.Status = models.LicenseStatusActive
		existingLicense.ActivatedAt = now
		existingLicense.ExpiresAt = activateResp.ExpiresAt
		existingLicense.LastValidated = now
		existingLicense.Provider = models.LicenseProviderDodo
		existingLicense.DodoInstanceID = instanceID
		existingLicense.Username = username
		existingLicense.UpdatedAt = now

		if err := s.licenseRepo.UpdateLicenseActivation(ctx, existingLicense); err != nil {
			return nil, fmt.Errorf("failed to update license activation: %w", err)
		}

		log.Info().
			Str("productName", existingLicense.ProductName).
			Str("licenseKey", maskLicenseKey(licenseKey)).
			Msg("License re-activated and updated successfully")

		return existingLicense, nil
	}

	license := &models.ProductLicense{
		LicenseKey:     licenseKey,
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now,
		ExpiresAt:      activateResp.ExpiresAt,
		LastValidated:  now,
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: instanceID,
		Username:       username,
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := s.licenseRepo.StoreLicense(ctx, license); err != nil {
		return nil, fmt.Errorf("failed to store license: %w", err)
	}

	log.Info().
		Str("productName", license.ProductName).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License activated and stored successfully")

	return license, nil
}

func (s *Service) validateExistingDodoLicense(ctx context.Context, license *models.ProductLicense) error {
	if s.dodoClient == nil {
		return ErrDodoClientNotConfigured
	}

	validationResp, err := s.dodoClient.Validate(ctx, dodo.ValidateRequest{
		LicenseKey:           license.LicenseKey,
		LicenseKeyInstanceID: license.DodoInstanceID,
	})
	if err != nil {
		if errors.Is(err, dodo.ErrLicenseNotFound) {
			return models.ErrLicenseNotFound
		}
		return fmt.Errorf("failed to validate license: %w", err)
	}

	if !validationResp.Valid {
		return ErrLicenseNotActive
	}

	license.LastValidated = time.Now()
	if err := s.licenseRepo.UpdateLicenseValidation(ctx, license); err != nil {
		log.Error().Err(err).Msg("Failed to update license validation time")
	}

	instanceID := cmp.Or(license.DodoInstanceID, validationResp.InstanceID)
	if instanceID != license.DodoInstanceID {
		if err := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderDodo, instanceID); err != nil {
			log.Error().Err(err).Msg("Failed to update license provider")
		} else {
			license.DodoInstanceID = instanceID
		}
	}

	return nil
}

func (s *Service) ensureDodoActivation(ctx context.Context, license *models.ProductLicense, fingerprint string) error {
	if license.DodoInstanceID != "" {
		return nil
	}

	if s.dodoClient == nil {
		return ErrDodoClientNotConfigured
	}

	log.Info().
		Str("licenseKey", maskLicenseKey(license.LicenseKey)).
		Msg("Found Dodo license without instance ID, attempting to activate")

	activateResp, err := s.dodoClient.Activate(ctx, dodo.ActivateRequest{
		LicenseKey: license.LicenseKey,
		Name:       fingerprint,
	})
	if err != nil {
		return err
	}

	instanceID := activateResp.InstanceID
	if instanceID == "" {
		instanceID = activateResp.ID
	}

	license.Provider = models.LicenseProviderDodo
	license.DodoInstanceID = instanceID
	license.ActivatedAt = time.Now()
	license.ExpiresAt = activateResp.ExpiresAt
	license.Status = models.LicenseStatusActive

	if err := s.licenseRepo.UpdateLicenseActivation(ctx, license); err != nil {
		return err
	}

	log.Info().
		Str("licenseKey", maskLicenseKey(license.LicenseKey)).
		Str("instanceId", instanceID).
		Msg("Successfully activated Dodo license and updated instance ID")

	return nil
}

// HasPremiumAccess checks if the user has premium access
func (s *Service) HasPremiumAccess(ctx context.Context) (bool, error) {
	return s.licenseRepo.HasPremiumAccess(ctx)
}

// RefreshAllLicenses validates all stored licenses against the Dodo API
func (s *Service) RefreshAllLicenses(ctx context.Context) error {
	licenses, err := s.licenseRepo.GetAllLicenses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get licenses: %w", err)
	}

	log.Debug().Int("count", len(licenses)).Msg("Refreshing licenses")

	if len(licenses) == 0 {
		return nil
	}

	var refreshErr error
	recordRefreshErr := func(err error) {
		if err != nil && refreshErr == nil {
			refreshErr = err
		}
	}

	for _, license := range licenses {
		// Only refresh active licenses. Invalid licenses require an explicit user
		// action (re-activate) and should not be auto-activated in the background.
		if license.Status != models.LicenseStatusActive {
			continue
		}

		// Skip recently validated licenses (within 1 hour)
		if time.Since(license.LastValidated) < time.Hour {
			continue
		}

		if license.Username == "" {
			log.Error().Msg("no username found for license, skipping refresh")
			continue
		}

		fingerprint, err := GetDeviceID("qui-premium", license.Username, s.configDir)
		if err != nil {
			return fmt.Errorf("failed to get machine ID: %w", err)
		}

		log.Trace().Str("fingerprint", fingerprint).Msg("Refreshing licenses")

		if s.dodoClient == nil {
			log.Warn().
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Dodo client not configured, skipping license refresh")
			recordRefreshErr(ErrDodoClientNotConfigured)
			continue
		}
		if err := s.ensureDodoActivation(ctx, license, fingerprint); err != nil {
			log.Error().
				Err(err).
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Failed to activate Dodo license")
			if errors.Is(err, dodo.ErrActivationLimitExceeded) {
				if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
					log.Error().
						Err(updateErr).
						Int("licenseId", license.ID).
						Msg("Failed to update license status to invalid")
				}
			} else {
				recordRefreshErr(err)
			}
			continue
		}

		licenseInfo, err := s.dodoClient.Validate(ctx, dodo.ValidateRequest{
			LicenseKey:           license.LicenseKey,
			LicenseKeyInstanceID: license.DodoInstanceID,
		})
		if err != nil {
			log.Error().
				Err(err).
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Failed to validate Dodo license")
			recordRefreshErr(err)
			continue
		}

		newStatus := models.LicenseStatusActive
		if !licenseInfo.Valid {
			newStatus = models.LicenseStatusInvalid
		}

		if err := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, newStatus); err != nil {
			log.Error().
				Err(err).
				Int("licenseId", license.ID).
				Msg("Failed to update license status")
		}
	}

	return refreshErr
}

// ValidateLicenses validates all stored licenses against the Dodo API
func (s *Service) ValidateLicenses(ctx context.Context) (bool, error) {
	licenses, err := s.licenseRepo.GetAllLicenses(ctx)
	if err != nil {
		return false, fmt.Errorf("failed to get licenses: %w", err)
	}

	log.Debug().Int("count", len(licenses)).Msg("Refreshing licenses")

	if len(licenses) == 0 {
		return true, nil
	}

	allValid := true
	var transientErr error
	handleTransient := func(license *models.ProductLicense, err error) {
		log.Warn().
			Err(err).
			Str("licenseKey", maskLicenseKey(license.LicenseKey)).
			Msg("License validation failed, keeping existing status (soft-fail)")
		if license.Status != models.LicenseStatusActive {
			allValid = false
		}
		if transientErr == nil {
			transientErr = err
		}
	}

	for _, license := range licenses {
		// Only validate active licenses in the background. Invalid licenses
		// require an explicit user action (re-activate) and should not be
		// auto-activated in the checker.
		if license.Status != models.LicenseStatusActive {
			allValid = false
			continue
		}

		if license.Username == "" {
			log.Error().Msg("no username found for license, skipping refresh")
			continue
		}

		fingerprint, err := GetDeviceID("qui-premium", license.Username, s.configDir)
		if err != nil {
			return false, fmt.Errorf("failed to get machine ID: %w", err)
		}

		log.Trace().Str("fingerprint", fingerprint).Msg("Refreshing licenses")

		if s.dodoClient == nil {
			handleTransient(license, ErrDodoClientNotConfigured)
			continue
		}

		if err := s.ensureDodoActivation(ctx, license, fingerprint); err != nil {
			log.Error().
				Err(err).
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Failed to activate Dodo license")
			if errors.Is(err, dodo.ErrActivationLimitExceeded) {
				if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
					log.Error().
						Err(updateErr).
						Int("licenseId", license.ID).
						Msg("Failed to update license status to invalid")
				}
				allValid = false
			} else {
				handleTransient(license, err)
			}
			continue
		}

		licenseInfo, err := s.dodoClient.Validate(ctx, dodo.ValidateRequest{
			LicenseKey:           license.LicenseKey,
			LicenseKeyInstanceID: license.DodoInstanceID,
		})
		if err != nil {
			switch {
			case errors.Is(err, dodo.ErrLicenseNotFound), errors.Is(err, dodo.ErrInvalidLicenseKey), errors.Is(err, dodo.ErrActivationLimitExceeded):
				log.Error().
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("Invalid Dodo license key")
				allValid = false
				if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
					log.Error().
						Err(updateErr).
						Int("licenseId", license.ID).
						Msg("Failed to update license status to invalid")
				}
			default:
				handleTransient(license, err)
			}
			continue
		}

		newStatus := models.LicenseStatusActive
		if !licenseInfo.Valid {
			newStatus = models.LicenseStatusInvalid
			allValid = false
		}

		if err := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, newStatus); err != nil {
			log.Error().
				Err(err).
				Int("licenseId", license.ID).
				Msg("Failed to update license status")
		}
	}

	// Invalid state has priority over transient backend failures: report
	// deterministic invalid result and suppress soft-fail errors.
	if !allValid && transientErr != nil {
		return allValid, nil
	}

	return allValid, transientErr
}

func (s *Service) GetLicenseByKey(ctx context.Context, licenseKey string) (*models.ProductLicense, error) {
	return s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
}

func (s *Service) GetAllLicenses(ctx context.Context) ([]*models.ProductLicense, error) {
	return s.licenseRepo.GetAllLicenses(ctx)
}

func (s *Service) DeleteLicense(ctx context.Context, licenseKey string) error {
	license, err := s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
	if err != nil {
		return err
	}

	if license.DodoInstanceID != "" {
		if s.dodoClient == nil {
			log.Warn().
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Dodo client not configured, skipping remote deactivation")
		} else if _, err := s.dodoClient.Deactivate(ctx, dodo.DeactivateRequest{
			LicenseKey:           license.LicenseKey,
			LicenseKeyInstanceID: license.DodoInstanceID,
			InstanceID:           license.DodoInstanceID,
		}); err != nil && !errors.Is(err, dodo.ErrInstanceNotFound) && !errors.Is(err, dodo.ErrLicenseNotFound) {
			log.Warn().
				Err(err).
				Str("licenseKey", maskLicenseKey(license.LicenseKey)).
				Msg("Failed to deactivate Dodo license remotely, deleting local license anyway")
		}
	}

	deleteCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()

	return s.licenseRepo.DeleteLicense(deleteCtx, licenseKey)
}

// Helper function to mask license keys in logs
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}
