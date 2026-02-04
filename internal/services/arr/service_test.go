// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package arr

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/autobrr/qui/internal/models"
)

func TestService_getArrTypeForContent(t *testing.T) {
	// Create a minimal service for testing internal method
	s := &Service{}

	tests := []struct {
		name        string
		contentType ContentType
		want        models.ArrInstanceType
	}{
		{
			name:        "movie maps to radarr",
			contentType: ContentTypeMovie,
			want:        models.ArrInstanceTypeRadarr,
		},
		{
			name:        "tv maps to sonarr",
			contentType: ContentTypeTV,
			want:        models.ArrInstanceTypeSonarr,
		},
		{
			name:        "anime maps to sonarr",
			contentType: ContentTypeAnime,
			want:        models.ArrInstanceTypeSonarr,
		},
		{
			name:        "unknown returns empty",
			contentType: ContentTypeUnknown,
			want:        "",
		},
		{
			name:        "empty string returns empty",
			contentType: "",
			want:        "",
		},
		{
			name:        "invalid content type returns empty",
			contentType: ContentType("invalid"),
			want:        "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := s.getArrTypeForContent(tt.contentType)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNewService(t *testing.T) {
	s := NewService(nil, nil)

	assert.NotNil(t, s)
	assert.Equal(t, DefaultPositiveCacheTTL, s.positiveTTL)
	assert.Equal(t, DefaultNegativeCacheTTL, s.negativeTTL)
}

func TestService_WithPositiveTTL(t *testing.T) {
	s := NewService(nil, nil)
	customTTL := 30 * time.Minute

	result := s.WithPositiveTTL(customTTL)

	assert.Same(t, s, result, "should return same service for chaining")
	assert.Equal(t, customTTL, s.positiveTTL)
}

func TestService_WithNegativeTTL(t *testing.T) {
	s := NewService(nil, nil)
	customTTL := 15 * time.Minute

	result := s.WithNegativeTTL(customTTL)

	assert.Same(t, s, result, "should return same service for chaining")
	assert.Equal(t, customTTL, s.negativeTTL)
}

func TestService_TTLChaining(t *testing.T) {
	s := NewService(nil, nil).
		WithPositiveTTL(4 * time.Hour).
		WithNegativeTTL(30 * time.Minute)

	assert.Equal(t, 4*time.Hour, s.positiveTTL)
	assert.Equal(t, 30*time.Minute, s.negativeTTL)
}

func TestExternalIDsResult_Structure(t *testing.T) {
	// Test that ExternalIDsResult fields are correctly structured
	ids := &models.ExternalIDs{
		IMDbID:   "tt1234567",
		TMDbID:   12345,
		TVDbID:   67890,
		TVMazeID: 11111,
	}
	instanceID := 42

	result := ExternalIDsResult{
		IDs:           ids,
		FromCache:     true,
		ArrInstanceID: &instanceID,
		ContentType:   ContentTypeTV,
	}

	assert.Equal(t, ids, result.IDs)
	assert.True(t, result.FromCache)
	assert.Equal(t, 42, *result.ArrInstanceID)
	assert.Equal(t, ContentTypeTV, result.ContentType)
}

func TestExternalIDsResult_NilIDs(t *testing.T) {
	// Test negative cache result
	result := ExternalIDsResult{
		IDs:           nil,
		FromCache:     true,
		ArrInstanceID: nil,
		ContentType:   ContentTypeMovie,
	}

	assert.Nil(t, result.IDs)
	assert.True(t, result.FromCache)
	assert.Nil(t, result.ArrInstanceID)
	assert.Equal(t, ContentTypeMovie, result.ContentType)
}

func TestDebugResolveResult_Structure(t *testing.T) {
	result := DebugResolveResult{
		Title:              "Breaking Bad S01E01",
		TitleHash:          "abc123",
		ContentType:        ContentTypeTV,
		CacheHit:           false,
		InstancesAvailable: 2,
		InstanceResults: []DebugInstanceResult{
			{
				InstanceID:   1,
				InstanceName: "Sonarr 1",
				InstanceType: "sonarr",
				IDs: &models.ExternalIDs{
					TVDbID: 81189,
				},
			},
		},
	}

	assert.Equal(t, "Breaking Bad S01E01", result.Title)
	assert.Equal(t, "abc123", result.TitleHash)
	assert.Equal(t, ContentTypeTV, result.ContentType)
	assert.False(t, result.CacheHit)
	assert.Equal(t, 2, result.InstancesAvailable)
	assert.Len(t, result.InstanceResults, 1)
	assert.Equal(t, 81189, result.InstanceResults[0].IDs.TVDbID)
}

func TestDebugInstanceResult_WithError(t *testing.T) {
	result := DebugInstanceResult{
		InstanceID:   1,
		InstanceName: "Sonarr",
		InstanceType: "sonarr",
		IDs:          nil,
		Error:        "connection timeout",
	}

	assert.Equal(t, 1, result.InstanceID)
	assert.Equal(t, "Sonarr", result.InstanceName)
	assert.Equal(t, "sonarr", result.InstanceType)
	assert.Nil(t, result.IDs)
	assert.Equal(t, "connection timeout", result.Error)
}

func TestContentType_Constants(t *testing.T) {
	// Verify content type constant values
	assert.Equal(t, ContentType("movie"), ContentTypeMovie)
	assert.Equal(t, ContentType("tv"), ContentTypeTV)
	assert.Equal(t, ContentType("anime"), ContentTypeAnime)
	assert.Equal(t, ContentType("unknown"), ContentTypeUnknown)
}

func TestDefaultTTL_Values(t *testing.T) {
	// Verify default TTL values match expected configuration
	assert.Equal(t, 30*24*time.Hour, DefaultPositiveCacheTTL)
	assert.Equal(t, 1*time.Hour, DefaultNegativeCacheTTL)
}
