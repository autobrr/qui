// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/autobrr/qui/internal/database"
)

type templateState struct {
	once sync.Once
	path string
	err  error
}

var (
	templatesMu sync.Mutex
	templates   = make(map[string]*templateState)
)

// PathFromTemplate returns a fresh database file path for a test by cloning a
// package-level migrated template database. This avoids paying full migration
// cost for every test while keeping test database isolation.
func PathFromTemplate(t *testing.T, key, filename string) string {
	t.Helper()

	state := getTemplateState(key)
	state.once.Do(func() {
		state.path, state.err = createTemplateDB(key)
	})
	if state.err != nil {
		t.Fatalf("prepare test DB template %q: %v", key, state.err)
	}

	dbPath := filepath.Join(t.TempDir(), filename)
	if err := cloneDatabaseFiles(state.path, dbPath); err != nil {
		t.Fatalf("clone test DB template %q to %s: %v", key, dbPath, err)
	}

	return dbPath
}

func getTemplateState(key string) *templateState {
	templatesMu.Lock()
	defer templatesMu.Unlock()

	state, ok := templates[key]
	if ok {
		return state
	}

	state = &templateState{}
	templates[key] = state
	return state
}

func createTemplateDB(key string) (string, error) {
	templateDir, err := os.MkdirTemp("", fmt.Sprintf("qui-%s-template-", sanitizeKey(key)))
	if err != nil {
		return "", err
	}

	templatePath := filepath.Join(templateDir, "template.db")
	db, err := database.New(templatePath)
	if err != nil {
		return "", err
	}

	if err := db.Close(); err != nil {
		return "", err
	}

	return templatePath, nil
}

func sanitizeKey(key string) string {
	key = strings.TrimSpace(key)
	if key == "" {
		return "testdb"
	}

	var b strings.Builder
	b.Grow(len(key))
	for _, ch := range key {
		if (ch >= 'a' && ch <= 'z') || (ch >= 'A' && ch <= 'Z') || (ch >= '0' && ch <= '9') {
			b.WriteRune(ch)
			continue
		}
		b.WriteByte('-')
	}

	return b.String()
}

func copyFile(src, dst string) error {
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}

	if _, err := io.Copy(dstFile, srcFile); err != nil {
		_ = dstFile.Close()
		return err
	}

	if err := dstFile.Close(); err != nil {
		return err
	}

	return nil
}

func cloneDatabaseFiles(srcMain, dstMain string) error {
	if err := copyFile(srcMain, dstMain); err != nil {
		return err
	}

	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyOptionalFile(srcMain+suffix, dstMain+suffix); err != nil {
			return err
		}
	}

	return nil
}

func copyOptionalFile(src, dst string) error {
	if _, err := os.Stat(src); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	return copyFile(src, dst)
}
