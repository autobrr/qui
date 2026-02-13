// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNormalizeBasicAuthFromURL_InfersUserinfoWhenFieldsOmitted(t *testing.T) {
	baseURL, user, pass := normalizeBasicAuthFromURL("http://alice:secret@example.com/", nil, nil)

	require.Equal(t, "http://example.com", baseURL)
	require.NotNil(t, user)
	require.Equal(t, "alice", *user)
	require.NotNil(t, pass)
	require.Equal(t, "secret", *pass)
}

func TestNormalizeBasicAuthFromURL_RespectsExplicitClear(t *testing.T) {
	clearUser := ""

	baseURL, user, pass := normalizeBasicAuthFromURL("http://alice:secret@example.com/", &clearUser, nil)

	require.Equal(t, "http://example.com", baseURL)
	require.NotNil(t, user)
	require.Equal(t, "", *user)
	require.Nil(t, pass)

	normUser, normPass := normalizeBasicAuthForUpdate(user, pass)
	require.NotNil(t, normUser)
	require.NotNil(t, normPass)
	require.Equal(t, "", *normUser)
	require.Equal(t, "", *normPass)
}
