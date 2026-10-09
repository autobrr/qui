// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"flag"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

func TestBuildCommand_WindowsArgumentRoundTrip(t *testing.T) {
	type recording struct {
		Args []string
		PID  int
	}
	// The detached child reports its actual argv; cmd.Args alone cannot prove it.
	if os.Getenv("QUI_TEST_WINDOWS_ARGV_CHILD") == "1" {
		conn, err := net.DialTimeout("tcp", os.Getenv("QUI_TEST_WINDOWS_ARGV_ADDRESS"), 5*time.Second)
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.SetWriteDeadline(time.Now().Add(5*time.Second)))
		data, err := json.Marshal(recording{Args: flag.Args(), PID: os.Getpid()})
		require.NoError(t, err)
		_, err = conn.Write(append(data, '\n'))
		require.NoError(t, err)
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		var ack [1]byte
		_, err = io.ReadFull(conn, ack[:])
		require.NoError(t, err)
		return
	}

	executable, err := os.Executable()
	require.NoError(t, err)
	source, err := os.Open(executable)
	require.NoError(t, err)
	defer source.Close()
	programPath := filepath.Join(t.TempDir(), "argument recorder.exe")
	destination, err := os.OpenFile(programPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	require.NoError(t, err)
	defer destination.Close()
	_, err = io.Copy(destination, source)
	require.NoError(t, err)
	require.NoError(t, destination.Close())

	t.Setenv("QUI_TEST_WINDOWS_ARGV_CHILD", "1")
	t.Setenv("QUI_TEST_WINDOWS_ARGV_SENTINEL", "EXPANDED")
	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "plain control", args: []string{"literal", "hash123"}},
		{name: "spaces", args: []string{"value with spaces"}},
		{name: "ampersand", args: []string{"Fixture&rem"}},
		{name: "pipe", args: []string{"Fixture|rem"}},
		{name: "redirection", args: []string{"Fixture>nul"}},
		{name: "caret", args: []string{"Fixture^Name"}},
		{name: "percent expansion", args: []string{"%QUI_TEST_WINDOWS_ARGV_SENTINEL%"}},
		{name: "quote", args: []string{`a"b`}},
		{name: "empty argument", args: []string{""}},
		{name: "unicode", args: []string{"媒体 library"}},
		{name: "trailing backslash", args: []string{`C:\Media Library\`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			defer listener.Close()
			t.Setenv("QUI_TEST_WINDOWS_ARGV_ADDRESS", listener.Addr().String())
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			args := append([]string{"-test.run=^TestBuildCommand_WindowsArgumentRoundTrip$", "--"}, tc.args...)
			cmd, _ := (&Service{}).buildCommand(ctx, &models.ExternalProgram{Path: programPath}, args)
			require.NoError(t, cmd.Start())
			waited := false
			defer func() {
				if !waited {
					cancel()
					_ = cmd.Wait()
				}
			}()
			require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
			conn, err := listener.AcceptTCP()
			require.NoError(t, err, "detached child did not report argv")
			defer conn.Close()
			require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
			data, err := bufio.NewReader(conn).ReadBytes('\n')
			require.NoError(t, err)
			var received recording
			require.NoError(t, json.Unmarshal(data, &received))
			// Open the live child's handle before acknowledging it, so cleanup cannot target a reused PID.
			child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(received.PID))
			require.NoError(t, err)
			defer func() {
				_ = windows.TerminateProcess(child, 1)
				_ = windows.CloseHandle(child)
			}()
			_, err = conn.Write([]byte{1})
			require.NoError(t, err)
			status, err := windows.WaitForSingleObject(child, 5000)
			require.NoError(t, err)
			require.EqualValues(t, windows.WAIT_OBJECT_0, status, "recorder must exit before its temporary executable is removed")
			err = cmd.Wait()
			waited = true
			require.NoError(t, err)
			assert.Equal(t, tc.args, received.Args, "substituted metadata must reach the child literally")
		})
	}

	t.Run("direct process holds its execution slot until exit", func(t *testing.T) {
		listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
		require.NoError(t, err)
		defer listener.Close()
		t.Setenv("QUI_TEST_WINDOWS_ARGV_ADDRESS", listener.Addr().String())
		service := NewService(nil, nil, &domain.Config{ExternalProgramMaxRunning: 1})
		service.maxWaiting = 0
		program := &models.ExternalProgram{ID: 1, Name: "recorder", Enabled: true, Path: programPath,
			ArgsTemplate: "-test.run=^TestBuildCommand_WindowsArgumentRoundTrip$ --"}
		result := service.Execute(t.Context(), ExecuteRequest{Program: program, InstanceID: 1,
			Torrent: &qbt.Torrent{Hash: "synthetic-first", Name: "synthetic"}})
		require.True(t, result.Success, "%v", result.Error)
		t.Cleanup(func() {
			require.Eventually(t, func() bool { return service.admitted.Load() == 0 }, 6*time.Second, 10*time.Millisecond)
		})
		require.NoError(t, listener.SetDeadline(time.Now().Add(5*time.Second)))
		conn, err := listener.AcceptTCP()
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
		_, err = bufio.NewReader(conn).ReadBytes('\n')
		require.NoError(t, err)
		assert.Len(t, service.slots, 1, "the recorder is alive and waiting for acknowledgment")
		blocked := service.Execute(t.Context(), ExecuteRequest{Program: program, InstanceID: 1,
			Torrent: &qbt.Torrent{Hash: "synthetic-second", Name: "synthetic"}})
		require.False(t, blocked.Success)
		require.ErrorContains(t, blocked.Error, "execution queue full")
		_, err = conn.Write([]byte{1})
		require.NoError(t, err)
		require.Eventually(t, func() bool { return service.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
	})
}

func TestBuildCommand_WindowsBatchScripts(t *testing.T) {
	for _, ext := range []string{".bat", ".CMD"} {
		t.Run(ext, func(t *testing.T) {
			program := filepath.Join(t.TempDir(), "batch control"+ext)
			require.NoError(t, os.WriteFile(program, []byte("@echo off\r\necho %~1\r\nexit\r\n"), 0o600))
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			cmd, launcher := (&Service{}).buildCommand(ctx, &models.ExternalProgram{Path: program}, []string{"literal"})
			cmd.WaitDelay = 5 * time.Second
			require.True(t, launcher)
			out, err := cmd.CombinedOutput()
			require.NoError(t, err, "%s", out)
			assert.Equal(t, "literal\r\n", string(out))
		})
	}
}
