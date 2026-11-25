// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"

	"github.com/autobrr/qui/internal/models"
)

type SearchSettingsPatch struct {
	InstanceIDSet          bool
	InstanceID             *int
	Categories             *[]string
	Tags                   *[]string
	ExcludeCategories      *[]string
	ExcludeTags            *[]string
	IndexerIDs             *[]int
	IntervalSeconds        *int
	CooldownMinutes        *int
	MaxAddedAgeDays        *int
	SkipIndividualEpisodes *bool
	TorznabEnabled         *bool
	ScheduleEnabled        *bool
	ScheduleIntervalHours  *int
	ScheduleMaxSearches    *int
	ScheduleMaxAgeDays     *int
}

func (s *Service) GetAutomationSettings(ctx context.Context) (*models.CrossSeedAutomationSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, automationSettingsQueryTimeout)
	defer cancel()

	if s.automationSettingsLoader != nil {
		return s.automationSettingsLoader(ctx)
	}
	if s.automationStore == nil {
		return models.DefaultCrossSeedAutomationSettings(), nil
	}

	settings, err := s.automationStore.GetSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load automation settings: %w", err)
	}

	if settings == nil {
		settings = models.DefaultCrossSeedAutomationSettings()
	}

	return settings, nil
}

func (s *Service) savedAutomationSettings(ctx context.Context) (*models.CrossSeedAutomationSettings, error) {
	if settings := s.automationSettings.Load(); settings != nil {
		return settings, nil
	}
	load := s.automationSettingsLoad.DoChan("", func() (any, error) {
		s.settingsMu.Lock()
		defer s.settingsMu.Unlock()
		if settings := s.automationSettings.Load(); settings != nil {
			return settings, nil
		}
		settings, err := s.GetAutomationSettings(ctx)
		if err != nil {
			return nil, err
		}
		s.automationSettings.Store(settings)
		return settings, nil
	})
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-load:
		if result.Err != nil {
			return nil, result.Err
		}
		settings, _ := result.Val.(*models.CrossSeedAutomationSettings)
		return settings, nil
	}
}

func (s *Service) ListBlocklist(ctx context.Context, instanceID int) ([]*models.CrossSeedBlocklistEntry, error) {
	if s.blocklistStore == nil {
		return nil, errors.New("blocklist store unavailable")
	}
	return s.blocklistStore.List(ctx, instanceID)
}

func (s *Service) UpsertBlocklistEntry(ctx context.Context, entry *models.CrossSeedBlocklistEntry) (*models.CrossSeedBlocklistEntry, error) {
	if s.blocklistStore == nil {
		return nil, errors.New("blocklist store unavailable")
	}
	return s.blocklistStore.Upsert(ctx, entry)
}

func (s *Service) DeleteBlocklistEntry(ctx context.Context, instanceID int, infoHash string) error {
	if s.blocklistStore == nil {
		return errors.New("blocklist store unavailable")
	}
	if err := s.blocklistStore.Delete(ctx, instanceID, infoHash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return ErrBlocklistEntryNotFound
		}
		return err
	}
	return nil
}

func (s *Service) UpdateAutomationSettings(ctx context.Context, settings *models.CrossSeedAutomationSettings) (*models.CrossSeedAutomationSettings, error) {
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	s.validateAndNormalizeSettings(settings)

	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	previous, previousErr := s.GetAutomationSettings(ctx)
	updated, err := s.automationStore.UpsertSettings(ctx, settings)
	if err != nil {
		s.automationSettings.Store(nil)
		return nil, fmt.Errorf("persist automation settings: %w", err)
	}
	s.automationSettings.Store(updated)
	if previousErr != nil || !slices.EqualFunc(previous.CategoryMappingRules, updated.CategoryMappingRules, func(a, b models.CategoryMappingRule) bool {
		return a.ContentType == b.ContentType && slices.Equal(a.Categories, b.Categories)
	}) {
		s.forgetLocalReads()
	}

	s.signalAutomationWake()
	s.partialPool.wake(partialPoolWake{})

	return updated, nil
}

func (s *Service) validateAndNormalizeSettings(settings *models.CrossSeedAutomationSettings) {
	if settings.RunIntervalMinutes <= 0 {
		settings.RunIntervalMinutes = 120
	} else if settings.RunIntervalMinutes < 30 {
		settings.RunIntervalMinutes = 30
	}
	if settings.MaxResultsPerRun <= 0 {
		settings.MaxResultsPerRun = 50
	}
}

func (s *Service) normalizeSearchSettings(settings *models.CrossSeedSearchSettings) {
	if settings == nil {
		return
	}
	settings.Categories = normalizeStringSlice(settings.Categories)
	settings.Tags = normalizeStringSlice(settings.Tags)
	settings.ExcludeCategories = normalizeStringSlice(settings.ExcludeCategories)
	settings.ExcludeTags = normalizeStringSlice(settings.ExcludeTags)
	settings.IndexerIDs = uniquePositiveInts(settings.IndexerIDs)
	settings.IntervalSeconds, settings.CooldownMinutes = normalizeSearchRunTiming(settings.IntervalSeconds, settings.CooldownMinutes, !settings.TorznabEnabled)
	settings.MaxAddedAgeDays = max(settings.MaxAddedAgeDays, 0)
	settings.ScheduleIntervalHours = min(max(settings.ScheduleIntervalHours, 1), maxScheduleIntervalHours)
	settings.ScheduleMaxSearches = max(settings.ScheduleMaxSearches, 1)
	settings.ScheduleMaxAgeDays = min(max(settings.ScheduleMaxAgeDays, 1), maxScheduleMaxAgeDays)
}

