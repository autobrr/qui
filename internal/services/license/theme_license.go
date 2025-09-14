// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"database/sql"
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

// ThemeLicenseService handles theme license operations
type ThemeLicenseService struct {
	db          *database.DB
	polarClient *polar.Client
}

// NewThemeLicenseService creates a new theme license service
func NewThemeLicenseService(db *database.DB, polarClient *polar.Client) *ThemeLicenseService {
	return &ThemeLicenseService{
		db:          db,
		polarClient: polarClient,
	}
}

// ActivateAndStoreLicense activates a license key and stores it if valid
func (s *ThemeLicenseService) ActivateAndStoreLicense(ctx context.Context, licenseKey string) (*models.ThemeLicense, error) {
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

	themeName := mapBenefitToTheme(activateResp.LicenseKey.BenefitID, "validation")

	// Create a license record
	license := &models.ThemeLicense{
		LicenseKey:        licenseKey,
		ThemeName:         themeName,
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
	if err := s.storeLicense(ctx, license); err != nil {
		return nil, fmt.Errorf("failed to store license: %w", err)
	}

	log.Info().
		Str("themeName", license.ThemeName).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License validated and stored successfully")

	return license, nil
}

// ValidateAndStoreLicense validates a license key and stores it if valid
func (s *ThemeLicenseService) ValidateAndStoreLicense(ctx context.Context, licenseKey string) (*models.ThemeLicense, error) {
	// Validate with Polar API
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		return nil, fmt.Errorf("polar client not configured")
	}

	// Check if license already exists
	existingLicense, err := s.GetLicenseByKey(ctx, licenseKey)
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
	if err := s.updateLicenseValidation(ctx, existingLicense); err != nil {
		log.Error().Err(err).Msg("Failed to update license validation time")
	}

	log.Info().
		Str("themeName", existingLicense.ThemeName).
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License validated and updated successfully")

	return existingLicense, nil
}

// GetLicenseByKey retrieves a license by its key
func (s *ThemeLicenseService) GetLicenseByKey(ctx context.Context, licenseKey string) (*models.ThemeLicense, error) {
	query := `
		SELECT id, license_key, theme_name, status, activated_at, expires_at, 
		       last_validated, polar_customer_id, polar_product_id, polar_activation_id, created_at, updated_at
		FROM theme_licenses 
		WHERE license_key = ?
	`

	license := &models.ThemeLicense{}
	var activationId sql.Null[string]

	err := s.db.Conn().QueryRowContext(ctx, query, licenseKey).Scan(
		&license.ID,
		&license.LicenseKey,
		&license.ThemeName,
		&license.Status,
		&license.ActivatedAt,
		&license.ExpiresAt,
		&license.LastValidated,
		&license.PolarCustomerID,
		&license.PolarProductID,
		&activationId,
		&license.CreatedAt,
		&license.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrLicenseNotFound
		}
		return nil, err
	}

	license.PolarActivationID = activationId.V

	return license, nil
}

// GetAllLicenses retrieves all theme licenses
func (s *ThemeLicenseService) GetAllLicenses(ctx context.Context) ([]*models.ThemeLicense, error) {
	query := `
		SELECT id, license_key, theme_name, status, activated_at, expires_at, 
		       last_validated, polar_customer_id, polar_product_id, polar_activation_id, created_at, updated_at
		FROM theme_licenses 
		ORDER BY created_at DESC
	`

	rows, err := s.db.Conn().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var licenses []*models.ThemeLicense
	for rows.Next() {
		license := &models.ThemeLicense{}

		var activationId sql.Null[string]

		err := rows.Scan(
			&license.ID,
			&license.LicenseKey,
			&license.ThemeName,
			&license.Status,
			&license.ActivatedAt,
			&license.ExpiresAt,
			&license.LastValidated,
			&license.PolarCustomerID,
			&license.PolarProductID,
			&activationId,
			&license.CreatedAt,
			&license.UpdatedAt,
		)
		if err != nil {
			return nil, err
		}

		license.PolarActivationID = activationId.V

		licenses = append(licenses, license)
	}

	return licenses, nil
}

