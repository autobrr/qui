// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"time"
)

// ThemeLicense represents a theme license key in the database
type ThemeLicense struct {
	ID              int        `json:"id" db:"id"`
	LicenseKey      string     `json:"licenseKey" db:"license_key"`
	ThemeName       string     `json:"themeName" db:"theme_name"`
	Status          string     `json:"status" db:"status"`
	ActivatedAt     time.Time  `json:"activatedAt" db:"activated_at"`
	ExpiresAt       *time.Time `json:"expiresAt,omitempty" db:"expires_at"`
	LastValidated   time.Time  `json:"lastValidated" db:"last_validated"`
	PolarCustomerID *string    `json:"polarCustomerId,omitempty" db:"polar_customer_id"`
	PolarProductID  *string    `json:"polarProductId,omitempty" db:"polar_product_id"`
	CreatedAt       time.Time  `json:"createdAt" db:"created_at"`
	UpdatedAt       time.Time  `json:"updatedAt" db:"updated_at"`
}

// LicenseStatus constants
const (
	LicenseStatusActive  = "active"
	LicenseStatusInvalid = "invalid"
)
