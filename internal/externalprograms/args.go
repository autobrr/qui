package externalprograms

import (
	"fmt"
	"os/exec"
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

func createTerminalCommand(cmdLine string) *exec.Cmd {
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
