// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"
)

func TestSupportsProcessInfo(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		webAPIVersion string
		expected      bool
	}{
		{name: "supported web api", webAPIVersion: "2.15.1", expected: true},
		{name: "newer web api", webAPIVersion: "2.16.0", expected: true},
		{name: "older web api", webAPIVersion: "2.11.4", expected: false},
		{name: "empty version", webAPIVersion: "", expected: false},
		{name: "invalid version", webAPIVersion: "not-a-version", expected: false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.expected, supportsProcessInfo(tc.webAPIVersion))
		})
	}
}

func TestBuildProcessInfo(t *testing.T) {
	processInfoErr := errors.New("process info unavailable")

	tests := []struct {
		name            string
		webAPIVersion   string
		processInfo     qbt.ProcessInfo
		processInfoErr  error
		wantFetchCalled bool
		wantLaunchTime  int64
		wantProcessInfo bool
	}{
		{
			name:            "includes process info for supported web api",
			webAPIVersion:   "2.15.1",
			processInfo:     qbt.ProcessInfo{LaunchTime: 1769331513},
			wantFetchCalled: true,
			wantLaunchTime:  1769331513,
			wantProcessInfo: true,
		},
		{
			name:            "skips process info for older web api",
			webAPIVersion:   "2.11.4",
			processInfo:     qbt.ProcessInfo{LaunchTime: 1769331513},
			wantFetchCalled: false,
			wantProcessInfo: false,
		},
		{
			name:            "omits process info when supported call fails",
			webAPIVersion:   "2.15.1",
			processInfoErr:  processInfoErr,
			wantFetchCalled: true,
			wantProcessInfo: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fetchCalled := false
			info := buildProcessInfo(tc.webAPIVersion, func() (qbt.ProcessInfo, error) {
				fetchCalled = true
				return tc.processInfo, tc.processInfoErr
			})

			require.Equal(t, tc.wantFetchCalled, fetchCalled)
			if !tc.wantProcessInfo {
				require.Nil(t, info)
				return
			}

			require.NotNil(t, info)
			require.Equal(t, tc.wantLaunchTime, info.LaunchTime)
		})
	}
}
