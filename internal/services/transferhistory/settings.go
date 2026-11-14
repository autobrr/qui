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

// Settings are the Statistics settings. While Recording is off, the module
// stores nothing, and that time becomes a Recording gap. DetailRetentionDays
// is the Detail retention: 90 or 400, or 0 to keep the detail forever.
type Settings struct {
	Recording           bool `json:"recording"`
	DetailRetentionDays int  `json:"detailRetentionDays"`
}

// DetailRetentions are the values of Settings.DetailRetentionDays.
var DetailRetentions = []int{90, 400, 0}

// ErrInvalidSettings is the error of settings that the module does not take.
var ErrInvalidSettings = errors.New("invalid settings")

// SetSettings stores the settings and applies them. When Recording goes off,
// the module drops the readings that wait and writes the changes in memory.
// A new Detail retention folds at the next reading.
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

// currentSettings returns the settings, and reads them from the database the
// first time.
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
	// SetSettings can store newer settings during the read.
	if !m.settingsLoaded {
		m.settings, m.settingsLoaded = Settings(stored), true
	}
	return m.settings, nil
}

// recording reports whether a reading can count. It is true until the module
// knows the settings, because the module drops a reading that came while
// recording was off.
func (m *Module) recording() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return !m.settingsLoaded || m.settings.Recording
}

// pauseInstances writes the changes of each instance, after recording went
// off. The next reading of an instance ends a Recording gap that starts at
// its last reading.
func (m *Module) pauseInstances() {
	for _, inst := range m.instances {
		_ = m.write(inst)
		inst.resumed = true
	}
	// An instance that a write with an unknown outcome dropped from memory
	// was not read while recording was off either.
	clear(m.reloads)
}

// foldIfNewDay starts a fold of the detail before the Detail retention when
// a reading at today starts a new UTC day. The fold runs beside the worker. A
// transfer query waits until the fold of the hours ends, because it reads the
// hours. The fold of the torrent days moves each byte in one transaction.
func (m *Module) foldIfNewDay(today int64, retentionDays int) {
	if today <= m.foldedDay || retentionDays == 0 {
		return
	}
	beforeDay := today - int64(retentionDays)
	// The changes in memory can hold a day that the fold covers, as for an
	// instance that is offline. A later write would make that day partial
	// again, so a failed write stops the fold until the next reading.
	for _, inst := range m.instances {
		if inst.holdsDaysBefore(beforeDay) && m.write(inst) != nil {
			return
		}
	}
	m.foldedDay = today
	// One fold runs at a time. When no fold runs, the lock is taken here, so
	// that a query after this reading sees the fold. A fold that still runs
	// moves its torrent days without the lock, so this fold waits for it
	// before it takes the lock, and the queries do not wait for that move.
	// Until then, a query reads the hours that this fold deletes.
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
		err := m.foldHours(ctx, beforeDay)
		m.foldMu.Unlock()
		if err == nil {
			m.answers.foldStarted()
			err = m.store.FoldTorrentDays(ctx, beforeDay)
			m.answers.foldEnded()
		}
		// At shutdown, the first reading after the next start folds again.
		if err != nil && m.foldCtx.Err() == nil {
			log.Error().Err(err).Int64("beforeDay", beforeDay).Msg("transfer history: failed to fold old detail; the next UTC day tries again")
		}
		m.dropSize()
	})
}

// sizeLife is how long the module keeps a measured size of the history. On
// SQLite, a measure reads each page of the history.
const sizeLife = time.Hour

// historySize returns the bytes that the history takes in the database,
// measured at most once in sizeLife and after each fold.
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

// foldHours folds the hourly totals of each instance before beforeDay.
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
