// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package version

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIsDevelop(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		version  string
		expected bool
	}{
		// Development versions
		{"empty string", "", true},
		{"dev", "dev", true},
		{"develop", "develop", true},
		{"main", "main", true},
		{"latest", "latest", true},
		{"pr prefix", "pr-123", true},
		{"dev suffix", "1.0.0-dev", true},
		{"develop suffix", "1.0.0-develop", true},

		// Release versions
		{"simple version", "1.0.0", false},
		{"version with v prefix", "v1.0.0", false},
		{"semver with patch", "1.2.3", false},
		{"version with prerelease", "1.0.0-alpha", false},
		{"version with rc", "1.0.0-rc1", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := isDevelop(tt.version)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewChecker(t *testing.T) {
	t.Parallel()

	checker := NewChecker("owner", "repo", "test-agent/1.0")

	assert.NotNil(t, checker)
	assert.Equal(t, "owner", checker.Owner)
	assert.Equal(t, "repo", checker.Repo)
	assert.Equal(t, "test-agent/1.0", checker.UserAgent)
	assert.NotNil(t, checker.httpClient)
}

func TestChecker_compareVersions(t *testing.T) {
	t.Parallel()

	checker := NewChecker("owner", "repo", "test-agent")

	tests := []struct {
		name           string
		currentVersion string
		releaseTag     string
		expectNewer    bool
		expectError    bool
	}{
		// Newer versions available
		{"newer patch version", "1.0.0", "1.0.1", true, false},
		{"newer minor version", "1.0.0", "1.1.0", true, false},
		{"newer major version", "1.0.0", "2.0.0", true, false},

		// Same or older versions
		{"same version", "1.0.0", "1.0.0", false, false},
		{"older patch version", "1.0.1", "1.0.0", false, false},
		{"older minor version", "1.1.0", "1.0.0", false, false},
		{"older major version", "2.0.0", "1.0.0", false, false},

		// Prerelease handling
		{"stable to prerelease", "1.0.0", "1.0.1-alpha", false, false},
		{"prerelease to newer stable", "1.0.0-alpha", "1.0.0", true, false},
		{"prerelease to newer prerelease", "1.0.0-alpha", "1.0.0-beta", true, false},

		// With v prefix
		{"v prefix on release", "1.0.0", "v1.0.1", true, false},
		{"v prefix on both", "v1.0.0", "v1.0.1", true, false},

		// Invalid versions
		{"invalid current version", "not-a-version", "1.0.0", false, true},
		{"invalid release version", "1.0.0", "not-a-version", false, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			release := &Release{TagName: tt.releaseTag}
			newer, _, err := checker.compareVersions(tt.currentVersion, release)

			if tt.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.expectNewer, newer)
			}
		})
	}
}

func TestRelease_Struct(t *testing.T) {
	t.Parallel()

	// Test that Release struct can be instantiated and fields work
	name := "Test Release"
	body := "Release notes"
	release := Release{
		ID:         123,
		TagName:    "v1.0.0",
		Name:       &name,
		Body:       &body,
		Draft:      false,
		Prerelease: false,
	}

	assert.Equal(t, int64(123), release.ID)
	assert.Equal(t, "v1.0.0", release.TagName)
	assert.Equal(t, "Test Release", *release.Name)
	assert.Equal(t, "Release notes", *release.Body)
	assert.False(t, release.Draft)
	assert.False(t, release.Prerelease)
}

func TestAsset_Struct(t *testing.T) {
	t.Parallel()

	asset := Asset{
		ID:                 456,
		Name:               "release.zip",
		ContentType:        "application/zip",
		State:              "uploaded",
		Size:               1024,
		DownloadCount:      100,
		BrowserDownloadURL: "https://example.com/release.zip",
	}

	assert.Equal(t, int64(456), asset.ID)
	assert.Equal(t, "release.zip", asset.Name)
	assert.Equal(t, "application/zip", asset.ContentType)
	assert.Equal(t, "uploaded", asset.State)
	assert.Equal(t, int64(1024), asset.Size)
	assert.Equal(t, int64(100), asset.DownloadCount)
	assert.Equal(t, "https://example.com/release.zip", asset.BrowserDownloadURL)
}
