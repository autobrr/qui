// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"bytes"
	"context"
	"errors"
	"os/exec"
	"runtime"
	"time"

	"github.com/Hellseher/go-shellquote"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// ExecutionMode determines whether to wait for the command to complete.
type ExecutionMode int

const (
	// ModeAsync starts the command and returns immediately without waiting.
	// The command runs in a background goroutine. Errors during execution
	// are logged but not returned to the caller.
	ModeAsync ExecutionMode = iota

	// ModeSync waits for the command to complete and returns the result.
	// This blocks until the command finishes or the context is cancelled.
	ModeSync
)

// ExecutionResult contains the outcome of an external program execution.
type ExecutionResult struct {
	// Started indicates whether the command was successfully started.
	Started bool

	// Completed indicates whether the command completed (only meaningful in sync mode).
	// In async mode, this is always false since we don't wait.
	Completed bool

	// ExitCode is the process exit code (only available if Completed is true).
	// -1 indicates the exit code is unknown or the process didn't complete.
	ExitCode int

	// Stdout contains the captured stdout (only in sync mode with CaptureOutput).
	Stdout string

	// Stderr contains the captured stderr (only in sync mode with CaptureOutput).
	Stderr string

	// Error contains any error that occurred during execution.
	// This could be a start error, context cancellation, or wait error.
	Error error

	// Duration is how long the command ran (only meaningful if Started is true).
	Duration time.Duration
}

// ExecuteOptions configures how an external program is executed.
type ExecuteOptions struct {
	// Mode determines sync vs async execution.
	Mode ExecutionMode

	// CaptureOutput captures stdout/stderr in sync mode.
	// Has no effect in async mode.
	CaptureOutput bool

	// AllowList is a list of allowed program paths/directories.
	// If empty, all paths are allowed. If non-empty, the program path
	// must be in or under one of the allowed paths.
	AllowList []string
}

// DefaultOptions returns sensible defaults for external program execution.
func DefaultOptions() ExecuteOptions {
	return ExecuteOptions{
		Mode:          ModeAsync,
		CaptureOutput: false,
	}
}

// Execute runs an external program for a torrent.
//
// The context controls timeout and cancellation. In sync mode, if the context
// is cancelled or times out, the process is killed. In async mode, context
// cancellation stops the background goroutine from logging completion.
//
// Returns an ExecutionResult with details about the execution. In async mode,
// the result only indicates whether the command was successfully started.
func Execute(ctx context.Context, program *models.ExternalProgram, torrent *qbt.Torrent, opts ExecuteOptions) ExecutionResult {
	// Validate params
	if program == nil || torrent == nil {
		log.Warn().Any("program", program).Any("torrent", torrent).Msg("Skipping external program execution - invalid params")
		return ExecutionResult{
			Error: errors.New("invalid parameters: program and torrent must not be nil"),
		}
	}

	// Check path allowlist
	if len(opts.AllowList) > 0 && !IsPathAllowed(program.Path, opts.AllowList) {
		log.Warn().
			Str("path", program.Path).
			Strs("allowList", opts.AllowList).
			Msg("External program path blocked by allow list")
		return ExecutionResult{
			Error: errors.New("program path is not allowed"),
		}
	}

	// Build command arguments
	args := buildArguments(program, torrent)

	// Build command
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmdArgs := []string{"/c", "start", ""}
		if program.UseTerminal {
			// Windows: Use cmd.exe /c start cmd /k to open a new visible terminal window
			cmdArgs = append(cmdArgs, "cmd", "/k")
		} else {
			// Windows: Use 'start' to launch GUI apps properly (detached from parent process)
			cmdArgs = append(cmdArgs, "/b")
		}
		cmdArgs = append(cmdArgs, program.Path)
		cmdArgs = append(cmdArgs, args...)
		cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...)
	} else {
		if program.UseTerminal {
			// Unix/Linux: Build command string and spawn in a terminal
			allArgs := append([]string{program.Path}, args...)
			fullCmd := shellquote.Join(allArgs...)
			var err error
			cmd, err = createTerminalCommand(ctx, fullCmd)
			if err != nil {
				log.Error().
					Err(err).
					Str("hash", torrent.Hash).
					Str("programName", program.Name).
					Int("programId", program.ID).
					Msg("Failed to create terminal command")
				return ExecutionResult{
					Started: false,
					Error:   err,
				}
			}
		} else {
			// Launch directly without terminal
			cmd = exec.CommandContext(ctx, program.Path, args...)
		}
	}

	// Log the command being executed
	log.Debug().
		Str("hash", torrent.Hash).
		Str("programName", program.Name).
		Int("programId", program.ID).
		Str("path", program.Path).
		Strs("args", args).
		Strs("command", cmd.Args).
		Bool("sync", opts.Mode == ModeSync).
		Msg("Executing external program")

	if opts.Mode == ModeSync {
		return executeSync(ctx, cmd, program, torrent, args, opts.CaptureOutput)
	}
	return executeAsync(ctx, cmd, program, torrent, args)
}

