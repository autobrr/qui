// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

func TestNewService(t *testing.T) {
	t.Run("applies the allowlist from the config", func(t *testing.T) {
		allowed := t.TempDir()
		service := NewService(nil, nil, &domain.Config{ExternalProgramAllowList: []string{allowed}})
		assert.True(t, service.IsPathAllowed(filepath.Join(allowed, "script.sh")))
		assert.False(t, service.IsPathAllowed(filepath.Join(t.TempDir(), "script.sh")))
	})

	t.Run("sizes the execution limit from the config", func(t *testing.T) {
		assert.Equal(t, 3, cap(NewService(nil, nil, &domain.Config{ExternalProgramMaxRunning: 3}).slots))
		assert.Equal(t, defaultMaxRunningPrograms, cap(NewService(nil, nil, &domain.Config{}).slots))
		assert.Equal(t, defaultMaxRunningPrograms, cap(NewService(nil, nil, &domain.Config{ExternalProgramMaxRunning: -1}).slots))
		assert.Equal(t, defaultMaxRunningPrograms, cap(NewService(nil, nil, nil).slots))
	})
}

func TestService_Execute_NilService(t *testing.T) {
	var s *Service

	result := s.Execute(context.Background(), ExecuteRequest{
		ProgramID:  1,
		Torrent:    &qbt.Torrent{Hash: "abc123"},
		InstanceID: 1,
	})

	assert.False(t, result.Success)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "not initialized")
}

func TestService_Execute_NilProgramStore(t *testing.T) {
	s := &Service{
		programStore: nil,
	}

	result := s.Execute(context.Background(), ExecuteRequest{
		ProgramID:  1,
		Torrent:    &qbt.Torrent{Hash: "abc123"},
		InstanceID: 1,
	})

	assert.False(t, result.Success)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "not initialized")
}

func TestService_Execute_NilTorrent(t *testing.T) {
	s := &Service{}

	result := s.Execute(context.Background(), ExecuteRequest{
		Program:    &models.ExternalProgram{},
		Torrent:    nil,
		InstanceID: 1,
	})

	assert.False(t, result.Success)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "torrent is required")
}

func TestService_Execute_DisabledProgram(t *testing.T) {
	s := &Service{}

	program := &models.ExternalProgram{
		ID:      1,
		Name:    "Test Program",
		Enabled: false,
		Path:    "/usr/bin/test",
	}

	torrent := &qbt.Torrent{
		Hash: "abc123",
		Name: "Test Torrent",
	}

	result := s.Execute(context.Background(), ExecuteRequest{
		Program:    program,
		Torrent:    torrent,
		InstanceID: 1,
	})

	assert.False(t, result.Success)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "program is disabled")
}

func TestService_Execute_PathBlocked(t *testing.T) {
	tempDir := t.TempDir()
	otherDir := t.TempDir()

	s := &Service{
		config: &domain.Config{
			ExternalProgramAllowList: []string{otherDir},
		},
	}

	program := &models.ExternalProgram{
		ID:      1,
		Name:    "Test Program",
		Enabled: true,
		Path:    filepath.Join(tempDir, "script.sh"),
	}

	torrent := &qbt.Torrent{
		Hash: "abc123",
		Name: "Test Torrent",
	}

	result := s.Execute(context.Background(), ExecuteRequest{
		Program:    program,
		Torrent:    torrent,
		InstanceID: 1,
	})

	assert.False(t, result.Success)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "not allowed by allowlist")
}

