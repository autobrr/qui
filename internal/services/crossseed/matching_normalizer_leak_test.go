// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"runtime"
	"testing"

	"github.com/moistari/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

// Regression test for the cross-seed RAM/goroutine explosion introduced in
// #1687 (season pack support). The matching hot path used to call
// stringutils.NewDefaultNormalizer() on every release comparison. Each call
// builds a ttlcache whose startExpirations goroutine never terminates (the
// throwaway cache is never closed), so cross-seed matching leaked one
// goroutine per release pair and OOM'd containers overnight.

func TestNormalizerForService_ReusesSharedSingleton(t *testing.T) {
	// A Service without an explicit normalizer must fall back to the
	// process-wide singleton, not allocate a fresh (leaking) one.
	require.Same(t, stringutils.DefaultNormalizer, normalizerForService(nil))
	require.Same(t, stringutils.DefaultNormalizer, normalizerForService(&Service{}))

	// Explicit per-service normalizer is still honored.
	custom := stringutils.NewDefaultNormalizer()
	require.Same(t, custom, normalizerForService(&Service{stringNormalizer: custom}))
}

func TestReleasesMatch_DoesNotLeakGoroutines(t *testing.T) {
	// Service with nil stringNormalizer: this is the path that previously
	// allocated and leaked a ttlcache goroutine on every comparison.
	s := &Service{}

	source := rls.ParseString("Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GROUP")
	candidate := rls.ParseString("Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GROUP")

	// Warm up any one-time allocations before sampling.
	for range 50 {
		s.releasesMatch(&source, &candidate, false)
	}
	runtime.GC()
	before := runtime.NumGoroutine()

	const iterations = 5000
	for range iterations {
		s.releasesMatch(&source, &candidate, false)
	}
	runtime.GC()
	after := runtime.NumGoroutine()

	// Before the fix this grew by ~iterations (one leaked startExpirations
	// goroutine per call). Allow a small slack for unrelated runtime/test
	// goroutines.
	require.LessOrEqualf(t, after-before, 10,
		"goroutine count grew by %d over %d match calls - normalizer cache is leaking",
		after-before, iterations)
}
