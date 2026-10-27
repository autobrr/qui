// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"bytes"
	"context"
	"encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
)

// restartExitCode is the exit code that a supervised child uses to ask the
// supervisor for a Restart. 75 is EX_TEMPFAIL: qui itself exits with 0 or 1,
// and a Go panic exits with 2.
const restartExitCode = 75

// supervisedEnv marks the child of a supervisor.
const supervisedEnv = "QUI_SUPERVISED"

// runSupervisor starts the binary at path as a child with qui's own argv, and
// starts it again each time the child exits with restartExitCode. It returns
// the first other exit code. With a nil stderr the child shares the
// supervisor's stdio. qui-tray.exe has no console, so its child gets no stdin
// or stdout and writes stderr to the given writer.
func runSupervisor(path string, stderr io.Writer) (int, error) {
	for {
		//nolint:gosec // G204: path is qui's own binary, resolved at startup, and argv is qui's own
		cmd := exec.CommandContext(context.Background(), path)
		cmd.Args = os.Args
		cmd.Env = append(os.Environ(), supervisedEnv+"=1")
		if stderr == nil {
			cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
		} else {
			cmd.Stderr = stderr
		}
		if err := cmd.Run(); err != nil {
			if _, ok := errors.AsType[*exec.ExitError](err); !ok {
				return 0, err
			}
		}
		if code := cmd.ProcessState.ExitCode(); code != restartExitCode {
			return code, nil
		}
	}
}

const stderrTailSize = 16 << 10

// stderrTail keeps the end of the child's stderr for the error dialog. Only
// the exec copy goroutine writes to it, and Wait returns after that goroutine
// ends, so the supervisor reads it without a lock.
type stderrTail struct {
	b []byte
}

func (t *stderrTail) Write(p []byte) (int, error) {
	t.b = append(t.b, p...)
	if len(t.b) > 2*stderrTailSize {
		t.b = append(t.b[:0], t.b[len(t.b)-stderrTailSize:]...)
	}
	return len(p), nil
}

// exitMessage explains a child that exited with code, from the end of its
// stderr: the panic line of a crash, or else the last line when it is plain
// text or a zerolog fatal event.
func exitMessage(code int, stderr []byte) string {
	var line, crash []byte
	for l := range bytes.SplitSeq(bytes.TrimSpace(stderr), []byte("\n")) {
		line = l
		if bytes.HasPrefix(l, []byte("panic: ")) || bytes.HasPrefix(l, []byte("fatal error: ")) {
			crash = l
		}
	}
	if crash != nil {
		line = crash
	}

	text := string(bytes.TrimSpace(line))
	var event struct {
		Level   string `json:"level"`
		Message string `json:"message"`
		Error   string `json:"error"`
	}
	if json.Unmarshal(line, &event) == nil {
		// Any other log line was last only by chance, as after a kill.
		text = ""
		if event.Level == "fatal" || event.Level == "panic" {
			text = event.Message
			if event.Error != "" {
				text += ": " + event.Error
			}
		}
	}
	if text == "" {
		return fmt.Sprintf("qui stopped with exit code %d.", code)
	}
	return fmt.Sprintf("qui stopped with exit code %d:\n\n%s", code, text)
}
