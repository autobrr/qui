// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

type Settings struct {
	Recording           bool `json:"recording"`
	DetailRetentionDays int  `json:"detailRetentionDays"`
}

var DetailRetentions = []int{90, 400, 0}

var ErrInvalidSettings = errors.New("invalid settings")

func (m *Module) SetSettings(ctx context.Context, s Settings) error {
	if !slices.Contains(DetailRetentions, s.DetailRetentionDays) {
		return fmt.Errorf("%w: the detail retention is not one of %v days", ErrInvalidSettings, DetailRetentions)
	}
	m.settingsMu.Lock()
	defer m.settingsMu.Unlock()
	if err := m.store.SaveSettings(ctx, models.TransferHistorySettings(s)); err != nil {
		return err
	}
	m.mu.Lock()
	old, known := m.settings, m.settingsLoaded
	m.settings, m.settingsLoaded = s, true
	if (!known || old.Recording) && !s.Recording {
		m.queue = nil
		m.pause = true
	}
	if !known || old.DetailRetentionDays != s.DetailRetentionDays {
		m.refold = true
	}
	m.mu.Unlock()
	m.signal()
	return nil
}

func (m *Module) currentSettings(ctx context.Context) (Settings, error) {
	m.mu.Lock()
	settings, known := m.settings, m.settingsLoaded
	m.mu.Unlock()
	if known {
		return settings, nil
	}
	stored, err := m.store.Settings(ctx)
	if err != nil {
		return Settings{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.settingsLoaded {
		m.settings, m.settingsLoaded = Settings(stored), true
	}
	return m.settings, nil
}

func (m *Module) recording() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.settingsLoaded || m.settings.Recording
}

func (m *Module) pauseInstances() {
	for _, inst := range m.instances {
		_ = m.write(inst)
		inst.resumed = true
	}
	clear(m.reloads)
}

func (m *Module) foldIfNewDay(today int64, retentionDays int) {
	if today <= m.foldedDay {
		return
	}
	retain := retentionDays != 0
	beforeDay := today - int64(retentionDays)
	for _, inst := range m.instances {
		if retain && inst.holdsDaysBefore(beforeDay) && m.write(inst) != nil {
			return
		}
	}
	m.foldedDay = today
	idle := m.foldRun.TryLock()
	if idle {
		m.foldMu.Lock()
	}
	m.folds.Go(func() {
		if !idle {
			m.foldRun.Lock()
			m.foldMu.Lock()
		}
		defer m.foldRun.Unlock()
		ctx, cancel := context.WithTimeout(m.foldCtx, maxWriteTimeout)
		defer cancel()
		var err error
		if retain {
			err = m.foldHours(ctx, beforeDay)
		}
		m.foldMu.Unlock()
		if err == nil {
			err = m.store.MoveRecentTorrentDays(ctx, today)
		}
		if err == nil && retain {
			m.answers.foldStarted()
			err = m.store.FoldTorrentDays(ctx, beforeDay)
			m.answers.foldEnded()
		}
		if err != nil && m.foldCtx.Err() == nil {
			log.Error().Err(err).Int64("beforeDay", beforeDay).Msg("transfer history: failed to fold old detail; the next UTC day tries again")
		}
		m.dropSize()
	})
}

const sizeLife = time.Hour

func (m *Module) historySize(ctx context.Context) (int64, error) {
	m.sizeMu.Lock()
	defer m.sizeMu.Unlock()
	if !m.sizeAt.IsZero() && time.Since(m.sizeAt) < sizeLife {
		return m.size, nil
	}
	size, err := m.store.Size(ctx)
	if err != nil {
		return 0, err
	}
	m.size, m.sizeAt = size, time.Now()
	return size, nil
}

func (m *Module) dropSize() {
	m.sizeMu.Lock()
	m.sizeAt = time.Time{}
	m.sizeMu.Unlock()
}

func (m *Module) foldHours(ctx context.Context, beforeDay int64) error {
	instances, err := m.store.Instances(ctx)
	if err != nil {
		return err
	}
	for _, inst := range instances {
		if err := m.store.FoldHours(ctx, inst.InstanceID, beforeDay); err != nil {
			return err
		}
	}
	return nil
}
