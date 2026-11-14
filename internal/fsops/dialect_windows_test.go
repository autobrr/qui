// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build windows

package fsops

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// regression: #2919
func TestHostPathsRelShareRoots(t *testing.T) {
	t.Parallel()

	for _, tt := range []struct {
		name, base, target, want string
		wantErr                  bool
	}{
		{name: "base without the separator", base: `\\nas\media`, target: `\\nas\media\`, want: "."},
		{name: "target without the separator", base: `\\nas\media\`, target: `\\nas\media`, want: "."},
		{name: "forward slashes", base: "//nas/media", target: `\\nas\media\`, want: "."},
		{name: "child of a share root", base: `\\nas\media`, target: `\\nas\media\Show`, want: "Show"},
		{name: "host with no share", base: `\\nas`, target: `\\nas\`, wantErr: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := HostPaths.Rel(tt.base, tt.target)
			if tt.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.want, got)
		})
	}
}
