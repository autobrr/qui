// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/polar"
)

var (
	ErrLicenseNotFound = errors.New("license not found")
)

const offlineGracePeriod = 7 * 24 * time.Hour

// Service handles license operations
type Service struct {
	db          *database.DB
	licenseRepo *database.LicenseRepo
	polarClient *polar.Client
	dodoClient  *dodo.Client
	configDir   string
}

// NewLicenseService creates a new license service
func NewLicenseService(repo *database.LicenseRepo, polarClient *polar.Client, dodoClient *dodo.Client, configDir string) *Service {
	return &Service{
		licenseRepo: repo,
		polarClient: polarClient,
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

	provider := ""
	if existingLicense != nil {
		provider = normalizeProvider(existingLicense.Provider)
	}

	switch provider {
	case models.LicenseProviderDodo:
		return s.activateWithDodo(ctx, licenseKey, username, fingerprint, existingLicense)
	case models.LicenseProviderPolar:
		return s.activateWithPolar(ctx, licenseKey, username, fingerprint, existingLicense)
	default:
		return s.activateWithDodo(ctx, licenseKey, username, fingerprint, existingLicense)
	}
}

// ValidateAndStoreLicense validates a license key and stores it if valid
func (s *Service) ValidateAndStoreLicense(ctx context.Context, licenseKey string, username string) (*models.ProductLicense, error) {
	// Check if license already exists
	existingLicense, err := s.licenseRepo.GetLicenseByKey(ctx, licenseKey)
	if err != nil {
		return nil, err
	}

	fingerprint, err := GetDeviceID("qui-premium", username, s.configDir)
	if err != nil {
		return nil, fmt.Errorf("failed to get machine ID: %w", err)
	}

	provider := normalizeProvider(existingLicense.Provider)
	switch provider {
	case models.LicenseProviderDodo:
		if err := s.validateExistingDodoLicense(ctx, existingLicense); err != nil {
			return nil, err
		}
	case models.LicenseProviderPolar:
		if err := s.validateExistingPolarLicense(ctx, existingLicense, fingerprint); err != nil {
			return nil, err
		}
	default:
		if err := s.validateExistingDodoLicense(ctx, existingLicense); err != nil {
			return nil, err
		}
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

	productName := ProductNamePremium

	if existingLicense != nil {
		existingLicense.ProductName = productName
		existingLicense.Status = models.LicenseStatusActive
		existingLicense.ActivatedAt = time.Now()
		existingLicense.ExpiresAt = activateResp.ExpiresAt
		existingLicense.LastValidated = time.Now()
		existingLicense.Provider = models.LicenseProviderDodo
		existingLicense.DodoInstanceID = instanceID
		existingLicense.PolarCustomerID = nil
		existingLicense.PolarProductID = nil
		existingLicense.PolarActivationID = ""
		existingLicense.Username = username
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

	license := &models.ProductLicense{
		LicenseKey:     licenseKey,
		ProductName:    productName,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    time.Now(),
		ExpiresAt:      activateResp.ExpiresAt,
		LastValidated:  time.Now(),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: instanceID,
		Username:       username,
		CreatedAt:      time.Now(),
		UpdatedAt:      time.Now(),
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

func (s *Service) activateWithPolar(ctx context.Context, licenseKey, username, fingerprint string, existingLicense *models.ProductLicense) (*models.ProductLicense, error) {
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		return nil, errors.New("polar client not configured")
	}

	log.Debug().Msgf("attempting Polar license activation..")

	activateReq := polar.ActivateRequest{Key: licenseKey, Label: defaultLabel}
	activateReq.SetCondition("fingerprint", fingerprint)
	activateReq.SetMeta("product", defaultLabel)

	activateResp, err := s.polarClient.Activate(ctx, activateReq)
	switch {
	case errors.Is(err, polar.ErrActivationLimitExceeded):
		return nil, errors.New("activation limit exceeded")
	case err != nil:
		return nil, errors.Wrap(err, "failed to activate license")
	}

	log.Info().Msgf("license successfully activated!")

	productName := mapBenefitToProduct(activateResp.LicenseKey.BenefitID, "activation")

	if existingLicense != nil {
		existingLicense.ProductName = productName
		existingLicense.Status = models.LicenseStatusActive
		existingLicense.ActivatedAt = time.Now()
		existingLicense.ExpiresAt = activateResp.LicenseKey.ExpiresAt
		existingLicense.LastValidated = time.Now()
		existingLicense.Provider = models.LicenseProviderPolar
		existingLicense.DodoInstanceID = ""
		existingLicense.PolarCustomerID = &activateResp.LicenseKey.CustomerID
		existingLicense.PolarProductID = &activateResp.LicenseKey.BenefitID
		existingLicense.PolarActivationID = activateResp.Id
		existingLicense.Username = username
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

	license := &models.ProductLicense{
		LicenseKey:        licenseKey,
		ProductName:       productName,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       time.Now(),
		ExpiresAt:         activateResp.LicenseKey.ExpiresAt,
		LastValidated:     time.Now(),
		Provider:          models.LicenseProviderPolar,
		PolarCustomerID:   &activateResp.LicenseKey.CustomerID,
		PolarProductID:    &activateResp.LicenseKey.BenefitID,
		PolarActivationID: activateResp.Id,
		Username:          username,
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
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
		return fmt.Errorf("failed to validate license: %w", err)
	}

	if !validationResp.Valid {
		return ErrLicenseNotActive
	}

	license.LastValidated = time.Now()
	if err := s.licenseRepo.UpdateLicenseValidation(ctx, license); err != nil {
		log.Error().Err(err).Msg("Failed to update license validation time")
	}

	instanceID := license.DodoInstanceID
	if instanceID == "" {
		instanceID = validationResp.InstanceID
	}

	if license.Provider != models.LicenseProviderDodo || instanceID != license.DodoInstanceID {
		if err := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderDodo, instanceID); err != nil {
			log.Error().Err(err).Msg("Failed to update license provider")
		} else {
			license.Provider = models.LicenseProviderDodo
			license.DodoInstanceID = instanceID
		}
	}

	return nil
}

func (s *Service) validateExistingPolarLicense(ctx context.Context, license *models.ProductLicense, fingerprint string) error {
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		return errors.New("polar client not configured")
	}

	validationReq := polar.ValidateRequest{Key: license.LicenseKey, ActivationID: license.PolarActivationID}
	validationReq.SetCondition("fingerprint", fingerprint)

	validationResp, err := s.polarClient.Validate(ctx, validationReq)
	if err != nil {
		return fmt.Errorf("failed to validate license: %w", err)
	}

	if validationResp.Status != "granted" {
		return fmt.Errorf("validation error: %s", validationResp.Status)
	}

	license.LastValidated = time.Now()
	if err := s.licenseRepo.UpdateLicenseValidation(ctx, license); err != nil {
		log.Error().Err(err).Msg("Failed to update license validation time")
	}

	if license.Provider != models.LicenseProviderPolar {
		if err := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderPolar, ""); err != nil {
			log.Error().Err(err).Msg("Failed to update license provider")
		} else {
			license.Provider = models.LicenseProviderPolar
		}
	}

	return nil
}

func (s *Service) ensureDodoActivation(ctx context.Context, license *models.ProductLicense, fingerprint string) error {
	if license.DodoInstanceID != "" {
		if license.Provider != models.LicenseProviderDodo {
			if err := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderDodo, license.DodoInstanceID); err != nil {
				log.Error().Err(err).Msg("Failed to update license provider")
			}
		}
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

func (s *Service) ensurePolarActivation(ctx context.Context, license *models.ProductLicense, fingerprint string) error {
	if license.PolarActivationID != "" {
		if license.Provider != models.LicenseProviderPolar {
			if err := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderPolar, ""); err != nil {
				log.Error().Err(err).Msg("Failed to update license provider")
			}
		}
		return nil
	}

	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		return errors.New("polar client not configured")
	}

	log.Info().
		Str("licenseKey", maskLicenseKey(license.LicenseKey)).
		Msg("Found license without activation ID, attempting to activate")

	activateReq := polar.ActivateRequest{Key: license.LicenseKey, Label: defaultLabel}
	activateReq.SetCondition("fingerprint", fingerprint)
	activateReq.SetMeta("product", defaultLabel)

	activateResp, err := s.polarClient.Activate(ctx, activateReq)
	if err != nil {
		return err
	}

	license.Provider = models.LicenseProviderPolar
	license.DodoInstanceID = ""
	license.PolarActivationID = activateResp.Id
	license.PolarCustomerID = &activateResp.LicenseKey.CustomerID
	license.PolarProductID = &activateResp.LicenseKey.BenefitID
	license.ActivatedAt = time.Now()
	license.ExpiresAt = activateResp.LicenseKey.ExpiresAt

	if err := s.licenseRepo.UpdateLicenseActivation(ctx, license); err != nil {
		return err
	}

	log.Info().
		Str("licenseKey", maskLicenseKey(license.LicenseKey)).
		Str("activationId", activateResp.Id).
		Msg("Successfully activated license and updated activation ID")

	return nil
}

// HasPremiumAccess checks if the user has premium access
func (s *Service) HasPremiumAccess(ctx context.Context) (bool, error) {
	return s.licenseRepo.HasPremiumAccess(ctx)
}

// RefreshAllLicenses validates all stored licenses against Polar API
func (s *Service) RefreshAllLicenses(ctx context.Context) error {
	licenses, err := s.licenseRepo.GetAllLicenses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get licenses: %w", err)
	}

	log.Debug().Int("count", len(licenses)).Msg("Refreshing licenses")

	if len(licenses) == 0 {
		return nil
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

		switch normalizeProvider(license.Provider) {
		case models.LicenseProviderDodo:
			if s.dodoClient == nil {
				return ErrDodoClientNotConfigured
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
				return err
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
		case models.LicenseProviderPolar:
			if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
				log.Warn().Msg("Polar client not configured, skipping license refresh")
				continue
			}

			if err := s.ensurePolarActivation(ctx, license, fingerprint); err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg(polar.ActivateFailedMsg)
				if errors.Is(err, polar.ErrActivationLimitExceeded) {
					if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
						log.Error().
							Err(updateErr).
							Int("licenseId", license.ID).
							Msg("Failed to update license status to invalid")
					}
				}
				continue
			}

			validationRequest := polar.ValidateRequest{Key: license.LicenseKey, ActivationID: license.PolarActivationID}
			validationRequest.SetCondition("fingerprint", fingerprint)

			licenseInfo, err := s.polarClient.Validate(ctx, validationRequest)
			if err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg(polar.LicenseFailedMsg)
				return err
			}

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
		default:
			if s.dodoClient == nil {
				return ErrDodoClientNotConfigured
			}
			licenseInfo, err := s.dodoClient.Validate(ctx, dodo.ValidateRequest{
				LicenseKey: license.LicenseKey,
			})
			if err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg("Failed to validate Dodo license")
				return err
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
			instanceID := licenseInfo.InstanceID
			if instanceID == "" {
				instanceID = license.DodoInstanceID
			}
			if updateErr := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderDodo, instanceID); updateErr != nil {
				log.Error().Err(updateErr).Int("licenseId", license.ID).Msg("Failed to store Dodo provider")
			}
			continue
		}
	}

	return nil
}

