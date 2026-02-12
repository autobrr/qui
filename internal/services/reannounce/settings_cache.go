// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reannounce

import (
	"context"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// SettingsCache provides an in-memory copy of instance reannounce settings.
type SettingsCache struct {
	store *models.InstanceReannounceStore
	mu    sync.RWMutex
	data  map[int]*models.InstanceReannounceSettings
}

// NewSettingsCache creates a cache backed by the given store.
func NewSettingsCache(store *models.InstanceReannounceStore) *SettingsCache {
	return &SettingsCache{
		store: store,
		data:  make(map[int]*models.InstanceReannounceSettings),
	}
}

// LoadAll refreshes the cache with all stored settings.
func (c *SettingsCache) LoadAll(ctx context.Context) error {
	if c.store == nil {
		return nil
	}
	settings, err := c.store.List(ctx)
	if err != nil {
		return err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	newData := make(map[int]*models.InstanceReannounceSettings, len(settings))
	for _, setting := range settings {
		newData[setting.InstanceID] = cloneSettings(setting)
	}
	c.data = newData
	return nil
}

// Replace updates the cache for a single instance.
func (c *SettingsCache) Replace(settings *models.InstanceReannounceSettings) {
	if c == nil || settings == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.data == nil {
		c.data = make(map[int]*models.InstanceReannounceSettings)
	}
	c.data[settings.InstanceID] = cloneSettings(settings)
}

// Get returns a copy of cached settings for an instance, or nil if absent.
func (c *SettingsCache) Get(instanceID int) *models.InstanceReannounceSettings {
	if c == nil {
		return nil
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	if settings, ok := c.data[instanceID]; ok {
		return cloneSettings(settings)
	}
	return nil
}

// StartAutoRefresh periodically reloads settings into the cache.
func (c *SettingsCache) StartAutoRefresh(ctx context.Context, interval time.Duration) {
	if c == nil || c.store == nil || interval <= 0 {
		return
	}
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if ctx.Err() != nil {
					return
				}
				if err := c.LoadAll(ctx); err != nil {
					log.Error().Err(err).Msg("Failed to refresh reannounce settings cache")
				}
			}
		}
	}()
}

func cloneSettings(src *models.InstanceReannounceSettings) *models.InstanceReannounceSettings {
	if src == nil {
		return nil
	}
	clone := *src
	clone.Categories = append([]string{}, src.Categories...)
	clone.Tags = append([]string{}, src.Tags...)
	clone.Trackers = append([]string{}, src.Trackers...)
	return &clone
}
