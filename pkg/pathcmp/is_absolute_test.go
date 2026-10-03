// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathcmp

import "testing"

func TestIsAbsolute(t *testing.T) {
	t.Parallel()

	tests := []struct {
		path string
		want bool
	}{
		{"/data/archive", true},
		{"/", true},
		{`\\server\share\archive`, true},
		{`\archive`, true},
		{`C:\Downloads\archive`, true},
		{"d:/downloads/archive", true},
		{"", false},
		{"archive", false},
		{"rel3/sub", false},
		{"./archive", false},
		{"../archive", false},
		{`archive\sub`, false},
		{"C:", false},
		{"C:archive", false},
		{"1:/archive", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			t.Parallel()
			if got := IsAbsolute(tt.path); got != tt.want {
				t.Errorf("IsAbsolute(%q) = %v, want %v", tt.path, got, tt.want)
			}
		})
	}
}
