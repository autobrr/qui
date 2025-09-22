// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package database

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/models"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"
)

type LicenseRepo struct {
	db *DB
}

func NewLicenseRepo(db *DB) *LicenseRepo {
	return &LicenseRepo{db: db}
}

// GetLicenseByKey retrieves a license by its key
func (r *LicenseRepo) GetLicenseByKey(ctx context.Context, licenseKey string) (*models.ProductLicense, error) {
	query := `
		SELECT id, license_key, product_name,  status, activated_at, expires_at, 
		       last_validated, polar_customer_id, polar_product_id, polar_activation_id, username, created_at, updated_at
		FROM licenses 
		WHERE license_key = ?
	`

	license := &models.ProductLicense{}
	var activationId sql.Null[string]

	err := r.db.Conn().QueryRowContext(ctx, query, licenseKey).Scan(
		&license.ID,
		&license.LicenseKey,
		&license.ProductName,
		&license.Status,
		&license.ActivatedAt,
		&license.ExpiresAt,
		&license.LastValidated,
		&license.PolarCustomerID,
		&license.PolarProductID,
		&activationId,
		&license.Username,
		&license.CreatedAt,
		&license.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, models.ErrLicenseNotFound
		}
		return nil, err
	}

	license.PolarActivationID = activationId.V

	return license, nil
}

// GetAllLicenses retrieves all licenses
func (r *LicenseRepo) GetAllLicenses(ctx context.Context) ([]*models.ProductLicense, error) {
	query := `
		SELECT id, license_key, product_name, status, activated_at, expires_at, 
		       last_validated, polar_customer_id, polar_product_id, polar_activation_id, username, created_at, updated_at
		FROM licenses 
		ORDER BY created_at DESC
	`

	rows, err := r.db.Conn().QueryContext(ctx, query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var licenses []*models.ProductLicense
	for rows.Next() {
		license := &models.ProductLicense{}

		var activationId sql.Null[string]

		err := rows.Scan(
			&license.ID,
			&license.LicenseKey,
			&license.ProductName,
			&license.Status,
			&license.ActivatedAt,
			&license.ExpiresAt,
			&license.LastValidated,
			&license.PolarCustomerID,
			&license.PolarProductID,
			&activationId,
			&license.Username,
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

// HasPremiumAccess checks if the user has purchased premium access (one-time unlock)
func (r *LicenseRepo) HasPremiumAccess(ctx context.Context) (bool, error) {
	query := `
		SELECT COUNT(*) 
		FROM licenses 
		WHERE product_name = 'premium-access' 
		AND status = ?
		AND (expires_at IS NULL OR expires_at > datetime('now'))
	`

	var count int
	err := r.db.Conn().QueryRowContext(ctx, query, models.LicenseStatusActive).Scan(&count)
	if err != nil {
		return false, err
	}

	return count > 0, nil
}

// DeleteLicense removes a license from the database
func (r *LicenseRepo) DeleteLicense(ctx context.Context, licenseKey string) error {
	query := `DELETE FROM licenses WHERE license_key = ?`

	result, err := r.db.Conn().ExecContext(ctx, query, licenseKey)
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

func (r *LicenseRepo) StoreLicense(ctx context.Context, license *models.ProductLicense) error {
	query := `
		INSERT INTO licenses (license_key, product_name, status, activated_at, expires_at, 
		                           last_validated, polar_customer_id, polar_product_id, polar_activation_id, username, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	_, err := r.db.Conn().ExecContext(ctx, query,
		license.LicenseKey,
		license.ProductName,
		license.Status,
		license.ActivatedAt,
		timeToNullTime(license.ExpiresAt),
		license.LastValidated,
		license.PolarCustomerID,
		license.PolarProductID,
		license.PolarActivationID,
		license.Username,
		license.CreatedAt,
		license.UpdatedAt,
	)

	return err
}

func (r *LicenseRepo) UpdateLicenseStatus(ctx context.Context, licenseID int, status string) error {
	query := `
		UPDATE licenses 
		SET status = ?, last_validated = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.Conn().ExecContext(ctx, query, status, time.Now(), time.Now(), licenseID)
	return err
}

func (r *LicenseRepo) UpdateLicenseValidation(ctx context.Context, license *models.ProductLicense) error {
	query := `
		UPDATE licenses
		SET last_validated = ?, updated_at = ?
		WHERE id = ?
	`

	_, err := r.db.Conn().ExecContext(ctx, query, license.LastValidated, time.Now(), license.ID)
	return err
}

// UpdateLicenseActivation updates a license with activation details
func (r *LicenseRepo) UpdateLicenseActivation(ctx context.Context, license *models.ProductLicense) error {
	query := `
		UPDATE licenses
		SET polar_activation_id = ?, polar_customer_id = ?, polar_product_id = ?,
		    activated_at = ?, expires_at = ?, last_validated = ?, updated_at = ?, status = ?
		WHERE id = ?
	`

	_, err := r.db.Conn().ExecContext(ctx, query,
		license.PolarActivationID,
		license.PolarCustomerID,
		license.PolarProductID,
		license.ActivatedAt,
		timeToNullTime(license.ExpiresAt),
		time.Now(),
		time.Now(),
		license.Status,
		license.ID,
	)
	return err
}

func timeToNullTime(t *time.Time) sql.NullTime {
	if t == nil {
		return sql.NullTime{Valid: false}
	}
	return sql.NullTime{Time: *t, Valid: true}
}

// Helper function to mask license keys in logs
func maskLicenseKey(key string) string {
	if len(key) <= 8 {
		return "***"
	}
	return key[:8] + "***"
}
