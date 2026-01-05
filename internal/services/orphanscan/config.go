// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import "time"

// Config holds the service configuration.
type Config struct {
	// SchedulerInterval is how often to check for due scheduled scans.
	SchedulerInterval time.Duration

	// MaxJitter is the maximum random delay to spread out simultaneous scans.
	MaxJitter time.Duration

	// StuckRunThreshold is how long a run can be in pending/scanning before it's marked failed on restart.
	StuckRunThreshold time.Duration
}

// DefaultConfig returns the default service configuration.
func DefaultConfig() Config {
	return Config{
		SchedulerInterval: 5 * time.Minute,
		MaxJitter:         30 * time.Second,
		StuckRunThreshold: 1 * time.Hour,
	}
}

// DefaultSettings returns default settings for a new instance.
func DefaultSettings() Settings {
	return Settings{
		Enabled:             false,
		GracePeriodMinutes:  10,
		IgnorePaths:         []string{},
		ScanIntervalHours:   24,
		MaxFilesPerRun:      1000,
		AutoCleanupEnabled:  false,
		AutoCleanupMaxFiles: 100,
	}
}
