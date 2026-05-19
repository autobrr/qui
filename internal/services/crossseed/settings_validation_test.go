// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestUpdateAutomationSettingsSizeValidation(t *testing.T) {
	// Create a service with no automation store for this test
	s := &Service{}

	tests := []struct {
		name              string
		inputTolerance    float64
		expectedTolerance float64
		expectError       bool
	}{
		{
			name:              "Valid tolerance should be preserved",
			inputTolerance:    10.0,
			expectedTolerance: 10.0,
			expectError:       true, // No automation store configured
		},
		{
			name:              "Negative tolerance should be set to default",
			inputTolerance:    -5.0,
			expectedTolerance: 5.0,
			expectError:       true, // No automation store configured
		},
		{
			name:              "Zero tolerance should be preserved",
			inputTolerance:    0.0,
			expectedTolerance: 0.0,
			expectError:       true, // No automation store configured
		},
		{
			name:              "Very high tolerance should be capped at 100%",
			inputTolerance:    150.0,
			expectedTolerance: 100.0,
			expectError:       true, // No automation store configured
		},
		{
			name:              "Exactly 100% should be preserved",
			inputTolerance:    100.0,
			expectedTolerance: 100.0,
			expectError:       true, // No automation store configured
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			settings := &models.CrossSeedAutomationSettings{
				RunIntervalMinutes:           120,
				MaxResultsPerRun:             50,
				SizeMismatchTolerancePercent: tt.inputTolerance,
			}

			// Since we don't have an automation store, this should error but still validate the settings
			_, err := s.UpdateAutomationSettings(context.Background(), settings)

			if tt.expectError && err == nil {
				t.Errorf("Expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("Expected success but got error: %v", err)
			}

			// Check that the tolerance was properly validated regardless of the storage error
			if settings.SizeMismatchTolerancePercent != tt.expectedTolerance {
				t.Errorf("Expected tolerance %.1f, got %.1f",
					tt.expectedTolerance, settings.SizeMismatchTolerancePercent)
			}
		})
	}
}

func TestUpdateAutomationSettingsNilCheck(t *testing.T) {
	s := &Service{}

	_, err := s.UpdateAutomationSettings(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil settings but got none")
	}

	expectedMsg := "settings cannot be nil"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}
