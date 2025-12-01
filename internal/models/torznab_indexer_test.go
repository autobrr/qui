// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTorznabBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    TorznabBackend
		wantErr bool
	}{
		{
			name:  "jackett",
			value: "jackett",
			want:  TorznabBackendJackett,
		},
		{
			name:  "prowlarr",
			value: "prowlarr",
			want:  TorznabBackendProwlarr,
		},
		{
			name:  "native",
			value: "native",
			want:  TorznabBackendNative,
		},
		{
			name:  "empty defaults to jackett",
			value: "",
			want:  TorznabBackendJackett,
		},
		{
			name:    "invalid backend",
			value:   "invalid",
			wantErr: true,
		},
		{
			name:    "uppercase not supported",
			value:   "JACKETT",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseTorznabBackend(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid torznab backend")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMustTorznabBackend(t *testing.T) {
	t.Parallel()

	t.Run("valid backend", func(t *testing.T) {
		t.Parallel()

		// Should not panic
		backend := MustTorznabBackend("jackett")
		assert.Equal(t, TorznabBackendJackett, backend)
	})

	t.Run("empty defaults to jackett", func(t *testing.T) {
		t.Parallel()

		backend := MustTorznabBackend("")
		assert.Equal(t, TorznabBackendJackett, backend)
	})

	t.Run("invalid backend panics", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() {
			MustTorznabBackend("invalid")
		})
	})
}

func TestTorznabBackendConstants(t *testing.T) {
	t.Parallel()

	// Ensure constants have expected values
	assert.Equal(t, TorznabBackend("jackett"), TorznabBackendJackett)
	assert.Equal(t, TorznabBackend("prowlarr"), TorznabBackendProwlarr)
	assert.Equal(t, TorznabBackend("native"), TorznabBackendNative)
}

func TestTorznabIndexerStruct(t *testing.T) {
	t.Parallel()

	t.Run("default values", func(t *testing.T) {
		t.Parallel()

		indexer := &TorznabIndexer{}
		assert.Equal(t, 0, indexer.ID)
		assert.Empty(t, indexer.Name)
		assert.Empty(t, indexer.BaseURL)
		assert.Empty(t, indexer.IndexerID)
		assert.Empty(t, indexer.Backend)
		assert.False(t, indexer.Enabled)
		assert.Equal(t, 0, indexer.Priority)
		assert.Equal(t, 0, indexer.TimeoutSeconds)
		assert.Empty(t, indexer.Capabilities)
		assert.Empty(t, indexer.Categories)
		assert.Nil(t, indexer.LastTestAt)
		assert.Empty(t, indexer.LastTestStatus)
		assert.Nil(t, indexer.LastTestError)
	})

	t.Run("with all fields", func(t *testing.T) {
		t.Parallel()

		testError := "connection timeout"
		indexer := &TorznabIndexer{
			ID:             1,
			Name:           "Test Indexer",
			BaseURL:        "http://localhost:9117",
			IndexerID:      "aither",
			Backend:        TorznabBackendJackett,
			Enabled:        true,
			Priority:       100,
			TimeoutSeconds: 30,
			Capabilities:   []string{"search", "tv-search", "movie-search"},
			Categories: []TorznabIndexerCategory{
				{IndexerID: 1, CategoryID: 2000, CategoryName: "Movies"},
				{IndexerID: 1, CategoryID: 5000, CategoryName: "TV"},
			},
			LastTestStatus: "success",
			LastTestError:  &testError,
		}

		assert.Equal(t, 1, indexer.ID)
		assert.Equal(t, "Test Indexer", indexer.Name)
		assert.Equal(t, "http://localhost:9117", indexer.BaseURL)
		assert.Equal(t, "aither", indexer.IndexerID)
		assert.Equal(t, TorznabBackendJackett, indexer.Backend)
		assert.True(t, indexer.Enabled)
		assert.Equal(t, 100, indexer.Priority)
		assert.Equal(t, 30, indexer.TimeoutSeconds)
		assert.Len(t, indexer.Capabilities, 3)
		assert.Len(t, indexer.Categories, 2)
		assert.Equal(t, "success", indexer.LastTestStatus)
		assert.Equal(t, "connection timeout", *indexer.LastTestError)
	})
}

func TestTorznabIndexerUpdateParams(t *testing.T) {
	t.Parallel()

	t.Run("partial update params", func(t *testing.T) {
		t.Parallel()

		enabled := true
		priority := 50

		params := TorznabIndexerUpdateParams{
			Name:     "Updated Name",
			Enabled:  &enabled,
			Priority: &priority,
		}

		assert.Equal(t, "Updated Name", params.Name)
		assert.NotNil(t, params.Enabled)
		assert.True(t, *params.Enabled)
		assert.NotNil(t, params.Priority)
		assert.Equal(t, 50, *params.Priority)
		assert.Empty(t, params.BaseURL)
		assert.Nil(t, params.IndexerID)
		assert.Nil(t, params.Backend)
		assert.Empty(t, params.APIKey)
		assert.Nil(t, params.TimeoutSeconds)
	})
}

