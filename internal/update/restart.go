// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"fmt"
	"os"
	"sync"

	"github.com/rs/zerolog"
)

type Restarter struct {
	sync.Mutex
	binaryPath string
	requested  chan struct{}
}

func NewRestarter(binaryPath string) *Restarter {
	return &Restarter{binaryPath: binaryPath, requested: make(chan struct{}, 1)}
}

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

func (r *Restarter) Restart(log zerolog.Logger, shutdown func() error) {
	if err := shutdown(); err != nil {
		log.Warn().Err(err).Msg("graceful shutdown did not finish, restarting anyway")
	}
	log.Info().Str("binaryPath", r.binaryPath).Msg("restarting")
	err := execBinary(r.binaryPath)
	log.Error().Err(err).Str("binaryPath", r.binaryPath).Msg("restart failed")
	os.Exit(1)
}
