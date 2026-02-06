// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/polar"
)

func TestValidateLicenses_DoesNotAutoActivateInvalidDodoLicense(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "licenses.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	defer db.Close()

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

	service := NewLicenseService(repo, nil, client, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.NoError(t, err)
	require.False(t, valid)
}

func TestRefreshAllLicenses_UnknownProviderStoresDodoInstanceIDFromValidate(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "licenses.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "LIC-TEST",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       "",
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
				switch req.URL.Path {
				case "/licenses/validate":
					body := string(mustRead(req.Body))
					require.Contains(t, body, `"license_key":"LIC-TEST"`)
					return &http.Response{
						StatusCode: http.StatusOK,
						Body:       io.NopCloser(strings.NewReader(`{"valid":true,"instance_id":"inst_123"}`)),
						Header:     make(http.Header),
					}, nil
				case "/licenses/activate":
					t.Fatalf("unexpected activation call during unknown-provider validation")
					return nil, nil
				default:
					t.Fatalf("unexpected path %q", req.URL.Path)
					return nil, nil
				}
			}),
		}),
	)

	service := NewLicenseService(repo, nil, client, t.TempDir())
	require.NoError(t, service.RefreshAllLicenses(ctx))

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	require.Equal(t, models.LicenseProviderDodo, stored.Provider)
	require.Equal(t, "inst_123", stored.DodoInstanceID)
	require.Equal(t, models.LicenseStatusActive, stored.Status)
}

func TestValidateAndStoreLicense_UnknownProviderDodoInvalidDoesNotFallbackToPolar(t *testing.T) {
	ctx := context.Background()

	dbPath := filepath.Join(t.TempDir(), "licenses.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)
	defer db.Close()

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "LIC-TEST",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-2 * time.Hour),
		Provider:       "",
		DodoInstanceID: "",
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

	polarClient := polar.NewClient(
		polar.WithOrganizationID("org_test"),
		polar.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				t.Fatalf("unexpected call to polar %q", req.URL.Path)
				return nil, nil
			}),
		}),
	)

	service := NewLicenseService(repo, polarClient, dodoClient, t.TempDir())

	_, err = service.ValidateAndStoreLicense(ctx, license.LicenseKey, "tester")
	require.Error(t, err)
	require.ErrorIs(t, err, ErrLicenseNotActive)
}
