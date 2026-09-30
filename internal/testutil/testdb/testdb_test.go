// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package testdb

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/database"
)

func TestPostgresDSNWithSearchPath(t *testing.T) {
	const schema = "qui_test_schema"
	tests := []struct {
		name         string
		dsn          string
		wantPassword string
		wantErr      bool
	}{
		{
			name:         "URL",
			dsn:          "postgres://tester:url%20secret@localhost:5432/qui?sslmode=disable&application_name=suite&search_path=old",
			wantPassword: "url secret",
		},
		{
			name:         "keyword value",
			dsn:          "host=localhost port=5432 user=tester password='keyword secret' dbname=qui sslmode=disable application_name=suite search_path=old",
			wantPassword: "keyword secret",
		},
		{
			name:    "malformed URL",
			dsn:     "postgres://%zz",
			wantErr: true,
		},
		{
			name:    "malformed keyword value",
			dsn:     "host='unterminated",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, err := postgresDSNWithSearchPath(tt.dsn, schema)
			if tt.wantErr {
				if err == nil {
					t.Fatal("postgresDSNWithSearchPath() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("postgresDSNWithSearchPath() error = %v", err)
			}

			config, err := pgxpool.ParseConfig(dsn)
			if err != nil {
				t.Fatalf("parse modified DSN: %v", err)
			}
			if got := config.ConnConfig.RuntimeParams["search_path"]; got != schema {
				t.Fatalf("search_path = %q, want %q", got, schema)
			}
			if got := config.ConnConfig.RuntimeParams["application_name"]; got != "suite" {
				t.Fatalf("application_name = %q, want suite", got)
			}
			if got := config.ConnConfig.Password; got != tt.wantPassword {
				t.Fatalf("password = %q, want %q", got, tt.wantPassword)
			}
		})
	}
}

func BenchmarkFullMigrationTestDB(b *testing.B) {
	disableBenchmarkLogs(b)
	parent := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		db, err := database.New(filepath.Join(parent, fmt.Sprintf("full-%d.db", i)))
		if err != nil {
			b.Fatalf("create full migration db: %v", err)
		}
		if err := db.Close(); err != nil {
			b.Fatalf("close full migration db: %v", err)
		}
	}
}

func BenchmarkClonedMigratedTestDB(b *testing.B) {
	disableBenchmarkLogs(b)
	templatePath, err := migratedTemplatePath()
	if err != nil {
		b.Fatalf("prepare migrated template: %v", err)
	}

	parent := b.TempDir()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dbPath := filepath.Join(parent, fmt.Sprintf("clone-%d.db", i))
		if err := copyFile(templatePath, dbPath); err != nil {
			b.Fatalf("clone migrated template: %v", err)
		}
		db, err := database.New(dbPath)
		if err != nil {
			b.Fatalf("open cloned migration db: %v", err)
		}
		if err := db.Close(); err != nil {
			b.Fatalf("close cloned migration db: %v", err)
		}
	}
}

func TestRemoveSQLiteSidecars(t *testing.T) {
	t.Run("ignores missing sidecars", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "template.db")

		if err := removeSQLiteSidecars(dbPath); err != nil {
			t.Fatalf("remove missing sidecars: %v", err)
		}
	})

	t.Run("removes existing sidecars", func(t *testing.T) {
		dbPath := filepath.Join(t.TempDir(), "template.db")
		for _, suffix := range []string{"-wal", "-shm"} {
			if err := os.WriteFile(dbPath+suffix, []byte("sidecar"), 0o600); err != nil {
				t.Fatalf("create sidecar %s: %v", suffix, err)
			}
		}

		if err := removeSQLiteSidecars(dbPath); err != nil {
			t.Fatalf("remove sidecars: %v", err)
		}
		for _, suffix := range []string{"-wal", "-shm"} {
			if _, err := os.Stat(dbPath + suffix); !os.IsNotExist(err) {
				t.Fatalf("sidecar %s still exists: %v", suffix, err)
			}
		}
	})
}

