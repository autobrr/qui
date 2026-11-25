// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/autobrr/qui/pkg/version"

	"github.com/rs/zerolog"
)

const defaultCheckInterval = 2 * time.Hour

type Service struct {
	log            zerolog.Logger
	currentVersion string

	mu             sync.RWMutex
	releaseChecker *version.Checker
	latestRelease  *version.Release
	lastChecked    time.Time
	lastTag        string
	isEnabled      atomic.Bool
}

func NewService(log zerolog.Logger, enabled bool, currentVersion, userAgent string) *Service {
	svc := &Service{
		log:            log.With().Str("component", "update").Logger(),
		currentVersion: currentVersion,
		releaseChecker: version.NewChecker("autobrr", "qui", userAgent),
	}
	svc.isEnabled.Store(enabled)
	return svc
}

func (s *Service) Start(ctx context.Context) {
	go func() {
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

func (s *Service) GetLatestRelease(_ context.Context) *version.Release {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.latestRelease
}

func (s *Service) CheckUpdates(ctx context.Context) {
	if !s.isEnabled.Load() {
		s.log.Trace().Msg("skipping update check - disabled in config")
		return
	}

	if _, err := s.CheckUpdateAvailable(ctx); err != nil {
		s.log.Error().Err(err).Msg("error checking new release")
	}
}

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

func (s *Service) SetEnabled(enabled bool) {
	s.isEnabled.Store(enabled)
	if !enabled {
		s.mu.Lock()
		s.latestRelease = nil
		s.lastTag = ""
		s.lastChecked = time.Time{}
		s.mu.Unlock()
	}
}
