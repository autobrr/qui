// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package main

import (
	"os"
	"syscall"
	"testing"
)

func readUmask() int {
	cur := syscall.Umask(0)
	syscall.Umask(cur)
	return cur
}

func TestApplyUmask(t *testing.T) {
	orig := readUmask()
	t.Cleanup(func() { syscall.Umask(orig) })

	tests := []struct {
		name     string
		set      bool
		value    string
		baseline int
		want     int
	}{
		{name: "unset leaves umask unchanged", set: false, baseline: 0o027, want: 0o027},
		{name: "applies octal value", set: true, value: "002", baseline: 0o022, want: 0o002},
		{name: "applies leading-zero value", set: true, value: "0027", baseline: 0o000, want: 0o027},
		{name: "empty value leaves umask unchanged", set: true, value: "", baseline: 0o027, want: 0o027},
		{name: "invalid value leaves umask unchanged", set: true, value: "notoctal", baseline: 0o027, want: 0o027},
		{name: "non-octal digits leave umask unchanged", set: true, value: "099", baseline: 0o027, want: 0o027},
		{name: "out-of-range value leaves umask unchanged", set: true, value: "1000", baseline: 0o027, want: 0o027},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			syscall.Umask(tt.baseline)
			if tt.set {
				t.Setenv("UMASK", tt.value)
			} else {
				os.Unsetenv("UMASK")
			}

			applyUmask()

			if got := readUmask(); got != tt.want {
				t.Fatalf("umask = %04o, want %04o", got, tt.want)
			}
		})
	}
}
