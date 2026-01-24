// Copyright (c) 2025-2026, s0up and the autobrr contributors.
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
	"strconv"
	"strings"

	shellquote "github.com/Hellseher/go-shellquote"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/go-chi/chi/v5"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/externalprograms"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type ExternalProgramsHandler struct {
	externalProgramStore *models.ExternalProgramStore
	clientPool           *qbittorrent.ClientPool
	config               *domain.Config
}

func NewExternalProgramsHandler(store *models.ExternalProgramStore, pool *qbittorrent.ClientPool, cfg *domain.Config) *ExternalProgramsHandler {
	return &ExternalProgramsHandler{
		externalProgramStore: store,
		clientPool:           pool,
		config:               cfg,
	}
}

// ListExternalPrograms handles GET /api/external-programs
func (h *ExternalProgramsHandler) ListExternalPrograms(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()

	programs, err := h.externalProgramStore.List(ctx)
	if err != nil {
		log.Error().Err(err).Msg("Failed to list external programs")
		http.Error(w, "Failed to list external programs", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(programs)
}

// CreateExternalProgram handles POST /api/external-programs
func (h *ExternalProgramsHandler) CreateExternalProgram(w http.ResponseWriter, r *http.Request) {
	var req models.ExternalProgramCreate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode create external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	if !h.isPathAllowed(req.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	program, err := h.externalProgramStore.Create(ctx, &req)
	if err != nil {
		log.Error().Err(err).Msg("Failed to create external program")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			http.Error(w, "A program with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to create external program", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	json.NewEncoder(w).Encode(program)
}

// UpdateExternalProgram handles PUT /api/external-programs/{id}
func (h *ExternalProgramsHandler) UpdateExternalProgram(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		http.Error(w, "Missing program ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid program ID", http.StatusBadRequest)
		return
	}

	var req models.ExternalProgramUpdate
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode update external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.Name == "" {
		http.Error(w, "Name is required", http.StatusBadRequest)
		return
	}

	req.Path = strings.TrimSpace(req.Path)
	if req.Path == "" {
		http.Error(w, "Path is required", http.StatusBadRequest)
		return
	}

	if !h.isPathAllowed(req.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
		return
	}

	ctx := r.Context()
	program, err := h.externalProgramStore.Update(ctx, id, &req)
	if err != nil {
		if err == models.ErrExternalProgramNotFound {
			http.Error(w, "Program not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to update external program")
		if strings.Contains(err.Error(), "UNIQUE constraint failed") {
			http.Error(w, "A program with this name already exists", http.StatusConflict)
			return
		}
		http.Error(w, "Failed to update external program", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(program)
}

// DeleteExternalProgram handles DELETE /api/external-programs/{id}
func (h *ExternalProgramsHandler) DeleteExternalProgram(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	if idStr == "" {
		http.Error(w, "Missing program ID", http.StatusBadRequest)
		return
	}

	id, err := strconv.Atoi(idStr)
	if err != nil {
		http.Error(w, "Invalid program ID", http.StatusBadRequest)
		return
	}

	ctx := r.Context()
	if err := h.externalProgramStore.Delete(ctx, id); err != nil {
		if err == models.ErrExternalProgramNotFound {
			http.Error(w, "Program not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("id", id).Msg("Failed to delete external program")
		http.Error(w, "Failed to delete external program", http.StatusInternalServerError)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// ExecuteExternalProgram handles POST /api/external-programs/execute
func (h *ExternalProgramsHandler) ExecuteExternalProgram(w http.ResponseWriter, r *http.Request) {
	var req models.ExternalProgramExecute
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		log.Error().Err(err).Msg("Failed to decode execute external program request")
		http.Error(w, "Invalid request body", http.StatusBadRequest)
		return
	}

	// Validate required fields
	if req.ProgramID == 0 {
		http.Error(w, "Program ID is required", http.StatusBadRequest)
		return
	}

	if req.InstanceID == 0 {
		http.Error(w, "Instance ID is required", http.StatusBadRequest)
		return
	}

	if len(req.Hashes) == 0 {
		http.Error(w, "At least one torrent hash is required", http.StatusBadRequest)
		return
	}

	ctx := r.Context()

	// Get the program configuration
	program, err := h.externalProgramStore.GetByID(ctx, req.ProgramID)
	if err != nil {
		if err == models.ErrExternalProgramNotFound {
			http.Error(w, "Program not found", http.StatusNotFound)
			return
		}
		log.Error().Err(err).Int("programId", req.ProgramID).Msg("Failed to get external program")
		http.Error(w, "Failed to get program configuration", http.StatusInternalServerError)
		return
	}

	if !h.isPathAllowed(program.Path) {
		http.Error(w, "Program path is not allowed", http.StatusForbidden)
		return
	}

	if !program.Enabled {
		http.Error(w, "Program is disabled", http.StatusBadRequest)
		return
	}

	// Get client for the instance
	client, err := h.clientPool.GetClient(ctx, req.InstanceID)
	if err != nil {
		log.Error().Err(err).Int("instanceId", req.InstanceID).Msg("Failed to get client for instance")
		http.Error(w, fmt.Sprintf("Failed to get client for instance: %v", err), http.StatusInternalServerError)
		return
	}

	// Fetch all torrents once (O(m) instead of O(n·m) where n=hashes, m=torrents)
	torrents, err := client.GetTorrents(qbt.TorrentFilterOptions{})
	if err != nil {
		log.Error().Err(err).Int("instanceId", req.InstanceID).Msg("Failed to get torrents from instance")
		http.Error(w, fmt.Sprintf("Failed to get torrents: %v", err), http.StatusInternalServerError)
		return
	}

	// Build hash index for O(1) lookups
	torrentIndex := make(map[string]*qbt.Torrent, len(torrents))
	for i := range torrents {
		torrentIndex[strings.ToLower(torrents[i].Hash)] = &torrents[i]
	}

	// Execute for each torrent hash
	results := make([]map[string]any, 0, len(req.Hashes))
	for _, hash := range req.Hashes {
		result := h.executeForHash(ctx, program, hash, torrentIndex)
		results = append(results, result)
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"results": results,
	})
}

// executeForHash executes the external program for a single torrent hash
func (h *ExternalProgramsHandler) executeForHash(ctx context.Context, program *models.ExternalProgram, hash string, torrentIndex map[string]*qbt.Torrent) map[string]any {
	result := map[string]any{
		"hash":    hash,
		"success": false,
	}

	// Look up torrent in the pre-built index (O(1) lookup)
	torrent, found := torrentIndex[strings.ToLower(hash)]
	if !found {
		result["error"] = fmt.Sprintf("Torrent with hash %s not found", hash)
		return result
	}

	// Convert torrent to map format for variable substitution
	// Apply path mappings to convert remote paths to local paths
	savePath := externalprograms.ApplyPathMappings(torrent.SavePath, program.PathMappings)
	contentPath := externalprograms.ApplyPathMappings(torrent.ContentPath, program.PathMappings)

	torrentData := map[string]string{
		"hash":         torrent.Hash,
		"name":         torrent.Name,
		"save_path":    savePath,
		"category":     torrent.Category,
		"tags":         torrent.Tags,
		"state":        string(torrent.State),
		"size":         fmt.Sprintf("%d", torrent.Size),
		"progress":     fmt.Sprintf("%.2f", torrent.Progress),
		"content_path": contentPath,
		"comment":      torrent.Comment,
	}

	// Build command arguments by substituting variables
	args := externalprograms.BuildArguments(program.ArgsTemplate, torrentData)

	// Build the command - construct as array then let shell handle it
	// Use context.Background() so the command isn't cancelled when the HTTP request completes
	var cmd *exec.Cmd

	// Build command based on platform and use_terminal setting
	if program.UseTerminal {
		// Launch in a terminal window
		if runtime.GOOS == "windows" {
			// Windows: Use cmd.exe /c start cmd /k to open a new visible terminal window
			// Empty string after "start" prevents quoted paths from being interpreted as window title
			// When using "cmd /k", we need to pass the program path and args as separate arguments
			// exec.Command will handle quoting for CreateProcess, and cmd.exe will receive them properly
			cmdArgs := []string{"/c", "start", "", "cmd", "/k", program.Path}
			cmdArgs = append(cmdArgs, args...)
			cmd = exec.Command("cmd.exe", cmdArgs...)
		} else {
			// Unix/Linux: Build command string and spawn in a terminal
			// Use shellquote library to properly escape for Unix shells
			allArgs := append([]string{program.Path}, args...)
			fullCmd := shellquote.Join(allArgs...)
			// Try to find an available terminal emulator and spawn the command in it
			cmd = h.createTerminalCommand(fullCmd)
		}
	} else {
		// Launch directly without terminal (for GUI apps or background processes)
		if runtime.GOOS == "windows" {
			// Windows: Use 'start' to launch GUI apps properly (detached from parent process)
			// Empty string after "start" prevents quoted paths from being interpreted as window title
			// Pass program path and args as separate arguments - exec.Command handles quoting
			cmdArgs := []string{"/c", "start", "", "/b", program.Path}
			cmdArgs = append(cmdArgs, args...)
			cmd = exec.Command("cmd.exe", cmdArgs...)
		} else {
			// Unix/Linux: Direct execution
			if len(args) > 0 {
				cmd = exec.Command(program.Path, args...)
			} else {
				cmd = exec.Command(program.Path)
			}
		}
	}

	// Log the full command being executed for debugging
	log.Debug().
		Str("program", program.Name).
		Str("path", program.Path).
		Strs("args", args).
		Str("hash", hash).
		Str("full_command", fmt.Sprintf("%v", cmd.Args)).
		Msg("Executing external program")

	// Execute the command in a goroutine so it doesn't block qui or the HTTP response
	// This allows qui to shut down independently of the external program
	go func() {
		var execErr error

		if runtime.GOOS == "windows" {
			// Windows: Use Run() which waits for cmd.exe to complete
			// The 'start' command will spawn the process and cmd.exe will exit quickly
			execErr = cmd.Run()
			if execErr != nil {
				// Log the error for debugging, but note that 'start' command
				// may return non-zero exit code even on successful spawn
				log.Debug().
					Err(execErr).
					Str("program", program.Name).
					Str("hash", hash).
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
					Str("hash", hash).
					Str("command", fmt.Sprintf("%v", cmd.Args)).
					Msg("External program failed to start")
				return
			}

			// Wait for the process to prevent zombie processes
			// This runs in the goroutine, so it won't block qui
			waitErr := cmd.Wait()
			if waitErr != nil {
				// Terminal emulators may exit with non-zero status
				log.Debug().
					Err(waitErr).
					Str("program", program.Name).
					Str("hash", hash).
					Str("command", fmt.Sprintf("%v", cmd.Args)).
					Msg("Terminal emulator exited with error (may be normal)")
			}
		}
	}()

	// Return immediately without waiting for the command to complete
	result["success"] = true
	if program.UseTerminal {
		result["message"] = "Terminal window opened successfully"
		log.Info().
			Str("program", program.Name).
			Str("hash", hash).
			Str("command", fmt.Sprintf("%v", cmd.Args)).
			Msg("External program terminal launched")
	} else {
		result["message"] = "Program started successfully"
		log.Info().
			Str("program", program.Name).
			Str("hash", hash).
			Str("command", fmt.Sprintf("%v", cmd.Args)).
			Msg("External program launched")
	}

	return result
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

func (h *ExternalProgramsHandler) isPathAllowed(programPath string) bool {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return false
	}

	if h == nil || h.config == nil {
		return true
	}

	allowList := h.config.ExternalProgramAllowList
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

	log.Warn().Str("path", programPath).Msg("External program path blocked by allow list")
	return false
}

func normalizePathCase(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}

	return p
}

// createTerminalCommand creates a command that spawns a terminal window on Unix/Linux
// It tries different terminal emulators in order of preference
// Note: Does not use context so the terminal process isn't cancelled when the HTTP request completes
func (h *ExternalProgramsHandler) createTerminalCommand(cmdLine string) *exec.Cmd {
	// List of terminal emulators to try, in order of preference
	// Each has different syntax for executing a command
	terminals := []struct {
		name string
		args []string
	}{
		// gnome-terminal (GNOME)
		{"gnome-terminal", []string{"--", "bash", "-c", cmdLine + "; exec bash"}},
		// konsole (KDE)
		{"konsole", []string{"--hold", "-e", "bash", "-c", cmdLine}},
		// xfce4-terminal (XFCE)
		{"xfce4-terminal", []string{"--hold", "-e", "bash", "-c", cmdLine}},
		// mate-terminal (MATE)
		{"mate-terminal", []string{"-e", "bash", "-c", cmdLine + "; exec bash"}},
		// xterm (fallback, available on most systems)
		{"xterm", []string{"-hold", "-e", "bash", "-c", cmdLine}},
		// kitty (modern terminal)
		{"kitty", []string{"bash", "-c", cmdLine + "; exec bash"}},
		// alacritty (modern terminal)
		{"alacritty", []string{"-e", "bash", "-c", cmdLine + "; exec bash"}},
		// terminator
		{"terminator", []string{"-e", "bash", "-c", cmdLine + "; exec bash"}},
	}

	// Try each terminal until we find one that exists
	for _, term := range terminals {
		if _, err := exec.LookPath(term.name); err == nil {
			// Found a terminal, use it
			log.Debug().
				Str("terminal", term.name).
				Str("command", cmdLine).
				Msg("Using terminal emulator for external program")
			return exec.Command(term.name, term.args...)
		}
	}

	// Fallback: if no terminal emulator found, just run in background
	log.Warn().
		Str("command", cmdLine).
		Msg("No terminal emulator found, running command in background")
	return exec.Command("sh", "-c", cmdLine)
}
