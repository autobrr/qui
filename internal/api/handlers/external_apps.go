// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

type ExternalAppsHandler struct {
	syncManager *qbittorrent.SyncManager
}

type TorrentData struct {
	Hash         string  `json:"hash"`
	Name         string  `json:"name"`
	SavePath     string  `json:"save_path"`
	Category     string  `json:"category"`
	Tags         string  `json:"tags"`
	Size         int64   `json:"size"`
	Progress     float64 `json:"progress"`
	DlSpeed      int64   `json:"dlspeed"`
	UpSpeed      int64   `json:"upspeed"`
	Priority     int     `json:"priority"`
	NumSeeds     int     `json:"num_seeds"`
	NumLeechs    int     `json:"num_leechs"`
	Ratio        float64 `json:"ratio"`
	ETA          int64   `json:"eta"`
	State        string  `json:"state"`
	Downloaded   int64   `json:"downloaded"`
	Uploaded     int64   `json:"uploaded"`
	Availability float64 `json:"availability"`
	ForceStart   bool    `json:"force_start"`
	SuperSeeding bool    `json:"super_seeding"`
}

type ExecuteCustomActionRequest struct {
	InstanceID       int         `json:"instanceId"`
	Torrent          TorrentData `json:"torrent"`
	Executable       string      `json:"executable"`
	Arguments        string      `json:"arguments"`
	PathMapping      string      `json:"pathMapping"`
	WorkingDir       string      `json:"workingDir,omitempty"`
	HighPrivileges   bool        `json:"highPrivileges,omitempty"`
	UseCommandLine   bool        `json:"useCommandLine,omitempty"`
	KeepTerminalOpen bool        `json:"keepTerminalOpen,omitempty"`
}

type ExecuteCustomActionResponse struct {
	Success  bool   `json:"success"`
	Message  string `json:"message"`
	Command  string `json:"command"`
	Output   string `json:"output,omitempty"`
	Error    string `json:"error,omitempty"`
	ExitCode int    `json:"exitCode,omitempty"`
}

func NewExternalAppsHandler(syncManager *qbittorrent.SyncManager) *ExternalAppsHandler {
	return &ExternalAppsHandler{
		syncManager: syncManager,
	}
}

