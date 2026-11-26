// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"runtime"
	"testing"

	"github.com/autobrr/rls"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/stringutils"
)

func TestNormalizerForService_ReusesSharedSingleton(t *testing.T) {
	require.Same(t, stringutils.DefaultNormalizer, normalizerForService(nil))
	require.Same(t, stringutils.DefaultNormalizer, normalizerForService(&Service{}))

	custom := stringutils.NewDefaultNormalizer()
	require.Same(t, custom, normalizerForService(&Service{stringNormalizer: custom}))
}

func TestReleasesMatch_DoesNotLeakGoroutines(t *testing.T) {
	s := matcher{}

	source := rls.ParseString("Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GROUP")
	candidate := rls.ParseString("Some.Show.S01.1080p.WEB-DL.DDP5.1.H.264-GROUP")

	for range 50 {
		s.strictCompare(&source, &candidate, false)
	}
	runtime.GC()
	before := runtime.NumGoroutine()

	const iterations = 5000
	for range iterations {
		s.strictCompare(&source, &candidate, false)
	}
	runtime.GC()
	after := runtime.NumGoroutine()

	require.LessOrEqualf(t, after-before, 10,
		"goroutine count grew by %d over %d match calls - normalizer cache is leaking",
		after-before, iterations)
}
