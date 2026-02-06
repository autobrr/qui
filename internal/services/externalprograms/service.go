// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package externalprograms provides a unified service for executing external programs
// with torrent data. It is used by automations, cross-seed, and the API handler.
package externalprograms

import (
	"context"
	"encoding/json"
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
//
// WARNING: This function spawns processes without any rate limiting or process count limits.
// Callers should be aware that rapid invocations (e.g., from automations matching many torrents)
// can spawn a large number of concurrent processes. If the external program runs indefinitely
// or takes a long time to complete, this can exhaust system resources. Consider implementing
// caller-side throttling or ensuring the external programs exit promptly.
func (s *Service) Execute(ctx context.Context, req ExecuteRequest) ExecuteResult {
	// Validate request first
	if err := req.Validate(); err != nil {
		return FailureResult(err)
	}

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
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, "program is disabled")
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
	// Use background context since the command runs async and parent context may be cancelled
	cmd := s.buildCommand(context.Background(), program, args)

	// Log the command being executed
	log.Debug().
		Str("program", program.Name).
		Str("path", program.Path).
		Strs("args", args).
		Str("hash", req.Torrent.Hash).
		Str("full_command", fmt.Sprintf("%v", cmd.Args)).
		Msg("executing external program")

	// Execute in goroutine (fire-and-forget)
	// Activity logging happens inside executeAsync after cmd.Start() succeeds
	go s.executeAsync(cmd, program, req)

	message := "Program execution initiated"
	if program.UseTerminal {
		message = "Terminal window execution initiated"
	}

	return SuccessResult(message)
}

// executeAsync runs the command in a goroutine and handles process lifecycle.
// Activity logging happens here after the command actually starts successfully.
func (s *Service) executeAsync(
	cmd *exec.Cmd,
	program *models.ExternalProgram,
	req ExecuteRequest,
) {
	// Use background context for activity logging since parent context may be cancelled
	ctx := context.Background()

	if runtime.GOOS == "windows" {
		// Windows: Use Run() which waits for cmd.exe to complete
		// The 'start' command will spawn the process and cmd.exe will exit quickly
		execErr := cmd.Run()
		if execErr != nil {
			log.Error().
				Err(execErr).
				Str("program", program.Name).
				Str("hash", req.Torrent.Hash).
				Str("command", fmt.Sprintf("%v", cmd.Args)).
				Msg("external program failed to start")
			s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, fmt.Sprintf("program failed to start: %v", execErr))
			return
		}
		// Log success - on Windows, Run() completing without error means the program started
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, true, "program started")
	} else {
		// Unix/Linux: Start the terminal emulator or direct process
		execErr := cmd.Start()
		if execErr != nil {
			log.Error().
				Err(execErr).
				Str("program", program.Name).
				Str("hash", req.Torrent.Hash).
				Str("command", fmt.Sprintf("%v", cmd.Args)).
				Msg("external program failed to start")
			// Log failure activity
			s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, fmt.Sprintf("failed to start: %v", execErr))
			return
		}

		// Log success - the program has actually started
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, true, "program started")

		// Wait for the process to prevent zombie processes
		waitErr := cmd.Wait()
		if waitErr != nil {
			log.Warn().
				Err(waitErr).
				Str("program", program.Name).
				Str("hash", req.Torrent.Hash).
				Str("command", fmt.Sprintf("%v", cmd.Args)).
				Msg("process exited with error (may be normal for terminal emulators)")
		}
	}

	log.Info().
		Str("program", program.Name).
		Str("hash", req.Torrent.Hash).
		Bool("useTerminal", program.UseTerminal).
		Msg("external program execution completed")
}

// buildCommand creates the appropriate exec.Cmd based on platform and settings.
func (s *Service) buildCommand(ctx context.Context, program *models.ExternalProgram, args []string) *exec.Cmd {
	if program.UseTerminal {
		return s.buildTerminalCommand(ctx, program, args)
	}
	return s.buildDirectCommand(ctx, program, args)
}

