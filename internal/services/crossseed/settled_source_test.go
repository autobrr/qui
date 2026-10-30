// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
)

// regression: #3626
func TestCanGiveFiles(t *testing.T) {
	t.Parallel()

	rows := []struct {
		state       qbt.TorrentState
		recoveryOff bool
		recoveryOn  bool
	}{
		{qbt.TorrentStateUploading, true, true},
		{qbt.TorrentStatePausedUp, true, true},
		{qbt.TorrentStateStoppedUp, true, true},
		{qbt.TorrentStateQueuedUp, true, true},
		{qbt.TorrentStateStalledUp, true, true},
		{qbt.TorrentStateForcedUp, true, true},
		{qbt.TorrentStateAllocating, true, true},
		{qbt.TorrentStateDownloading, true, true},
		{qbt.TorrentStateMetaDl, true, true},
		{qbt.TorrentStatePausedDl, true, true},
		{qbt.TorrentStateStoppedDl, true, true},
		{qbt.TorrentStateQueuedDl, true, true},
		{qbt.TorrentStateStalledDl, true, true},
		{qbt.TorrentStateForcedDl, true, true},
		{qbt.TorrentStateUnknown, true, true},
		{qbt.TorrentStateError, false, true},
		{qbt.TorrentStateMissingFiles, false, true},
		{qbt.TorrentStateMoving, false, false},
		{qbt.TorrentStateCheckingDl, false, false},
		{qbt.TorrentStateCheckingUp, false, false},
		{qbt.TorrentStateCheckingResumeData, false, false},
	}
	for _, row := range rows {
		torrent := &qbt.Torrent{State: row.state, Progress: 1}
		assert.Equal(t, row.recoveryOff, (&Service{}).canGiveFiles(torrent), "%s, recovery off", row.state)
		assert.Equal(t, row.recoveryOn, (&Service{recoverErroredTorrentsEnabled: true}).canGiveFiles(torrent), "%s, recovery on", row.state)

		torrent.Progress = 0.99
		assert.False(t, (&Service{recoverErroredTorrentsEnabled: true}).canGiveFiles(torrent), "%s, incomplete", row.state)
	}
}
