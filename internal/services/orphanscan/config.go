// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import "time"

const (
	MaxSyncAge = 2 * time.Minute
)

type Config struct {
	SchedulerInterval time.Duration

	MaxJitter time.Duration
}

func DefaultConfig() Config {
	return Config{
		SchedulerInterval: 5 * time.Minute,
		MaxJitter:         30 * time.Second,
	}
}

func DefaultSettings() Settings {
	return Settings{
		Enabled:             false,
		GracePeriodMinutes:  10,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupEnabled:  false,
		AutoCleanupMaxFiles: 100,
		ScanDefaultSavePath: false,
		ScanCategoryPaths:   false,
		DeleteAbandonedDirs: false,
	}
}