func (s *Service) GetSearchSettings(ctx context.Context) (*models.CrossSeedSearchSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, automationSettingsQueryTimeout)
	defer cancel()

	if s.automationStore == nil {
		settings := models.DefaultCrossSeedSearchSettings()
		s.normalizeSearchSettings(settings)
		return settings, nil
	}

	settings, err := s.automationStore.GetSearchSettings(ctx)
	if err != nil {
		return nil, fmt.Errorf("load search settings: %w", err)
	}
	if settings == nil {
		settings = models.DefaultCrossSeedSearchSettings()
	}

	s.normalizeSearchSettings(settings)

	if settings.InstanceID != nil {
		instance, err := s.instanceStore.Get(ctx, *settings.InstanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				settings.InstanceID = nil
			} else {
				return nil, fmt.Errorf("load instance %d: %w", *settings.InstanceID, err)
			}
		}
		if instance == nil {
			settings.InstanceID = nil
		}
	}

	return settings, nil
}

func (s *Service) PatchSearchSettings(ctx context.Context, patch SearchSettingsPatch) (*models.CrossSeedSearchSettings, error) {
	if ctx == nil {
		ctx = context.Background()
	} else {
		ctx = context.WithoutCancel(ctx)
	}

	ctx, cancel := context.WithTimeout(ctx, automationSettingsQueryTimeout)
	defer cancel()

	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}

	s.settingsMu.Lock()
	defer s.settingsMu.Unlock()
	settings, err := s.GetSearchSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedSearchSettings()
	}

	turnedOn := patch.ScheduleEnabled != nil && *patch.ScheduleEnabled && !settings.ScheduleEnabled
	patch.apply(settings)

	s.normalizeSearchSettings(settings)

	if settings.InstanceID != nil {
		instance, err := s.instanceStore.Get(ctx, *settings.InstanceID)
		if err != nil {
			if errors.Is(err, models.ErrInstanceNotFound) {
				return nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, *settings.InstanceID)
			}
			return nil, fmt.Errorf("load instance %d: %w", *settings.InstanceID, err)
		}
		if instance == nil {
			return nil, fmt.Errorf("%w: instance %d not found", ErrInvalidRequest, *settings.InstanceID)
		}
	}

	if turnedOn {
		if err := s.forgetLastScheduledStart(ctx); err != nil {
			return nil, err
		}
	}
	saved, err := s.automationStore.UpsertSearchSettings(ctx, settings)
	if err != nil {
		return nil, err
	}
	s.librarySettingsSaved()
	return saved, nil
}

func (patch SearchSettingsPatch) apply(settings *models.CrossSeedSearchSettings) {
	if patch.InstanceIDSet {
		settings.InstanceID = patch.InstanceID
	}
	if patch.Categories != nil {
		settings.Categories = append([]string(nil), *patch.Categories...)
	}
	if patch.Tags != nil {
		settings.Tags = append([]string(nil), *patch.Tags...)
	}
	if patch.IndexerIDs != nil {
		settings.IndexerIDs = append([]int(nil), *patch.IndexerIDs...)
	}
	if patch.IntervalSeconds != nil {
		settings.IntervalSeconds = *patch.IntervalSeconds
	}
	if patch.CooldownMinutes != nil {
		settings.CooldownMinutes = *patch.CooldownMinutes
	}
	if patch.ExcludeCategories != nil {
		settings.ExcludeCategories = slices.Clone(*patch.ExcludeCategories)
	}
	if patch.ExcludeTags != nil {
		settings.ExcludeTags = slices.Clone(*patch.ExcludeTags)
	}
	if patch.MaxAddedAgeDays != nil {
		settings.MaxAddedAgeDays = *patch.MaxAddedAgeDays
	}
	if patch.SkipIndividualEpisodes != nil {
		settings.SkipIndividualEpisodes = *patch.SkipIndividualEpisodes
	}
	if patch.TorznabEnabled != nil {
		settings.TorznabEnabled = *patch.TorznabEnabled
	}
	if patch.ScheduleEnabled != nil {
		settings.ScheduleEnabled = *patch.ScheduleEnabled
	}
	if patch.ScheduleIntervalHours != nil {
		settings.ScheduleIntervalHours = *patch.ScheduleIntervalHours
	}
	if patch.ScheduleMaxSearches != nil {
		settings.ScheduleMaxSearches = *patch.ScheduleMaxSearches
	}
	if patch.ScheduleMaxAgeDays != nil {
		settings.ScheduleMaxAgeDays = *patch.ScheduleMaxAgeDays
	}
}
