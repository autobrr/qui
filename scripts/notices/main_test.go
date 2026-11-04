// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package main

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestLicenseFiles(t *testing.T) {
	root := t.TempDir()
	pkg := filepath.Join(root, "internal", "pkg")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	// A folder that matches the name is not a license file.
	if err := os.Mkdir(filepath.Join(root, "license"), 0o755); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{
		"LICENSE-MIT", "NOTICE.txt", "licence.md", "COPYING", "PATENTS", "README.md", "license.go", "notice.js",
		"internal/pkg/LICENSE", "internal/UNRELATED",
	} {
		if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(name)), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// A sibling package outside the walk from pkg to root stays out.
	sibling := filepath.Join(root, "other")
	if err := os.Mkdir(sibling, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sibling, "LICENSE"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	got, err := licenseFiles(pkg, root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"COPYING", "LICENSE-MIT", "NOTICE.txt", "PATENTS", "internal/pkg/LICENSE", "licence.md"}
	for i, path := range got {
		rel, err := filepath.Rel(root, path)
		if err != nil {
			t.Fatal(err)
		}
		got[i] = filepath.ToSlash(rel)
	}
	slices.Sort(got)
	if !slices.Equal(got, want) {
		t.Errorf("licenseFiles = %v, want %v", got, want)
	}
}