func TestService_IsPathAllowed(t *testing.T) {
	tempDir := t.TempDir()
	allowedFile := filepath.Join(tempDir, "script.sh")

	tests := []struct {
		name     string
		config   *domain.Config
		path     string
		expected bool
	}{
		{
			name:     "nil config allows all",
			config:   nil,
			path:     "/any/path",
			expected: true,
		},
		{
			name:     "empty allowlist allows all",
			config:   &domain.Config{ExternalProgramAllowList: []string{}},
			path:     "/any/path",
			expected: true,
		},
		{
			name:     "directory in allowlist allows files within",
			config:   &domain.Config{ExternalProgramAllowList: []string{tempDir}},
			path:     allowedFile,
			expected: true,
		},
		{
			name:     "exact path match",
			config:   &domain.Config{ExternalProgramAllowList: []string{allowedFile}},
			path:     allowedFile,
			expected: true,
		},
		{
			name:     "path not in allowlist blocked",
			config:   &domain.Config{ExternalProgramAllowList: []string{"/other/dir"}},
			path:     allowedFile,
			expected: false,
		},
		{
			name:     "empty path blocked",
			config:   &domain.Config{ExternalProgramAllowList: []string{tempDir}},
			path:     "",
			expected: false,
		},
		{
			name:     "whitespace path blocked",
			config:   &domain.Config{ExternalProgramAllowList: []string{tempDir}},
			path:     "   ",
			expected: false,
		},
		{
			name:     "allowlist with whitespace entry ignored",
			config:   &domain.Config{ExternalProgramAllowList: []string{"  ", tempDir}},
			path:     allowedFile,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &Service{config: tt.config}
			result := s.IsPathAllowed(tt.path)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestBuildTorrentData(t *testing.T) {
	torrent := &qbt.Torrent{
		Hash:        "abc123def456",
		Name:        "Test.Torrent.Name",
		SavePath:    "/downloads/complete",
		Category:    "movies",
		Tags:        "tag1,tag2",
		State:       qbt.TorrentStateUploading,
		Size:        1024 * 1024 * 100,
		Progress:    0.75,
		ContentPath: "/downloads/complete/Test.Torrent.Name",
		Comment:     "Test comment",
	}

	pathMappings := []models.PathMapping{
		{From: "/downloads", To: "/mnt/data"},
	}

	data := buildTorrentData(torrent, pathMappings)

	assert.Equal(t, "abc123def456", data["hash"])
	assert.Equal(t, "Test.Torrent.Name", data["name"])
	assert.Equal(t, "/mnt/data/complete", data["save_path"])
	assert.Equal(t, "movies", data["category"])
	assert.Equal(t, "tag1,tag2", data["tags"])
	assert.Equal(t, "uploading", data["state"])
	assert.Equal(t, "104857600", data["size"])
	assert.Equal(t, "0.75", data["progress"])
	assert.Equal(t, "/mnt/data/complete/Test.Torrent.Name", data["content_path"])
	assert.Equal(t, "Test comment", data["comment"])
}

func TestBuildTorrentData_NoPathMappings(t *testing.T) {
	torrent := &qbt.Torrent{
		Hash:        "abc123",
		SavePath:    "/original/path",
		ContentPath: "/original/path/file",
	}

	data := buildTorrentData(torrent, nil)

	assert.Equal(t, "/original/path", data["save_path"])
	assert.Equal(t, "/original/path/file", data["content_path"])
}

func TestBuildTorrentData_SpecialCharacters(t *testing.T) {
	tests := []struct {
		name     string
		torrent  *qbt.Torrent
		checkKey string
	}{
		{
			name: "shell command injection attempt in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: "Movie; rm -rf /",
			},
			checkKey: "name",
		},
		{
			name: "backtick command substitution in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: "Movie `whoami`",
			},
			checkKey: "name",
		},
		{
			name: "dollar command substitution in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: "Movie $(whoami)",
			},
			checkKey: "name",
		},
		{
			name: "pipe command in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: "Movie | cat /etc/passwd",
			},
			checkKey: "name",
		},
		{
			name: "ampersand background in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: "Movie & rm -rf /",
			},
			checkKey: "name",
		},
		{
			name: "quotes in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: `Movie "with" 'quotes'`,
			},
			checkKey: "name",
		},
		{
			name: "newline injection in name",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Name: "Movie\nrm -rf /",
			},
			checkKey: "name",
		},
		{
			name: "special chars in save_path",
			torrent: &qbt.Torrent{
				Hash:     "abc123",
				SavePath: "/path/with spaces; rm -rf /",
			},
			checkKey: "save_path",
		},
		{
			name: "special chars in category",
			torrent: &qbt.Torrent{
				Hash:     "abc123",
				Category: "movies; rm -rf /",
			},
			checkKey: "category",
		},
		{
			name: "special chars in tags",
			torrent: &qbt.Torrent{
				Hash: "abc123",
				Tags: "tag1,tag2; rm -rf /",
			},
			checkKey: "tags",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := buildTorrentData(tt.torrent, nil)

			assert.NotEmpty(t, data[tt.checkKey])

			if tt.checkKey == "name" {
				assert.Equal(t, tt.torrent.Name, data["name"])
			}
		})
	}
}

