// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"fmt"
	"os"
	"sync"

	"github.com/rs/zerolog"
)

// Restarter carries a Restart request from the API to the serve loop. See
// docs/adr/0011-restart-replaces-the-process-in-place.md.
type Restarter struct {
	// Mutex is the one lock over every system action, taken with TryLock by the
	// API and the Tray. A successful action ends in a Restart, so only a failed
	// action unlocks it.
	sync.Mutex
	binaryPath string
	requested  chan struct{}
}

// NewRestarter takes the binary path that Measure resolved at startup, before a
// Self-update can replace the file.
func NewRestarter(binaryPath string) *Restarter {
	return &Restarter{binaryPath: binaryPath, requested: make(chan struct{}, 1)}
}

// Request checks the binary, then asks the serve loop to restart. On an error
// qui keeps running: on Unix without a supervisor, a failed exec leaves qui down.
func (r *Restarter) Request() error {
	if err := checkBinary(r.binaryPath); err != nil {
		return err
	}
	select {
	case r.requested <- struct{}{}:
	default:
	}
	return nil
}

func checkBinary(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	return checkExecutable(path)
}

func (r *Restarter) Requested() <-chan struct{} {
	return r.requested
}

// Restart runs the graceful shutdown, then starts the binary again: on Unix
// through an exec in place, on Windows through the supervisor. It never returns. A shutdown error, such as the 30-second timeout,
// does not stop the restart.
func (r *Restarter) Restart(log zerolog.Logger, shutdown func() error) {
	if err := shutdown(); err != nil {
		log.Warn().Err(err).Msg("graceful shutdown did not finish, restarting anyway")
	}
	log.Info().Str("binaryPath", r.binaryPath).Msg("restarting")
	err := execBinary(r.binaryPath)
	// Only a failed Unix exec gets here. Exit non-zero so that
	// Restart=on-failure starts qui again.
	log.Error().Err(err).Str("binaryPath", r.binaryPath).Msg("restart failed")
	os.Exit(1)
}