// HasPremiumAccess checks if the user has premium access
func (s *ThemeLicenseService) HasPremiumAccess(ctx context.Context) (bool, error) {
	return s.hasPremiumAccess(ctx)
}

// hasPremiumAccess checks if the user has purchased premium access (one-time unlock)
func (s *ThemeLicenseService) hasPremiumAccess(ctx context.Context) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM theme_licenses 
		WHERE theme_name = 'premium-access' 
		AND status = ?
		AND (expires_at IS NULL OR expires_at > datetime('now'))
	`

	var count int
	err := s.db.Conn().QueryRowContext(ctx, query, models.LicenseStatusActive).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// RefreshAllLicenses validates all stored licenses against Polar API
func (s *ThemeLicenseService) RefreshAllLicenses(ctx context.Context) error {
	if s.polarClient == nil || !s.polarClient.IsClientConfigured() {
		log.Warn().Msg("Polar client not configured, skipping license refresh")
		return nil
	}

	licenses, err := s.GetAllLicenses(ctx)
	if err != nil {
		return fmt.Errorf("failed to get licenses: %w", err)
	}

	log.Debug().Int("count", len(licenses)).Msg("Refreshing theme licenses")

	if len(licenses) == 0 {
		return nil
	}

	fingerprint, err := machineid.ProtectedID("qui-premium")
	if err != nil {
		return fmt.Errorf("failed to get machine ID: %w", err)
	}

	log.Debug().Str("fingerprint", fingerprint).Msg("Refreshing theme licenses")

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

		if err := s.updateLicenseStatus(ctx, license.ID, newStatus); err != nil {
			log.Error().
				Err(err).
				Int("licenseId", license.ID).
				Msg("Failed to update license status")
		}
	}

	return nil
}

// DeleteLicense removes a license from the database
func (s *ThemeLicenseService) DeleteLicense(ctx context.Context, licenseKey string) error {
	query := `DELETE FROM theme_licenses WHERE license_key = ?`

	result, err := s.db.Conn().ExecContext(ctx, query, licenseKey)
	if err != nil {
		return err
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return err
	}

	if rowsAffected == 0 {
		return fmt.Errorf("license not found")
	}

	log.Info().
		Str("licenseKey", maskLicenseKey(licenseKey)).
		Msg("License deleted successfully")

	return nil
}

// Private helper methods

func (s *ThemeLicenseService) storeLicense(ctx context.Context, license *models.ThemeLicense) error {
	query := `
		INSERT INTO theme_licenses (license_key, theme_name, status, activated_at, expires_at, 
		                           last_validated, polar_customer_id, polar_product_id, polar_activation_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := s.db.Conn().ExecContext(ctx, query,
		license.LicenseKey,
		license.ThemeName,
		license.Status,
		license.ActivatedAt,
		timeToNullTime(license.ExpiresAt),
		license.LastValidated,
		license.PolarCustomerID,
		license.PolarProductID,
		license.PolarActivationID,
		license.CreatedAt,
		license.UpdatedAt,
	)

	return err
}

func (s *ThemeLicenseService) updateLicenseStatus(ctx context.Context, licenseID int, status string) error {
	query := `
		UPDATE theme_licenses 
		SET status = ?, last_validated = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := s.db.Conn().ExecContext(ctx, query, status, time.Now(), time.Now(), licenseID)
	return err
}

func (s *ThemeLicenseService) updateLicenseValidation(ctx context.Context, license *models.ThemeLicense) error {
	query := `
		UPDATE theme_licenses 
		SET last_validated = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := s.db.Conn().ExecContext(ctx, query, license.LastValidated, time.Now(), license.ID)
	return err
}

// Helper function to mask license keys in logs
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}

func timeToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

const (
	premiumThemeName = "premium-access"
	unknownThemeName = "unknown"
	defaultLabel     = "qui Theme License"
)

// mapBenefitToTheme maps a benefit ID to theme name
func mapBenefitToTheme(benefitID, operation string) string {
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
