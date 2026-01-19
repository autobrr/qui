package externalprograms

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
)

// buildArguments substitutes variables in the program's arguments template with torrent data.
// Returns arguments as an array suitable for exec.Command (no manual quoting needed).
func buildArguments(program *models.ExternalProgram, torrent *qbt.Torrent) []string {
	torrentData := map[string]string{
		"hash":         torrent.Hash,
		"name":         torrent.Name,
		"save_path":    applyPathMappings(torrent.SavePath, program.PathMappings),
		"category":     torrent.Category,
		"tags":         torrent.Tags,
		"state":        string(torrent.State),
		"size":         strconv.FormatInt(torrent.Size, 10),
		"progress":     fmt.Sprintf("%.2f", torrent.Progress),
		"content_path": applyPathMappings(torrent.ContentPath, program.PathMappings),
		"comment":      torrent.Comment,
	}

	if program.ArgsTemplate == "" {
		return []string{}
	}

	args := splitArgs(program.ArgsTemplate)
	for i := range args {
		for key, value := range torrentData {
			placeholder := "{" + key + "}"
			args[i] = strings.ReplaceAll(args[i], placeholder, value)
		}
	}

	return args
}

func createTerminalCommand(ctx context.Context, cmdLine string) *exec.Cmd {
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
			return exec.CommandContext(ctx, term.name, term.args...)
		}
	}

	// Fallback: if no terminal emulator found, just run in background
	log.Warn().
		Str("command", cmdLine).
		Msg("No terminal emulator found, running command in background")
	return exec.CommandContext(ctx, "sh", "-c", cmdLine)
}

// splitArgs splits a command line string into arguments, respecting quoted strings.
// It strips surrounding single/double quotes from quoted segments.
func splitArgs(s string) []string {
	var args []string
	var current strings.Builder
	inQuote := false
	quoteChar := rune(0)

	for _, r := range s {
		switch {
		case r == '"' || r == '\'':
			switch {
			case !inQuote:
				inQuote = true
				quoteChar = r
			case r == quoteChar:
				inQuote = false
				quoteChar = 0
			default:
				current.WriteRune(r)
			}
		case r == ' ' && !inQuote:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteRune(r)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

// applyPathMappings applies configured path mappings to convert remote paths to local paths.
//
// Mappings are matched longest-prefix-first to handle overlapping prefixes correctly.
// Prefix matching requires a path separator boundary (/ or \) to avoid false matches
// like "/data" matching "/data-backup".
func applyPathMappings(path string, mappings []models.PathMapping) string {
	if len(mappings) == 0 {
		return path
	}

	sortedMappings := make([]models.PathMapping, len(mappings))
	copy(sortedMappings, mappings)
	sort.Slice(sortedMappings, func(i, j int) bool {
		return len(sortedMappings[i].From) > len(sortedMappings[j].From)
	})

	for _, mapping := range sortedMappings {
		if mapping.From == "" || mapping.To == "" {
			continue
		}
		if strings.HasPrefix(path, mapping.From) {
			// Ensure we match at a path boundary, not mid-component.
			// E.g., From="/data" should match "/data/foo" but not "/data-backup".
			remainder := path[len(mapping.From):]
			// Boundary match if:
			// - exact match (remainder empty)
			// - From ends with separator (e.g., "/data/" or "C:\data\")
			// - remainder starts with separator
			fromEndsWithSep := strings.HasSuffix(mapping.From, "/") || strings.HasSuffix(mapping.From, "\\")
			if remainder == "" || fromEndsWithSep || remainder[0] == '/' || remainder[0] == '\\' {
				mappedTo := mapping.To
				if remainder != "" &&
					!strings.HasSuffix(mappedTo, "/") && !strings.HasSuffix(mappedTo, "\\") &&
					remainder[0] != '/' && remainder[0] != '\\' {
					// Prefer the separator style from mapping.From or mapping.To
					if strings.Contains(mapping.To, "\\") {
						mappedTo += "\\"
					} else {
						mappedTo += "/"
					}
				}
				return mappedTo + remainder
			}
		}
	}

	return path
}

// IsPathAllowed checks if a program path is permitted by the allowlist.
// If the allowlist is empty or nil, all paths are allowed.
// The path is normalized before comparison to handle symlinks and case differences (Windows).
func IsPathAllowed(programPath string, allowList []string) bool {
	programPath = strings.TrimSpace(programPath)
	if programPath == "" {
		return false
	}

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

		// Directory prefix match
		allowedPrefix := normalizedAllowedPath
		if !strings.HasSuffix(allowedPrefix, sep) {
			allowedPrefix += sep
		}

		if strings.HasPrefix(normalizedProgramPath, allowedPrefix) {
			return true
		}
	}

	return false
}

// normalizePath returns a canonical form of the path for comparison.
// Resolves symlinks, makes path absolute, and normalizes case on Windows.
func normalizePath(p string) string {
	cleaned, err := filepath.Abs(p)
	if err != nil {
		cleaned = filepath.Clean(p)
	}

	if resolved, err := filepath.EvalSymlinks(cleaned); err == nil {
		cleaned = resolved
	} else {
		// If symlink resolution fails (e.g., path doesn't exist yet),
		// try to resolve just the parent directory
		dir := filepath.Dir(cleaned)
		if dirResolved, dirErr := filepath.EvalSymlinks(dir); dirErr == nil {
			cleaned = filepath.Join(dirResolved, filepath.Base(cleaned))
		}
	}

	return normalizePathCase(cleaned)
}

// normalizePathCase normalizes path case for case-insensitive file systems (Windows).
func normalizePathCase(p string) string {
	if runtime.GOOS == "windows" {
		return strings.ToLower(p)
	}
	return p
}
