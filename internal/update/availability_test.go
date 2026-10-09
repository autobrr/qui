// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestAvailability(t *testing.T) {
	tests := []struct {
		name           string
		files          []string
		pid            int
		kubernetes     bool
		disabled       bool
		version        string
		dirWritable    bool
		wantReason     string
		wantSelfUpdate bool
		wantRestart    bool
	}{
		{name: "all conditions true", version: "1.30.0", dirWritable: true, wantSelfUpdate: true, wantRestart: true},
		{name: "v prefixed release", version: "v1.30.0", dirWritable: true, wantSelfUpdate: true, wantRestart: true},
		{name: "docker", wantReason: "container", files: []string{"/.dockerenv"}, version: "1.30.0", dirWritable: true},
		{name: "podman", wantReason: "container", files: []string{"/run/.containerenv"}, version: "1.30.0", dirWritable: true},
		{name: "pid 1", wantReason: "container", pid: 1, version: "1.30.0", dirWritable: true},
		{name: "kubernetes pod", wantReason: "container", kubernetes: true, version: "1.30.0", dirWritable: true},
		{name: "lxc system container", files: []string{"/dev/.lxc-boot-id"}, version: "1.30.0", dirWritable: true, wantSelfUpdate: true, wantRestart: true},
		{name: "opt-out", wantReason: "disabled", disabled: true, version: "1.30.0", dirWritable: true, wantRestart: true},
		{name: "dev build", wantReason: "development", version: "0.0.0-dev", dirWritable: true, wantRestart: true},
		{name: "git describe build", wantReason: "development", version: "v1.30.0-30-gde99118d", dirWritable: true, wantRestart: true},
		{name: "build metadata", wantReason: "development", version: "1.30.0+meta", dirWritable: true, wantRestart: true},
		{name: "not semver", wantReason: "development", version: "dev", dirWritable: true, wantRestart: true},
		{name: "directory not writable", wantReason: "directory", version: "1.30.0", wantRestart: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exists := func(path string) bool { return slices.Contains(tt.files, path) }
			got := Decide(Inputs{
				AppContainer:      appContainer(exists, tt.pid, tt.kubernetes),
				DisableSelfUpdate: tt.disabled,
				ReleaseVersion:    isReleaseVersion(tt.version),
				DirWritable:       tt.dirWritable,
			})
			require.Equal(t, Availability{SelfUpdate: tt.wantSelfUpdate, Restart: tt.wantRestart, SelfUpdateUnavailableReason: tt.wantReason}, got)
		})
	}
}

func TestProbeWritable(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, probeWritable(dir))

	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries, "the probe file must be deleted")

	require.Error(t, probeWritable(filepath.Join(dir, "missing")))
}

func TestAvailabilityReasonPriority(t *testing.T) {
	in := Inputs{AppContainer: true, DisableSelfUpdate: true}
	require.Equal(t, "container", Decide(in).SelfUpdateUnavailableReason)
	in.AppContainer = false
	require.Equal(t, "disabled", Decide(in).SelfUpdateUnavailableReason)
	in.DisableSelfUpdate = false
	require.Equal(t, "development", Decide(in).SelfUpdateUnavailableReason)
	in.ReleaseVersion = true
	require.Equal(t, "directory", Decide(in).SelfUpdateUnavailableReason)
	in.DirWritable = true
	require.Empty(t, Decide(in).SelfUpdateUnavailableReason)
	require.True(t, Decide(in).SelfUpdate)
}
