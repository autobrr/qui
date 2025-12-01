// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package buildinfo

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestString(t *testing.T) {
	t.Parallel()

	s := String()

	assert.Contains(t, s, "Version:")
	assert.Contains(t, s, "Commit:")
	assert.Contains(t, s, "Build date:")
}

func TestJSON(t *testing.T) {
	t.Parallel()

	data, err := JSON()
	require.NoError(t, err)

	// Verify it's valid JSON
	var result map[string]interface{}
	err = json.Unmarshal(data, &result)
	require.NoError(t, err)

	// Verify expected fields exist
	assert.Contains(t, result, "version")
	assert.Contains(t, result, "commit")
	assert.Contains(t, result, "date")
}

func TestUserAgent(t *testing.T) {
	t.Parallel()

	// UserAgent should be set in init()
	assert.NotEmpty(t, UserAgent)
	assert.Contains(t, UserAgent, "qui/")
	assert.Contains(t, UserAgent, runtime.GOOS)
	assert.Contains(t, UserAgent, runtime.GOARCH)
}

func TestDefaultValues(t *testing.T) {
	t.Parallel()

	// Default version should be set for dev builds
	assert.NotEmpty(t, Version)

	// These may be empty in dev mode, but shouldn't cause issues
	_ = Commit
	_ = Date
}

func TestJSONStructure(t *testing.T) {
	t.Parallel()

	data, err := JSON()
	require.NoError(t, err)

	type buildInfoStruct struct {
		Version string `json:"version"`
		Commit  string `json:"commit"`
		Date    string `json:"date"`
	}

	var info buildInfoStruct
	err = json.Unmarshal(data, &info)
	require.NoError(t, err)

	assert.Equal(t, Version, info.Version)
	assert.Equal(t, Commit, info.Commit)
	assert.Equal(t, Date, info.Date)
}

func TestStringFormat(t *testing.T) {
	t.Parallel()

	s := String()
	lines := strings.Split(strings.TrimSpace(s), "\n")

	// Should have 3 lines
	assert.Len(t, lines, 3)

	// First line should be version
	assert.True(t, strings.HasPrefix(lines[0], "Version:"))

	// Second line should be commit
	assert.True(t, strings.HasPrefix(lines[1], "Commit:"))

	// Third line should be date
	assert.True(t, strings.HasPrefix(lines[2], "Build date:"))
}
