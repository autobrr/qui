// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import "time"

// Safety constants for orphan scan readiness checks.
// These are intentionally non-configurable to prevent data loss.
const (
	// RecoveryGracePeriod is how long to wait after qBit client recovers before scanning.
	RecoveryGracePeriod = 3 * time.Minute

	// SettlingSampleInterval is the time between stability samples.
	SettlingSampleInterval = 20 * time.Second

	// SettlingSampleCount is the number of samples to take (4 × 20s = ~60s window).
	SettlingSampleCount = 4

	// MaxSyncAge is the maximum age of sync data for it to be trusted.
	MaxSyncAge = 2 * time.Minute

	// MaxCheckingStatePercent is the threshold for torrents in checking states.
	MaxCheckingStatePercent = 5.0

	// MaxMissingFilesCount is the maximum number of eligible torrents allowed to have no files.
	// Zero tolerance: any missing files = partial data detected = fail scan.
	MaxMissingFilesCount = 0

	// SettlingCountToleranceMin is the minimum tolerance for count fluctuations.
	SettlingCountToleranceMin = 10
)

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
		PreviewSort:         "size_desc",
		MaxFilesPerRun:      1000,
		AutoCleanupEnabled:  false,
		AutoCleanupMaxFiles: 100,
	}
}
