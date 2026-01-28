// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/services/externalprograms"
)

func TestExternalProgramsService_IsPathAllowed(t *testing.T) {
	tempDir := t.TempDir()
	allowedFile := filepath.Join(tempDir, "script.sh")

	// Test: path allowed when directory is whitelisted
	service := externalprograms.NewService(nil, nil, &domain.Config{ExternalProgramAllowList: []string{tempDir}})
	if !service.IsPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be allowed when directory is whitelisted", allowedFile)
	}

	// Test: exact path allowed when explicitly listed
	service = externalprograms.NewService(nil, nil, &domain.Config{ExternalProgramAllowList: []string{allowedFile}})
	if !service.IsPathAllowed(allowedFile) {
		t.Fatalf("expected exact path %s to be allowed when explicitly listed", allowedFile)
	}

	// Test: path blocked when not in allow list
	otherDir := t.TempDir()
	service = externalprograms.NewService(nil, nil, &domain.Config{ExternalProgramAllowList: []string{otherDir}})
	if service.IsPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be blocked when not in allow list", allowedFile)
	}

	// Test: all paths allowed when allow list is empty
	service = externalprograms.NewService(nil, nil, &domain.Config{ExternalProgramAllowList: nil})
	if !service.IsPathAllowed(allowedFile) {
		t.Fatalf("expected path %s to be allowed when allow list is empty", allowedFile)
	}
}
