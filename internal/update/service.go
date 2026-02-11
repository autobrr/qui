// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"sync"
	"time"

	"github.com/autobrr/qui/pkg/version"

	"github.com/rs/zerolog"
)

const defaultCheckInterval = 2 * time.Hour

// Service periodically checks api.autobrr.com for new qui releases and caches the latest result.
type Service struct {
	log            zerolog.Logger
	currentVersion string

	mu             sync.RWMutex
	releaseChecker *version.Checker
	latestRelease  *version.Release
	lastChecked    time.Time
	lastTag        string
	isEnabled      bool
}

// NewService creates a new update Service instance.
func NewService(log zerolog.Logger, enabled bool, currentVersion, userAgent string) *Service {
	svc := &Service{
		log:            log.With().Str("component", "update").Logger(),
		currentVersion: currentVersion,
		releaseChecker: version.NewChecker("autobrr", "qui", userAgent),
		isEnabled:      enabled,
	}
	return svc
}

// Start launches a background loop that periodically checks for updates while the context is active.
func (s *Service) Start(ctx context.Context) {
	go func() {
		// Run an initial check shortly after startup so the banner can appear quickly.
		s.initialCheck(ctx)

		ticker := time.NewTicker(defaultCheckInterval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.CheckUpdates(ctx)
			}
		}
	}()
}

func (s *Service) initialCheck(ctx context.Context) {
	timer := time.NewTimer(2 * time.Second)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return
	case <-timer.C:
		s.CheckUpdates(ctx)
	}
}

// GetLatestRelease returns the last known release if a newer version has been found.
func (s *Service) GetLatestRelease(_ context.Context) *version.Release {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestRelease
}

// CheckUpdates triggers a refresh of the latest release information if updates are enabled.
func (s *Service) CheckUpdates(ctx context.Context) {
	if !s.isEnabled {
		s.log.Trace().Msg("skipping update check - disabled in config")
		return
	}

	if _, err := s.CheckUpdateAvailable(ctx); err != nil {
		s.log.Error().Err(err).Msg("error checking new release")
	}
}

// CheckUpdateAvailable performs an update check and returns the new release if one is available.
func (s *Service) CheckUpdateAvailable(ctx context.Context) (*version.Release, error) {
	s.log.Trace().Msg("checking for updates")

	newAvailable, release, err := s.releaseChecker.CheckNewVersion(ctx, s.currentVersion)
	if err != nil {
		return nil, err
	}

	if !newAvailable || release == nil {
		s.mu.Lock()
		s.latestRelease = nil
		s.mu.Unlock()
		return nil, nil
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if s.lastTag == release.TagName {
		s.lastChecked = time.Now()
		return s.latestRelease, nil
	}

	s.lastTag = release.TagName
	s.lastChecked = time.Now()
	s.latestRelease = release

	s.log.Info().Str("tag", release.TagName).Msg("new qui release detected")

	return release, nil
}

// SetEnabled toggles whether periodic update checks should run.
func (s *Service) SetEnabled(enabled bool) {
	s.isEnabled = enabled
	if !enabled {
		s.mu.Lock()
		s.latestRelease = nil
		s.lastTag = ""
		s.lastChecked = time.Time{}
		s.mu.Unlock()
	}
}
