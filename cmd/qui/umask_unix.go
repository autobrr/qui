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

// applyUmask honors the UMASK environment variable (octal, e.g. "002") by
// setting the process umask before qui creates any files or directories.
//
// Content directories (hardlink/reflink/cross-seed trees) are created with mode
// 0o777 and rely on the umask to decide the final permissions (see discussion
// #1704). The official Docker image runs as root with the default umask 022, so
// this is the knob that lets operators get group-write (e.g. UMASK=002) without
// switching to a third-party base image.
//
// When UMASK is unset, empty, or not valid octal, the inherited umask is left
// unchanged, so the default behavior matches previous releases. No-op on Windows
// (see umask_other.go).
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

	syscall.Umask(int(mask))
	log.Debug().Str("umask", v).Msg("applied process umask from UMASK environment variable")
}
