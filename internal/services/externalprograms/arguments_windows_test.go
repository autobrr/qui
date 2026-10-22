// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"bufio"
	"context"
	"encoding/json/v2"
	"errors"
	"flag"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestBuildCommand_WindowsArgumentRoundTrip(t *testing.T) {
	type recording struct {
		Args   []string
		PID    int
		Parent int
	}
	// The detached child reports its actual argv; cmd.Args alone cannot prove it.
	if os.Getenv("QUI_TEST_WINDOWS_ARGV_CHILD") == "1" {
		conn, err := net.DialTimeout("tcp", os.Getenv("QUI_TEST_WINDOWS_ARGV_ADDRESS"), 5*time.Second)
		require.NoError(t, err)
		defer conn.Close()
		require.NoError(t, conn.SetWriteDeadline(time.Now().Add(5*time.Second)))
		data, err := json.Marshal(recording{Args: flag.Args(), PID: os.Getpid(), Parent: os.Getppid()})
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
	guiPath := filepath.Join(t.TempDir(), "GUI recorder.exe")
	build := exec.CommandContext(t.Context(), "go", "build", "-ldflags=-H=windowsgui", "-o", guiPath, "./testdata/argv")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)
	t.Run("GUI metadata paths", func(t *testing.T) {
		require.True(t, isWindowsGUIExecutable(guiPath))
		require.False(t, isWindowsGUIExecutable(programPath))
		longDir := filepath.Join(t.TempDir(), strings.Repeat("long", 45))
		require.NoError(t, os.Mkdir(longDir, 0o750))
		longPath := filepath.Join(longDir, "GUI recorder.exe")
		require.GreaterOrEqual(t, len(longPath), windows.MAX_PATH)
		data, err := os.ReadFile(guiPath)
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(longPath, data, 0o600))
		require.True(t, isWindowsGUIExecutable(longPath), "long paths retain PE classification")
	})
	t.Run("GUI metadata on an STA thread", func(t *testing.T) {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
		if errors.Is(err, syscall.Errno(windows.RPC_E_CHANGED_MODE)) {
			t.Skip("test thread already uses a different COM apartment")
		}
		require.True(t, err == nil || errors.Is(err, syscall.Errno(1)), "%v", err)
		defer windows.CoUninitialize()
		require.True(t, isWindowsGUIExecutable(guiPath))
		require.ErrorIs(t, windows.CoInitializeEx(0, windows.COINIT_MULTITHREADED), syscall.Errno(windows.RPC_E_CHANGED_MODE),
			"classification must preserve the caller's COM apartment")
	})

	t.Setenv("QUI_TEST_WINDOWS_ARGV_CHILD", "1")
	t.Setenv("QUI_TEST_WINDOWS_ARGV_SENTINEL", "EXPANDED")
	for _, tc := range []struct {
		name string
		args []string
		gui  bool
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
		{name: "GUI literal arguments", gui: true, args: []string{"Fixture&rem", "%QUI_TEST_WINDOWS_ARGV_SENTINEL%", `a"b`, "value with spaces", "", `C:\Media Library\`}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			defer listener.Close()
			t.Setenv("QUI_TEST_WINDOWS_ARGV_ADDRESS", listener.Addr().String())
			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()
			args := append([]string{"-test.run=^TestBuildCommand_WindowsArgumentRoundTrip$", "--"}, tc.args...)
			configured := programPath
			if tc.gui {
				configured, args = guiPath, tc.args
			}
			cmd, _ := (&Service{}).buildCommand(ctx, &models.ExternalProgram{Path: configured}, args)
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

	for _, kind := range []string{"native process", "GUI process", "extensionless batch launcher", "terminal launcher", "terminal expansion"} {
		t.Run(kind+" execution slot lifetime", func(t *testing.T) {
			listener, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
			require.NoError(t, err)
			defer listener.Close()
			t.Setenv("QUI_TEST_WINDOWS_ARGV_ADDRESS", listener.Addr().String())
			service := NewService(nil, nil, &domain.Config{ExternalProgramMaxRunning: 1})
			service.maxWaiting = 0
			program := &models.ExternalProgram{ID: 1, Name: "recorder", Enabled: true, Path: programPath,
				ArgsTemplate: "-test.run=^TestBuildCommand_WindowsArgumentRoundTrip$ --"}
			instanceID := 1
			if kind == "GUI process" {
				db := testdb.NewMigratedSQLite(t, t.Name())
				instances, err := models.NewInstanceStore(db, make([]byte, 32))
				require.NoError(t, err)
				instance, err := instances.Create(t.Context(), "synthetic", "http://localhost:8080", "", "", nil, nil, false, new(false))
				require.NoError(t, err)
				instanceID = instance.ID
				service.activityStore = models.NewAutomationActivityStore(db)
				program.Path, program.ArgsTemplate = guiPath, "literal"
			}
			if strings.HasPrefix(kind, "terminal") {
				program.Path = filepath.Join(t.TempDir(), "terminal.cmd")
				body := "@echo off\r\n\"" + guiPath + "\" %*\r\nexit\r\n"
				require.NoError(t, os.WriteFile(program.Path, []byte(body), 0o600))
				program.UseTerminal, program.ArgsTemplate = true, "literal"
				if kind == "terminal expansion" {
					program.ArgsTemplate = "%QUI_TEST_WINDOWS_ARGV_SENTINEL%"
				}
			}
			if kind == "extensionless batch launcher" {
				batch := filepath.Join(t.TempDir(), "recorder launcher.bat")
				body := "@echo off\r\n\"" + programPath + "\" %*\r\nexit\r\n"
				require.NoError(t, os.WriteFile(batch, []byte(body), 0o600))
				program.Path = strings.TrimSuffix(batch, ".bat")
			}
			result := service.Execute(t.Context(), ExecuteRequest{Program: program, InstanceID: instanceID,
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
			data, err := bufio.NewReader(conn).ReadBytes('\n')
			require.NoError(t, err)
			var received recording
			require.NoError(t, json.Unmarshal(data, &received))
			child, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(received.PID))
			require.NoError(t, err)
			defer func() { _ = windows.TerminateProcess(child, 1); _ = windows.CloseHandle(child) }()
			handles := []windows.Handle{child}
			if program.UseTerminal {
				terminal, err := windows.OpenProcess(windows.SYNCHRONIZE|windows.PROCESS_TERMINATE, false, uint32(received.Parent))
				require.NoError(t, err)
				defer func() { _ = windows.TerminateProcess(terminal, 1); _ = windows.CloseHandle(terminal) }()
				handles = append(handles, terminal)
				want := "literal"
				if kind == "terminal expansion" {
					want = "EXPANDED"
				}
				require.Equal(t, []string{want}, received.Args)
			}
			if kind == "native process" {
				assert.Len(t, service.slots, 1, "the recorder is alive and waiting for acknowledgment")
				blocked := service.Execute(t.Context(), ExecuteRequest{Program: program, InstanceID: instanceID,
					Torrent: &qbt.Torrent{Hash: "synthetic-second", Name: "synthetic"}})
				require.False(t, blocked.Success)
				require.ErrorContains(t, blocked.Error, "execution queue full")
			} else {
				require.Eventually(t, func() bool { return service.admitted.Load() == 0 }, time.Second, 10*time.Millisecond,
					"GUI and command launchers release their slot while the child waits for acknowledgment")
				state, err := windows.WaitForSingleObject(child, 0)
				require.NoError(t, err)
				require.EqualValues(t, windows.WAIT_TIMEOUT, state)
			}
			if kind == "GUI process" {
				require.EventuallyWithT(t, func(c *assert.CollectT) {
					entries, err := service.activityStore.ListByInstance(t.Context(), instanceID, 10)
					assert.NoError(c, err)
					if assert.Len(c, entries, 1, "GUI activity is reported before the child exits") {
						assert.Equal(c, models.ActivityOutcomeSuccess, entries[0].Outcome)
					}
				}, time.Second, 10*time.Millisecond)
			}
			_, err = conn.Write([]byte{1})
			require.NoError(t, err)
			for _, handle := range handles {
				state, err := windows.WaitForSingleObject(handle, 5000)
				require.NoError(t, err)
				require.EqualValues(t, windows.WAIT_OBJECT_0, state)
			}
			require.Eventually(t, func() bool { return service.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
		})
	}
}

func TestBuildCommand_WindowsRequiresExecutable(t *testing.T) {
	for _, ext := range []string{".ps1", ".py", ".lnk"} {
		t.Run(ext, func(t *testing.T) {
			file := filepath.Join(t.TempDir(), "synthetic"+ext)
			require.NoError(t, os.WriteFile(file, []byte("synthetic non-executable fixture"), 0o600))
			cmd, _ := (&Service{}).buildCommand(t.Context(), &models.ExternalProgram{Path: file}, nil)
			err := cmd.Run()
			require.ErrorIs(t, err, windows.ERROR_BAD_EXE_FORMAT)
		})
	}
}

func TestBuildCommand_WindowsBatchScripts(t *testing.T) {
	for _, ext := range []string{".bat", ".CMD"} {
		t.Run(ext, func(t *testing.T) {
			dir := t.TempDir()
			program := filepath.Join(dir, "batch control"+ext)
			require.NoError(t, os.WriteFile(program, []byte("@echo off\r\necho %~1\r\nexit\r\n"), 0o600))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
			for _, form := range []string{"explicit", "absolute extensionless", "relative extensionless", "PATH extensionless"} {
				t.Run(form, func(t *testing.T) {
					configured := program
					switch form {
					case "absolute extensionless":
						configured = strings.TrimSuffix(program, ext)
					case "relative extensionless":
						t.Chdir(dir)
						configured = ".\\batch control"
					case "PATH extensionless":
						configured = "batch control"
					}
					ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
					defer cancel()
					cmd, launcher := (&Service{}).buildCommand(ctx, &models.ExternalProgram{Path: configured}, []string{"literal"})
					cmd.WaitDelay = 5 * time.Second
					assert.True(t, launcher.releaseOnStart)
					out, err := cmd.CombinedOutput()
					require.NoError(t, err, "%s", out)
					assert.Equal(t, "literal\r\n", string(out))
				})
			}
		})
	}
}

func TestBuildCommand_WindowsBatchPathOperators(t *testing.T) {
	t.Setenv("QUI_TEST_WINDOWS_BATCH_PATH", "expanded")
	for _, name := range []string{"Jobs&Audit", "Jobs(Archive)", "Jobs^Audit", "Jobs%QUI_TEST_WINDOWS_BATCH_PATH%", "Jobs!QUI_TEST_WINDOWS_BATCH_PATH!"} {
		t.Run(name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), name)
			require.NoError(t, os.Mkdir(dir, 0o750))
			program := filepath.Join(dir, "runner.bat")
			require.NoError(t, os.WriteFile(program, []byte("@echo off\r\necho %~1\r\necho %~2\r\nexit\r\n"), 0o600))
			t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("PATHEXT", ".COM;.EXE;.BAT;.CMD")
			for _, configured := range []string{program, "runner"} {
				ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
				defer cancel()
				cmd, launcher := (&Service{}).buildCommand(ctx, &models.ExternalProgram{Path: configured}, []string{"literal", "%QUI_TEST_WINDOWS_BATCH_PATH%"})
				cmd.WaitDelay = 5 * time.Second
				require.True(t, launcher.releaseOnStart)
				output, err := cmd.CombinedOutput()
				require.NoError(t, err, "%s", output)
				require.Equal(t, "literal\r\nexpanded\r\n", string(output), "path stays literal; arguments retain batch expansion")
			}
		})
	}
}

func TestExecuteAsync_WindowsBatchActivity(t *testing.T) {
	db := testdb.NewMigratedSQLite(t, t.Name())
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "synthetic", "http://localhost:8080", "", "", nil, nil, false, new(false))
	require.NoError(t, err)
	activity := models.NewAutomationActivityStore(db)
	for _, code := range []string{"0", "7"} {
		t.Run(code, func(t *testing.T) {
			program := &models.ExternalProgram{Name: "batch " + code, Path: filepath.Join(t.TempDir(), "activity.cmd")}
			require.NoError(t, os.WriteFile(program.Path, []byte("@exit /b "+code+"\r\n"), 0o600))
			service := NewService(nil, activity, &domain.Config{ExternalProgramMaxRunning: 1})
			cmd, launcher := service.buildCommand(t.Context(), program, nil)
			service.admitted.Add(1)
			service.executeAsync(cmd, launcher, waitKey{}, program, ExecuteRequest{InstanceID: instance.ID,
				Torrent: &qbt.Torrent{Hash: "synthetic-" + code, Name: "synthetic"}}, time.Now().Add(time.Second))
			entries, err := activity.ListByInstance(t.Context(), instance.ID, 10)
			require.NoError(t, err)
			for _, entry := range entries {
				if entry.Hash == "synthetic-"+code {
					want := models.ActivityOutcomeSuccess
					if code != "0" {
						want = models.ActivityOutcomeFailed
					}
					require.Equal(t, want, entry.Outcome)
					return
				}
			}
			t.Fatal("missing batch activity")
		})
	}
}
