package license

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestMapBenefitToTheme(t *testing.T) {
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
			expected:  unknownThemeName,
		},
		{
			name:      "non-empty benefit ID returns premium",
			benefitID: "benefit-123",
			operation: "activation",
			expected:  premiumThemeName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := mapBenefitToTheme(tt.benefitID, tt.operation)
			assert.Equal(t, tt.expected, result)
		})
	}
}