// buildTerminalCommand creates a command that opens in a terminal window.
func (s *Service) buildTerminalCommand(ctx context.Context, program *models.ExternalProgram, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// Windows: Use cmd.exe /c start cmd /k to open a new visible terminal window
		cmdArgs := make([]string, 0, 6+len(args))
		cmdArgs = append(cmdArgs, "/c", "start", "", "cmd", "/k", program.Path)
		cmdArgs = append(cmdArgs, args...)
		return exec.CommandContext(ctx, "cmd.exe", cmdArgs...) //nolint:gosec // intentional external program execution
	}

	// Unix/Linux: Build command string and spawn in a terminal
	allArgs := append([]string{program.Path}, args...)
	fullCmd := shellquote.Join(allArgs...)
	return s.createTerminalCommand(ctx, fullCmd)
}

// buildDirectCommand creates a command that runs directly without a terminal.
func (s *Service) buildDirectCommand(ctx context.Context, program *models.ExternalProgram, args []string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		// Windows: Use 'start' to launch GUI apps properly (detached from parent process)
		cmdArgs := make([]string, 0, 5+len(args))
		cmdArgs = append(cmdArgs, "/c", "start", "", "/b", program.Path)
		cmdArgs = append(cmdArgs, args...)
		return exec.CommandContext(ctx, "cmd.exe", cmdArgs...) //nolint:gosec // intentional external program execution
	}

	// Unix/Linux: Direct execution
	if len(args) > 0 {
		return exec.CommandContext(ctx, program.Path, args...) //nolint:gosec // intentional external program execution
	}
	return exec.CommandContext(ctx, program.Path) //nolint:gosec // intentional external program execution
}

// terminalCandidate represents a terminal emulator to check for availability.
type terminalCandidate struct {
	name string
}

// getTerminalCandidates returns terminal emulators in priority order for the current platform.
// Priority: cross-platform CLI terminals → Linux terminals → macOS native terminals.
func getTerminalCandidates() []terminalCandidate {
	// Cross-platform terminals (highest priority, checked on all platforms)
	candidates := []terminalCandidate{
		{"wezterm"},
		{"hyper"},
		{"kitty"},
		{"alacritty"},
	}

	// Linux terminal emulators (not checked on macOS)
	if runtime.GOOS != "darwin" {
		candidates = append(candidates,
			terminalCandidate{"gnome-terminal"},
			terminalCandidate{"konsole"},
			terminalCandidate{"xfce4-terminal"},
			terminalCandidate{"mate-terminal"},
			terminalCandidate{"xterm"},
			terminalCandidate{"terminator"},
		)
	}

	// macOS native terminals (lower priority than CLI terminals)
	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			terminalCandidate{"iterm2"},
			terminalCandidate{"apple-terminal"},
		)
	}

	return candidates
}

// escapeAppleScript escapes a string for use in AppleScript.
// AppleScript requires backslashes, double quotes, and control characters to be escaped.
func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

// detectTerminalFromEnv checks the TERM_PROGRAM environment variable to detect the current terminal.
// Returns the terminal name and true if a known terminal is detected, empty string and false otherwise.
func detectTerminalFromEnv() (string, bool) {
	termProgram := os.Getenv("TERM_PROGRAM")
	if termProgram == "" {
		return "", false
	}

	switch termProgram {
	case "iTerm.app":
		return "iterm2", true
	case "Apple_Terminal":
		return "apple-terminal", true
	case "WezTerm":
		return "wezterm", true
	case "Hyper":
		return "hyper", true
	case "kitty":
		return "kitty", true
	case "alacritty":
		return "alacritty", true
	default:
		return "", false
	}
}

