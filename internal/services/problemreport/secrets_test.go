// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package problemreport

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/domain"
)

// regression: #4288
func TestConfigSecretsTakeThePasswordFromTheDSN(t *testing.T) {
	for _, dsn := range []string{
		"postgres://qui:dsn-password-123@db.example.invalid:5432/qui?sslmode=disable",
		"host=db.example.invalid user=qui password=dsn-password-123 dbname=qui sslmode=disable",
	} {
		t.Run(dsn, func(t *testing.T) {
			assert.Contains(t, ConfigSecrets(&domain.Config{DatabaseDSN: dsn}), "dsn-password-123")
		})
	}
}
