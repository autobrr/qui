// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package main

import (
	"os"
	"strconv"
	"syscall"

	"github.com/rs/zerolog/log"
)

func applyUmask() {
	v, ok := os.LookupEnv("UMASK")
	if !ok || v == "" {
		return
	}

	mask, err := strconv.ParseUint(v, 8, 32)
	if err != nil {
		log.Warn().Str("umask", v).Err(err).Msg("ignoring invalid UMASK (expected octal, e.g. 002)")
		return
	}
	if mask > 0o777 {
		log.Warn().Str("umask", v).Msg("ignoring out-of-range UMASK (expected octal 000-777)")
		return
	}

	syscall.Umask(int(mask))
	log.Debug().Str("umask", v).Msg("applied process umask from UMASK environment variable")
}
