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
		tt := tt
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
