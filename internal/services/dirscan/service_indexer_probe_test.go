// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
)

type failingDirScanIndexerStore struct {
	err error
}

func (s *failingDirScanIndexerStore) Get(context.Context, int) (*models.TorznabIndexer, error) {
	return nil, nil
}

func (s *failingDirScanIndexerStore) List(context.Context) ([]*models.TorznabIndexer, error) {
	return []*models.TorznabIndexer{}, nil
}

func (s *failingDirScanIndexerStore) ListEnabled(context.Context) ([]*models.TorznabIndexer, error) {
	if s.err != nil {
		return nil, s.err
	}
	return []*models.TorznabIndexer{}, nil
}

func (s *failingDirScanIndexerStore) GetDecryptedAPIKey(*models.TorznabIndexer) (string, error) {
	return "", nil
}

func (s *failingDirScanIndexerStore) GetDecryptedBasicPassword(*models.TorznabIndexer) (string, error) {
	return "", nil
}

func (s *failingDirScanIndexerStore) GetCapabilities(context.Context, int) ([]string, error) {
	return []string{}, nil
}

func (s *failingDirScanIndexerStore) SetCapabilities(context.Context, int, []string) error {
	return nil
}

func (s *failingDirScanIndexerStore) SetCategories(context.Context, int, []models.TorznabIndexerCategory) error {
	return nil
}

func (s *failingDirScanIndexerStore) SetLimits(context.Context, int, int, int) error {
	return nil
}

func (s *failingDirScanIndexerStore) RecordLatency(context.Context, int, string, int, bool) error {
	return nil
}

func (s *failingDirScanIndexerStore) RecordError(context.Context, int, string, string) error {
	return nil
}

func (s *failingDirScanIndexerStore) ListRateLimitCooldowns(context.Context) ([]models.TorznabIndexerCooldown, error) {
	return []models.TorznabIndexerCooldown{}, nil
}

func (s *failingDirScanIndexerStore) UpsertRateLimitCooldown(context.Context, int, time.Time, time.Duration, string) error {
	return nil
}

func (s *failingDirScanIndexerStore) DeleteRateLimitCooldown(context.Context, int) error {
	return nil
}

func TestService_ProcessSearchee_EnabledIndexerProbeError_ReturnsSearchError(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	l := zerolog.New(io.Discard)

	jackettSvc := jackett.NewService(&failingDirScanIndexerStore{err: errors.New("jackett down")})
	svc := &Service{
		jackettService: jackettSvc,
		parser:         NewParser(nil),
	}

	dir := &models.DirScanDirectory{ID: 1}
	settings := &models.DirScanSettings{MatchMode: models.MatchModeStrict}
	matcher := NewMatcher(MatchModeStrict, 0)

	searchee := &Searchee{
		Name: "Example.Movie.2024.1080p.WEB-DL",
		Path: "/tmp/example",
		Files: []*ScannedFile{
			{Path: "/tmp/example/video.mkv", RelPath: "video.mkv", Size: 123},
		},
	}

	matches, outcome := svc.processSearchee(ctx, dir, searchee, settings, matcher, 1, &l)
	require.Nil(t, matches)
	require.False(t, outcome.searched)
	require.True(t, outcome.searchError)
}
