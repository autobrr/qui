// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package server

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/pkg/agent/proto"
)

func TestServer_DiagEcho_RoundTrip(t *testing.T) {
	// Create pipes for stdin/stdout.
	stdinR, stdinW := io.Pipe()
	var stdout bytes.Buffer

	srv := NewServer(stdinR, &stdout)

	// Run server in a goroutine.
	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(context.Background(), []string{"/data"})
	}()

	// Write a diag.echo command.
	args, _ := json.Marshal(proto.DiagEchoRequest{Message: "hello helper"})
	cmd := proto.Command{
		RequestID: "test-1",
		Op:        proto.OpDiagEcho,
		Args:      args,
	}
	cmdBytes, _ := json.Marshal(cmd)
	cmdBytes = append(cmdBytes, '\n')
	_, err := stdinW.Write(cmdBytes)
	require.NoError(t, err)

	// Close stdin to signal EOF.
	stdinW.Close()

	// Wait for server to finish.
	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit within timeout")
	}

	// Parse output: first line is HelloBanner, second is the Result.
	scanner := bufio.NewScanner(&stdout)

	// Line 1: HelloBanner
	require.True(t, scanner.Scan(), "expected HelloBanner line")
	var banner proto.HelloBanner
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &banner))
	assert.Equal(t, "1", banner.ProtoVersion)
	assert.Contains(t, banner.Capabilities, proto.OpDiagEcho)

	// Line 2: Result for diag.echo
	require.True(t, scanner.Scan(), "expected Result line")
	var result proto.Result
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &result))
	assert.Equal(t, "test-1", result.RequestID)
	assert.True(t, result.OK)
	assert.True(t, result.Done)

	var echoResp proto.DiagEchoResponse
	require.NoError(t, json.Unmarshal(result.Payload, &echoResp))
	assert.Equal(t, "hello helper", echoResp.Message)
}

func TestServer_UnsupportedOp(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	var stdout bytes.Buffer

	srv := NewServer(stdinR, &stdout)

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(context.Background(), nil)
	}()

	args, _ := json.Marshal(map[string]string{"path": "/data"})
	cmd := proto.Command{RequestID: "bad-1", Op: "fs.stat", Args: args}
	cmdBytes, _ := json.Marshal(cmd)
	cmdBytes = append(cmdBytes, '\n')
	_, _ = stdinW.Write(cmdBytes)
	stdinW.Close()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	scanner := bufio.NewScanner(&stdout)
	scanner.Scan() // skip banner
	require.True(t, scanner.Scan())

	var result proto.Result
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &result))
	assert.Equal(t, "bad-1", result.RequestID)
	assert.False(t, result.OK)
	assert.Equal(t, proto.CodeInternal, result.Code)
	assert.Contains(t, result.Error, "unsupported op")
}

func TestServer_MalformedCommand(t *testing.T) {
	stdinR, stdinW := io.Pipe()
	var stdout bytes.Buffer

	srv := NewServer(stdinR, &stdout)

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(context.Background(), nil)
	}()

	_, _ = stdinW.Write([]byte("this is not json\n"))
	stdinW.Close()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	scanner := bufio.NewScanner(&stdout)
	scanner.Scan() // skip banner
	require.True(t, scanner.Scan())

	var result proto.Result
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &result))
	assert.False(t, result.OK)
	assert.Equal(t, proto.CodeInternal, result.Code)
	assert.Contains(t, result.Error, "malformed")
}

func TestServer_ContextCancellation(t *testing.T) {
	stdinR, _ := io.Pipe() // never close stdin — rely on ctx cancel
	var stdout bytes.Buffer

	srv := NewServer(stdinR, &stdout)
	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(ctx, nil)
	}()

	// Give server time to start, then cancel.
	time.Sleep(50 * time.Millisecond)
	cancel()
	stdinR.Close() // unblock scanner

	select {
	case <-done:
		// Server exited — success.
	case <-time.After(5 * time.Second):
		t.Fatal("server did not exit after context cancellation")
	}
}

func TestServer_VersionJSON(t *testing.T) {
	// This tests the HelloBanner output specifically.
	stdinR, stdinW := io.Pipe()
	var stdout bytes.Buffer

	srv := NewServer(stdinR, &stdout)

	done := make(chan error, 1)
	go func() {
		done <- srv.Serve(context.Background(), []string{"/home/user/data", "/home/user/seed"})
	}()

	stdinW.Close() // immediate EOF

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("timeout")
	}

	scanner := bufio.NewScanner(&stdout)
	require.True(t, scanner.Scan())

	var banner proto.HelloBanner
	require.NoError(t, json.Unmarshal(scanner.Bytes(), &banner))
	assert.Equal(t, "1", banner.ProtoVersion)
	assert.Equal(t, []string{"/home/user/data", "/home/user/seed"}, banner.AllowedRoots)
	assert.NotEmpty(t, banner.Hostname)
	assert.NotZero(t, banner.PID)
}
