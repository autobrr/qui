// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

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
	"sync"
	"sync/atomic"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	extargs "github.com/autobrr/qui/internal/externalprograms"
	"github.com/autobrr/qui/internal/models"
)

const ActivityActionExternalProgram = "external_program"

const (
	defaultMaxRunningPrograms = 8
	maxWaitingPrograms        = 1000
	programWaitTimeout        = 30 * time.Minute
)

var (
	errExecutionQueueFull = errors.New("not started: execution queue full")
	errExecutionLimitWait = errors.New("not started: execution limit reached")
	errHelperTorrent      = errors.New("not started: qui added this torrent for a Transfer or a Share")
)

type Service struct {
	programStore  *models.ExternalProgramStore
	activityStore *models.AutomationActivityStore
	config        *domain.Config

	slots       chan struct{}
	admitted    atomic.Int32
	waiting     sync.Map
	maxWaiting  int32
	waitTimeout time.Duration

	helpers helperTorrents
}

type helperTorrents interface {
	IsHelper(instanceID int, hash string) bool
}

func (s *Service) SetHelperTorrents(helpers helperTorrents) {
	s.helpers = helpers
}

func NewService(
	programStore *models.ExternalProgramStore,
	activityStore *models.AutomationActivityStore,
	config *domain.Config,
) *Service {
	maxRunning := defaultMaxRunningPrograms
	if config != nil && config.ExternalProgramMaxRunning > 0 {
		maxRunning = config.ExternalProgramMaxRunning
	}
	return &Service{
		programStore:  programStore,
		activityStore: activityStore,
		config:        config,
		slots:         make(chan struct{}, maxRunning),
		maxWaiting:    maxWaitingPrograms,
		waitTimeout:   programWaitTimeout,
	}
}

type waitKey struct {
	programID  int
	instanceID int
	hash       string
}

type executionPolicy struct {
	releaseOnStart  bool
	reportAfterExit bool
}

type ExecuteRequest struct {
	ProgramID int
	Program   *models.ExternalProgram

	Torrent    *qbt.Torrent
	InstanceID int

	RuleID   *int
	RuleName string
}

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

type ExecuteResult struct {
	Success bool
	Message string
	Error   error
}

func SuccessResult(message string) ExecuteResult {
	return ExecuteResult{Success: true, Message: message}
}

func FailureResult(err error) ExecuteResult {
	return ExecuteResult{Success: false, Error: err}
}

func (s *Service) Execute(ctx context.Context, req ExecuteRequest) ExecuteResult {
	if err := req.Validate(); err != nil {
		return FailureResult(err)
	}
	if s != nil && s.helpers != nil && s.helpers.IsHelper(req.InstanceID, req.Torrent.Hash) {
		return FailureResult(errHelperTorrent)
	}

	program := req.Program

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

func (s *Service) executeProgram(ctx context.Context, program *models.ExternalProgram, req ExecuteRequest) ExecuteResult {
	if program == nil {
		return FailureResult(errors.New("program is nil"))
	}

	if req.Torrent == nil {
		return FailureResult(errors.New("torrent is nil"))
	}

	if !program.Enabled {
		log.Debug().
			Int("programId", program.ID).
			Str("programName", program.Name).
			Msg("external program is disabled, skipping execution")
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, "program is disabled")
		return FailureResult(errors.New("program is disabled"))
	}

	if !s.IsPathAllowed(program.Path) {
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, "path not allowed by allowlist")
		return FailureResult(errors.New("program path is not allowed by allowlist"))
	}

	torrentData := buildTorrentData(req.Torrent, program.PathMappings)

	args := extargs.BuildArguments(program.ArgsTemplate, torrentData)

	cmd, policy := s.buildCommand(context.Background(), program, args)

	key := waitKey{programID: program.ID, instanceID: req.InstanceID, hash: req.Torrent.Hash}
	if _, waiting := s.waiting.LoadOrStore(key, struct{}{}); waiting {
		log.Debug().
			Str("program", program.Name).
			Str("hash", req.Torrent.Hash).
			Msg("external program already waits for this torrent, skipping")
		return SuccessResult("Program already waiting for this torrent")
	}

	if s.admitted.Add(1) > int32(cap(s.slots))+s.maxWaiting {
		s.admitted.Add(-1)
		s.waiting.Delete(key)
		log.Warn().
			Str("program", program.Name).
			Str("hash", req.Torrent.Hash).
			Int("maxRunning", cap(s.slots)).
			Msg("external program not started: execution queue full")
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, errExecutionQueueFull.Error())
		return FailureResult(errExecutionQueueFull)
	}

	log.Debug().
		Str("program", program.Name).
		Str("path", program.Path).
		Strs("args", args).
		Str("hash", req.Torrent.Hash).
		Str("full_command", fmt.Sprintf("%v", cmd.Args)).
		Msg("executing external program")

	go s.executeAsync(cmd, policy, key, program, req, time.Now().Add(s.waitTimeout)) //nolint:gosec // G118: external program runs past the request that queued it

	message := "Program execution initiated"
	if program.UseTerminal {
		message = "Terminal window execution initiated"
	}

	return SuccessResult(message)
}

