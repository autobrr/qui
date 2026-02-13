// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/internal/domain"
)

func TestExternalProgramsHandler_isPathAllowed(t *testing.T) {
	tempDir := t.TempDir()
	allowedFile := filepath.Join(tempDir, "script.sh")

	handler := &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{tempDir}}}
	if !handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be allowed when directory is whitelisted", allowedFile)
	}

	handler = &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{allowedFile}}}
	if !handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected exact path %s to be allowed when explicitly listed", allowedFile)
	}

	otherDir := t.TempDir()
	handler = &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: []string{otherDir}}}
	if handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be blocked when not in allow list", allowedFile)
	}

	handler = &ExternalProgramsHandler{config: &domain.Config{ExternalProgramAllowList: nil}}
	if !handler.isPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be allowed when allow list is empty", allowedFile)
	}
}
