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

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

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
}