// executeSync runs the command synchronously and waits for completion.
func executeSync(ctx context.Context, cmd *exec.Cmd, program *models.ExternalProgram, torrent *qbt.Torrent, args []string, captureOutput bool) ExecutionResult {
	startTime := time.Now()

	var stdout, stderr bytes.Buffer
	if captureOutput {
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		log.Error().
			Err(err).
			Str("hash", torrent.Hash).
			Str("programName", program.Name).
			Int("programId", program.ID).
			Str("path", program.Path).
			Strs("args", args).
			Strs("command", cmd.Args).
			Msg("Failed to start external program")
		return ExecutionResult{
			Started:  false,
			Error:    err,
			Duration: time.Since(startTime),
		}
	}

	// Wait for completion
	waitErr := cmd.Wait()
	duration := time.Since(startTime)

	result := ExecutionResult{
		Started:   true,
		Completed: true,
		Duration:  duration,
		ExitCode:  -1,
	}

	if captureOutput {
		result.Stdout = stdout.String()
		result.Stderr = stderr.String()
	}

	// Extract exit code
	if cmd.ProcessState != nil {
		result.ExitCode = cmd.ProcessState.ExitCode()
	}

	if waitErr != nil {
		// Check if it was a context cancellation
		if ctx.Err() != nil {
			result.Error = ctx.Err()
			log.Warn().
				Err(ctx.Err()).
				Str("hash", torrent.Hash).
				Str("programName", program.Name).
				Int("programId", program.ID).
				Dur("duration", duration).
				Msg("External program execution cancelled or timed out")
		} else {
			result.Error = waitErr
			// Non-zero exit is common for terminal emulators - log at debug level
			log.Debug().
				Err(waitErr).
				Int("exitCode", result.ExitCode).
				Str("hash", torrent.Hash).
				Str("programName", program.Name).
				Int("programId", program.ID).
				Dur("duration", duration).
				Msg("External program exited with non-zero status")
		}
	} else {
		log.Debug().
			Str("hash", torrent.Hash).
			Str("programName", program.Name).
			Int("programId", program.ID).
			Dur("duration", duration).
			Msg("External program completed successfully")
	}

	return result
}

// executeAsync starts the command and returns immediately.
// The command continues running in a background goroutine.
func executeAsync(ctx context.Context, cmd *exec.Cmd, program *models.ExternalProgram, torrent *qbt.Torrent, args []string) ExecutionResult {
	startTime := time.Now()

	if runtime.GOOS == "windows" {
		// Windows: Use Run() which waits for cmd.exe to complete
		// The 'start' command will spawn the process and cmd.exe will exit quickly
		go func() {
			if err := cmd.Run(); err != nil {
				// Only log if context hasn't been cancelled
				if ctx.Err() == nil {
					log.Error().
						Err(err).
						Str("hash", torrent.Hash).
						Str("programName", program.Name).
						Int("programId", program.ID).
						Str("path", program.Path).
						Strs("args", args).
						Strs("command", cmd.Args).
						Msg("Failed to launch external program via cmd.exe")
				}
				return
			}
			log.Debug().
				Str("hash", torrent.Hash).
				Str("programName", program.Name).
				Int("programId", program.ID).
				Dur("duration", time.Since(startTime)).
				Msg("Launched external program (Windows - outcome unknown)")
		}()

		return ExecutionResult{
			Started:  true,
			Duration: time.Since(startTime),
		}
	}

	// Unix/Linux: Start the process
	if err := cmd.Start(); err != nil {
		log.Error().
			Err(err).
			Str("hash", torrent.Hash).
			Str("programName", program.Name).
			Int("programId", program.ID).
			Str("path", program.Path).
			Strs("args", args).
			Strs("command", cmd.Args).
			Msg("Failed to start external program")
		return ExecutionResult{
			Started:  false,
			Error:    err,
			Duration: time.Since(startTime),
		}
	}

	// Wait in background goroutine to prevent zombie processes
	go func() {
		waitErr := cmd.Wait()
		duration := time.Since(startTime)

		// Only log if context hasn't been cancelled
		if ctx.Err() != nil {
			return
		}

		if waitErr != nil {
			// Log at debug level - non-zero exit is common for terminal emulators
			log.Debug().
				Err(waitErr).
				Str("hash", torrent.Hash).
				Str("programName", program.Name).
				Int("programId", program.ID).
				Dur("duration", duration).
				Msg("External program exited with error (may be normal for terminal emulators)")
		} else {
			log.Debug().
				Str("hash", torrent.Hash).
				Str("programName", program.Name).
				Int("programId", program.ID).
				Dur("duration", duration).
				Msg("External program completed successfully")
		}
	}()

	return ExecutionResult{
		Started:  true,
		Duration: time.Since(startTime),
	}
}