// ValidateLicenses validates all stored licenses against Polar API
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

		// Skip recently validated licenses (within 1 hour)
		//if time.Since(license.LastValidated) < time.Hour {
		//	continue
		//}

		if license.Username == "" {
			log.Error().Msg("no username found for license, skipping refresh")
			continue
		}

		fingerprint, err := GetDeviceID("qui-premium", license.Username, s.configDir)
		if err != nil {
			return false, fmt.Errorf("failed to get machine ID: %w", err)
		}

		log.Trace().Str("fingerprint", fingerprint).Msg("Refreshing licenses")

		switch normalizeProvider(license.Provider) {
		case models.LicenseProviderDodo:
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
		case models.LicenseProviderPolar:
			if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
				log.Warn().Msg("Polar client not configured, skipping license refresh")
				allValid = false
				continue
			}

			if err := s.ensurePolarActivation(ctx, license, fingerprint); err != nil {
				log.Error().
					Err(err).
					Str("licenseKey", maskLicenseKey(license.LicenseKey)).
					Msg(polar.ActivateFailedMsg)

				if errors.Is(err, polar.ErrActivationLimitExceeded) {
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

			validationRequest := polar.ValidateRequest{Key: license.LicenseKey, ActivationID: license.PolarActivationID}
			validationRequest.SetCondition("fingerprint", fingerprint)

			licenseInfo, err := s.polarClient.Validate(ctx, validationRequest)
			if err != nil {
				switch {
				case errors.Is(err, polar.ErrConditionMismatch):
					log.Error().
						Str("licenseKey", maskLicenseKey(license.LicenseKey)).
						Msg("License fingerprint mismatch - database appears to have been copied from another machine")
					allValid = false
				case errors.Is(err, polar.ErrActivationLimitExceeded):
					log.Error().
						Str("licenseKey", maskLicenseKey(license.LicenseKey)).
						Msg("License activation limit exceeded")
					allValid = false
				case errors.Is(err, polar.ErrInvalidLicenseKey):
					log.Error().
						Str("licenseKey", maskLicenseKey(license.LicenseKey)).
						Msg("Invalid license key - license does not exist")
					allValid = false
				default:
					handleTransient(license, err)
					continue
				}

				if updateErr := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, models.LicenseStatusInvalid); updateErr != nil {
					log.Error().
						Err(updateErr).
						Int("licenseId", license.ID).
						Msg("Failed to update license status to invalid")
				}

				continue
			}

			newStatus := models.LicenseStatusActive
			if !licenseInfo.ValidLicense() {
				newStatus = models.LicenseStatusInvalid
				allValid = false
			}

			if err := s.licenseRepo.UpdateLicenseStatus(ctx, license.ID, newStatus); err != nil {
				log.Error().
					Err(err).
					Int("licenseId", license.ID).
					Msg("Failed to update license status")
			}
		default:
			if s.dodoClient == nil {
				handleTransient(license, ErrDodoClientNotConfigured)
				continue
			}

			licenseInfo, err := s.dodoClient.Validate(ctx, dodo.ValidateRequest{
				LicenseKey: license.LicenseKey,
			})
			if err != nil {
				switch {
				case errors.Is(err, dodo.ErrLicenseNotFound), errors.Is(err, dodo.ErrInvalidLicenseKey), errors.Is(err, dodo.ErrActivationLimitExceeded):
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

			instanceID := licenseInfo.InstanceID
			if instanceID == "" {
				instanceID = license.DodoInstanceID
			}
			if updateErr := s.licenseRepo.UpdateLicenseProvider(ctx, license.ID, models.LicenseProviderDodo, instanceID); updateErr != nil {
				log.Error().Err(updateErr).Int("licenseId", license.ID).Msg("Failed to store Dodo provider")
			}
			continue
		}
	}

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

	switch normalizeProvider(license.Provider) {
	case models.LicenseProviderDodo:
		if license.DodoInstanceID != "" {
			if s.dodoClient == nil {
				return ErrDodoClientNotConfigured
			}
			_, err := s.dodoClient.Deactivate(ctx, dodo.DeactivateRequest{
				LicenseKey:           license.LicenseKey,
				LicenseKeyInstanceID: license.DodoInstanceID,
				InstanceID:           license.DodoInstanceID,
			})
			if err != nil {
				if !errors.Is(err, dodo.ErrInstanceNotFound) && !errors.Is(err, dodo.ErrLicenseNotFound) {
					return fmt.Errorf("failed to deactivate license: %w", err)
				}
			}
		}
	case models.LicenseProviderPolar:
		if license.PolarActivationID != "" && s.polarClient != nil && s.polarClient.IsClientConfigured() {
			err := s.polarClient.Deactivate(ctx, polar.DeactivateRequest{
				Key:          license.LicenseKey,
				ActivationID: license.PolarActivationID,
			})
			if err != nil && !errors.Is(err, polar.ErrLicenseNotActivated) && !errors.Is(err, polar.ErrInvalidLicenseKey) {
				return fmt.Errorf("failed to deactivate license: %w", err)
			}
		}
	}

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
