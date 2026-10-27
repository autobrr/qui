// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestCancelAutomationRun(t *testing.T) {
	tests := []struct {
		name             string
		active           bool
		hasCancel        bool
		wantCanceled     bool
		wantCancelCalled bool
	}{
		{
			name: "no active run",
		},
		{
			name:             "active run",
			active:           true,
			hasCancel:        true,
			wantCanceled:     true,
			wantCancelCalled: true,
		},
		{
			name:   "active run with nil cancel",
			active: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{}
			s.runActive.Store(tt.active)
			cancelCalled := false
			if tt.hasCancel {
				s.runCancel = func() { cancelCalled = true }
			}

			got := s.CancelAutomationRun()

			if got != tt.wantCanceled {
				t.Errorf("CancelAutomationRun() = %v, want %v", got, tt.wantCanceled)
			}
			if cancelCalled != tt.wantCancelCalled {
				t.Errorf("CancelAutomationRun() cancel called = %v, want %v", cancelCalled, tt.wantCancelCalled)
			}
		})
	}
}

// TestShouldSkipErroredTorrent tests the actual Service.shouldSkipErroredTorrent
// method used by findCandidates and refreshSearchQueue to filter errored torrents.
func TestShouldSkipErroredTorrent(t *testing.T) {
	tests := []struct {
		name           string
		state          qbt.TorrentState
		recoverEnabled bool
		shouldSkip     bool
	}{
		{"error state, recovery disabled", qbt.TorrentStateError, false, true},
		{"missingFiles state, recovery disabled", qbt.TorrentStateMissingFiles, false, true},
		{"completed state, recovery disabled", qbt.TorrentStatePausedUp, false, false},
		{"error state, recovery enabled", qbt.TorrentStateError, true, false},
		{"missingFiles state, recovery enabled", qbt.TorrentStateMissingFiles, true, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{
				recoverErroredTorrentsEnabled: tt.recoverEnabled,
			}

			got := s.shouldSkipErroredTorrent(tt.state)
			if got != tt.shouldSkip {
				t.Errorf("shouldSkipErroredTorrent(%v) with recoverEnabled=%v: got %v, want %v",
					tt.state, tt.recoverEnabled, got, tt.shouldSkip)
			}
		})
	}
}