// ExecuteCustomAction executes an external program with torrent-specific arguments
func (h *ExternalAppsHandler) ExecuteCustomAction(w http.ResponseWriter, r *http.Request) {
	var req ExecuteCustomActionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode execute custom action request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Log the request for debugging
	log.Info().
		Str("executable", req.Executable).
		Str("torrentHash", req.Torrent.Hash).
		Str("torrentName", req.Torrent.Name).
		Int("instanceId", req.InstanceID).
		Str("arguments", req.Arguments).
		Bool("highPrivileges", req.HighPrivileges).
		Bool("useCommandLine", req.UseCommandLine).
		Bool("keepTerminalOpen", req.KeepTerminalOpen).
		Msg("Received execute custom action request")

	// Validate required fields
	// In direct executable mode, executable is required; in command line mode, it's optional
	if !req.UseCommandLine && req.Executable == "" {
		log.Error().Msg("Executable path is required when not using command line mode")
		http.Error(w, "Executable path is required when not using command line mode", http.StatusBadRequest)
		return
	}

	if req.Torrent.Hash == "" {
		log.Error().Msg("Torrent hash is required")
		http.Error(w, "Torrent hash is required", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
	defer cancel()

	// Use the provided torrent data directly - no need to lookup
	log.Info().
		Str("torrentName", req.Torrent.Name).
		Str("torrentHash", req.Torrent.Hash).
		Msg("Using provided torrent data")

	// Apply path mapping if configured
	finalPath := req.Torrent.SavePath
	if req.PathMapping != "" {
		finalPath = applyPathMapping(finalPath, req.PathMapping)
	}

	// Parse arguments and substitute variables
	args := parseArgumentsFromData(req.Arguments, &req.Torrent, finalPath)

	// Security validation
	if !isExecutableAllowed(req.Executable) {
		log.Error().Str("executable", req.Executable).Msg("Executable not allowed")
		http.Error(w, "Executable not allowed", http.StatusForbidden)
		return
	}

	// Execute the command
	response := h.executeCommand(ctx, req.Executable, args, req.WorkingDir, req.HighPrivileges, req.UseCommandLine, req.KeepTerminalOpen)

	// Log the execution
	log.Info().
		Str("executable", req.Executable).
		Str("torrent", req.Torrent.Name).
		Bool("success", response.Success).
		Msg("Executed custom action")

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(response)
}

// executeCommand executes the external program with proper platform handling
func (h *ExternalAppsHandler) executeCommand(ctx context.Context, executable string, args []string, workingDir string, highPrivileges bool, useCommandLine bool, keepTerminalOpen bool) ExecuteCustomActionResponse {
	var cmd *exec.Cmd

	// Log input parameters for debugging
	log.Info().
		Str("executable", executable).
		Strs("args", args).
		Str("workingDir", workingDir).
		Bool("highPrivileges", highPrivileges).
		Bool("useCommandLine", useCommandLine).
		Bool("keepTerminalOpen", keepTerminalOpen).
		Msg("executeCommand called with parameters")

	// Platform-specific command execution
	switch runtime.GOOS {
	case "windows":
		if useCommandLine {
			// Command line mode: use args directly since executable is hidden in UI
			// In command line mode, the entire command comes through args

			// Use /k to keep terminal open, /c to close after execution
			terminalFlag := "/c"
			if keepTerminalOpen {
				terminalFlag = "/k"
			}

			if highPrivileges {
				// Use high privileges with command line
				cmdArgs := []string{"/c", "start", "/high", "cmd", terminalFlag}
				cmdArgs = append(cmdArgs, args...)
				cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...)
			} else {
				// Normal command line execution
				cmdArgs := []string{"/c", "start", "cmd", terminalFlag}
				cmdArgs = append(cmdArgs, args...)
				cmd = exec.CommandContext(ctx, "cmd.exe", cmdArgs...)
			}
		} else {
			// Direct executable mode: use the executable path directly
			if highPrivileges {
				// For direct executables with high privileges, wrap with start /high
				var cmdParts []string

				// Quote the executable if it contains spaces
				quotedExecutable := executable
				if strings.Contains(executable, " ") && !strings.HasPrefix(executable, "\"") {
					quotedExecutable = fmt.Sprintf("\"%s\"", executable)
				}
				cmdParts = append(cmdParts, quotedExecutable)

				// Quote arguments that contain spaces
				for _, arg := range args {
					if strings.Contains(arg, " ") && !strings.HasPrefix(arg, "\"") {
						cmdParts = append(cmdParts, fmt.Sprintf("\"%s\"", arg))
					} else {
						cmdParts = append(cmdParts, arg)
					}
				}

				// Build the full command string with high privileges
				cmdStr := strings.Join(cmdParts, " ")
				fullCmd := fmt.Sprintf("start /high %s", cmdStr)
				cmd = exec.CommandContext(ctx, "cmd.exe", "/c", fullCmd)
			} else {
				// Normal direct executable execution
				// Handle .exe extension for Windows executables
				if !strings.HasSuffix(strings.ToLower(executable), ".exe") &&
					!strings.Contains(executable, " ") &&
					!strings.Contains(executable, "\"") {
					// Check if it's a system command
					if _, err := exec.LookPath(executable); err != nil {
						// Try adding .exe extension
						executable = executable + ".exe"
					}
				}
				cmd = exec.CommandContext(ctx, executable, args...)
			}
		}
	case "darwin", "linux":
		// On Unix-like systems, check if executable exists and is executable
		if _, err := os.Stat(executable); os.IsNotExist(err) {
			return ExecuteCustomActionResponse{
				Success: false,
				Error:   fmt.Sprintf("Executable not found: %s", executable),
			}
		}
		cmd = exec.CommandContext(ctx, executable, args...)
	default:
		return ExecuteCustomActionResponse{
			Success: false,
			Error:   fmt.Sprintf("Unsupported platform: %s", runtime.GOOS),
		}
	}

	// Set working directory if specified
	if workingDir != "" {
		cmd.Dir = workingDir
	}

	// Build command string for logging
	var commandStr string
	if runtime.GOOS == "windows" {
		if useCommandLine {
			// Command line mode logging
			terminalFlag := "/c"
			if keepTerminalOpen {
				terminalFlag = "/k"
			}

			if highPrivileges {
				commandStr = fmt.Sprintf("cmd.exe /c start /high cmd %s %s", terminalFlag, strings.Join(args, " "))
			} else {
				commandStr = fmt.Sprintf("cmd.exe /c start cmd %s %s", terminalFlag, strings.Join(args, " "))
			}
		} else {
			// Direct executable mode logging
			if highPrivileges {
				var cmdParts []string
				quotedExecutable := executable
				if strings.Contains(executable, " ") && !strings.HasPrefix(executable, "\"") {
					quotedExecutable = fmt.Sprintf("\"%s\"", executable)
				}
				cmdParts = append(cmdParts, quotedExecutable)

				for _, arg := range args {
					if strings.Contains(arg, " ") && !strings.HasPrefix(arg, "\"") {
						cmdParts = append(cmdParts, fmt.Sprintf("\"%s\"", arg))
					} else {
						cmdParts = append(cmdParts, arg)
					}
				}
				cmdStr := strings.Join(cmdParts, " ")
				commandStr = fmt.Sprintf("cmd.exe /c start /high %s", cmdStr)
			} else {
				commandStr = executable + " " + strings.Join(args, " ")
			}
		}
	} else {
		commandStr = executable + " " + strings.Join(args, " ")
	}

	// Log the command being executed for debugging
	log.Info().
		Str("commandStr", commandStr).
		Strs("args", args).
		Str("workingDir", workingDir).
		Msg("Executing command")

	// Execute with timeout
	output, err := cmd.CombinedOutput()
	if err != nil {
		var exitCode int
		if exitError, ok := err.(*exec.ExitError); ok {
			exitCode = exitError.ExitCode()
		}

		log.Error().
			Err(err).
			Str("command", commandStr).
			Str("output", string(output)).
			Int("exitCode", exitCode).
			Msg("Command execution failed")

		return ExecuteCustomActionResponse{
			Success:  false,
			Command:  commandStr,
			Output:   string(output),
			Error:    err.Error(),
			ExitCode: exitCode,
		}
	}

	return ExecuteCustomActionResponse{
		Success: true,
		Command: commandStr,
		Output:  string(output),
		Message: "Command executed successfully",
	}
}

// applyPathMapping converts server paths to local paths using the mapping configuration
func applyPathMapping(serverPath, pathMapping string) string {
	if pathMapping == "" {
		return serverPath
	}

	parts := strings.SplitN(pathMapping, ":", 2)
	if len(parts) != 2 {
		return serverPath
	}

	serverPrefix := parts[0]
	localPrefix := parts[1]

	// Replace {pathSeparator} with OS-appropriate separator
	var pathSeparator string
	if runtime.GOOS == "windows" {
		pathSeparator = "\\"
	} else {
		pathSeparator = "/"
	}
	localPrefix = strings.ReplaceAll(localPrefix, "{pathSeparator}", pathSeparator)

	// Apply the mapping
	if strings.HasPrefix(serverPath, serverPrefix) {
		return strings.Replace(serverPath, serverPrefix, localPrefix, 1)
	}

	return serverPath
}

// parseArgumentsFromData processes the argument string and replaces variables with torrent data from TorrentData struct
func parseArgumentsFromData(arguments string, torrent *TorrentData, finalPath string) []string {
	if arguments == "" {
		return []string{finalPath}
	}

	// Replace torrent variables
	processed := arguments
	processed = strings.ReplaceAll(processed, "{torrent.hash}", torrent.Hash)
	processed = strings.ReplaceAll(processed, "{torrent.name}", torrent.Name)
	processed = strings.ReplaceAll(processed, "{torrent.save_path}", torrent.SavePath)
	processed = strings.ReplaceAll(processed, "{torrent.category}", torrent.Category)
	processed = strings.ReplaceAll(processed, "{torrent.tags}", torrent.Tags)
	processed = strings.ReplaceAll(processed, "{torrent.size}", fmt.Sprintf("%d", torrent.Size))
	processed = strings.ReplaceAll(processed, "{torrent.progress}", fmt.Sprintf("%.3f", torrent.Progress))
	processed = strings.ReplaceAll(processed, "{torrent.dlspeed}", fmt.Sprintf("%d", torrent.DlSpeed))
	processed = strings.ReplaceAll(processed, "{torrent.upspeed}", fmt.Sprintf("%d", torrent.UpSpeed))
	processed = strings.ReplaceAll(processed, "{torrent.priority}", fmt.Sprintf("%d", torrent.Priority))
	processed = strings.ReplaceAll(processed, "{torrent.num_seeds}", fmt.Sprintf("%d", torrent.NumSeeds))
	processed = strings.ReplaceAll(processed, "{torrent.num_leechs}", fmt.Sprintf("%d", torrent.NumLeechs))
	processed = strings.ReplaceAll(processed, "{torrent.ratio}", fmt.Sprintf("%.3f", torrent.Ratio))
	processed = strings.ReplaceAll(processed, "{torrent.eta}", fmt.Sprintf("%d", torrent.ETA))
	processed = strings.ReplaceAll(processed, "{torrent.state}", torrent.State)
	processed = strings.ReplaceAll(processed, "{torrent.downloaded}", fmt.Sprintf("%d", torrent.Downloaded))
	processed = strings.ReplaceAll(processed, "{torrent.uploaded}", fmt.Sprintf("%d", torrent.Uploaded))
	processed = strings.ReplaceAll(processed, "{torrent.availability}", fmt.Sprintf("%.3f", torrent.Availability))
	processed = strings.ReplaceAll(processed, "{torrent.force_start}", fmt.Sprintf("%t", torrent.ForceStart))
	processed = strings.ReplaceAll(processed, "{torrent.super_seeding}", fmt.Sprintf("%t", torrent.SuperSeeding))

	// Split arguments respecting quotes
	args := splitArguments(processed)

	// Add final path if not already included
	pathIncluded := false
	for _, arg := range args {
		if strings.Contains(arg, finalPath) {
			pathIncluded = true
			break
		}
	}

	if !pathIncluded {
		args = append(args, finalPath)
	}

	return args
}

// splitArguments splits a command line string into arguments, respecting quotes
func splitArguments(s string) []string {
	var args []string
	var current strings.Builder
	inQuotes := false
	quoteChar := byte(0)

	for i := 0; i < len(s); i++ {
		c := s[i]

		switch c {
		case '"', '\'':
			if !inQuotes {
				inQuotes = true
				quoteChar = c
			} else if c == quoteChar {
				inQuotes = false
				quoteChar = 0
			} else {
				current.WriteByte(c)
			}
		case ' ', '\t':
			if inQuotes {
				current.WriteByte(c)
			} else if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(c)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// isExecutableAllowed checks if the executable is allowed to run (basic security)
func isExecutableAllowed(executable string) bool {
	// On Windows, check if it's a built-in command first
	if runtime.GOOS == "windows" {
		builtinCommands := []string{"cmd", "cmd.exe", "start", "dir", "copy", "move", "del", "type", "echo", "set"}
		execLower := strings.ToLower(executable)
		for _, builtin := range builtinCommands {
			if execLower == builtin {
				return true // Built-in commands are allowed
			}
		}
	}

	// Basic security: check if executable exists and is not in system directories
	absPath, err := filepath.Abs(executable)
	if err != nil {
		return false
	}

	// Check if file exists
	if _, err := os.Stat(absPath); os.IsNotExist(err) {
		// On Windows, try with .exe extension
		if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(executable), ".exe") {
			if _, err := os.Stat(absPath + ".exe"); os.IsNotExist(err) {
				// Check if it's in PATH
				if _, err := exec.LookPath(executable); err != nil {
					return false
				}
			}
		} else {
			// Check if it's in PATH
			if _, err := exec.LookPath(executable); err != nil {
				return false
			}
		}
	}

	// Platform-specific security checks
	switch runtime.GOOS {
	case "windows":
		// Prevent execution of system-critical executables
		forbidden := []string{
			"powershell.exe", "pwsh.exe", "wscript.exe", "cscript.exe",
			"mshta.exe", "rundll32.exe", "regsvr32.exe", "sc.exe", "schtasks.exe",
			"net.exe", "netsh.exe", "cacls.exe", "icacls.exe",
		}
		execName := strings.ToLower(filepath.Base(absPath))
		for _, f := range forbidden {
			if execName == f {
				return false
			}
		}
	case "darwin", "linux":
		// Prevent execution of system-critical executables
		forbidden := []string{
			"rm", "rmdir", "sudo", "su", "chmod", "chown", "mount", "umount",
			"systemctl", "service", "passwd", "adduser", "deluser", "crontab",
		}
		execName := filepath.Base(absPath)
		for _, f := range forbidden {
			if execName == f {
				return false
			}
		}
	}

	return true
}
