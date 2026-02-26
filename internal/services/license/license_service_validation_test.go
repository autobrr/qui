// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/polar"
)

func TestValidateLicenses_NetworkTimeoutDoesNotInvalidate(t *testing.T) {
	ctx := context.Background()

	db := setupLicenseTestDB(t)

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:        "QUI-TEST-KEY",
		ProductName:       ProductNamePremium,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       now.Add(-time.Hour),
		LastValidated:     now.Add(-time.Hour),
		Provider:          models.LicenseProviderPolar,
		PolarActivationID: "activation-id",
		Username:          "tester",
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, license))

	timeoutErr := context.DeadlineExceeded
	client := polar.NewClient(
		polar.WithOrganizationID("test-org"),
		polar.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				return nil, timeoutErr
			}),
		}),
	)

	service := NewLicenseService(repo, client, nil, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.Error(t, err)
	assert.True(t, errors.Is(err, timeoutErr))
	assert.True(t, valid, "transient errors should not mark the license invalid")

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, stored.Status)
}

func TestValidateLicenses_OfflineBeyondGraceDoesNotInvalidate(t *testing.T) {
	ctx := context.Background()

	db := setupLicenseTestDB(t)

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:        "QUI-TEST-KEY",
		ProductName:       ProductNamePremium,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       now.Add(-time.Hour),
		LastValidated:     now.Add(-(offlineGracePeriod + time.Hour)),
		Provider:          models.LicenseProviderPolar,
		PolarActivationID: "activation-id",
		Username:          "tester",
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, license))

	timeoutErr := context.DeadlineExceeded
	client := polar.NewClient(
		polar.WithOrganizationID("test-org"),
		polar.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				return nil, timeoutErr
			}),
		}),
	)

	service := NewLicenseService(repo, client, nil, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, timeoutErr)
	assert.True(t, valid, "transient errors should not mark the license invalid, regardless of last validation time")

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, stored.Status)
}

func TestValidateLicenses_InvalidThenTransientStillReturnsInvalid(t *testing.T) {
	ctx := context.Background()

	db := setupLicenseTestDB(t)

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	licenseBad := &models.ProductLicense{
		LicenseKey:        "QUI-BAD",
		ProductName:       ProductNamePremium,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       now.Add(-time.Hour),
		LastValidated:     now.Add(-time.Hour),
		Provider:          models.LicenseProviderPolar,
		PolarActivationID: "activation-bad",
		Username:          "tester",
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now.Add(-time.Hour),
	}
	licenseSlow := &models.ProductLicense{
		LicenseKey:        "QUI-SLOW",
		ProductName:       ProductNamePremium,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       now.Add(-time.Hour),
		LastValidated:     now.Add(-time.Hour),
		Provider:          models.LicenseProviderPolar,
		PolarActivationID: "activation-slow",
		Username:          "tester",
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, licenseBad))
	require.NoError(t, repo.StoreLicense(ctx, licenseSlow))

	client := polar.NewClient(
		polar.WithOrganizationID("test-org"),
		polar.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				// First call (bad license) returns revoked, second returns timeout
				if strings.Contains(req.URL.Path, "validate") && strings.Contains(string(mustRead(req.Body)), "QUI-BAD") {
					body := `{"status":"revoked"}`
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(body)),
						Header:     make(http.Header),
					}, nil
				}
				return nil, context.DeadlineExceeded
			}),
		}),
	)

	service := NewLicenseService(repo, client, nil, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.NoError(t, err, "transient error should be suppressed when invalid licenses were found")
	assert.False(t, valid, "overall validity should be false when any license is invalid")

	storedBad, err := repo.GetLicenseByKey(ctx, licenseBad.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusInvalid, storedBad.Status)

	storedSlow, err := repo.GetLicenseByKey(ctx, licenseSlow.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, storedSlow.Status, "transient failure keeps prior status")
}

func TestValidateLicenses_InvalidStatusMarksLicenseInvalid(t *testing.T) {
	ctx := context.Background()

	db := setupLicenseTestDB(t)

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:        "QUI-TEST-KEY",
		ProductName:       ProductNamePremium,
		Status:            models.LicenseStatusActive,
		ActivatedAt:       now.Add(-time.Hour),
		LastValidated:     now.Add(-time.Hour),
		Provider:          models.LicenseProviderPolar,
		PolarActivationID: "activation-id",
		Username:          "tester",
		CreatedAt:         now.Add(-time.Hour),
		UpdatedAt:         now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, license))

	client := polar.NewClient(
		polar.WithOrganizationID("test-org"),
		polar.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				body := `{"status":"revoked"}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		}),
	)

	service := NewLicenseService(repo, client, nil, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.NoError(t, err)
	assert.False(t, valid)

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusInvalid, stored.Status)
}

type roundTripper func(*http.Request) (*http.Response, error)

func (rt roundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return rt(r)
}

func mustRead(rc io.ReadCloser) []byte {
	defer rc.Close()
	b, _ := io.ReadAll(rc)
	return b
}
