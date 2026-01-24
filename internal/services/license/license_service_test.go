// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapBenefitToProduct(t *testing.T) {
	tests := []struct {
		name      string
		benefitID string
		operation string
		expected  string
	}{
		{
			name:      "empty benefit ID returns unknown",
			benefitID: "",
			operation: "validation",
			expected:  ProductNameUnknown,
		},
		{
			name:      "non-empty benefit ID returns premium",
			benefitID: "benefit-123",
			operation: "activation",
			expected:  ProductNamePremium,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapBenefitToProduct(tt.benefitID, tt.operation)
			assert.Equal(t, tt.expected, result)
		})
	}
}
