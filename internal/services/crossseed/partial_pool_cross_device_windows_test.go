// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package crossseed

import (
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestPartialPoolPropagationPairIncompatibleRecognizesWindowsHardlinkError(t *testing.T) {
	require.True(t, partialPoolPropagationPairIncompatible(windows.ERROR_NOT_SAME_DEVICE))
}
