// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package server implements the NDJSON stdio loop for the qui-helper process.
// It reads Command envelopes from stdin, dispatches them to the executor, and
// writes Result envelopes to stdout. On stdin EOF, it initiates a graceful
// shutdown with a 30-second drain period for in-flight operations.
package server

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"github.com/autobrr/qui/cmd/qui-helper/internal/executor"
	"github.com/autobrr/qui/pkg/agent/proto"
)

const (
	// gracePeriod is the maximum time to wait for in-flight ops after stdin EOF.
	gracePeriod = 30 * time.Second
)

// Server reads NDJSON commands from stdin and writes results to stdout.
type Server struct {
	executor *executor.Executor
	stdin    io.Reader
	stdout   io.Writer

	// Tracks in-flight operations for graceful shutdown.
	wg sync.WaitGroup

	// writeMu serializes writes to stdout (multiple goroutines may write results).
	writeMu sync.Mutex
}

// NewServer creates a new stdio server.
func NewServer(stdin io.Reader, stdout io.Writer) *Server {
	return &Server{
		executor: executor.NewExecutor(),
		stdin:    stdin,
		stdout:   stdout,
	}
}

// Serve runs the NDJSON stdio loop. It blocks until stdin is closed or ctx is
// cancelled. On stdin EOF, it waits up to 30s for in-flight ops to drain.
func (s *Server) Serve(ctx context.Context, allowedRoots []string) error {
	// Write HelloBanner as the first line.
	banner := proto.HelloBanner{
		HelperVersion: "0.0.1-scaffold",
		ProtoVersion:  "1",
		Capabilities:  []string{proto.OpDiagEcho},
		AllowedRoots:  allowedRoots,
		Platform:      runtime.GOOS,
		Hostname:      hostname(),
		PID:           os.Getpid(),
		StartedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.writeJSON(banner); err != nil {
		return fmt.Errorf("write hello banner: %w", err)
	}

	// Create a cancellable context for in-flight ops.
	opCtx, opCancel := context.WithCancel(ctx)
	defer opCancel()

	scanner := bufio.NewScanner(s.stdin)
	scanner.Buffer(make([]byte, 16*1024*1024), 16*1024*1024) // 16 MiB max line

	for scanner.Scan() {
		if ctx.Err() != nil {
			break
		}

		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var cmd proto.Command
		if err := json.Unmarshal(line, &cmd); err != nil {
			// Malformed command — write an error result with empty requestID.
			s.writeResult(proto.Result{
				OK:    false,
				Code:  proto.CodeInternal,
				Error: fmt.Sprintf("malformed command: %v", err),
				Done:  true,
			})
			continue
		}

		if cmd.Op == proto.OpControlCancel {
			// Cancel ops are handled inline (no goroutine).
			// In this scaffold, we just acknowledge — real cancellation comes in Stage C.
			s.writeResult(proto.Result{
				RequestID: cmd.RequestID,
				OK:        true,
				Done:      true,
			})
			continue
		}

		// Dispatch to executor in a goroutine for concurrency.
		s.wg.Add(1)
		go func(c proto.Command) {
			defer s.wg.Done()
			result := s.executor.Execute(opCtx, c)
			s.writeResult(result)
		}(cmd)
	}

	// Stdin EOF or scan error — initiate graceful shutdown.
	opCancel() // Cancel all in-flight op contexts.

	// Wait for in-flight ops with a grace period.
	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// All ops drained cleanly.
	case <-time.After(gracePeriod):
		// Force exit after grace period.
		fmt.Fprintln(os.Stderr, `{"level":"warn","message":"forced exit after grace period"}`)
	}

	return scanner.Err()
}

func (s *Server) writeResult(r proto.Result) {
	_ = s.writeJSON(r)
}

func (s *Server) writeJSON(v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	_, err = s.stdout.Write(data)
	return err
}

func hostname() string {
	h, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	return h
}
