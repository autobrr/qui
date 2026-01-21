// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package externalprograms provides a unified service for executing external programs
// with torrent data. It is used by automations, cross-seed, and the API handler.
package externalprograms

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	shellquote "github.com/Hellseher/go-shellquote"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	extargs "github.com/autobrr/qui/internal/externalprograms"
	"github.com/autobrr/qui/internal/models"
)

// Activity action constant for external program execution.
// Success/failure is indicated via the Outcome field, following the same pattern as other actions.
const ActivityActionExternalProgram = "external_program"

// Service provides unified external program execution for all consumers.
type Service struct {
	programStore  *models.ExternalProgramStore
	activityStore *models.AutomationActivityStore
	config        *domain.Config
}

// NewService creates a new external programs service.
// activityStore may be nil if activity logging is not needed.
func NewService(
	programStore *models.ExternalProgramStore,
	activityStore *models.AutomationActivityStore,
	config *domain.Config,
) *Service {
	return &Service{
		programStore:  programStore,
		activityStore: activityStore,
		config:        config,
	}
}

// ExecuteRequest contains all parameters needed to execute an external program.
type ExecuteRequest struct {
	// ProgramID is used to fetch the program from the store.
	// Either ProgramID or Program must be provided.
	ProgramID int
	// Program is an optional pre-loaded program configuration.
	// When provided, ProgramID is ignored and no database lookup is performed.
	Program *models.ExternalProgram

	Torrent    *qbt.Torrent
	InstanceID int

	// Optional: automation context for activity logging
	RuleID   *int
	RuleName string
}

// Validate checks that the request has all required fields.
func (r ExecuteRequest) Validate() error {
	if r.Program == nil && r.ProgramID <= 0 {
		return errors.New("either programID or program must be provided")
	}
	if r.Torrent == nil {
		return errors.New("torrent is required")
	}
	if r.InstanceID <= 0 {
		return errors.New("instanceID must be positive")
	}
	return nil
}

// ExecuteResult contains the result of an execution attempt.
type ExecuteResult struct {
	Success bool
	Message string
	Error   error
}

// SuccessResult creates a successful execution result.
func SuccessResult(message string) ExecuteResult {
	return ExecuteResult{Success: true, Message: message}
}

// FailureResult creates a failed execution result.
func FailureResult(err error) ExecuteResult {
	return ExecuteResult{Success: false, Error: err}
}

// FailureResultWithMessage creates a failed execution result with an additional message.
func FailureResultWithMessage(err error, message string) ExecuteResult {
	return ExecuteResult{Success: false, Error: err, Message: message}
}

// Execute runs an external program asynchronously with the given torrent data.
// It returns immediately after launching the program (fire-and-forget).
//
// The program can be provided in two ways:
//   - By ID: Set ProgramID to fetch the program from the store
//   - Directly: Set Program to use a pre-loaded program configuration
func (s *Service) Execute(ctx context.Context, req ExecuteRequest) ExecuteResult {
	program := req.Program

	// If no pre-loaded program, fetch by ID
	if program == nil {
		if s == nil || s.programStore == nil {
			return FailureResult(errors.New("external program service not initialized"))
		}

		var err error
		program, err = s.programStore.GetByID(ctx, req.ProgramID)
		if err != nil {
			if errors.Is(err, models.ErrExternalProgramNotFound) {
				return FailureResult(fmt.Errorf("program not found: %d", req.ProgramID))
			}
			return FailureResult(fmt.Errorf("failed to get program: %w", err))
		}
	}

	return s.executeProgram(ctx, program, req)
}

// executeProgram is the internal implementation that runs a program with a pre-loaded configuration.
func (s *Service) executeProgram(ctx context.Context, program *models.ExternalProgram, req ExecuteRequest) ExecuteResult {
	if program == nil {
		return FailureResult(errors.New("program is nil"))
	}

	if req.Torrent == nil {
		return FailureResult(errors.New("torrent is nil"))
	}

	// Check if program is enabled
	if !program.Enabled {
		log.Debug().
			Int("programId", program.ID).
			Str("programName", program.Name).
			Msg("external program is disabled, skipping execution")
		return FailureResult(errors.New("program is disabled"))
	}

	// Validate against allowlist
	if !s.IsPathAllowed(program.Path) {
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, "path not allowed by allowlist")
		return FailureResult(errors.New("program path is not allowed by allowlist"))
	}

	// Build torrent data map for variable substitution
	torrentData := buildTorrentData(req.Torrent, program.PathMappings)

	// Build command arguments
	args := extargs.BuildArguments(program.ArgsTemplate, torrentData)

	// Build and execute command
	cmd := s.buildCommand(program, args)

	// Log the command being executed
	log.Debug().
		Str("program", program.Name).
		Str("path", program.Path).
		Strs("args", args).
		Str("hash", req.Torrent.Hash).
		Str("full_command", fmt.Sprintf("%v", cmd.Args)).
		Msg("executing external program")

	// Execute in goroutine (fire-and-forget)
	go s.executeAsync(cmd, program, req.Torrent)

	// Log success activity immediately (we've launched the program)
	s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, true, "program launched")

	message := "Program started successfully"
	if program.UseTerminal {
		message = "Terminal window opened successfully"
	}

	return SuccessResult(message)
}

