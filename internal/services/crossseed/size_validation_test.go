// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
)

func TestIsSizeWithinTolerance(t *testing.T) {
	s := matcher{}

	tests := []struct {
		name             string
		sourceSize       int64
		candidateSize    int64
		tolerancePercent float64
		expectedResult   bool
	}{
		{
			name:             "Within 5% tolerance - slightly smaller",
			sourceSize:       1000000000,
			candidateSize:    970000000,
			tolerancePercent: 5.0,
			expectedResult:   true,
		},
		{
			name:             "Within 5% tolerance - slightly larger",
			sourceSize:       1000000000,
			candidateSize:    1030000000,
			tolerancePercent: 5.0,
			expectedResult:   true,
		},
		{
			name:             "Outside 5% tolerance - too small",
			sourceSize:       1000000000,
			candidateSize:    940000000,
			tolerancePercent: 5.0,
			expectedResult:   false,
		},
		{
			name:             "Outside 5% tolerance - too large",
			sourceSize:       1000000000,
			candidateSize:    1060000000,
			tolerancePercent: 5.0,
			expectedResult:   false,
		},
		{
			name:             "Zero tolerance requires exact match",
			sourceSize:       1000000000,
			candidateSize:    1000000001,
			tolerancePercent: 0.0,
			expectedResult:   false,
		},
		{
			name:             "Zero tolerance with exact match",
			sourceSize:       1000000000,
			candidateSize:    1000000000,
			tolerancePercent: 0.0,
			expectedResult:   true,
		},
		{
			name:             "Both sizes zero should match",
			sourceSize:       0,
			candidateSize:    0,
			tolerancePercent: 5.0,
			expectedResult:   true,
		},
		{
			name:             "One size zero should not match",
			sourceSize:       1000000000,
			candidateSize:    0,
			tolerancePercent: 5.0,
			expectedResult:   false,
		},
		{
			name:             "Large tolerance - 20%",
			sourceSize:       1000000000,
			candidateSize:    1150000000,
			tolerancePercent: 20.0,
			expectedResult:   true,
		},
		{
			name:             "Negative tolerance treated as zero",
			sourceSize:       1000000000,
			candidateSize:    1000000000,
			tolerancePercent: -5.0,
			expectedResult:   true,
		},
		{
			name:             "Negative tolerance with mismatch",
			sourceSize:       1000000000,
			candidateSize:    1000000001,
			tolerancePercent: -5.0,
			expectedResult:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := s.isSizeWithinTolerance(tt.sourceSize, tt.candidateSize, tt.tolerancePercent)
			if result != tt.expectedResult {
				t.Errorf("isSizeWithinTolerance(%d, %d, %.1f) = %v, want %v",
					tt.sourceSize, tt.candidateSize, tt.tolerancePercent, result, tt.expectedResult)
			}
		})
	}
}