func TestTemplatePathIsCached(t *testing.T) {
	disableTestLogs(t)

	first, err := migratedTemplatePath()
	if err != nil {
		t.Fatalf("first migrated template path: %v", err)
	}

	second, err := migratedTemplatePath()
	if err != nil {
		t.Fatalf("second migrated template path: %v", err)
	}

	if first != second {
		t.Fatalf("migrated template path differs across calls: first=%q, second=%q", first, second)
	}
}

func TestNewIsIsolated(t *testing.T) {
	disableTestLogs(t)
	ctx := context.Background()
	first := NewMigratedSQLite(t, "isolated-first")
	second := NewMigratedSQLite(t, "isolated-second")

	insertIsolationProbe(ctx, t, first, "first")
	insertIsolationProbe(ctx, t, second, "second")

	assertIsolationProbe(ctx, t, first, "first")
	assertIsolationProbe(ctx, t, second, "second")
}

func TestNewParallel(t *testing.T) {
	disableTestLogs(t)
	for i := range 8 {
		t.Run(fmt.Sprintf("db-%d", i), func(t *testing.T) {
			t.Parallel()

			db := NewMigratedSQLite(t, t.Name())
			if err := db.Conn().PingContext(context.Background()); err != nil {
				t.Fatalf("ping migrated sqlite: %v", err)
			}
		})
	}
}

func insertIsolationProbe(ctx context.Context, t *testing.T, db *database.DB, value string) {
	t.Helper()

	if _, err := db.Conn().ExecContext(ctx, "CREATE TABLE IF NOT EXISTS isolation_probe (value TEXT NOT NULL)"); err != nil {
		t.Fatalf("create isolation probe table: %v", err)
	}
	if _, err := db.Conn().ExecContext(ctx, "INSERT INTO isolation_probe (value) VALUES (?)", value); err != nil {
		t.Fatalf("insert isolation probe: %v", err)
	}
}

func assertIsolationProbe(ctx context.Context, t *testing.T, db *database.DB, want string) {
	t.Helper()

	var total int
	var value string
	if err := db.Conn().QueryRowContext(ctx, "SELECT COUNT(*), MAX(value) FROM isolation_probe").Scan(&total, &value); err != nil {
		t.Fatalf("query isolation probe: %v", err)
	}
	if total != 1 || value != want {
		t.Fatalf("isolation probe = count %d, value %q; want count 1, value %q", total, value, want)
	}
}

func disableTestLogs(t *testing.T) {
	t.Helper()

	original := log.Logger
	log.Logger = zerolog.Nop()
	t.Cleanup(func() {
		log.Logger = original
	})
}

func disableBenchmarkLogs(b *testing.B) {
	b.Helper()

	original := log.Logger
	log.Logger = zerolog.Nop()
	b.Cleanup(func() {
		log.Logger = original
	})
}

// Two tests that start in the same clock tick must not claim the same schema.
func TestSchemaNamesDifferWithinOneClockTick(t *testing.T) {
	now := time.Now()
	first := testSchemaName("filesmanager", now)
	second := testSchemaName("filesmanager", now)
	if first == second {
		t.Fatalf("schema names collide: %s", first)
	}
}

// cloneHelperEnv makes TestTemplateSharedAcrossProcesses act as the subprocess
// that clones the template, the way another package process would. The
// cross-process tests start it with cloneHelperCmd.
const cloneHelperEnv = "QUI_TESTDB_CLONE_HELPER"