// executeAsync runs the command in a goroutine and handles process lifecycle.
func (s *Service) executeAsync(
	cmd *exec.Cmd,
	program *models.ExternalProgram,
	torrent *qbt.Torrent,
) {
	var execErr error

	if runtime.GOOS == "windows" {
		// Windows: Use Run() which waits for cmd.exe to complete
		// The 'start' command will spawn the process and cmd.exe will exit quickly
		execErr = cmd.Run()
		if execErr != nil {
			log.Debug().
				Err(execErr).
				Str("program", program.Name).
				Str("hash", torrent.Hash).
				Str("command", fmt.Sprintf("%v", cmd.Args)).
				Msg("cmd.exe exited with error (may be normal for 'start' command)")
		}
	} else {
		// Unix/Linux: Start the terminal emulator or direct process
		execErr = cmd.Start()
		if execErr != nil {
			log.Error().
				Err(execErr).
				Str("program", program.Name).
				Str("hash", torrent.Hash).
				Str("command", fmt.Sprintf("%v", cmd.Args)).
				Msg("external program failed to start")
			return
		}

		// Wait for the process to prevent zombie processes
		waitErr := cmd.Wait()
		if waitErr != nil {
			log.Debug().
				Err(waitErr).
				Str("program", program.Name).
				Str("hash", torrent.Hash).
				Str("command", fmt.Sprintf("%v", cmd.Args)).
				Msg("process exited with error (may be normal for terminal emulators)")
		}
	}

	log.Info().
		Str("program", program.Name).
		Str("hash", torrent.Hash).
		Bool("useTerminal", program.UseTerminal).
		Msg("external program execution completed")
}

// buildCommand creates the appropriate exec.Cmd based on platform and settings.
func (s *Service) buildCommand(program *models.ExternalProgram, args []string) *exec.Cmd {
	if program.UseTerminal {
		return s.buildTerminalCommand(program, args)
	}
	return s.buildDirectCommand(program, args)
}

// buildTerminalCommand creates a command that opens in a terminal window.
func (s *Service) buildTerminalCommand(program *models.ExternalProgram, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// Windows: Use cmd.exe /c start cmd /k to open a new visible terminal window
		cmdArgs := []string{"/c", "start", "", "cmd", "/k", program.Path}
		cmdArgs = append(cmdArgs, args...)
		return exec.Command("cmd.exe", cmdArgs...)
	}

	// Unix/Linux: Build command string and spawn in a terminal
	allArgs := append([]string{program.Path}, args...)
	fullCmd := shellquote.Join(allArgs...)
	return s.createTerminalCommand(fullCmd)
}

// buildDirectCommand creates a command that runs directly without a terminal.
func (s *Service) buildDirectCommand(program *models.ExternalProgram, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// Windows: Use 'start' to launch GUI apps properly (detached from parent process)
		cmdArgs := []string{"/c", "start", "", "/b", program.Path}
		cmdArgs = append(cmdArgs, args...)
		return exec.Command("cmd.exe", cmdArgs...)
	}

	// Unix/Linux: Direct execution
	if len(args) > 0 {
		return exec.Command(program.Path, args...)
	}
	return exec.Command(program.Path)
}

