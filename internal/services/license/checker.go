// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package license

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog/log"
)

type ValidationChecker interface {
	ValidateLicenses(ctx context.Context) (bool, error)
}

type Checker struct {
	service       ValidationChecker
	lastCheck     time.Time
	isValid       atomic.Bool
	checkInterval time.Duration
	graceUntil    time.Time
	mu            sync.RWMutex
}

func NewLicenseChecker(service ValidationChecker) *Checker {
	return &Checker{
		service:       service,
		checkInterval: 24 * time.Hour,
	}
}

func (lc *Checker) StartPeriodicChecks(ctx context.Context) {
	// Initial check
	lc.validateLicense(ctx)

	ticker := time.NewTicker(lc.checkInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			lc.validateLicense(ctx)
		case <-ctx.Done():
			return
		}
	}
}

func (lc *Checker) validateLicense(ctx context.Context) {
	lc.mu.Lock()
	defer lc.mu.Unlock()

	isValid, err := lc.service.ValidateLicenses(ctx)
	if err != nil {
		// Don't change validity on network errors
		return
	}

	lc.lastCheck = time.Now()
	lc.isValid.Store(isValid)

	if !isValid {
		// Set grace period
		if lc.graceUntil.IsZero() {
			lc.graceUntil = time.Now().Add(offlineGracePeriod)
			log.Warn().Time("grace_until", lc.graceUntil).Msg("License invalid - grace period started")
		}
	} else {
		// Reset grace period on valid license
		lc.graceUntil = time.Time{}
	}
}