// buildTerminalArgs constructs the command name and arguments for a specific terminal.
// Returns the executable name and arguments to spawn a terminal with the given command.
func buildTerminalArgs(terminal, cmdLine string) (string, []string) {
	// Command suffix to keep terminal open after execution
	keepOpen := cmdLine + "; exec bash"

	switch terminal {
	// macOS native terminals (use osascript/AppleScript)
	case "iterm2":
		script := fmt.Sprintf(`tell application "iTerm"
	create window with default profile
	tell current session of current window
		write text "%s"
	end tell
end tell`, escapeAppleScript(keepOpen))
		return "osascript", []string{"-e", script}

	case "apple-terminal":
		script := fmt.Sprintf(`tell application "Terminal"
	do script "%s"
	activate
end tell`, escapeAppleScript(keepOpen))
		return "osascript", []string{"-e", script}

	// Cross-platform terminals
	case "wezterm":
		return "wezterm", []string{"start", "--", "bash", "-c", keepOpen}
	case "hyper":
		return "hyper", []string{"-e", "bash", "-c", keepOpen}
	case "kitty":
		return "kitty", []string{"bash", "-c", keepOpen}
	case "alacritty":
		return "alacritty", []string{"-e", "bash", "-c", keepOpen}

	// Linux terminal emulators
	case "gnome-terminal":
		return "gnome-terminal", []string{"--", "bash", "-c", keepOpen}
	case "konsole":
		// konsole uses --hold flag to keep window open, so no need for "; exec bash"
		return "konsole", []string{"--hold", "-e", "bash", "-c", cmdLine}
	case "xfce4-terminal":
		// xfce4-terminal uses --hold flag to keep window open, so no need for "; exec bash"
		return "xfce4-terminal", []string{"--hold", "-e", "bash", "-c", cmdLine}
	case "mate-terminal":
		return "mate-terminal", []string{"-e", "bash", "-c", keepOpen}
	case "xterm":
		// xterm uses -hold flag to keep window open, so no need for "; exec bash"
		return "xterm", []string{"-hold", "-e", "bash", "-c", cmdLine}
	case "terminator":
		return "terminator", []string{"-e", "bash", "-c", keepOpen}

	default:
		return "", nil
	}
}

// isTerminalAvailable checks if a terminal is available on the system.
func isTerminalAvailable(terminal string) bool {
	switch terminal {
	case "iterm2":
		// Check if iTerm2 app bundle exists
		_, err := os.Stat("/Applications/iTerm.app")
		return err == nil
	case "apple-terminal":
		// Terminal.app is always available on macOS
		return runtime.GOOS == "darwin"
	default:
		// Check if executable is in PATH
		_, err := exec.LookPath(terminal)
		return err == nil
	}
}

// createTerminalCommand creates a command that spawns a terminal window on Unix/Linux/macOS.
func (s *Service) createTerminalCommand(ctx context.Context, cmdLine string) *exec.Cmd {
	// Priority 1: Check TERM_PROGRAM env var (user's current terminal)
	if terminal, found := detectTerminalFromEnv(); found {
		if isTerminalAvailable(terminal) {
			cmdName, args := buildTerminalArgs(terminal, cmdLine)
			if cmdName != "" {
				log.Debug().
					Str("terminal", terminal).
					Str("source", "TERM_PROGRAM").
					Str("command", cmdLine).
					Msg("using terminal emulator for external program")
				return exec.CommandContext(ctx, cmdName, args...) //nolint:gosec // intentional external program execution
			}
		}
	}

	// Priority 2: Try terminal candidates in order
	for _, tc := range getTerminalCandidates() {
		if isTerminalAvailable(tc.name) {
			cmdName, args := buildTerminalArgs(tc.name, cmdLine)
			if cmdName != "" {
				log.Debug().
					Str("terminal", tc.name).
					Str("source", "detection").
					Str("command", cmdLine).
					Msg("using terminal emulator for external program")
				return exec.CommandContext(ctx, cmdName, args...) //nolint:gosec // intentional external program execution
			}
		}
	}

	// Fallback: if no terminal emulator found, just run in background
	log.Warn().
		Str("command", cmdLine).
		Msg("no terminal emulator found, running command in background")
	return exec.CommandContext(ctx, "sh", "-c", cmdLine) //nolint:gosec // intentional external program execution
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

	details, _ := json.Marshal(map[string]string{
		"programName": program.Name,
	})

	activity := &models.AutomationActivity{
		InstanceID:  instanceID,
		Hash:        torrent.Hash,
		TorrentName: torrent.Name,
		Action:      ActivityActionExternalProgram,
		RuleID:      ruleID,
		RuleName:    ruleName,
		Outcome:     outcome,
		Reason:      fmt.Sprintf("%s: %s", program.Name, reason),
		Details:     details,
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
