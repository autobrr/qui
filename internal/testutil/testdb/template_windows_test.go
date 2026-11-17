// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/require"
)

func TestCachedTemplatePreservesConcurrentWinner(t *testing.T) {
	dir := t.TempDir()
	migrations := fstest.MapFS{"001.sql": {Data: []byte("synthetic migration")}}
	path, err := cachedTemplate(dir, migrations, func(dst string) error {
		_, err := cachedTemplate(dir, migrations, func(winner string) error {
			return os.WriteFile(winner, []byte("first builder"), 0o600)
		})
		if err != nil {
			return err
		}
		return os.WriteFile(dst, []byte("later builder"), 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "first builder" {
		t.Fatalf("published template was replaced: %q", data)
	}
	onlyTemplate(t, dir)
}

func TestCachedTemplateLongWindowsPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), strings.Repeat("segment", 20), strings.Repeat("segment", 20))
	path, err := cachedTemplate(dir, fstest.MapFS{"001.sql": {Data: []byte("synthetic schema")}}, func(dst string) error {
		return os.WriteFile(dst, []byte("template"), 0o600)
	})
	require.NoError(t, err)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, "template", string(data))
	onlyTemplate(t, dir)
}
