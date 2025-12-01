// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestLicenseStatusConstants(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "active", LicenseStatusActive)
	assert.Equal(t, "invalid", LicenseStatusInvalid)
}

func TestErrLicenseNotFound(t *testing.T) {
	t.Parallel()

	assert.Error(t, ErrLicenseNotFound)
	assert.Equal(t, "license not found", ErrLicenseNotFound.Error())
}

func TestProductLicense(t *testing.T) {
	t.Parallel()

	t.Run("struct with all fields", func(t *testing.T) {
		t.Parallel()

		now := time.Now()
		expiresAt := now.Add(365 * 24 * time.Hour)
		customerID := "cust_123"
		productID := "prod_456"

		license := ProductLicense{
			ID:                1,
			LicenseKey:        "XXXX-XXXX-XXXX-XXXX",
			ProductName:       "Pro License",
			Status:            LicenseStatusActive,
			ActivatedAt:       now,
			ExpiresAt:         &expiresAt,
			LastValidated:     now,
			PolarCustomerID:   &customerID,
			PolarProductID:    &productID,
			PolarActivationID: "act_789",
			Username:          "testuser",
			CreatedAt:         now,
			UpdatedAt:         now,
		}

		assert.Equal(t, 1, license.ID)
		assert.Equal(t, "XXXX-XXXX-XXXX-XXXX", license.LicenseKey)
		assert.Equal(t, "Pro License", license.ProductName)
		assert.Equal(t, LicenseStatusActive, license.Status)
		assert.Equal(t, now, license.ActivatedAt)
		assert.NotNil(t, license.ExpiresAt)
		assert.Equal(t, expiresAt, *license.ExpiresAt)
		assert.Equal(t, now, license.LastValidated)
		assert.NotNil(t, license.PolarCustomerID)
		assert.Equal(t, "cust_123", *license.PolarCustomerID)
		assert.NotNil(t, license.PolarProductID)
		assert.Equal(t, "prod_456", *license.PolarProductID)
		assert.Equal(t, "act_789", license.PolarActivationID)
		assert.Equal(t, "testuser", license.Username)
	})

	t.Run("struct with nil optional fields", func(t *testing.T) {
		t.Parallel()

		now := time.Now()

		license := ProductLicense{
			ID:            1,
			LicenseKey:    "YYYY-YYYY-YYYY-YYYY",
			ProductName:   "Basic License",
			Status:        LicenseStatusActive,
			ActivatedAt:   now,
			LastValidated: now,
			Username:      "user",
			CreatedAt:     now,
			UpdatedAt:     now,
		}

		assert.Nil(t, license.ExpiresAt)
		assert.Nil(t, license.PolarCustomerID)
		assert.Nil(t, license.PolarProductID)
	})

	t.Run("invalid status", func(t *testing.T) {
		t.Parallel()

		license := ProductLicense{
			Status: LicenseStatusInvalid,
		}

		assert.Equal(t, "invalid", license.Status)
	})
}

func TestLicenseInfo(t *testing.T) {
	t.Parallel()

	t.Run("valid license info", func(t *testing.T) {
		t.Parallel()

		expiresAt := time.Now().Add(30 * 24 * time.Hour)

		info := LicenseInfo{
			Key:         "LICENSE-KEY-123",
			ProductName: "Enterprise",
			CustomerID:  "cust_abc",
			ProductID:   "prod_xyz",
			ExpiresAt:   &expiresAt,
			Valid:       true,
		}

		assert.Equal(t, "LICENSE-KEY-123", info.Key)
		assert.Equal(t, "Enterprise", info.ProductName)
		assert.Equal(t, "cust_abc", info.CustomerID)
		assert.Equal(t, "prod_xyz", info.ProductID)
		assert.NotNil(t, info.ExpiresAt)
		assert.True(t, info.Valid)
		assert.Empty(t, info.ErrorMessage)
	})

	t.Run("invalid license info with error", func(t *testing.T) {
		t.Parallel()

		info := LicenseInfo{
			Key:          "INVALID-KEY",
			Valid:        false,
			ErrorMessage: "License key not found",
		}

		assert.Equal(t, "INVALID-KEY", info.Key)
		assert.False(t, info.Valid)
		assert.Equal(t, "License key not found", info.ErrorMessage)
		assert.Nil(t, info.ExpiresAt)
	})

	t.Run("license info without expiration", func(t *testing.T) {
		t.Parallel()

		info := LicenseInfo{
			Key:         "LIFETIME-KEY",
			ProductName: "Lifetime",
			Valid:       true,
			ExpiresAt:   nil,
		}

		assert.Nil(t, info.ExpiresAt)
		assert.True(t, info.Valid)
	})
}
