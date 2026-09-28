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
		wantSelfUpdate bool
		wantRestart    bool
	}{
		{name: "all conditions true", version: "1.30.0", dirWritable: true, wantSelfUpdate: true, wantRestart: true},
		{name: "v prefixed release", version: "v1.30.0", dirWritable: true, wantSelfUpdate: true, wantRestart: true},
		{name: "docker", files: []string{"/.dockerenv"}, version: "1.30.0", dirWritable: true},
		{name: "podman", files: []string{"/run/.containerenv"}, version: "1.30.0", dirWritable: true},
		{name: "pid 1", pid: 1, version: "1.30.0", dirWritable: true},
		{name: "kubernetes pod", kubernetes: true, version: "1.30.0", dirWritable: true},
		{name: "lxc system container", files: []string{"/dev/.lxc-boot-id"}, version: "1.30.0", dirWritable: true, wantSelfUpdate: true, wantRestart: true},
		{name: "opt-out", disabled: true, version: "1.30.0", dirWritable: true, wantRestart: true},
		{name: "dev build", version: "0.0.0-dev", dirWritable: true, wantRestart: true},
		{name: "git describe build", version: "v1.30.0-30-gde99118d", dirWritable: true, wantRestart: true},
		{name: "build metadata", version: "1.30.0+meta", dirWritable: true, wantRestart: true},
		{name: "not semver", version: "dev", dirWritable: true, wantRestart: true},
		{name: "directory not writable", version: "1.30.0", wantRestart: true},
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
			require.Equal(t, Availability{SelfUpdate: tt.wantSelfUpdate && restartSupported, Restart: tt.wantRestart && restartSupported}, got)
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