func TestTemplateSharedAcrossProcesses(t *testing.T) {
	if os.Getenv(cloneHelperEnv) == "1" {
		disableTestLogs(t)
		db := NewMigratedSQLite(t, "helper")
		var n int
		if err := db.Conn().QueryRowContext(t.Context(), "SELECT COUNT(*) FROM string_pool").Scan(&n); err != nil {
			t.Fatalf("query migrated schema: %v", err)
		}
		return
	}

	cacheDir := t.TempDir()
	tmpDir := t.TempDir()

	runCloneHelper(t, cacheDir, tmpDir)
	first := onlyTemplate(t, cacheDir)
	runCloneHelper(t, cacheDir, tmpDir)
	second := onlyTemplate(t, cacheDir)

	if !first.ModTime().Equal(second.ModTime()) {
		t.Fatal("second process rebuilt the template instead of reusing it")
	}
	if entries, _ := os.ReadDir(tmpDir); len(entries) != 0 {
		t.Fatalf("temp dir holds leftovers: %v", entries)
	}
}

func TestTemplateConcurrentBuilders(t *testing.T) {
	cacheDir := t.TempDir()

	cmds := make([]*exec.Cmd, 4)
	outputs := make([]strings.Builder, len(cmds))
	for i := range cmds {
		cmds[i] = cloneHelperCmd(t, cacheDir, t.TempDir())
		cmds[i].Stdout = &outputs[i]
		cmds[i].Stderr = &outputs[i]
		if err := cmds[i].Start(); err != nil {
			t.Fatalf("start clone helper: %v", err)
		}
	}
	for i, cmd := range cmds {
		if err := cmd.Wait(); err != nil {
			t.Errorf("clone helper %d: %v\n%s", i, err, outputs[i].String())
		}
	}
	onlyTemplate(t, cacheDir)
}

func TestCachedTemplateKeyFollowsMigrations(t *testing.T) {
	dir := t.TempDir()
	builds := 0
	build := func(dst string) error {
		builds++
		return os.WriteFile(dst, []byte("template"), 0o600)
	}
	v1 := fstest.MapFS{"migrations/001_init.sql": {Data: []byte("CREATE TABLE a (id INTEGER);")}}
	v2 := fstest.MapFS{
		"migrations/001_init.sql": {Data: []byte("CREATE TABLE a (id INTEGER);")},
		"migrations/002_b.sql":    {Data: []byte("CREATE TABLE b (id INTEGER);")},
	}

	first, err := cachedTemplate(dir, v1, build)
	if err != nil {
		t.Fatalf("build v1 template: %v", err)
	}
	again, err := cachedTemplate(dir, v1, build)
	if err != nil {
		t.Fatalf("reuse v1 template: %v", err)
	}
	if again != first || builds != 1 {
		t.Fatalf("same migrations: path %q, want %q; builds %d, want 1", again, first, builds)
	}

	second, err := cachedTemplate(dir, v2, build)
	if err != nil {
		t.Fatalf("build v2 template: %v", err)
	}
	if second == first || builds != 2 {
		t.Fatalf("changed migrations: path %q reused or builds %d, want 2", second, builds)
	}
}

func cloneHelperCmd(t *testing.T, cacheDir, tmpDir string) *exec.Cmd {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestTemplateSharedAcrossProcesses$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		cloneHelperEnv+"=1",
		cacheDirEnv+"="+cacheDir,
		"TMPDIR="+tmpDir, "TMP="+tmpDir, "TEMP="+tmpDir,
	)
	return cmd
}

func runCloneHelper(t *testing.T, cacheDir, tmpDir string) {
	t.Helper()
	if output, err := cloneHelperCmd(t, cacheDir, tmpDir).CombinedOutput(); err != nil {
		t.Fatalf("clone helper: %v\n%s", err, output)
	}
}

// onlyTemplate fails unless dir holds exactly one finished template file.
func onlyTemplate(t *testing.T, dir string) os.FileInfo {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read cache dir: %v", err)
	}
	if len(entries) != 1 || !strings.HasPrefix(entries[0].Name(), "template-") || filepath.Ext(entries[0].Name()) != ".db" {
		t.Fatalf("cache dir = %v, want one template-*.db file", entries)
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatalf("stat template: %v", err)
	}
	return info
}
