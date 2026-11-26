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

func dodoAnswering(bodies map[string]string) *dodo.Client {
	return dodo.NewClient(
		dodo.WithBaseURL("http://dodo.test"),
		dodo.WithHTTPClient(&http.Client{
			Transport: roundTripper(func(req *http.Request) (*http.Response, error) {
				return &http.Response{
					StatusCode: http.StatusOK,
					Body:       io.NopCloser(strings.NewReader(bodies[req.URL.Path])),
					Header:     make(http.Header),
				}, nil
			}),
		}),
	)
}

func TestActivateTellsSupporterKeyFromThemeLicense(t *testing.T) {
	tests := []struct {
		name          string
		productID     string
		wantProduct   string
		wantSupporter bool
	}{
		{
			name:          "key of a supporter product",
			productID:     supporterProductIDs[0],
			wantProduct:   ProductNameSupporter,
			wantSupporter: true,
		},
		{
			name:        "key of the theme license product",
			productID:   "pdt_theme_license",
			wantProduct: ProductNamePremium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-supporter"))
			service := NewLicenseService(repo, dodoAnswering(map[string]string{
				"/licenses/activate": `{"id":"lki_1","product":{"product_id":"` + tt.productID + `","name":"qui"}}`,
			}), t.TempDir())

			activated, err := service.ActivateAndStoreLicense(ctx, "KEY-1", "tester")
			require.NoError(t, err)
			assert.Equal(t, tt.wantProduct, activated.ProductName)

			isSupporter, err := service.IsSupporter(ctx)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSupporter, isSupporter)

			hasPremium, err := service.HasPremiumAccess(ctx)
			require.NoError(t, err)
			assert.True(t, hasPremium)
		})
	}
}

func TestInvalidSupporterKeyRecoversWhenDodoAcceptsItAgain(t *testing.T) {
	tests := []struct {
		name  string
		check func(context.Context, *Service) error
	}{
		{
			name: "background check",
			check: func(ctx context.Context, s *Service) error {
				_, err := s.ValidateLicenses(ctx)
				return err
			},
		},
		{
			name:  "manual refresh",
			check: func(ctx context.Context, s *Service) error { return s.RefreshAllLicenses(ctx) },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx := t.Context()
			repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-supporter"))
			lastValidated := time.Now().Add(-2 * time.Hour)
			require.NoError(t, repo.StoreLicense(ctx, &models.ProductLicense{
				LicenseKey:     "KEY-1",
				ProductName:    ProductNameSupporter,
				Status:         models.LicenseStatusInvalid,
				ActivatedAt:    lastValidated,
				LastValidated:  lastValidated,
				Provider:       models.LicenseProviderDodo,
				DodoInstanceID: "lki_1",
				Username:       "tester",
				CreatedAt:      lastValidated,
				UpdatedAt:      lastValidated,
			}))
			service := NewLicenseService(repo, dodoAnswering(map[string]string{
				"/licenses/validate": `{"valid":true}`,
			}), t.TempDir())

			require.NoError(t, tt.check(ctx, service))

			isSupporter, err := service.IsSupporter(ctx)
			require.NoError(t, err)
			assert.True(t, isSupporter)
		})
	}
}

func TestActivateStoresTheProductOfAKeyThatIsAlreadyStored(t *testing.T) {
	ctx := t.Context()
	repo := database.NewLicenseRepo(testdb.NewMigratedSQLite(t, "license-supporter"))
	stored := time.Now().Add(-2 * time.Hour)
	require.NoError(t, repo.StoreLicense(ctx, &models.ProductLicense{
		LicenseKey:     "KEY-1",
		ProductName:    ProductNamePremium,
		Status:         models.LicenseStatusInvalid,
		ActivatedAt:    stored,
		LastValidated:  stored,
		Provider:       models.LicenseProviderDodo,
		DodoInstanceID: "lki_1",
		Username:       "tester",
		CreatedAt:      stored,
		UpdatedAt:      stored,
	}))
	service := NewLicenseService(repo, dodoAnswering(map[string]string{
		"/licenses/activate": `{"id":"lki_2","product":{"product_id":"` + supporterProductIDs[0] + `","name":"qui"}}`,
	}), t.TempDir())

	_, err := service.ActivateAndStoreLicense(ctx, "KEY-1", "tester")
	require.NoError(t, err)

	isSupporter, err := service.IsSupporter(ctx)
	require.NoError(t, err)
	assert.True(t, isSupporter)
}