func TestBuildTorrentData_EmptyFields(t *testing.T) {
	torrent := &qbt.Torrent{
		Hash:        "",
		Name:        "",
		SavePath:    "",
		Category:    "",
		Tags:        "",
		State:       "",
		Size:        0,
		Progress:    0,
		ContentPath: "",
		Comment:     "",
	}

	data := buildTorrentData(torrent, nil)

	assert.Empty(t, data["hash"])
	assert.Empty(t, data["name"])
	assert.Empty(t, data["save_path"])
	assert.Empty(t, data["category"])
	assert.Empty(t, data["tags"])
	assert.Empty(t, data["state"])
	assert.Equal(t, "0", data["size"])
	assert.Equal(t, "0.00", data["progress"])
	assert.Empty(t, data["content_path"])
	assert.Empty(t, data["comment"])
}

func TestExecuteRequest_Validate(t *testing.T) {
	torrent := &qbt.Torrent{Hash: "abc123"}
	program := &models.ExternalProgram{ID: 1, Name: "Test"}

	tests := []struct {
		name    string
		req     ExecuteRequest
		wantErr bool
		errMsg  string
	}{
		{
			name: "valid request with program ID",
			req: ExecuteRequest{
				ProgramID:  1,
				Torrent:    torrent,
				InstanceID: 1,
			},
			wantErr: false,
		},
		{
			name: "valid request with program object",
			req: ExecuteRequest{
				Program:    program,
				Torrent:    torrent,
				InstanceID: 1,
			},
			wantErr: false,
		},
		{
			name: "neither program ID nor program object",
			req: ExecuteRequest{
				ProgramID:  0,
				Program:    nil,
				Torrent:    torrent,
				InstanceID: 1,
			},
			wantErr: true,
			errMsg:  "either programID or program",
		},
		{
			name: "nil torrent",
			req: ExecuteRequest{
				ProgramID:  1,
				Torrent:    nil,
				InstanceID: 1,
			},
			wantErr: true,
			errMsg:  "torrent",
		},
		{
			name: "zero instance ID",
			req: ExecuteRequest{
				ProgramID:  1,
				Torrent:    torrent,
				InstanceID: 0,
			},
			wantErr: true,
			errMsg:  "instanceID",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.req.Validate()
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.errMsg)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestGetTerminalCandidates(t *testing.T) {
	candidates := getTerminalCandidates()

	assert.NotEmpty(t, candidates)

	crossPlatformTerminals := []string{"wezterm", "hyper", "kitty", "alacritty"}
	for i, term := range crossPlatformTerminals {
		assert.Equal(t, term, candidates[i].name, "cross-platform terminal %s should be at position %d", term, i)
	}

	switch runtime.GOOS {
	case "darwin":
		var hasITerm2, hasAppleTerminal bool
		for _, c := range candidates {
			if c.name == "iterm2" {
				hasITerm2 = true
			}
			if c.name == "apple-terminal" {
				hasAppleTerminal = true
			}
		}
		assert.True(t, hasITerm2, "macOS should have iterm2 as a candidate")
		assert.True(t, hasAppleTerminal, "macOS should have apple-terminal as a candidate")

		for _, c := range candidates {
			assert.NotEqual(t, "gnome-terminal", c.name, "macOS should not have gnome-terminal")
			assert.NotEqual(t, "konsole", c.name, "macOS should not have konsole")
			assert.NotEqual(t, "xterm", c.name, "macOS should not have xterm")
		}

	case "windows":
		for _, c := range candidates {
			assert.NotEqual(t, "iterm2", c.name, "Windows should not have iterm2")
			assert.NotEqual(t, "apple-terminal", c.name, "Windows should not have apple-terminal")
		}

		var hasGnomeTerminal, hasXterm bool
		for _, c := range candidates {
			if c.name == "gnome-terminal" {
				hasGnomeTerminal = true
			}
			if c.name == "xterm" {
				hasXterm = true
			}
		}
		assert.True(t, hasGnomeTerminal, "Windows candidate list should include gnome-terminal")
		assert.True(t, hasXterm, "Windows candidate list should include xterm")

	default:
		var hasGnomeTerminal, hasXterm bool
		for _, c := range candidates {
			if c.name == "gnome-terminal" {
				hasGnomeTerminal = true
			}
			if c.name == "xterm" {
				hasXterm = true
			}
		}
		assert.True(t, hasGnomeTerminal, "Linux should have gnome-terminal as a candidate")
		assert.True(t, hasXterm, "Linux should have xterm as a candidate")

		for _, c := range candidates {
			assert.NotEqual(t, "iterm2", c.name, "Linux should not have iterm2")
			assert.NotEqual(t, "apple-terminal", c.name, "Linux should not have apple-terminal")
		}
	}
}

func TestEscapeAppleScript(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "no special characters",
			input:    "echo hello world",
			expected: "echo hello world",
		},
		{
			name:     "single backslash",
			input:    `echo hello\world`,
			expected: `echo hello\\world`,
		},
		{
			name:     "multiple backslashes",
			input:    `path\to\file\here`,
			expected: `path\\to\\file\\here`,
		},
		{
			name:     "double quote",
			input:    `echo "hello"`,
			expected: `echo \"hello\"`,
		},
		{
			name:     "multiple double quotes",
			input:    `echo "hello" "world"`,
			expected: `echo \"hello\" \"world\"`,
		},
		{
			name:     "backslash and double quote combined",
			input:    `echo "path\to\file"`,
			expected: `echo \"path\\to\\file\"`,
		},
		{
			name:     "already escaped backslash",
			input:    `echo \\n`,
			expected: `echo \\\\n`,
		},
		{
			name:     "single quotes not escaped",
			input:    `echo 'hello'`,
			expected: `echo 'hello'`,
		},
		{
			name:     "newlines escaped",
			input:    "line1\nline2",
			expected: "line1\\nline2",
		},
		{
			name:     "tabs escaped",
			input:    "col1\tcol2",
			expected: "col1\\tcol2",
		},
		{
			name:     "carriage return escaped",
			input:    "line1\rline2",
			expected: "line1\\rline2",
		},
		{
			name:     "injection attempt with quotes",
			input:    `"; do shell script "rm -rf /"`,
			expected: `\"; do shell script \"rm -rf /\"`,
		},
		{
			name:     "nested quotes attack",
			input:    `"" & do shell script "evil"`,
			expected: `\"\" & do shell script \"evil\"`,
		},
		{
			name:     "backslash quote sequence",
			input:    `\"`,
			expected: `\\\"`,
		},
		{
			name:     "multiple backslash quote sequence",
			input:    `\\\"`,
			expected: `\\\\\\\"`,
		},
		{
			name:     "unicode characters preserved",
			input:    `echo "hello 世界 🌍"`,
			expected: `echo \"hello 世界 🌍\"`,
		},
		{
			name:     "path with spaces and quotes",
			input:    `/path/to/"my file".txt`,
			expected: `/path/to/\"my file\".txt`,
		},
		{
			name:     "command substitution attempt",
			input:    `$(rm -rf /)`,
			expected: `$(rm -rf /)`,
		},
		{
			name:     "backtick command substitution",
			input:    "`rm -rf /`",
			expected: "`rm -rf /`",
		},
		{
			name:     "complex shell command",
			input:    `bash -c "cd /tmp && rm -rf *"`,
			expected: `bash -c \"cd /tmp && rm -rf *\"`,
		},
		{
			name:     "windows style path",
			input:    `C:\Users\test\file.txt`,
			expected: `C:\\Users\\test\\file.txt`,
		},
		{
			name:     "mixed slashes",
			input:    `/path/to\mixed/slashes`,
			expected: `/path/to\\mixed/slashes`,
		},
		{
			name:     "null byte injection attempt",
			input:    "hello\x00world",
			expected: "hello\x00world",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeAppleScript(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestDetectTerminalFromEnv(t *testing.T) {
	tests := []struct {
		name          string
		envValue      string
		expectedTerm  string
		expectedFound bool
	}{
		{
			name:          "empty TERM_PROGRAM",
			envValue:      "",
			expectedTerm:  "",
			expectedFound: false,
		},
		{
			name:          "iTerm.app detected",
			envValue:      "iTerm.app",
			expectedTerm:  "iterm2",
			expectedFound: true,
		},
		{
			name:          "Apple_Terminal detected",
			envValue:      "Apple_Terminal",
			expectedTerm:  "apple-terminal",
			expectedFound: true,
		},
		{
			name:          "WezTerm detected",
			envValue:      "WezTerm",
			expectedTerm:  "wezterm",
			expectedFound: true,
		},
		{
			name:          "Hyper detected",
			envValue:      "Hyper",
			expectedTerm:  "hyper",
			expectedFound: true,
		},
		{
			name:          "kitty detected",
			envValue:      "kitty",
			expectedTerm:  "kitty",
			expectedFound: true,
		},
		{
			name:          "alacritty detected",
			envValue:      "alacritty",
			expectedTerm:  "alacritty",
			expectedFound: true,
		},
		{
			name:          "unknown terminal",
			envValue:      "unknown-terminal",
			expectedTerm:  "",
			expectedFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			originalValue := os.Getenv("TERM_PROGRAM")
			defer func() {
				if originalValue == "" {
					os.Unsetenv("TERM_PROGRAM")
				} else {
					os.Setenv("TERM_PROGRAM", originalValue)
				}
			}()

			if tt.envValue == "" {
				os.Unsetenv("TERM_PROGRAM")
			} else {
				os.Setenv("TERM_PROGRAM", tt.envValue)
			}

			term, found := detectTerminalFromEnv()
			assert.Equal(t, tt.expectedTerm, term)
			assert.Equal(t, tt.expectedFound, found)
		})
	}
}

func TestBuildTerminalArgs(t *testing.T) {
	testCmd := "echo hello"
	keepOpenCmd := testCmd + "; exec bash"

	tests := []struct {
		name         string
		terminal     string
		cmdLine      string
		expectedExe  string
		expectedArgs []string
	}{
		{
			name:         "wezterm",
			terminal:     "wezterm",
			cmdLine:      testCmd,
			expectedExe:  "wezterm",
			expectedArgs: []string{"start", "--", "bash", "-c", keepOpenCmd},
		},
		{
			name:         "hyper",
			terminal:     "hyper",
			cmdLine:      testCmd,
			expectedExe:  "hyper",
			expectedArgs: []string{"-e", "bash", "-c", keepOpenCmd},
		},
		{
			name:         "kitty",
			terminal:     "kitty",
			cmdLine:      testCmd,
			expectedExe:  "kitty",
			expectedArgs: []string{"bash", "-c", keepOpenCmd},
		},
		{
			name:         "alacritty",
			terminal:     "alacritty",
			cmdLine:      testCmd,
			expectedExe:  "alacritty",
			expectedArgs: []string{"-e", "bash", "-c", keepOpenCmd},
		},
		{
			name:         "gnome-terminal",
			terminal:     "gnome-terminal",
			cmdLine:      testCmd,
			expectedExe:  "gnome-terminal",
			expectedArgs: []string{"--", "bash", "-c", keepOpenCmd},
		},
		{
			name:         "konsole uses hold flag",
			terminal:     "konsole",
			cmdLine:      testCmd,
			expectedExe:  "konsole",
			expectedArgs: []string{"--hold", "-e", "bash", "-c", testCmd},
		},
		{
			name:         "xfce4-terminal uses hold flag",
			terminal:     "xfce4-terminal",
			cmdLine:      testCmd,
			expectedExe:  "xfce4-terminal",
			expectedArgs: []string{"--hold", "-e", "bash", "-c", testCmd},
		},
		{
			name:         "mate-terminal",
			terminal:     "mate-terminal",
			cmdLine:      testCmd,
			expectedExe:  "mate-terminal",
			expectedArgs: []string{"-e", "bash", "-c", keepOpenCmd},
		},
		{
			name:         "xterm uses hold flag",
			terminal:     "xterm",
			cmdLine:      testCmd,
			expectedExe:  "xterm",
			expectedArgs: []string{"-hold", "-e", "bash", "-c", testCmd},
		},
		{
			name:         "terminator",
			terminal:     "terminator",
			cmdLine:      testCmd,
			expectedExe:  "terminator",
			expectedArgs: []string{"-e", "bash", "-c", keepOpenCmd},
		},
		{
			name:         "unknown terminal returns empty",
			terminal:     "unknown-terminal",
			cmdLine:      testCmd,
			expectedExe:  "",
			expectedArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe, args := buildTerminalArgs(tt.terminal, tt.cmdLine)
			assert.Equal(t, tt.expectedExe, exe)

			assert.Equal(t, tt.expectedArgs, args)
		})
	}
}

func TestBuildTerminalArgs_AppleScriptContent(t *testing.T) {
	cmdLine := `echo "hello world"`
	keepOpenCmd := cmdLine + "; exec bash"
	escapedCmd := escapeAppleScript(keepOpenCmd)

	t.Run("iterm2 script structure", func(t *testing.T) {
		exe, args := buildTerminalArgs("iterm2", cmdLine)
		assert.Equal(t, "osascript", exe)
		assert.Len(t, args, 2)
		assert.Equal(t, "-e", args[0])

		script := args[1]
		assert.Contains(t, script, `tell application "iTerm"`)
		assert.Contains(t, script, "create window with default profile")
		assert.Contains(t, script, "tell current session of current window")
		assert.Contains(t, script, "write text")
		assert.Contains(t, script, escapedCmd)
		assert.Contains(t, script, "end tell")
	})

	t.Run("apple-terminal script structure", func(t *testing.T) {
		exe, args := buildTerminalArgs("apple-terminal", cmdLine)
		assert.Equal(t, "osascript", exe)
		assert.Len(t, args, 2)
		assert.Equal(t, "-e", args[0])

		script := args[1]
		assert.Contains(t, script, `tell application "Terminal"`)
		assert.Contains(t, script, "do script")
		assert.Contains(t, script, "activate")
		assert.Contains(t, script, escapedCmd)
		assert.Contains(t, script, "end tell")
	})
}

func TestIsTerminalAvailable(t *testing.T) {
	t.Run("apple-terminal availability by platform", func(t *testing.T) {
		result := isTerminalAvailable("apple-terminal")
		switch runtime.GOOS {
		case "darwin":
			assert.True(t, result, "apple-terminal should always be available on macOS")
		case "windows":
			assert.False(t, result, "apple-terminal should not be available on Windows")
		default:
			assert.False(t, result, "apple-terminal should not be available on Linux")
		}
	})

	t.Run("nonexistent terminal returns false", func(t *testing.T) {
		result := isTerminalAvailable("definitely-not-a-real-terminal-12345")
		assert.False(t, result)
	})
}

func TestTerminalCandidate_Integration(t *testing.T) {
	candidates := getTerminalCandidates()
	testCmd := "echo test"

	for _, candidate := range candidates {
		t.Run(candidate.name, func(t *testing.T) {
			exe, args := buildTerminalArgs(candidate.name, testCmd)
			assert.NotEmpty(t, exe, "buildTerminalArgs should return exe for candidate %s", candidate.name)
			assert.NotNil(t, args, "buildTerminalArgs should return args for candidate %s", candidate.name)

			_ = isTerminalAvailable(candidate.name)
		})
	}
}

func TestBuildCommand_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows command building tests on non-Windows platform")
	}

	service := &Service{}
	ctx := context.Background()
	assertWindowsCmdPath := func(t *testing.T, path string) {
		t.Helper()
		assert.Equal(t, "cmd.exe", strings.ToLower(filepath.Base(path)))
	}

	t.Run("terminal mode runs cmd /k in a new console", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: true,
		}
		cmd, _ := service.buildCommand(ctx, program, []string{"arg1", "arg2"})

		assertWindowsCmdPath(t, cmd.Path)
		assert.Equal(t, []string{"cmd.exe", "/d", "/e:on", "/v:off", "/s", "/k", program.Path, "arg1", "arg2"}, cmd.Args)
		assert.Contains(t, cmd.Env, "QUI_EXTERNAL_PROGRAM_PATH="+program.Path)
	})

	t.Run("direct mode launches the executable without a shell", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: false,
		}
		cmd, launcher := service.buildCommand(ctx, program, []string{"arg1"})
		assert.Equal(t, program.Path, cmd.Path)
		assert.Equal(t, []string{program.Path, "arg1"}, cmd.Args)
		assert.False(t, launcher.releaseOnStart, "the service owns an unclassified process")
	})

	t.Run("terminal mode with no arguments", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: true,
		}
		cmd, _ := service.buildCommand(ctx, program, nil)

		assertWindowsCmdPath(t, cmd.Path)
		assert.Equal(t, []string{"cmd.exe", "/d", "/e:on", "/v:off", "/s", "/k", program.Path}, cmd.Args)
	})

	t.Run("direct mode with no arguments", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: false,
		}
		cmd, _ := service.buildCommand(ctx, program, nil)

		assert.Equal(t, program.Path, cmd.Path)
		assert.Equal(t, []string{program.Path}, cmd.Args)
	})
}

