// Copyright (c) 2025, s0up and the autobrr contributors.
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
	ActivatedAt       time.Time  `json:"activatedAt"`
	LastValidated     time.Time  `json:"lastValidated"`
	CreatedAt         time.Time  `json:"createdAt"`
	UpdatedAt         time.Time  `json:"updatedAt"`
	ExpiresAt         *time.Time `json:"expiresAt,omitempty"`
	PolarCustomerID   *string    `json:"polarCustomerId,omitempty"`
	PolarProductID    *string    `json:"polarProductId,omitempty"`
	LicenseKey        string     `json:"licenseKey"`
	ProductName       string     `json:"productName"`
	Status            string     `json:"status"`
	PolarActivationID string     `json:"polarActivationId,omitempty"`
	Username          string     `json:"username"`
	ID                int        `json:"id"`
}

// LicenseInfo contains license validation information
type LicenseInfo struct {
	ExpiresAt    *time.Time `json:"expiresAt,omitempty"`
	Key          string     `json:"key"`
	ProductName  string     `json:"productName"`
	CustomerID   string     `json:"customerId"`
	ProductID    string     `json:"productId"`
	ErrorMessage string     `json:"errorMessage,omitempty"`
	Valid        bool       `json:"valid"`
}

// LicenseStatus constants
const (
	LicenseStatusActive  = "active"
	LicenseStatusInvalid = "invalid"
)
