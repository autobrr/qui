// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"fmt"
	"net/http"
	"runtime"

	"github.com/rs/zerolog/log"
)

// PprofController manages runtime profiling controls
type PprofController struct {
	blockRate     int
	mutexFraction int
}

// NewPprofController creates a new profiling controller with initial rates
func NewPprofController(blockRate, mutexFraction int) *PprofController {
	return &PprofController{
		blockRate:     blockRate,
		mutexFraction: mutexFraction,
	}
}

// EnableBlockProfile enables block profiling
func (pc *PprofController) EnableBlockProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	rate := 1
	if r.URL.Query().Get("rate") != "" {
		fmt.Sscanf(r.URL.Query().Get("rate"), "%d", &rate)
	}

	runtime.SetBlockProfileRate(rate)
	pc.blockRate = rate
	log.Info().Int("rate", rate).Msg("Block profiling enabled via API")
	fmt.Fprintf(w, "Block profiling enabled with rate=%d\n", rate)
}

// DisableBlockProfile disables block profiling
func (pc *PprofController) DisableBlockProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	runtime.SetBlockProfileRate(0)
	pc.blockRate = 0
	log.Info().Msg("Block profiling disabled via API")
	fmt.Fprintln(w, "Block profiling disabled")
}

// EnableMutexProfile enables mutex profiling
func (pc *PprofController) EnableMutexProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	fraction := 1
	if r.URL.Query().Get("fraction") != "" {
		fmt.Sscanf(r.URL.Query().Get("fraction"), "%d", &fraction)
	}

	runtime.SetMutexProfileFraction(fraction)
	pc.mutexFraction = fraction
	log.Info().Int("fraction", fraction).Msg("Mutex profiling enabled via API")
	fmt.Fprintf(w, "Mutex profiling enabled with fraction=%d\n", fraction)
}

// DisableMutexProfile disables mutex profiling
func (pc *PprofController) DisableMutexProfile(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	runtime.SetMutexProfileFraction(0)
	pc.mutexFraction = 0
	log.Info().Msg("Mutex profiling disabled via API")
	fmt.Fprintln(w, "Mutex profiling disabled")
}

// Status returns the current profiling status
func (pc *PprofController) Status(w http.ResponseWriter, r *http.Request) {
	fmt.Fprintf(w, "Profiling Status:\n")
	fmt.Fprintf(w, "Block Profile Rate: %d (0=disabled)\n", pc.blockRate)
	fmt.Fprintf(w, "Mutex Profile Fraction: %d (0=disabled)\n", pc.mutexFraction)
}
