// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"context"
	"strings"
	"testing"
)

func TestService_SearchWithScope_EmptyScope(t *testing.T) {
	var s *Service

	tests := []struct {
		name  string
		scope string
	}{
		{name: "empty string", scope: ""},
		{name: "whitespace only", scope: "   \t  \n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.SearchWithScope(context.Background(), nil, tt.scope)
			if err == nil {
				t.Fatal("expected error, got nil")
			}
			if !strings.Contains(err.Error(), "invalid scope") {
				t.Errorf("expected 'invalid scope' error, got: %v", err)
			}
		})
	}
}
