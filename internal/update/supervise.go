// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"errors"
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
// the first other exit code.
func runSupervisor(path string) (int, error) {
	for {
		//nolint:gosec // G204: path is qui's own binary, resolved at startup, and argv is qui's own
		cmd := exec.CommandContext(context.Background(), path)
		cmd.Args = os.Args
		cmd.Env = append(os.Environ(), supervisedEnv+"=1")
		cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
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