func TestTorznabIndexerCategory(t *testing.T) {
	t.Parallel()

	t.Run("with parent category", func(t *testing.T) {
		t.Parallel()

		parentID := 2000
		category := TorznabIndexerCategory{
			IndexerID:      1,
			CategoryID:     2010,
			CategoryName:   "Movies/Foreign",
			ParentCategory: &parentID,
		}

		assert.Equal(t, 1, category.IndexerID)
		assert.Equal(t, 2010, category.CategoryID)
		assert.Equal(t, "Movies/Foreign", category.CategoryName)
		require.NotNil(t, category.ParentCategory)
		assert.Equal(t, 2000, *category.ParentCategory)
	})

	t.Run("without parent category", func(t *testing.T) {
		t.Parallel()

		category := TorznabIndexerCategory{
			IndexerID:    1,
			CategoryID:   2000,
			CategoryName: "Movies",
		}

		assert.Nil(t, category.ParentCategory)
	})
}

func TestTorznabIndexerError(t *testing.T) {
	t.Parallel()

	t.Run("error struct fields", func(t *testing.T) {
		t.Parallel()

		indexerError := TorznabIndexerError{
			ID:           1,
			IndexerID:    5,
			ErrorMessage: "Connection refused",
			ErrorCode:    "CONN_REFUSED",
			ErrorCount:   3,
		}

		assert.Equal(t, 1, indexerError.ID)
		assert.Equal(t, 5, indexerError.IndexerID)
		assert.Equal(t, "Connection refused", indexerError.ErrorMessage)
		assert.Equal(t, "CONN_REFUSED", indexerError.ErrorCode)
		assert.Equal(t, 3, indexerError.ErrorCount)
		assert.Nil(t, indexerError.ResolvedAt)
	})
}

func TestTorznabIndexerLatencyStats(t *testing.T) {
	t.Parallel()

	t.Run("latency stats fields", func(t *testing.T) {
		t.Parallel()

		avgLatency := 150.5
		minLatency := 50
		maxLatency := 500

		stats := TorznabIndexerLatencyStats{
			IndexerID:          1,
			OperationType:      "search",
			TotalRequests:      100,
			SuccessfulRequests: 95,
			AvgLatencyMs:       &avgLatency,
			MinLatencyMs:       &minLatency,
			MaxLatencyMs:       &maxLatency,
			SuccessRatePct:     95.0,
		}

		assert.Equal(t, 1, stats.IndexerID)
		assert.Equal(t, "search", stats.OperationType)
		assert.Equal(t, 100, stats.TotalRequests)
		assert.Equal(t, 95, stats.SuccessfulRequests)
		assert.Equal(t, 150.5, *stats.AvgLatencyMs)
		assert.Equal(t, 50, *stats.MinLatencyMs)
		assert.Equal(t, 500, *stats.MaxLatencyMs)
		assert.Equal(t, 95.0, stats.SuccessRatePct)
	})
}

func TestTorznabIndexerHealth(t *testing.T) {
	t.Parallel()

	t.Run("health struct fields", func(t *testing.T) {
		t.Parallel()

		avgLatency := 200.0
		successRate := 98.5
		requests := 1000

		health := TorznabIndexerHealth{
			IndexerID:        1,
			IndexerName:      "Test Indexer",
			Enabled:          true,
			LastTestStatus:   "success",
			ErrorsLast24h:    2,
			UnresolvedErrors: 0,
			AvgLatencyMs:     &avgLatency,
			SuccessRatePct:   &successRate,
			RequestsLast7d:   &requests,
		}

		assert.Equal(t, 1, health.IndexerID)
		assert.Equal(t, "Test Indexer", health.IndexerName)
		assert.True(t, health.Enabled)
		assert.Equal(t, "success", health.LastTestStatus)
		assert.Equal(t, 2, health.ErrorsLast24h)
		assert.Equal(t, 0, health.UnresolvedErrors)
		assert.Equal(t, 200.0, *health.AvgLatencyMs)
		assert.Equal(t, 98.5, *health.SuccessRatePct)
		assert.Equal(t, 1000, *health.RequestsLast7d)
	})
}

func TestTorznabIndexerCooldown(t *testing.T) {
	t.Parallel()

	t.Run("cooldown struct", func(t *testing.T) {
		t.Parallel()

		cooldown := TorznabIndexerCooldown{
			IndexerID: 1,
			Reason:    "Rate limited",
		}

		assert.Equal(t, 1, cooldown.IndexerID)
		assert.Equal(t, "Rate limited", cooldown.Reason)
	})
}
