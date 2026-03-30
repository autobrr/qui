// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package metadata

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// Provider looks up episode counts from an external metadata source.
type Provider interface {
	EpisodesInSeason(ctx context.Context, title string, seasonNumber int) (int, error)
}

// Service orchestrates metadata lookups across configured providers with caching.
type Service struct {
	tvdb   Provider // nil if not configured
	tvmaze Provider // always available
	cache  *resultCache
}

// NewService creates a metadata service. TVMaze is always enabled.
// TVDB is only enabled when tvdbAPIKey is non-empty.
func NewService(tvdbAPIKey, tvdbPIN string) *Service {
	s := &Service{
		tvmaze: newTVMazeProvider(),
		cache:  newResultCache(),
	}

	if tvdbAPIKey != "" {
		s.tvdb = newTVDBProvider(tvdbAPIKey, tvdbPIN)
		log.Info().Msg("metadata: TVDB provider enabled")
	} else {
		log.Info().Msg("metadata: TVDB not configured, using TVMaze only")
	}

	return s
}

// LookupEpisodeTotal returns the total number of episodes in a season for the given show title.
// Results are cached for 1 hour after a successful lookup.
func (s *Service) LookupEpisodeTotal(ctx context.Context, title string, seasonNumber int) (int, error) {
	if count, ok := s.cache.Get(title, seasonNumber); ok {
		log.Debug().Str("title", title).Int("season", seasonNumber).Int("count", count).Msg("metadata: cache hit")
		return count, nil
	}

	count, err := s.queryProviders(ctx, title, seasonNumber)
	if err != nil {
		return 0, err
	}

	s.cache.Set(title, seasonNumber, count)

	return count, nil
}

func (s *Service) queryProviders(ctx context.Context, title string, seasonNumber int) (int, error) {
	if s.tvdb == nil {
		return s.tvmaze.EpisodesInSeason(ctx, title, seasonNumber)
	}

	return s.queryBothProviders(ctx, title, seasonNumber)
}

func (s *Service) queryBothProviders(ctx context.Context, title string, seasonNumber int) (int, error) {
	type result struct {
		count int
		err   error
	}

	var (
		tvdbRes, tvmazeRes result
		wg                 sync.WaitGroup
	)

	wg.Add(2)

	go func() {
		defer wg.Done()
		count, err := s.tvdb.EpisodesInSeason(ctx, title, seasonNumber)
		tvdbRes = result{count, err}
	}()

	go func() {
		defer wg.Done()
		count, err := s.tvmaze.EpisodesInSeason(ctx, title, seasonNumber)
		tvmazeRes = result{count, err}
	}()

	wg.Wait()

	return s.resolveResults(title, seasonNumber, tvdbRes.count, tvdbRes.err, tvmazeRes.count, tvmazeRes.err)
}

func (s *Service) resolveResults(title string, season, tvdbCount int, tvdbErr error, tvmazeCount int, tvmazeErr error) (int, error) {
	tvdbOK := tvdbErr == nil
	tvmazeOK := tvmazeErr == nil

	switch {
	case tvdbOK && tvmazeOK:
		if tvdbCount != tvmazeCount {
			log.Warn().
				Str("title", title).
				Int("season", season).
				Int("tvdb", tvdbCount).
				Int("tvmaze", tvmazeCount).
				Msg("metadata: providers disagree on episode count, using TVDB")
		}
		return tvdbCount, nil

	case tvdbOK:
		log.Debug().Str("title", title).Err(tvmazeErr).Msg("metadata: TVMaze failed, using TVDB result")
		return tvdbCount, nil

	case tvmazeOK:
		log.Debug().Str("title", title).Err(tvdbErr).Msg("metadata: TVDB failed, using TVMaze result")
		return tvmazeCount, nil

	default:
		return 0, fmt.Errorf("metadata: all providers failed for %q S%02d: tvdb: %w; tvmaze: %s",
			title, season, tvdbErr, tvmazeErr.Error())
	}
}

// HasTVDB reports whether the TVDB provider is configured.
func (s *Service) HasTVDB() bool {
	return s.tvdb != nil
}