func TestIsPathAllowed_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows path allowlist tests on non-Windows platform")
	}

	t.Run("case insensitive matching on Windows", func(t *testing.T) {
		s := &Service{
			config: &domain.Config{
				ExternalProgramAllowList: []string{"C:\\Programs"},
			},
		}

		assert.True(t, s.IsPathAllowed("C:\\Programs\\test.exe"))
		assert.True(t, s.IsPathAllowed("c:\\programs\\test.exe"))
		assert.True(t, s.IsPathAllowed("C:\\PROGRAMS\\TEST.EXE"))
	})

	t.Run("Windows path separators", func(t *testing.T) {
		s := &Service{
			config: &domain.Config{
				ExternalProgramAllowList: []string{"C:\\Programs\\Scripts"},
			},
		}

		assert.True(t, s.IsPathAllowed("C:\\Programs\\Scripts\\test.bat"))
		assert.False(t, s.IsPathAllowed("C:\\Programs\\Other\\test.bat"))
	})
}

func TestBuildCommand_Unix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping Unix command building tests on Windows")
	}

	service := &Service{}
	ctx := context.Background()

	t.Run("direct mode executes program directly", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "/usr/bin/test",
			UseTerminal: false,
		}
		cmd, _ := service.buildCommand(ctx, program, []string{"arg1", "arg2"})

		assert.Equal(t, "/usr/bin/test", cmd.Path)
		assert.Equal(t, []string{"/usr/bin/test", "arg1", "arg2"}, cmd.Args)
	})

	t.Run("direct mode with no args", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "/usr/bin/test",
			UseTerminal: false,
		}
		cmd, _ := service.buildCommand(ctx, program, nil)

		assert.Equal(t, "/usr/bin/test", cmd.Path)
		assert.Equal(t, []string{"/usr/bin/test"}, cmd.Args)
	})
}

