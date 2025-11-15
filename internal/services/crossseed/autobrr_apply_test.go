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
			return &models.CrossSeedAutomationSettings{FindIndividualEpisodes: true}, nil
		},
	}

	var captured *CrossSeedRequest
	service.crossSeedInvoker = func(ctx context.Context, req *CrossSeedRequest) (*CrossSeedResponse, error) {
		captured = req
		return &CrossSeedResponse{Success: true}, nil
	}

	req := &AutobrrApplyRequest{
		TorrentData: "ZGF0YQ==",
		InstanceID:  1,
	}

	_, err := service.AutobrrApply(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.True(t, captured.FindIndividualEpisodes)
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
		InstanceID:             1,
		FindIndividualEpisodes: &override,
	}

	_, err := service.AutobrrApply(ctx, req)
	require.NoError(t, err)
	require.NotNil(t, captured)
	require.False(t, captured.FindIndividualEpisodes)
}
