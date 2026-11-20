// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Command serve runs the fake brrbin for a live run:
//
//	go run ./internal/testutil/fakebrrbin/serve -addr 127.0.0.1:0
//
// It prints its URL. GET /fake/requests lists what it received,
// POST /fake/refuse with {"status":429,"reason":"anonymous_full"} sets a refusal,
// POST /fake/status with {"id":"r1","status":"issue","issue":{"number":4321,"url":"https://github.com/autobrr/qui/issues/4321","closed":false}}
// sets a Report status, and POST /fake/forget with {"id":"r1"} drops a report.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/autobrr/qui/internal/testutil/fakebrrbin"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:0", "address to listen on")
	flag.Parse()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	err := run(ctx, *addr)
	stop()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context, addr string) error {
	listener, err := (&net.ListenConfig{}).Listen(ctx, "tcp", addr)
	if err != nil {
		return err
	}
	server := &http.Server{Handler: fakebrrbin.New(), ReadHeaderTimeout: 10 * time.Second}
	fmt.Printf("fake brrbin at http://%s\n", listener.Addr())
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()
	if err := server.Serve(listener); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
