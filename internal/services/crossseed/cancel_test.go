// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
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