func TestService_Execute_LimitsConcurrentPrograms(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("this fixture needs a POSIX shell; Windows uses the native recorder test")
	}

	ctx := t.Context()
	dir := t.TempDir()
	script := filepath.Join(dir, "block.sh")
	//nolint:gosec // G306: the test runs this script, so it must be executable
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
d=$(dirname "$0")
touch "$d/started-$1"
while [ -d "$d" ] && [ ! -e "$d/release" ]; do sleep 0.01; done
`), 0o700))

	s := NewService(nil, nil, &domain.Config{ExternalProgramAllowList: []string{dir}})
	s.slots = make(chan struct{}, 2)
	s.maxWaiting = 1

	program := &models.ExternalProgram{ID: 1, Name: "block", Enabled: true, Path: script, ArgsTemplate: "{hash}"}
	execute := func(hash string) ExecuteResult {
		return s.Execute(ctx, ExecuteRequest{Program: program, Torrent: &qbt.Torrent{Hash: hash, Name: hash}, InstanceID: 1})
	}
	started := func() []string {
		matches, globErr := filepath.Glob(filepath.Join(dir, "started-*"))
		require.NoError(t, globErr)
		return matches
	}
	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(dir, "release"), nil, 0o600)
		require.Eventually(t, func() bool { return s.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
	})

	require.True(t, execute("a").Success)
	require.True(t, execute("b").Success)
	require.Eventually(t, func() bool { return len(started()) == 2 }, 5*time.Second, 10*time.Millisecond)

	s.waitTimeout = 100 * time.Millisecond
	require.True(t, execute("c").Success, "a request that must wait is still admitted")
	require.Equal(t, "Program already waiting for this torrent", execute("c").Message, "a second request for a waiting torrent is skipped, not rejected")

	full := execute("d")
	require.False(t, full.Success)
	require.ErrorContains(t, full.Error, "execution queue full")

	require.Eventually(t, func() bool { return s.admitted.Load() == 2 }, 5*time.Second, 10*time.Millisecond, "c gives up after the wait timeout")
	assert.ElementsMatch(t, []string{filepath.Join(dir, "started-a"), filepath.Join(dir, "started-b")}, started())

	require.NoError(t, os.WriteFile(filepath.Join(dir, "release"), nil, 0o600))
	s.waitTimeout = 5 * time.Second
	require.Eventually(t, func() bool { return s.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
	require.Equal(t, "Program execution initiated", execute("c").Message, "a dropped run no longer blocks its torrent")
	require.Equal(t, "Program execution initiated", execute("d").Message, "a rejected run does not block its torrent")
	require.Eventually(t, func() bool { return len(started()) == 4 }, 5*time.Second, 10*time.Millisecond)

	require.Equal(t, "Program execution initiated", execute("a").Message, "a run that got a slot no longer blocks its torrent")

	require.Eventually(t, func() bool { return s.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
	for _, h := range []string{"f", "g", "h"} {
		require.True(t, execute(h).Success, "finished runs free their admission: %s", h)
	}
}

func TestService_Execute_LimitsTerminalFallback(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the sh fallback needs a host with no terminal emulator")
	}

	bin := t.TempDir()
	for _, tool := range []string{"sh", "dirname", "touch", "sleep"} {
		target, err := exec.LookPath(tool)
		require.NoError(t, err)
		require.NoError(t, os.Symlink(target, filepath.Join(bin, tool)))
	}
	t.Setenv("PATH", bin)
	t.Setenv("TERM_PROGRAM", "")

	dir := t.TempDir()
	script := filepath.Join(dir, "block.sh")
	//nolint:gosec // G306: the test runs this script, so it must be executable
	require.NoError(t, os.WriteFile(script, []byte(`#!/bin/sh
