// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"os"
	"testing"
	"testing/fstest"
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
