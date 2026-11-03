// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	internalqbittorrent "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
)

func TestInstanceCapabilitiesResponseByWebAPIVersion(t *testing.T) {
	t.Parallel()

	// The values the response had before the version table, one row for each
	// qBittorrent release line: 4.6, 5.1, 5.2 and 5.3.
	tests := map[string]string{
		"2.9.3":  `{"subcategoriesAlwaysEnabled":false,"supportsFilePriority":true,"supportsPathAutocomplete":false,"supportsRenameFile":true,"supportsRenameFolder":true,"supportsRenameTorrent":true,"supportsSetComment":false,"supportsSetRSSFeedURL":true,"supportsSetTags":false,"supportsShareLimitsAction":false,"supportsShareLimitsMode":false,"supportsSubcategories":true,"supportsTestEmail":false,"supportsTorrentCreation":false,"supportsTorrentExport":true,"supportsTorrentTmpPath":true,"supportsTrackerEditing":true,"supportsTrackerHealth":false,"webAPIVersion":"2.9.3"}`,
		"2.11.4": `{"subcategoriesAlwaysEnabled":false,"supportsFilePriority":true,"supportsPathAutocomplete":true,"supportsRenameFile":true,"supportsRenameFolder":true,"supportsRenameTorrent":true,"supportsSetComment":false,"supportsSetRSSFeedURL":true,"supportsSetTags":true,"supportsShareLimitsAction":false,"supportsShareLimitsMode":false,"supportsSubcategories":true,"supportsTestEmail":true,"supportsTorrentCreation":true,"supportsTorrentExport":true,"supportsTorrentTmpPath":true,"supportsTrackerEditing":true,"supportsTrackerHealth":true,"webAPIVersion":"2.11.4"}`,
		"2.15.1": `{"subcategoriesAlwaysEnabled":true,"supportsFilePriority":true,"supportsPathAutocomplete":true,"supportsRenameFile":true,"supportsRenameFolder":true,"supportsRenameTorrent":true,"supportsSetComment":true,"supportsSetRSSFeedURL":true,"supportsSetTags":true,"supportsShareLimitsAction":true,"supportsShareLimitsMode":false,"supportsSubcategories":true,"supportsTestEmail":true,"supportsTorrentCreation":true,"supportsTorrentExport":true,"supportsTorrentTmpPath":true,"supportsTrackerEditing":true,"supportsTrackerHealth":true,"webAPIVersion":"2.15.1"}`,
		"2.16.2": `{"subcategoriesAlwaysEnabled":true,"supportsFilePriority":true,"supportsPathAutocomplete":true,"supportsRenameFile":true,"supportsRenameFolder":true,"supportsRenameTorrent":true,"supportsSetComment":true,"supportsSetRSSFeedURL":true,"supportsSetTags":true,"supportsShareLimitsAction":true,"supportsShareLimitsMode":true,"supportsSubcategories":true,"supportsTestEmail":true,"supportsTorrentCreation":true,"supportsTorrentExport":true,"supportsTorrentTmpPath":true,"supportsTrackerEditing":true,"supportsTrackerHealth":true,"webAPIVersion":"2.16.2"}`,
	}
	for version, want := range tests {
		t.Run(version, func(t *testing.T) {
			t.Parallel()
			stub := qbtstub.New(t, "/data")
			stub.WebAPIVersion = version
			client, err := internalqbittorrent.NewClientWithTimeout(1, stub.URL, "user", "pass", "", nil, nil, false, 5*time.Second, 10*time.Second)
			require.NoError(t, err)

			body, err := json.Marshal(NewInstanceCapabilitiesResponse(client))
			require.NoError(t, err)
			var got map[string]any
			require.NoError(t, json.Unmarshal(body, &got))
			delete(got, "supportsFreeSpacePathSource") // follows the host OS, not the version
			out, err := json.Marshal(got)
			require.NoError(t, err)
			require.JSONEq(t, want, string(out))
		})
	}
}
