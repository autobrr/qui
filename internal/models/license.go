// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"time"

	"github.com/pkg/errors"
)

var (
	ErrLicenseNotFound = errors.New("license not found")
)

// ProductLicense represents a product license in the database
type ProductLicense struct {
	ID                int        `json:"id"`
	LicenseKey        string     `json:"licenseKey"`
	ProductName       string     `json:"productName"`
	Status            string     `json:"status"`
	ActivatedAt       time.Time  `json:"activatedAt"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
	LastValidated     time.Time  `json:"lastValidated"`
	Provider          string     `json:"provider,omitempty"`
	DodoInstanceID    string     `json:"dodoInstanceId,omitempty"`
	PolarCustomerID   *string    `json:"polarCustomerId,omitempty"`
	PolarProductID    *string    `json:"polarProductId,omitempty"`
	PolarActivationID string     `json:"polarActivationId,omitempty"`
	Username          string     `json:"username"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
}

// LicenseInfo contains license validation information
type LicenseInfo struct {
	Key          string     `json:"key"`
	ProductName  string     `json:"productName"`
	CustomerID   string     `json:"customerId"`
	ProductID    string     `json:"productId"`
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Valid        bool       `json:"valid"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
}

// LicenseStatus constants
const (
	LicenseStatusActive  = "active"
	LicenseStatusInvalid = "invalid"
)

// LicenseProvider constants
const (
	LicenseProviderDodo  = "dodo"
	LicenseProviderPolar = "polar"
)
