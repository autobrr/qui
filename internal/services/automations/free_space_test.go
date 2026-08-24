// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build !windows

package automations

import (
	"os"
	"testing"

	"github.com/autobrr/qui/internal/models"
)

func TestResolveFreeSpaceSource(t *testing.T) {
	tests := []struct {
		name     string
		input    *models.FreeSpaceSource
		wantType models.FreeSpaceSourceType
		wantPath string
	}{
		{
			name:     "nil defaults to qbittorrent",
			input:    nil,
			wantType: models.FreeSpaceSourceQBittorrent,
			wantPath: "",
		},
		{
			name:     "empty type defaults to qbittorrent",
			input:    &models.FreeSpaceSource{Type: ""},
			wantType: models.FreeSpaceSourceQBittorrent,
			wantPath: "",
		},
		{
			name:     "explicit qbittorrent",
			input:    &models.FreeSpaceSource{Type: models.FreeSpaceSourceQBittorrent},
			wantType: models.FreeSpaceSourceQBittorrent,
			wantPath: "",
		},
		{
			name:     "path type with path",
			input:    &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"},
			wantType: models.FreeSpaceSourcePath,
			wantPath: "/mnt/data",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveFreeSpaceSource(tt.input)
			if got.Type != tt.wantType {
				t.Errorf("resolveFreeSpaceSource().Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Path != tt.wantPath {
				t.Errorf("resolveFreeSpaceSource().Path = %v, want %v", got.Path, tt.wantPath)
			}
		})
	}
}

func TestGetLocalFreeSpaceBytes(t *testing.T) {
	// Test with a known-existing directory (temp dir should always exist)
	tmpDir := os.TempDir()
	bytes, err := getLocalFreeSpaceBytes(tmpDir)
	if err != nil {
		t.Fatalf("getLocalFreeSpaceBytes(%q) returned error: %v", tmpDir, err)
	}
	if bytes <= 0 {
		t.Errorf("getLocalFreeSpaceBytes(%q) returned %d, want > 0", tmpDir, bytes)
	}
}

func TestGetLocalFreeSpaceBytes_InvalidPath(t *testing.T) {
	_, err := getLocalFreeSpaceBytes("/nonexistent/path/that/should/not/exist")
	if err == nil {
		t.Error("getLocalFreeSpaceBytes with invalid path should return error")
	}
}

func TestGetFreeSpaceSourceKey(t *testing.T) {
	tests := []struct {
		name    string
		input   *models.FreeSpaceSource
		wantKey string
	}{
		{
			name:    "nil defaults to qbt",
			input:   nil,
			wantKey: "qbt",
		},
		{
			name:    "empty type defaults to qbt",
			input:   &models.FreeSpaceSource{Type: ""},
			wantKey: "qbt",
		},
		{
			name:    "explicit qbittorrent",
			input:   &models.FreeSpaceSource{Type: models.FreeSpaceSourceQBittorrent},
			wantKey: "qbt",
		},
		{
			name:    "path type with path",
			input:   &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data"},
			wantKey: "path:/mnt/data",
		},
		{
			name:    "path type with trailing slash is cleaned",
			input:   &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "/mnt/data/"},
			wantKey: "path:/mnt/data",
		},
		{
			name:    "path type with whitespace is cleaned",
			input:   &models.FreeSpaceSource{Type: models.FreeSpaceSourcePath, Path: "  /mnt/data  "},
			wantKey: "path:/mnt/data",
		},
		{
			name:    "unknown type defaults to qbt",
			input:   &models.FreeSpaceSource{Type: "unknown"},
			wantKey: "qbt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetFreeSpaceSourceKey(tt.input)
			if got != tt.wantKey {
				t.Errorf("GetFreeSpaceSourceKey() = %v, want %v", got, tt.wantKey)
			}
		})
	}
}
