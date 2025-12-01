// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewService(t *testing.T) {
	log := zerolog.Nop()

	tests := []struct {
		name           string
		enabled        bool
		currentVersion string
		userAgent      string
	}{
		{
			name:           "enabled service",
			enabled:        true,
			currentVersion: "1.0.0",
			userAgent:      "qui/1.0.0",
		},
		{
			name:           "disabled service",
			enabled:        false,
			currentVersion: "2.0.0",
			userAgent:      "qui/2.0.0",
		},
		{
			name:           "empty version",
			enabled:        true,
			currentVersion: "",
			userAgent:      "qui/unknown",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewService(log, tt.enabled, tt.currentVersion, tt.userAgent)

			require.NotNil(t, svc)
			assert.Equal(t, tt.currentVersion, svc.currentVersion)
			assert.Equal(t, tt.enabled, svc.isEnabled)
			assert.NotNil(t, svc.releaseChecker)
		})
	}
}

func TestService_SetEnabled(t *testing.T) {
	log := zerolog.Nop()
	svc := NewService(log, false, "1.0.0", "test")

	assert.False(t, svc.isEnabled)

	svc.SetEnabled(true)
	assert.True(t, svc.isEnabled)

	svc.SetEnabled(false)
	assert.False(t, svc.isEnabled)
}

func TestService_GetLatestRelease_Initial(t *testing.T) {
	log := zerolog.Nop()
	svc := NewService(log, true, "1.0.0", "test")

	// Initially no release should be available
	release := svc.GetLatestRelease(context.Background())
	assert.Nil(t, release)
}

func TestService_CheckUpdates_Disabled(t *testing.T) {
	log := zerolog.Nop()
	svc := NewService(log, false, "1.0.0", "test")

	// Should not panic when disabled
	ctx := context.Background()
	svc.CheckUpdates(ctx)

	// Should still have no release
	release := svc.GetLatestRelease(ctx)
	assert.Nil(t, release)
}

func TestService_ConcurrentAccess(t *testing.T) {
	log := zerolog.Nop()
	svc := NewService(log, true, "1.0.0", "test")

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup

	// Multiple goroutines reading
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				_ = svc.GetLatestRelease(ctx)
			}
		}()
	}

	// Multiple goroutines toggling enabled
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				svc.SetEnabled(j%2 == 0)
			}
		}()
	}

	wg.Wait()
}

func TestService_Start_ContextCancellation(t *testing.T) {
	log := zerolog.Nop()
	svc := NewService(log, true, "1.0.0", "test")

	ctx, cancel := context.WithCancel(context.Background())

	// Start the service
	svc.Start(ctx)

	// Give it a moment to start
	time.Sleep(10 * time.Millisecond)

	// Cancel should stop the service gracefully
	cancel()

	// Give it a moment to stop
	time.Sleep(50 * time.Millisecond)
}

func TestNewUpdater(t *testing.T) {
	tests := []struct {
		name   string
		config Config
	}{
		{
			name: "valid config",
			config: Config{
				Repository: "autobrr/qui",
				Version:    "1.0.0",
			},
		},
		{
			name: "empty config",
			config: Config{
				Repository: "",
				Version:    "",
			},
		},
		{
			name: "prerelease version",
			config: Config{
				Repository: "autobrr/qui",
				Version:    "1.0.0-alpha.1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			updater := NewUpdater(tt.config)

			require.NotNil(t, updater)
			assert.Equal(t, tt.config.Repository, updater.config.Repository)
			assert.Equal(t, tt.config.Version, updater.config.Version)
		})
	}
}

func TestUpdater_Run_InvalidVersion(t *testing.T) {
	updater := NewUpdater(Config{
		Repository: "autobrr/qui",
		Version:    "not-a-valid-semver",
	})

	ctx := context.Background()
	err := updater.Run(ctx)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not parse version")
}
