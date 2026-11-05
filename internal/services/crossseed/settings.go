// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/autobrr/qui/internal/models"
)

// SearchSettingsPatch captures optional updates to seeded search defaults.
type SearchSettingsPatch struct {
	InstanceIDSet   bool
	InstanceID      *int
	Categories      *[]string
	Tags            *[]string
	IndexerIDs      *[]int
	IntervalSeconds *int
	CooldownMinutes *int
}

// GetAutomationSettings returns the persisted automation configuration.
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

// UpdateAutomationSettings persists automation configuration and wakes the scheduler.
func (s *Service) UpdateAutomationSettings(ctx context.Context, settings *models.CrossSeedAutomationSettings) (*models.CrossSeedAutomationSettings, error) {
	if settings == nil {
		return nil, errors.New("settings cannot be nil")
	}

	// Validate and normalize settings before checking store
	s.validateAndNormalizeSettings(settings)

	if s.automationStore == nil {
		return nil, errors.New("automation storage not configured")
	}

	updated, err := s.automationStore.UpsertSettings(ctx, settings)
	if err != nil {
		return nil, fmt.Errorf("persist automation settings: %w", err)
	}
	s.forgetLocalReads()

	s.signalAutomationWake()
	s.partialPool.wake(partialPoolWake{})

	return updated, nil
}

// validateAndNormalizeSettings validates and normalizes automation settings.
// These settings apply to RSS Automation only, not Seeded Torrent Search.
func (s *Service) validateAndNormalizeSettings(settings *models.CrossSeedAutomationSettings) {
	// RSS Automation: minimum 30 minutes between RSS feed polls, default 120 minutes
	if settings.RunIntervalMinutes <= 0 {
		settings.RunIntervalMinutes = 120
	} else if settings.RunIntervalMinutes < 30 {
		settings.RunIntervalMinutes = 30
	}
	// RSS Automation: maximum number of RSS results to process per run
	if settings.MaxResultsPerRun <= 0 {
		settings.MaxResultsPerRun = 50
	}
}

func normalizeSearchTiming(intervalSeconds, cooldownMinutes int) (int, int) {
	if intervalSeconds < minSearchIntervalSecondsTorznab {
		intervalSeconds = minSearchIntervalSecondsTorznab
	}
	if cooldownMinutes < minSearchCooldownMinutes {
		cooldownMinutes = minSearchCooldownMinutes
	}
	return intervalSeconds, cooldownMinutes
}

func (s *Service) normalizeSearchSettings(settings *models.CrossSeedSearchSettings) {
	if settings == nil {
		return
	}
	settings.Categories = normalizeStringSlice(settings.Categories)
	settings.Tags = normalizeStringSlice(settings.Tags)
	settings.IndexerIDs = uniquePositiveInts(settings.IndexerIDs)
	settings.IntervalSeconds, settings.CooldownMinutes = normalizeSearchTiming(settings.IntervalSeconds, settings.CooldownMinutes)
}

// GetSearchSettings returns stored defaults for seeded torrent searches.
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

// PatchSearchSettings merges updates into stored seeded search defaults.
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

	settings, err := s.GetSearchSettings(ctx)
	if err != nil {
		return nil, err
	}
	if settings == nil {
		settings = models.DefaultCrossSeedSearchSettings()
	}

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

	return s.automationStore.UpsertSearchSettings(ctx, settings)
}