func (s *Service) executeAsync(
	cmd *exec.Cmd,
	policy executionPolicy,
	key waitKey,
	program *models.ExternalProgram,
	req ExecuteRequest,
	deadline time.Time,
) {
	ctx := context.Background()

	select {
	case s.slots <- struct{}{}:
		s.waiting.Delete(key)
	case <-time.After(time.Until(deadline)):
		s.admitted.Add(-1)
		s.waiting.Delete(key)
		log.Warn().
			Str("program", program.Name).
			Str("hash", req.Torrent.Hash).
			Int("maxRunning", cap(s.slots)).
			Msg("external program not started: execution limit reached")
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, errExecutionLimitWait.Error())
		return
	}
	release := sync.OnceFunc(func() {
		<-s.slots
		s.admitted.Add(-1)
	})
	defer release()

	execErr := cmd.Start()
	if execErr != nil {
		log.Error().
			Err(execErr).
			Str("program", program.Name).
			Str("hash", req.Torrent.Hash).
			Str("command", fmt.Sprintf("%v", cmd.Args)).
			Msg("external program failed to start")
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, fmt.Sprintf("failed to start: %v", execErr))
		return
	}

	if policy.releaseOnStart {
		release()
	}

	if !policy.reportAfterExit {
		s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, true, "program started")
	}

	waitErr := cmd.Wait()
	if policy.reportAfterExit {
		if waitErr != nil {
			s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, false, fmt.Sprintf("program failed: %v", waitErr))
		} else {
			s.logActivity(ctx, req.InstanceID, req.Torrent, program, req.RuleID, req.RuleName, true, "program started")
		}
	}
	if waitErr != nil {
		log.Warn().
			Err(waitErr).
			Str("program", program.Name).
			Str("hash", req.Torrent.Hash).
			Str("command", fmt.Sprintf("%v", cmd.Args)).
			Msg("process exited with error (may be normal for terminal emulators)")
	}

	log.Info().
		Str("program", program.Name).
		Str("hash", req.Torrent.Hash).
		Bool("useTerminal", program.UseTerminal).
		Msg("external program execution completed")
}

func (s *Service) buildCommand(ctx context.Context, program *models.ExternalProgram, args []string) (*exec.Cmd, executionPolicy) {
	if program.UseTerminal {
		return s.buildTerminalCommand(ctx, program, args)
	}
	return s.buildDirectCommand(ctx, program, args)
}

func shellJoin(args []string) string {
	quoted := make([]string, len(args))
	for i, arg := range args {
		quoted[i] = "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
	}
	return strings.Join(quoted, " ")
}

type terminalCandidate struct {
	name string
}