d=$(dirname "$0")
touch "$d/started-$1"
while [ -d "$d" ] && [ ! -e "$d/release" ]; do sleep 0.01; done
`), 0o700))

	s := NewService(nil, nil, nil)
	program := &models.ExternalProgram{ID: 1, Name: "block", Enabled: true, Path: script, ArgsTemplate: "{hash}", UseTerminal: true}

	_, launcher := s.buildCommand(t.Context(), program, nil)
	require.False(t, launcher.releaseOnStart, "no terminal emulator, so qui runs the program with sh -c")

	t.Cleanup(func() {
		_ = os.WriteFile(filepath.Join(dir, "release"), nil, 0o600)
		require.Eventually(t, func() bool { return s.admitted.Load() == 0 }, 5*time.Second, 10*time.Millisecond)
	})
	for i := range 10 {
		require.True(t, s.Execute(t.Context(), ExecuteRequest{Program: program, Torrent: &qbt.Torrent{Hash: strconv.Itoa(i)}, InstanceID: 1}).Success)
	}

	started := func() int {
		matches, err := filepath.Glob(filepath.Join(dir, "started-*"))
		require.NoError(t, err)
		return len(matches)
	}
	require.Eventually(t, func() bool { return started() == defaultMaxRunningPrograms }, 5*time.Second, 10*time.Millisecond)
	require.Never(t, func() bool { return started() > defaultMaxRunningPrograms }, 300*time.Millisecond, 10*time.Millisecond)
}

func TestShellJoin_RoundTripsThroughShell(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shellJoin targets POSIX shells")
	}

	args := []string{"/usr/bin/my prog", "", "it's", `a"b`, "$HOME", "`id`", "a\\b", "x;y|z&", "line1\nline2", "~", "*", "\t"}
	out, err := exec.CommandContext(t.Context(), "sh", "-c", `printf '%s\0' `+shellJoin(args)).Output()
	require.NoError(t, err)
	assert.Equal(t, args, strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"))
}
