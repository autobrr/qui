package crossseed

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestAutobrrApplyDefaultsToAutomationSetting(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	service := &Service{
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{
				FindIndividualEpisodes: true,
				IgnorePatterns:         []string{"*.nfo"},
			}, nil
		},
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Success: true}, nil
	}

	req := &AutobrrApplyRequest{
		TorrentData: "ZGF0YQ==",
		InstanceIDs: []int{1},
	}

	_, err := service.AutobrrApply(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.True(t, captured.FindIndividualEpisodes)
	require.Equal(t, []string{"*.nfo"}, captured.IgnorePatterns)
}

func TestAutobrrApplyHonorsRequestOverride(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	service := &Service{
		automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
			return &models.CrossSeedAutomationSettings{FindIndividualEpisodes: true}, nil
		},
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Success: true}, nil
	}

	override := false
	req := &AutobrrApplyRequest{
		TorrentData:            "ZGF0YQ==",
		InstanceIDs:            []int{1},
		FindIndividualEpisodes: &override,
		IgnorePatterns:         []string{"*.txt"},
	}

	_, err := service.AutobrrApply(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.False(t, captured.FindIndividualEpisodes)
	require.Equal(t, []string{"*.txt"}, captured.IgnorePatterns)
}

func TestAutobrrApplyTargetInstanceIDs(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name        string
		instanceIDs []int
		expectIDs   []int
		expectError string
	}{
		{
			name:        "globalWhenOmitted",
			instanceIDs: nil,
			expectIDs:   nil,
		},
		{
			name:        "globalWhenEmpty",
			instanceIDs: []int{},
			expectIDs:   nil,
		},
		{
			name:        "dedupePositiveOnly",
			instanceIDs: []int{2, 1, 2, -1},
			expectIDs:   []int{2, 1},
		},
		{
			name:        "invalidWhenNoPositiveRemain",
			instanceIDs: []int{-2, 0},
			expectError: "instanceIds must contain at least one positive integer",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{}
			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{Success: true}, nil
			}

			req := &AutobrrApplyRequest{
				TorrentData: "ZGF0YQ==",
				InstanceIDs: tt.instanceIDs,
			}

			resp, err := service.AutobrrApply(ctx, req)
			if tt.expectError != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.expectError)
				require.Nil(t, resp)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, captured)
			require.Equal(t, tt.expectIDs, captured.TargetInstanceIDs)
		})
	}
}

// TestAutobrrApply_RespectsWebhookSourceFilters verifies that AutobrrApply passes
// webhook source filters through to the CrossSeedRequest. This is an integration test
// that catches the bug where filters worked in isolation but weren't passed through the flow.
func TestAutobrrApply_RespectsWebhookSourceFilters(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name                     string
		settings                 *models.CrossSeedAutomationSettings
		expectCategories         []string
		expectTags               []string
		expectExcludeCategories  []string
		expectExcludeTags        []string
	}{
		{
			name: "include categories passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories: []string{"movies", "tv"},
			},
			expectCategories:         []string{"movies", "tv"},
			expectTags:               nil,
			expectExcludeCategories:  nil,
			expectExcludeTags:        nil,
		},
		{
			name: "include tags passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceTags: []string{"cross-seed", "priority"},
			},
			expectCategories:         nil,
			expectTags:               []string{"cross-seed", "priority"},
			expectExcludeCategories:  nil,
			expectExcludeTags:        nil,
		},
		{
			name: "exclude categories passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeCategories: []string{"cross-seed-link", "temp"},
			},
			expectCategories:         nil,
			expectTags:               nil,
			expectExcludeCategories:  []string{"cross-seed-link", "temp"},
			expectExcludeTags:        nil,
		},
		{
			name: "exclude tags passed through",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceExcludeTags: []string{"no-cross-seed", "blocked"},
			},
			expectCategories:         nil,
			expectTags:               nil,
			expectExcludeCategories:  nil,
			expectExcludeTags:        []string{"no-cross-seed", "blocked"},
		},
		{
			name: "all filters passed through together",
			settings: &models.CrossSeedAutomationSettings{
				WebhookSourceCategories:        []string{"movies-LTS"},
				WebhookSourceTags:              []string{"important"},
				WebhookSourceExcludeCategories: []string{"movies-Race"},
				WebhookSourceExcludeTags:       []string{"temporary"},
			},
			expectCategories:         []string{"movies-LTS"},
			expectTags:               []string{"important"},
			expectExcludeCategories:  []string{"movies-Race"},
			expectExcludeTags:        []string{"temporary"},
		},
		{
			name:                     "nil settings results in empty filters",
			settings:                 nil,
			expectCategories:         nil,
			expectTags:               nil,
			expectExcludeCategories:  nil,
			expectExcludeTags:        nil,
		},
		{
			name:                     "empty settings results in empty filters",
			settings:                 &models.CrossSeedAutomationSettings{},
			expectCategories:         nil,
			expectTags:               nil,
			expectExcludeCategories:  nil,
			expectExcludeTags:        nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			service := &Service{
				automationSettingsLoader: func(context.Context) (*models.CrossSeedAutomationSettings, error) {
					return tt.settings, nil
				},
			}

			var captured *CrossSeedRequest
			service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
				captured = req
				return &CrossSeedResponse{Success: true}, nil
			}

			req := &AutobrrApplyRequest{
				TorrentData: "ZGF0YQ==",
				InstanceIDs: []int{1},
			}

			_, err := service.AutobrrApply(ctx, req)
			require.NoError(t, err)
			require.NotNil(t, captured, "CrossSeedRequest should have been captured")

			// Verify source filters were passed through
			require.Equal(t, tt.expectCategories, captured.SourceFilterCategories, "SourceFilterCategories mismatch")
			require.Equal(t, tt.expectTags, captured.SourceFilterTags, "SourceFilterTags mismatch")
			require.Equal(t, tt.expectExcludeCategories, captured.SourceFilterExcludeCategories, "SourceFilterExcludeCategories mismatch")
			require.Equal(t, tt.expectExcludeTags, captured.SourceFilterExcludeTags, "SourceFilterExcludeTags mismatch")
		})
	}
}
