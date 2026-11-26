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

const restartExitCode = 75

const supervisedEnv = "QUI_SUPERVISED"

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
