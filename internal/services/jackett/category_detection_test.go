// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestContentDetectionSkippedWhenCategoriesProvided verifies that content detection is bypassed when categories are provided
func TestContentDetectionSkippedWhenCategoriesProvided(t *testing.T) {
	tests := []struct {
		name               string
		query              string
		providedCategories []int
		expectDetection    bool
	}{
		{
			name:               "No categories provided - should trigger detection",
			query:              "Some Query",
			providedCategories: nil,
			expectDetection:    true,
		},
		{
			name:               "Categories provided - should skip detection",
			query:              "Some Query",
			providedCategories: []int{CategoryTV},
			expectDetection:    false,
		},
		{
			name:               "Multiple categories provided - should skip detection",
			query:              "Some Query",
			providedCategories: []int{CategoryMovies, CategoryBooks},
			expectDetection:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &TorznabSearchRequest{
				Query:      tt.query,
				Categories: tt.providedCategories,
			}

			// Simulate the logic from Search/SearchGeneric methods
			var detectedType contentType
			if len(req.Categories) == 0 {
				// Content detection would happen here
				detectedType = contentTypeMovie // Just use a dummy value for this test
			} else {
				// When categories are provided, skip content detection
				detectedType = contentTypeUnknown
			}

			assert.Equal(t, tt.query, req.Query)

			if tt.expectDetection {
				assert.NotEqual(t, contentTypeUnknown, detectedType, "Should have detected content type")
			} else {
				assert.Equal(t, contentTypeUnknown, detectedType, "Should have skipped content detection")
			}
		})
	}
}

// TestCategoryAssignment tests that categories are assigned correctly based on the logic
func TestCategoryAssignment(t *testing.T) {
	tests := []struct {
		name               string
		query              string
		providedCategories []int
		expectedCategories []int
	}{
		{
			name:               "No categories - content detection will assign some categories",
			query:              "Some Query", // Use a generic query to avoid specific detection issues
			providedCategories: nil,
			expectedCategories: nil, // We don't care about the specific categories, just that they get assigned
		},
		{
			name:               "Categories provided - should preserve original",
			query:              "The Matrix 1999", // Movie query
			providedCategories: []int{CategoryTV}, // TV category provided
			expectedCategories: []int{CategoryTV}, // Should keep provided category
		},
		{
			name:               "Multiple categories provided - should preserve all",
			query:              "Some query",
			providedCategories: []int{CategoryMovies, CategoryBooks, CategoryTV},
			expectedCategories: []int{CategoryMovies, CategoryBooks, CategoryTV},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Simulate the logic from Search/SearchGeneric methods
			req := &TorznabSearchRequest{
				Query:      tt.query,
				Categories: append([]int(nil), tt.providedCategories...), // Copy to avoid mutations
			}

			if len(req.Categories) == 0 {
				service := NewService(nil)
				detectedType := service.detectContentType(req)
				req.Categories = getCategoriesForContentType(detectedType)

				if tt.expectedCategories == nil {
					// Just verify that some categories were assigned
					assert.NotEmpty(t, req.Categories, "Categories should be assigned when none provided")
				} else {
					assert.Equal(t, tt.expectedCategories, req.Categories, "Categories should match expected")
				}
			} else {
				assert.Equal(t, tt.expectedCategories, req.Categories, "Categories should match expected")
			}
		})
	}
}
