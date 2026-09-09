// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command postgres runs Go tests with a temporary Postgres server.
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, os.Args[1:])
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) (runErr error) {
	dsn := strings.TrimSpace(os.Getenv("QUI_TEST_POSTGRES_DSN"))
	if dsn == "" {
		cacheDir, err := os.UserCacheDir()
		if err != nil {
			return fmt.Errorf("find Postgres cache directory: %w", err)
		}
		cacheDir = filepath.Join(cacheDir, "qui", "embedded-postgres", string(embeddedpostgres.V17), runtime.GOOS+"-"+runtime.GOARCH)
		if err := os.MkdirAll(cacheDir, 0o700); err != nil {
			return fmt.Errorf("create Postgres cache directory: %w", err)
		}
		runtimeDir, err := os.MkdirTemp(cacheDir, "runtime-")
		if err != nil {
			return fmt.Errorf("create Postgres runtime directory: %w", err)
		}
		defer func() { runErr = errors.Join(runErr, os.RemoveAll(runtimeDir)) }()

		listener, err := new(net.ListenConfig).Listen(ctx, "tcp", "127.0.0.1:0")
		if err != nil {
			return fmt.Errorf("find Postgres port: %w", err)
		}
		port := listener.Addr().(*net.TCPAddr).Port
		if err := listener.Close(); err != nil {
			return err
		}

		config := embeddedpostgres.DefaultConfig().
			Version(embeddedpostgres.V17).
			Database("qui").
			Port(uint32(port)).
			CachePath(filepath.Join(cacheDir, "archives")).
			RuntimePath(runtimeDir).
			StartTimeout(45 * time.Second).
			StartParameters(map[string]string{"max_connections": "200"}).
			Logger(os.Stderr)
		pg := embeddedpostgres.NewDatabase(config)
		// The library downloads binaries through the default HTTP client.
		http.DefaultClient.Timeout = 2 * time.Minute
		if err := pg.Start(); err != nil {
			return fmt.Errorf("start test Postgres: %w", err)
		}
		defer func() { runErr = errors.Join(runErr, pg.Stop()) }()
		dsn = config.GetConnectionURL() + "?sslmode=disable"
		fmt.Fprintf(os.Stderr, "Test Postgres started on port %d in %s\n", port, runtimeDir)
	}

	if len(args) == 0 {
		args = []string{"-race", "-count=1", "-v", "-timeout=20m", "./..."}
	}
	cmd := exec.CommandContext(ctx, "go", append([]string{"test"}, args...)...) //nolint:gosec // Forward developer-supplied test arguments without a shell.
	cmd.Env = append(os.Environ(), "QUI_TEST_POSTGRES_DSN="+dsn)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
