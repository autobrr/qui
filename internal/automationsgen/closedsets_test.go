// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestReadTypedConstants(t *testing.T) {
	values, err := readTypedConstants(filepath.Join("testdata", "kinds", "typed"), "Kind")
	require.NoError(t, err)
	require.Equal(t, []string{"zulu", "alpha", "mike"}, values)
}

func TestReadTypedConstantsFailsOnAnUntypedConstantInATypedBlock(t *testing.T) {
	_, err := readTypedConstants(filepath.Join("testdata", "kinds", "untyped"), "Kind")
	require.ErrorContains(t, err, "KindBravo")
}

func TestReadTypedConstantsFailsOnAValueThatIsNotAStringLiteral(t *testing.T) {
	_, err := readTypedConstants(filepath.Join("testdata", "kinds", "notliteral"), "Kind")
	require.ErrorContains(t, err, "KindBravo")
}