// createTerminalCommand creates a command that spawns a terminal window on Unix/Linux.
func (s *Service) createTerminalCommand(cmdLine string) *exec.Cmd {
	// List of terminal emulators to try, in order of preference
	terminals := []struct {
		name string
		args []string
	}{
		{"gnome-terminal", []string{"--", "bash", "-c", cmdLine + "; exec bash"}},
		{"konsole", []string{"--hold", "-e", "bash", "-c", cmdLine}},
		{"xfce4-terminal", []string{"--hold", "-e", "bash", "-c", cmdLine}},
		{"mate-terminal", []string{"-e", "bash", "-c", cmdLine + "; exec bash"}},
		{"xterm", []string{"-hold", "-e", "bash", "-c", cmdLine}},
		{"kitty", []string{"bash", "-c", cmdLine + "; exec bash"}},
		{"alacritty", []string{"-e", "bash", "-c", cmdLine + "; exec bash"}},
		{"terminator", []string{"-e", "bash", "-c", cmdLine + "; exec bash"}},
	}

	// Try each terminal until we find one that exists
	for _, term := range terminals {
		if _, err := exec.LookPath(term.name); err == nil {
			log.Debug().
				Str("terminal", term.name).
				Str("command", cmdLine).
				Msg("using terminal emulator for external program")
			return exec.Command(term.name, term.args...)
		}
	}

	// Fallback: if no terminal emulator found, just run in background
	log.Warn().
		Str("command", cmdLine).
		Msg("no terminal emulator found, running command in background")
	return exec.Command("sh", "-c", cmdLine)
}

// IsPathAllowed checks if the program path is allowed by the allowlist.
func (s *Service) IsPathAllowed(programPath string) bool {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return false
	}

	if s == nil || s.config == nil {
		return true
	}

	allowList := s.config.ExternalProgramAllowList
	if len(allowList) == 0 {
		return true
	}

	normalizedProgramPath := normalizePath(programPath)
	sep := string(os.PathSeparator)

	for _, allowed := range allowList {
		allowed = strings.TrimSpace(allowed)
		if allowed == "" {
			continue
		}

		normalizedAllowedPath := normalizePath(allowed)

		// Exact match
		if normalizedProgramPath == normalizedAllowedPath {
			return true
		}

		// Prefix match with path separator boundary
		allowedPrefix := normalizedAllowedPath
		if !strings.HasSuffix(allowedPrefix, sep) {
			allowedPrefix += sep
		}

		if strings.HasPrefix(normalizedProgramPath, allowedPrefix) {
			return true
		}
	}

	log.Warn().Str("path", programPath).Msg("external program path blocked by allow list")
	return false
}

// buildTorrentData creates a map of torrent data for variable substitution.
func buildTorrentData(torrent *qbt.Torrent, pathMappings []models.PathMapping) map[string]string {
	savePath := extargs.ApplyPathMappings(torrent.SavePath, pathMappings)
	contentPath := extargs.ApplyPathMappings(torrent.ContentPath, pathMappings)

	return map[string]string{
		"hash":         torrent.Hash,
		"name":         torrent.Name,
		"save_path":    savePath,
		"category":     torrent.Category,
		"tags":         torrent.Tags,
		"state":        string(torrent.State),
		"size":         strconv.FormatInt(torrent.Size, 10),
		"progress":     fmt.Sprintf("%.2f", torrent.Progress),
		"content_path": contentPath,
		"comment":      torrent.Comment,
	}
}

// logActivity logs an execution attempt to the activity store.
func (s *Service) logActivity(
	ctx context.Context,
	instanceID int,
	torrent *qbt.Torrent,
	program *models.ExternalProgram,
	ruleID *int,
	ruleName string,
	success bool,
	reason string,
) {
	if s.activityStore == nil {
		return
	}

	outcome := models.ActivityOutcomeSuccess
	if !success {
		outcome = models.ActivityOutcomeFailed
	}

	activity := &models.AutomationActivity{
		InstanceID:  instanceID,
		Hash:        torrent.Hash,
		TorrentName: torrent.Name,
		Action:      ActivityActionExternalProgram,
		RuleID:      ruleID,
		RuleName:    ruleName,
		Outcome:     outcome,
		Reason:      fmt.Sprintf("%s: %s", program.Name, reason),
	}

	if err := s.activityStore.Create(ctx, activity); err != nil {
		log.Warn().Err(err).Msg("failed to log external program activity")
	}
}

// normalizePath normalizes a file path for comparison.
func normalizePath(p string) string {
	cleaned, err := filepath.Abs(p)
	if err != nil {
		cleaned = filepath.Clean(p)
	}

	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	} else {
		dir := filepath.Dir(cleaned)
		if dirResolved, dirErr := filepath.EvalSymlinks(dir); dirErr == nil {
			cleaned = filepath.Join(dirResolved, filepath.Base(cleaned))
		}
	}

	return normalizePathCase(cleaned)
}

// normalizePathCase normalizes path case for the current platform.
func normalizePathCase(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
