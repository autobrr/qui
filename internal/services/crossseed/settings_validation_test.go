// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"testing"
)

func TestUpdateAutomationSettingsNilCheck(t *testing.T) {
	s := &Service{}

	_, err := s.UpdateAutomationSettings(context.Background(), nil)
	if err == nil {
		t.Error("Expected error for nil settings but got none")
	}

	expectedMsg := "settings cannot be nil"
	if err.Error() != expectedMsg {
		t.Errorf("Expected error message '%s', got '%s'", expectedMsg, err.Error())
	}
}
