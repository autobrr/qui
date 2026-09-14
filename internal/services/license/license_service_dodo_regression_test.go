// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestValidateLicenses_DoesNotAutoActivateInvalidDodoLicense(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-dodo-regression")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "LIC-TEST",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusInvalid,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	require.NoError(t, repo.StoreLicense(ctx, license))

	client := dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected call to %q for invalid license", req.URL.Path)
				return nil, nil
			}),
		}),
	)

	service := NewLicenseService(repo, client, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.NoError(t, err)
	require.False(t, valid)
}

func TestValidateAndStoreLicense_DodoInvalidReturnsNotActive(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-dodo-regression")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "LIC-TEST",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst_123",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	require.NoError(t, repo.StoreLicense(ctx, license))

	dodoClient := dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				switch req.URL.Path {
				case "/licenses/validate":
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"valid":false}`)),
						Header:     make(http.Header),
					}, nil
				default:
					t.Fatalf("unexpected dodo path %q", req.URL.Path)
					return nil, nil
				}
			}),
		}),
	)

	service := NewLicenseService(repo, dodoClient, t.TempDir())

	_, err := service.ValidateAndStoreLicense(ctx, license.LicenseKey, "tester")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrLicenseNotActive)
}

func TestValidateLicenses_DodoProviderWithoutDodoClientDoesNotPanic(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-dodo-regression")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "LIC-DODO-NIL",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst_123",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	require.NoError(t, repo.StoreLicense(ctx, license))

	service := NewLicenseService(repo, nil, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.ErrorIs(t, err, ErrDodoClientNotConfigured)
	require.True(t, valid, "active license should remain active on transient validation failure")

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	require.Equal(t, models.LicenseStatusActive, stored.Status)
}

func TestRefreshAllLicenses_ContinuesAfterDodoError(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-dodo-regression")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	errLicense := &models.ProductLicense{
		LicenseKey:     "LIC-DODO-ERR",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst_err",
		Username:       "tester",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	require.NoError(t, repo.StoreLicense(ctx, errLicense))

	okLicense := &models.ProductLicense{
		LicenseKey:     "LIC-DODO-OK",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst_ok",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Minute),
		UpdatedAt:      now.Add(-time.Minute),
	}
	require.NoError(t, repo.StoreLicense(ctx, okLicense))

	timeoutErr := context.DeadlineExceeded
	validatedOK := false
	client := dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				require.Equal(t, "/licenses/validate", req.URL.Path)
				if strings.Contains(string(mustRead(req.Body)), "LIC-DODO-ERR") {
					return nil, timeoutErr
				}
				validatedOK = true
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"valid":true}`)),
					Header:     make(http.Header),
				}, nil
			}),
		}),
	)

	service := NewLicenseService(repo, client, t.TempDir())

	err := service.RefreshAllLicenses(ctx)
	require.ErrorIs(t, err, timeoutErr)
	require.True(t, validatedOK, "refresh must reach the second license after the first fails")

	storedOK, err := repo.GetLicenseByKey(ctx, okLicense.LicenseKey)
	require.NoError(t, err)
	require.Equal(t, models.LicenseStatusActive, storedOK.Status)
}

func TestDeleteLicense_DodoProviderWithoutDodoClientStillDeletesLocalLicense(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-dodo-regression")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "LIC-DODO-DELETE",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst-delete",
		Username:       "tester",
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	require.NoError(t, repo.StoreLicense(ctx, license))

	service := NewLicenseService(repo, nil, t.TempDir())
	require.NoError(t, service.DeleteLicense(ctx, license.LicenseKey))

	_, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.ErrorIs(t, err, models.ErrLicenseNotFound)
}
