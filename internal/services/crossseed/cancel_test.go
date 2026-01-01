// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"testing"
)

func TestCancelAutomationRun_NoActiveRun(t *testing.T) {
	s := &Service{}

	// When no run is active, cancel should return false
	if got := s.CancelAutomationRun(); got {
		t.Errorf("CancelAutomationRun() = %v, want false when no run is active", got)
	}
}

func TestCancelAutomationRun_ActiveRun(t *testing.T) {
	s := &Service{}

	// Simulate an active run
	s.runActive.Store(true)
	canceled := false
	s.runCancel = func() { canceled = true }

	// When a run is active, cancel should return true and call the cancel func
	if got := s.CancelAutomationRun(); !got {
		t.Errorf("CancelAutomationRun() = %v, want true when run is active", got)
	}
	if !canceled {
		t.Errorf("CancelAutomationRun() did not call the cancel function")
	}
}

func TestCancelAutomationRun_ActiveRunNilCancel(t *testing.T) {
	s := &Service{}

	// Simulate an active run but with nil cancel (shouldn't happen in practice)
	s.runActive.Store(true)
	s.runCancel = nil

	// Should return false since cancel is nil
	if got := s.CancelAutomationRun(); got {
		t.Errorf("CancelAutomationRun() = %v, want false when cancel is nil", got)
	}
}
