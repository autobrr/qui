// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestInstanceCapabilitiesResponse_Struct(t *testing.T) {
	t.Parallel()

	resp := InstanceCapabilitiesResponse{
		SupportsTorrentCreation: true,
		SupportsTorrentExport:   true,
		SupportsSetTags:         true,
		SupportsTrackerHealth:   true,
		SupportsTrackerEditing:  true,
		SupportsRenameTorrent:   true,
		SupportsRenameFile:      true,
		SupportsRenameFolder:    true,
		SupportsFilePriority:    true,
		SupportsSubcategories:   true,
		WebAPIVersion:           "2.9.0",
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	var decoded InstanceCapabilitiesResponse
	err = json.Unmarshal(data, &decoded)
	require.NoError(t, err)

	assert.Equal(t, resp.SupportsTorrentCreation, decoded.SupportsTorrentCreation)
	assert.Equal(t, resp.SupportsTorrentExport, decoded.SupportsTorrentExport)
	assert.Equal(t, resp.SupportsSetTags, decoded.SupportsSetTags)
	assert.Equal(t, resp.SupportsTrackerHealth, decoded.SupportsTrackerHealth)
	assert.Equal(t, resp.SupportsTrackerEditing, decoded.SupportsTrackerEditing)
	assert.Equal(t, resp.SupportsRenameTorrent, decoded.SupportsRenameTorrent)
	assert.Equal(t, resp.SupportsRenameFile, decoded.SupportsRenameFile)
	assert.Equal(t, resp.SupportsRenameFolder, decoded.SupportsRenameFolder)
	assert.Equal(t, resp.SupportsFilePriority, decoded.SupportsFilePriority)
	assert.Equal(t, resp.SupportsSubcategories, decoded.SupportsSubcategories)
	assert.Equal(t, resp.WebAPIVersion, decoded.WebAPIVersion)
}

func TestInstanceCapabilitiesResponse_OmitEmptyVersion(t *testing.T) {
	t.Parallel()

	resp := InstanceCapabilitiesResponse{
		SupportsTorrentCreation: true,
	}

	data, err := json.Marshal(resp)
	require.NoError(t, err)

	// WebAPIVersion should be omitted when empty
	assert.NotContains(t, string(data), `"webAPIVersion":`)
}

func TestInstanceCapabilitiesResponse_DefaultValues(t *testing.T) {
	t.Parallel()

	var resp InstanceCapabilitiesResponse

	// All boolean fields should default to false
	assert.False(t, resp.SupportsTorrentCreation)
	assert.False(t, resp.SupportsTorrentExport)
	assert.False(t, resp.SupportsSetTags)
	assert.False(t, resp.SupportsTrackerHealth)
	assert.False(t, resp.SupportsTrackerEditing)
	assert.False(t, resp.SupportsRenameTorrent)
	assert.False(t, resp.SupportsRenameFile)
	assert.False(t, resp.SupportsRenameFolder)
	assert.False(t, resp.SupportsFilePriority)
	assert.False(t, resp.SupportsSubcategories)
	assert.Empty(t, resp.WebAPIVersion)
}
