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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/dodo"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestValidateLicenses_NetworkTimeoutDoesNotInvalidate(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-validation")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "QUI-TEST-KEY",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst-id",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, license))

	timeoutErr := context.DeadlineExceeded
	client := dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				return nil, timeoutErr
			}),
		}),
	)

	service := NewLicenseService(repo, client, t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.Error(t, err)
	require.ErrorIs(t, err, timeoutErr)
	assert.True(t, valid, "transient errors should not mark the license invalid")

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, stored.Status)
}

func TestValidateLicenses_OfflineBeyondGraceRevokesPremium(t *testing.T) {
	ctx := t.Context()

	repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-validation"))
	license := storeLicense(t, repo, models.LicenseStatusActive, time.Now().Add(-(offlineGracePeriod + time.Hour)))

	service := NewLicenseService(repo, newFakeDodoClient(func() bool { return false }), t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.NoError(t, err)
	assert.False(t, valid, "a license that the server has not confirmed for more than the grace period must not stay valid")

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusOffline, stored.Status)

	hasPremium, err := service.HasPremiumAccess(ctx)
	require.NoError(t, err)
	assert.False(t, hasPremium)
}

func TestValidateLicenses_OfflineWithinGraceKeepsStatus(t *testing.T) {
	ctx := t.Context()

	repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-validation"))
	license := storeLicense(t, repo, models.LicenseStatusActive, time.Now().Add(-(offlineGracePeriod - time.Hour)))

	service := NewLicenseService(repo, newFakeDodoClient(func() bool { return false }), t.TempDir())

	valid, err := service.ValidateLicenses(ctx)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, valid)

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, stored.Status)

	hasPremium, err := service.HasPremiumAccess(ctx)
	require.NoError(t, err)
	assert.True(t, hasPremium)
}

func TestValidateLicenses_OfflineLicenseRecoversWhenServerAnswers(t *testing.T) {
	ctx := t.Context()

	repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-validation"))
	license := storeLicense(t, repo, models.LicenseStatusActive, time.Now().Add(-(offlineGracePeriod + time.Hour)))

	serverUp := false
	service := NewLicenseService(repo, newFakeDodoClient(func() bool { return serverUp }), t.TempDir())

	_, err := service.ValidateLicenses(ctx)
	require.NoError(t, err)
	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	require.Equal(t, models.LicenseStatusOffline, stored.Status)

	serverUp = true
	valid, err := service.ValidateLicenses(ctx)
	require.NoError(t, err)
	assert.True(t, valid)

	stored, err = repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, stored.Status)
	assert.WithinDuration(t, time.Now(), stored.LastValidated, time.Minute)

	hasPremium, err := service.HasPremiumAccess(ctx)
	require.NoError(t, err)
	assert.True(t, hasPremium)
}

func TestRefreshAllLicenses_RestoresOfflineLicense(t *testing.T) {
	ctx := t.Context()

	repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-validation"))
	license := storeLicense(t, repo, models.LicenseStatusOffline, time.Now().Add(-(offlineGracePeriod + time.Hour)))

	service := NewLicenseService(repo, newFakeDodoClient(func() bool { return true }), t.TempDir())

	require.NoError(t, service.RefreshAllLicenses(ctx))

	stored, err := repo.GetLicenseByKey(ctx, license.LicenseKey)
	require.NoError(t, err)
	assert.Equal(t, models.LicenseStatusActive, stored.Status)
}

// storeLicense stores a Dodo license with the given status that the server last confirmed at lastValidated.
func storeLicense(t *testing.T, repo *database.LicenseRepo, status string, lastValidated time.Time) *models.ProductLicense {
	t.Helper()

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "QUI-TEST-KEY",
		ProductName:    ProductNamePremium,
		Status:         status,
		ActivatedAt:    lastValidated,
		LastValidated:  lastValidated,
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst-id",
		Username:       "tester",
		CreatedAt:      lastValidated,
		UpdatedAt:      now,
	}
	require.NoError(t, repo.StoreLicense(t.Context(), license))
	return license
}

// newFakeDodoClient returns a client whose server answers valid while serverUp returns true, and times out otherwise.
func newFakeDodoClient(serverUp func() bool) *dodo.Client {
	return dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				if !serverUp() {
					return nil, context.DeadlineExceeded
				}
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(`{"valid":true}`)),
					Header:     make(http.Header),
				}, nil
			}),
		}),
	)
}

func TestValidateLicenses_InvalidThenTransientStillReturnsInvalid(t *testing.T) {
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-validation")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	licenseBad := &models.ProductLicense{
		LicenseKey:     "QUI-BAD",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst-bad",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}
	licenseSlow := &models.ProductLicense{
		LicenseKey:     "QUI-SLOW",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst-slow",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, licenseBad))
	require.NoError(t, repo.StoreLicense(ctx, licenseSlow))

	client := dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				// First call (bad license) returns invalid, second returns timeout
				if strings.Contains(req.URL.Path, "validate") && strings.Contains(string(mustRead(req.Body)), "QUI-BAD") {
					body := `{"valid":false}`
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

	service := NewLicenseService(repo, client, t.TempDir())

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
	ctx := t.Context()

	db := testdb.NewMigratedSQLite(t, "license-validation")

	repo := database.NewLicenseRepo(db)

	now := time.Now()
	license := &models.ProductLicense{
		LicenseKey:     "QUI-TEST-KEY",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusActive,
		ActivatedAt:    now.Add(-time.Hour),
		LastValidated:  now.Add(-time.Hour),
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "inst-id",
		Username:       "tester",
		CreatedAt:      now.Add(-time.Hour),
		UpdatedAt:      now.Add(-time.Hour),
	}

	require.NoError(t, repo.StoreLicense(ctx, license))

	client := dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(*http.Request) (*http.Response, error) {
				body := `{"valid":false}`
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(body)),
					Header:     make(http.Header),
				}, nil
			}),
		}),
	)

	service := NewLicenseService(repo, client, t.TempDir())

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