func getTerminalCandidates() []terminalCandidate {
	candidates := []terminalCandidate{
		{"wezterm"},
		{"hyper"},
		{"kitty"},
		{"alacritty"},
	}

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

	if runtime.GOOS == "darwin" {
		candidates = append(candidates,
			terminalCandidate{"iterm2"},
			terminalCandidate{"apple-terminal"},
		)
	}

	return candidates
}

func escapeAppleScript(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "\"", "\\\"")
	s = strings.ReplaceAll(s, "\n", "\\n")
	s = strings.ReplaceAll(s, "\r", "\\r")
	s = strings.ReplaceAll(s, "\t", "\\t")
	return s
}

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

func buildTerminalArgs(terminal, cmdLine string) (string, []string) {
	keepOpen := cmdLine + "; exec bash"

	switch terminal {
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

	case "wezterm":
		return "wezterm", []string{"start", "--", "bash", "-c", keepOpen}
	case "hyper":
		return "hyper", []string{"-e", "bash", "-c", keepOpen}
	case "kitty":
		return "kitty", []string{"bash", "-c", keepOpen}
	case "alacritty":
		return "alacritty", []string{"-e", "bash", "-c", keepOpen}

	case "gnome-terminal":
		return "gnome-terminal", []string{"--", "bash", "-c", keepOpen}
	case "konsole":
		return "konsole", []string{"--hold", "-e", "bash", "-c", cmdLine}
	case "xfce4-terminal":
		return "xfce4-terminal", []string{"--hold", "-e", "bash", "-c", cmdLine}
	case "mate-terminal":
		return "mate-terminal", []string{"-e", "bash", "-c", keepOpen}
	case "xterm":
		return "xterm", []string{"-hold", "-e", "bash", "-c", cmdLine}
	case "terminator":
		return "terminator", []string{"-e", "bash", "-c", keepOpen}

	default:
		return "", nil
	}
}

func isTerminalAvailable(terminal string) bool {
	switch terminal {
	case "iterm2":
		_, err := os.Stat("/Applications/iTerm.app")
		return err == nil
	case "apple-terminal":
		return runtime.GOOS == "darwin"
	default:
		_, err := exec.LookPath(terminal)
		return err == nil
	}
}

func (s *Service) createTerminalCommand(ctx context.Context, cmdLine string) (cmd *exec.Cmd, launcher bool) {
	if terminal, found := detectTerminalFromEnv(); found {
		if isTerminalAvailable(terminal) {
			cmdName, args := buildTerminalArgs(terminal, cmdLine)
			if cmdName != "" {
				log.Debug().
					Str("terminal", terminal).
					Str("source", "TERM_PROGRAM").
					Str("command", cmdLine).
					Msg("using terminal emulator for external program")
				return exec.CommandContext(ctx, cmdName, args...), true
			}
		}
	}

	for _, tc := range getTerminalCandidates() {
		if isTerminalAvailable(tc.name) {
			cmdName, args := buildTerminalArgs(tc.name, cmdLine)
			if cmdName != "" {
				log.Debug().
					Str("terminal", tc.name).
					Str("source", "detection").
					Str("command", cmdLine).
					Msg("using terminal emulator for external program")
				return exec.CommandContext(ctx, cmdName, args...), true
			}
		}
	}

	log.Warn().
		Str("command", cmdLine).
		Msg("no terminal emulator found, running command in background")
	return exec.CommandContext(ctx, "sh", "-c", cmdLine), false
}

func (s *Service) IsPathAllowed(programPath string) bool {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return false
	}

	if s == nil || s.config == nil {
		return true
	}

	allowList := s.config.Live().ExternalProgramAllowList
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

		if normalizedProgramPath == normalizedAllowedPath {
			return true
		}

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

	details, _ := json.Marshal(map[string]string{ //nolint:errchkjson // marshaling map[string]string cannot fail
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

func normalizePathCase(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
