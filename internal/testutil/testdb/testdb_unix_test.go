// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

//go:build unix

package testdb

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestFallbackDirSuffixUsesUID(t *testing.T) {
	if got, want := fallbackDirSuffix(), strconv.Itoa(os.Getuid()); got != want {
		t.Fatalf("fallbackDirSuffix() = %q, want %q", got, want)
	}
}

func TestEnsurePrivateDirUnixPermissions(t *testing.T) {
	tests := []struct {
		name    string
		mode    os.FileMode
		wantErr string
	}{
		{name: "accepts 0700", mode: 0o700},
		{name: "rejects group-writable", mode: 0o770, wantErr: "group- or world-writable"},
		{name: "rejects world-writable", mode: 0o702, wantErr: "group- or world-writable"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "qui-testdb-fallback")
			if err := os.Mkdir(path, 0o700); err != nil {
				t.Fatal(err)
			}
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatal(err)
			}
			err := ensurePrivateDir(path)
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("ensurePrivateDir() error = %v", err)
				}
				info, err := os.Lstat(path)
				if err != nil {
					t.Fatal(err)
				}
				if got := info.Mode().Perm(); got&0o077 != 0 {
					t.Fatalf("created dir mode = %04o, want no group/world bits", got)
				}
				return
			}
			if err == nil {
				t.Fatal("ensurePrivateDir() error = nil, want error")
			}
			if !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("ensurePrivateDir() error = %v, want substring %q", err, tt.wantErr)
			}
		})
	}
}

func TestEnsurePrivateDirCreates0700(t *testing.T) {
	path := filepath.Join(t.TempDir(), "qui-testdb-fallback")
	if err := ensurePrivateDir(path); err != nil {
		t.Fatalf("ensurePrivateDir() error = %v", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o700 {
		t.Fatalf("created dir mode = %04o, want 0700", got)
	}
}
