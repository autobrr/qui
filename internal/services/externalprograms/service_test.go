// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/domain"
	"github.com/autobrr/qui/internal/models"
)

// mockProgramStore implements a minimal mock for testing
type mockProgramStore struct {
	programs map[int]*models.ExternalProgram
	err      error
}

func (m *mockProgramStore) GetByID(_ context.Context, id int) (*models.ExternalProgram, error) {
	if m.err != nil {
		return nil, m.err
	}
	if program, ok := m.programs[id]; ok {
		return program, nil
	}
	return nil, models.ErrExternalProgramNotFound
}

// mockActivityStore implements a minimal mock for testing
type mockActivityStore struct {
	activities []*models.AutomationActivity
	err        error
}

func (m *mockActivityStore) Create(_ context.Context, activity *models.AutomationActivity) error {
	if m.err != nil {
		return m.err
	}
	m.activities = append(m.activities, activity)
	return nil
}

func TestNewService(t *testing.T) {
	t.Run("creates service with all dependencies", func(t *testing.T) {
		store := &mockProgramStore{}
		activityStore := &mockActivityStore{}
		config := &domain.Config{}

		// Note: NewService accepts the concrete types, not our mocks
		// This test validates the constructor pattern
		service := NewService(nil, nil, config)
		assert.NotNil(t, service)
		assert.Equal(t, config, service.config)

		// Test with nil config (should be allowed)
		service2 := NewService(nil, nil, nil)
		assert.NotNil(t, service2)
		assert.Nil(t, service2.config)

		_ = store
		_ = activityStore
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

func TestService_Execute_WithProgramObject(t *testing.T) {
	// Test that when a Program is provided, it's used directly without fetching
	s := &Service{} // No program store needed when Program is provided directly

	program := &models.ExternalProgram{
		ID:      1,
		Name:    "Test",
		Enabled: false, // Disabled so we don't actually execute
		Path:    "/test",
	}

	result := s.Execute(context.Background(), ExecuteRequest{
		Program:    program,
		Torrent:    &qbt.Torrent{Hash: "abc123"},
		InstanceID: 1,
	})

	// Should fail with "disabled" because we used the provided program
	assert.False(t, result.Success)
	require.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "program is disabled")
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
			ExternalProgramAllowList: []string{otherDir}, // Different directory
		},
	}

	program := &models.ExternalProgram{
		ID:      1,
		Name:    "Test Program",
		Enabled: true,
		Path:    filepath.Join(tempDir, "script.sh"), // Not in allowlist
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
			name:     "nil allowlist allows all",
			config:   &domain.Config{ExternalProgramAllowList: nil},
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

func TestService_IsPathAllowed_NilService(t *testing.T) {
	// This would panic, so we test that a properly initialized service handles nil config
	s := &Service{config: nil}
	assert.True(t, s.IsPathAllowed("/any/path"))
}

func TestBuildTorrentData(t *testing.T) {
	torrent := &qbt.Torrent{
		Hash:        "abc123def456",
		Name:        "Test.Torrent.Name",
		SavePath:    "/downloads/complete",
		Category:    "movies",
		Tags:        "tag1,tag2",
		State:       qbt.TorrentStateUploading,
		Size:        1024 * 1024 * 100, // 100 MB
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
	assert.Equal(t, "/mnt/data/complete", data["save_path"]) // Path mapped
	assert.Equal(t, "movies", data["category"])
	assert.Equal(t, "tag1,tag2", data["tags"])
	assert.Equal(t, "uploading", data["state"])
	assert.Equal(t, "104857600", data["size"])
	assert.Equal(t, "0.75", data["progress"])
	assert.Equal(t, "/mnt/data/complete/Test.Torrent.Name", data["content_path"]) // Path mapped
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
	// Test that special characters in torrent data are handled safely
	// These characters could potentially be used for shell injection attacks
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

			// Verify the data is stored as-is (not executed or interpreted)
			// The actual shell escaping happens in shellquote.Join when building commands
			assert.NotEmpty(t, data[tt.checkKey])

			// For name-based tests, verify the exact value is preserved
			if tt.checkKey == "name" {
				assert.Equal(t, tt.torrent.Name, data["name"])
			}
		})
	}
}

func TestBuildTorrentData_EmptyFields(t *testing.T) {
	// Test handling of empty and zero values
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
		{
			name: "with optional rule context",
			req: ExecuteRequest{
				ProgramID:  1,
				Torrent:    torrent,
				InstanceID: 1,
				RuleID:     new(42),
				RuleName:   "Test Rule",
			},
			wantErr: false,
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

func TestExecuteResult_Constructors(t *testing.T) {
	t.Run("SuccessResult", func(t *testing.T) {
		result := SuccessResult("Program started")
		assert.True(t, result.Success)
		require.NoError(t, result.Error)
		assert.Equal(t, "Program started", result.Message)
	})

	t.Run("FailureResult", func(t *testing.T) {
		err := errors.New("execution failed")
		result := FailureResult(err)
		assert.False(t, result.Success)
		assert.Equal(t, err, result.Error)
		assert.Empty(t, result.Message)
	})

	t.Run("FailureResultWithMessage", func(t *testing.T) {
		err := errors.New("execution failed")
		result := FailureResultWithMessage(err, "additional context")
		assert.False(t, result.Success)
		assert.Equal(t, err, result.Error)
		assert.Equal(t, "additional context", result.Message)
	})
}

// Helper function
//
//go:fix inline
func intPtr(i int) *int {
	return new(i)
}

// =============================================================================
// Terminal Detection Function Tests
// =============================================================================

func TestGetTerminalCandidates(t *testing.T) {
	candidates := getTerminalCandidates()

	// Should always return some candidates
	assert.NotEmpty(t, candidates)

	// Cross-platform terminals should always be present at the start
	crossPlatformTerminals := []string{"wezterm", "hyper", "kitty", "alacritty"}
	for i, term := range crossPlatformTerminals {
		assert.Equal(t, term, candidates[i].name, "cross-platform terminal %s should be at position %d", term, i)
	}

	// Platform-specific checks
	switch runtime.GOOS {
	case "darwin":
		// macOS should have iterm2 and apple-terminal
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

		// macOS should NOT have Linux-specific terminals
		for _, c := range candidates {
			assert.NotEqual(t, "gnome-terminal", c.name, "macOS should not have gnome-terminal")
			assert.NotEqual(t, "konsole", c.name, "macOS should not have konsole")
			assert.NotEqual(t, "xterm", c.name, "macOS should not have xterm")
		}

	case "windows":
		// Windows should NOT have macOS-specific terminals
		for _, c := range candidates {
			assert.NotEqual(t, "iterm2", c.name, "Windows should not have iterm2")
			assert.NotEqual(t, "apple-terminal", c.name, "Windows should not have apple-terminal")
		}

		// Windows should have Linux terminals in the candidate list (they won't be available but are checked)
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
		// Linux should have Linux terminals
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

		// Linux should NOT have macOS-specific terminals
		for _, c := range candidates {
			assert.NotEqual(t, "iterm2", c.name, "Linux should not have iterm2")
			assert.NotEqual(t, "apple-terminal", c.name, "Linux should not have apple-terminal")
		}
	}
}

func TestGetTerminalCandidates_OrderPreservation(t *testing.T) {
	// Call multiple times to ensure consistent ordering
	for range 5 {
		candidates := getTerminalCandidates()

		// First 4 should always be cross-platform in same order
		assert.Equal(t, "wezterm", candidates[0].name)
		assert.Equal(t, "hyper", candidates[1].name)
		assert.Equal(t, "kitty", candidates[2].name)
		assert.Equal(t, "alacritty", candidates[3].name)
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
		// Security-critical test cases
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
		{
			name:     "carriage return",
			input:    "line1\rline2",
			expected: "line1\\rline2",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeAppleScript(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestEscapeAppleScript_Idempotence(t *testing.T) {
	// Double escaping should produce different results (not idempotent)
	// This is important to understand for security
	input := `echo "hello"`
	firstEscape := escapeAppleScript(input)
	secondEscape := escapeAppleScript(firstEscape)

	assert.Equal(t, `echo \"hello\"`, firstEscape)
	assert.Equal(t, `echo \\\"hello\\\"`, secondEscape)
	assert.NotEqual(t, firstEscape, secondEscape, "escaping is not idempotent")
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
		{
			name:          "case sensitive - lowercase iterm",
			envValue:      "iterm.app",
			expectedTerm:  "",
			expectedFound: false,
		},
		{
			name:          "case sensitive - uppercase KITTY",
			envValue:      "KITTY",
			expectedTerm:  "",
			expectedFound: false,
		},
		{
			name:          "gnome-terminal not detected",
			envValue:      "gnome-terminal",
			expectedTerm:  "",
			expectedFound: false,
		},
		{
			name:          "konsole not detected",
			envValue:      "konsole",
			expectedTerm:  "",
			expectedFound: false,
		},
		{
			name:          "xterm not detected",
			envValue:      "xterm",
			expectedTerm:  "",
			expectedFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save original env value
			originalValue := os.Getenv("TERM_PROGRAM")
			defer func() {
				if originalValue == "" {
					os.Unsetenv("TERM_PROGRAM")
				} else {
					os.Setenv("TERM_PROGRAM", originalValue)
				}
			}()

			// Set test env value
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
		checkScript  bool // If true, check AppleScript content
	}{
		// macOS native terminals
		{
			name:        "iterm2",
			terminal:    "iterm2",
			cmdLine:     testCmd,
			expectedExe: "osascript",
			checkScript: true,
		},
		{
			name:        "apple-terminal",
			terminal:    "apple-terminal",
			cmdLine:     testCmd,
			expectedExe: "osascript",
			checkScript: true,
		},
		// Cross-platform terminals
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
		// Linux terminals
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
			expectedArgs: []string{"--hold", "-e", "bash", "-c", testCmd}, // No exec bash
		},
		{
			name:         "xfce4-terminal uses hold flag",
			terminal:     "xfce4-terminal",
			cmdLine:      testCmd,
			expectedExe:  "xfce4-terminal",
			expectedArgs: []string{"--hold", "-e", "bash", "-c", testCmd}, // No exec bash
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
			expectedArgs: []string{"-hold", "-e", "bash", "-c", testCmd}, // No exec bash
		},
		{
			name:         "terminator",
			terminal:     "terminator",
			cmdLine:      testCmd,
			expectedExe:  "terminator",
			expectedArgs: []string{"-e", "bash", "-c", keepOpenCmd},
		},
		// Unknown terminal
		{
			name:         "unknown terminal returns empty",
			terminal:     "unknown-terminal",
			cmdLine:      testCmd,
			expectedExe:  "",
			expectedArgs: nil,
		},
		{
			name:         "empty terminal returns empty",
			terminal:     "",
			cmdLine:      testCmd,
			expectedExe:  "",
			expectedArgs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exe, args := buildTerminalArgs(tt.terminal, tt.cmdLine)
			assert.Equal(t, tt.expectedExe, exe)

			switch {
			case tt.checkScript:
				// For AppleScript terminals, verify args structure
				assert.Len(t, args, 2)
				assert.Equal(t, "-e", args[0])
				// Verify script contains the command
				assert.Contains(t, args[1], keepOpenCmd)
			case tt.expectedArgs != nil:
				assert.Equal(t, tt.expectedArgs, args)
			default:
				assert.Nil(t, args)
			}
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

func TestBuildTerminalArgs_SpecialCharactersInCommand(t *testing.T) {
	tests := []struct {
		name    string
		cmdLine string
	}{
		{
			name:    "command with double quotes",
			cmdLine: `echo "hello world"`,
		},
		{
			name:    "command with single quotes",
			cmdLine: `echo 'hello world'`,
		},
		{
			name:    "command with backslashes",
			cmdLine: `echo hello\\world`,
		},
		{
			name:    "command with spaces",
			cmdLine: "/path/to/my program --arg value",
		},
		{
			name:    "command with shell operators",
			cmdLine: "cmd1 && cmd2 || cmd3",
		},
		{
			name:    "command with pipes",
			cmdLine: "echo hello | grep h",
		},
		{
			name:    "command with redirection",
			cmdLine: "echo hello > /tmp/out.txt",
		},
		{
			name:    "command with variables",
			cmdLine: "echo $HOME ${USER}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that all terminals handle the command without panic
			terminals := []string{
				"iterm2", "apple-terminal", "wezterm", "hyper",
				"kitty", "alacritty", "gnome-terminal", "konsole",
				"xfce4-terminal", "mate-terminal", "xterm", "terminator",
			}

			for _, terminal := range terminals {
				exe, args := buildTerminalArgs(terminal, tt.cmdLine)
				assert.NotEmpty(t, exe, "terminal %s should return executable", terminal)
				assert.NotNil(t, args, "terminal %s should return args", terminal)
			}
		})
	}
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

	t.Run("iterm2 checks app bundle", func(_ *testing.T) {
		// We can only verify the logic, not the actual availability
		// The function checks /Applications/iTerm.app
		result := isTerminalAvailable("iterm2")
		// Result depends on whether iTerm is installed
		_ = result // Just verify no panic
	})

	t.Run("nonexistent terminal returns false", func(t *testing.T) {
		result := isTerminalAvailable("definitely-not-a-real-terminal-12345")
		assert.False(t, result)
	})

	t.Run("empty terminal name returns false", func(t *testing.T) {
		result := isTerminalAvailable("")
		assert.False(t, result)
	})
}

func TestIsTerminalAvailable_CrossPlatformTerminals(t *testing.T) {
	// These tests verify the detection logic works without error
	// Actual availability depends on the test environment
	crossPlatformTerminals := []string{
		"wezterm", "hyper", "kitty", "alacritty",
	}

	for _, terminal := range crossPlatformTerminals {
		t.Run(terminal, func(_ *testing.T) {
			// Just verify no panic, result depends on environment
			_ = isTerminalAvailable(terminal)
		})
	}
}

func TestIsTerminalAvailable_LinuxTerminals(t *testing.T) {
	if runtime.GOOS == "darwin" || runtime.GOOS == "windows" {
		t.Skip("Skipping Linux terminal tests on non-Linux platforms")
	}

	linuxTerminals := []string{
		"gnome-terminal", "konsole", "xfce4-terminal",
		"mate-terminal", "xterm", "terminator",
	}

	for _, terminal := range linuxTerminals {
		t.Run(terminal, func(_ *testing.T) {
			// Just verify no panic, result depends on environment
			_ = isTerminalAvailable(terminal)
		})
	}
}

func TestIsTerminalAvailable_WindowsTerminals(t *testing.T) {
	t.Run("apple-terminal not available on Windows", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Skipping Windows-specific test on non-Windows platform")
		}
		result := isTerminalAvailable("apple-terminal")
		assert.False(t, result, "apple-terminal should not be available on Windows")
	})

	t.Run("iterm2 not available on Windows", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Skipping Windows-specific test on non-Windows platform")
		}
		result := isTerminalAvailable("iterm2")
		assert.False(t, result, "iterm2 should not be available on Windows")
	})

	t.Run("Linux terminals typically not in PATH on Windows", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("Skipping Windows-specific test on non-Windows platform")
		}

		linuxTerminals := []string{
			"gnome-terminal", "konsole", "xfce4-terminal",
			"mate-terminal", "xterm", "terminator",
		}

		for _, terminal := range linuxTerminals {
			t.Run(terminal, func(_ *testing.T) {
				// Just verify no panic occurs
				_ = isTerminalAvailable(terminal)
			})
		}
	})
}

// TestTerminalCandidate_Integration tests the integration between
// getTerminalCandidates, isTerminalAvailable, and buildTerminalArgs
func TestTerminalCandidate_Integration(t *testing.T) {
	candidates := getTerminalCandidates()
	testCmd := "echo test"

	for _, candidate := range candidates {
		t.Run(candidate.name, func(t *testing.T) {
			// Verify buildTerminalArgs works for all candidates
			exe, args := buildTerminalArgs(candidate.name, testCmd)
			assert.NotEmpty(t, exe, "buildTerminalArgs should return exe for candidate %s", candidate.name)
			assert.NotNil(t, args, "buildTerminalArgs should return args for candidate %s", candidate.name)

			// Verify isTerminalAvailable doesn't panic for any candidate
			_ = isTerminalAvailable(candidate.name)
		})
	}
}

// TestEscapeAppleScript_SecurityCritical specifically tests security-critical
// escaping scenarios that could lead to AppleScript injection
func TestEscapeAppleScript_SecurityCritical(t *testing.T) {
	tests := []struct {
		name        string
		malicious   string
		description string
	}{
		{
			name:        "script termination attack",
			malicious:   `"; end tell; do shell script "evil`,
			description: "Attempt to terminate AppleScript and inject new command",
		},
		{
			name:        "quote escape bypass",
			malicious:   `\"; do shell script "evil`,
			description: "Attempt to escape the quote with backslash",
		},
		{
			name:        "double escape bypass",
			malicious:   `\\"; do shell script "evil`,
			description: "Attempt double backslash to bypass escaping",
		},
		{
			name:        "unicode with real quotes",
			malicious:   "\u201c; do shell script \"evil\"\u201d",
			description: "Attempt with Unicode quotes mixed with real quotes",
		},
		{
			name:        "newline injection",
			malicious:   "cmd\"\ndo shell script \"evil",
			description: "Attempt to inject via newline",
		},
		{
			name:        "comment injection",
			malicious:   `cmd" -- do shell script "evil`,
			description: "Attempt to use AppleScript comments",
		},
		{
			name:        "concatenation attack",
			malicious:   `cmd" & "evil`,
			description: "Attempt AppleScript string concatenation",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			escaped := escapeAppleScript(tt.malicious)

			// Verify all double quotes are escaped
			// After escaping, there should be no unescaped double quotes
			// An unescaped quote is one not preceded by a backslash,
			// but we need to account for escaped backslashes

			// Simple check: the escaped string should not contain
			// an odd number of backslashes followed by a quote
			// This is a simplified security check

			// More importantly, verify the escaping happened
			assert.NotEqual(t, tt.malicious, escaped, "malicious input should be modified by escaping")

			// Verify double quotes are escaped
			if strings.Contains(tt.malicious, `"`) {
				assert.Contains(t, escaped, `\"`, "double quotes should be escaped")
			}
		})
	}
}

// TestBuildTerminalArgs_EmptyCommand tests handling of empty command
func TestBuildTerminalArgs_EmptyCommand(t *testing.T) {
	terminals := []string{
		"iterm2", "apple-terminal", "wezterm", "hyper",
		"kitty", "alacritty", "gnome-terminal", "konsole",
		"xfce4-terminal", "mate-terminal", "xterm", "terminator",
	}

	for _, terminal := range terminals {
		t.Run(terminal+"_empty_command", func(t *testing.T) {
			exe, args := buildTerminalArgs(terminal, "")
			assert.NotEmpty(t, exe)
			assert.NotNil(t, args)
			// The command will be "; exec bash" for most terminals
			// or just "" for --hold terminals
		})
	}
}

// TestBuildCommand_Windows tests Windows-specific command building.
// These tests document the expected Windows behavior and are skipped on other platforms.
func TestBuildCommand_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows command building tests on non-Windows platform")
	}

	service := &Service{}
	ctx := context.Background()

	t.Run("terminal mode uses cmd.exe with start cmd /k", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: true,
		}
		cmd := service.buildCommand(ctx, program, []string{"arg1", "arg2"})

		assert.Equal(t, "cmd.exe", cmd.Path)
		// Args should be: [cmd.exe, /c, start, "", cmd, /k, C:\Programs\test.exe, arg1, arg2]
		assert.Contains(t, cmd.Args, "/c")
		assert.Contains(t, cmd.Args, "start")
		assert.Contains(t, cmd.Args, "cmd")
		assert.Contains(t, cmd.Args, "/k")
		assert.Contains(t, cmd.Args, "C:\\Programs\\test.exe")
	})

	t.Run("direct mode uses cmd.exe with start /b", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: false,
		}
		cmd := service.buildCommand(ctx, program, []string{"arg1"})

		assert.Equal(t, "cmd.exe", cmd.Path)
		// Args should be: [cmd.exe, /c, start, "", /b, C:\Programs\test.exe, arg1]
		assert.Contains(t, cmd.Args, "/c")
		assert.Contains(t, cmd.Args, "start")
		assert.Contains(t, cmd.Args, "/b")
		assert.Contains(t, cmd.Args, "C:\\Programs\\test.exe")
	})

	t.Run("arguments are passed correctly", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: false,
		}
		args := []string{"--name", "test value", "--hash", "abc123"}
		cmd := service.buildCommand(ctx, program, args)

		for _, arg := range args {
			assert.Contains(t, cmd.Args, arg, "argument %q should be in command", arg)
		}
	})

	t.Run("terminal mode with no arguments", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: true,
		}
		cmd := service.buildCommand(ctx, program, nil)

		assert.Equal(t, "cmd.exe", cmd.Path)
		assert.Contains(t, cmd.Args, "/c")
		assert.Contains(t, cmd.Args, "start")
		assert.Contains(t, cmd.Args, "cmd")
		assert.Contains(t, cmd.Args, "/k")
		assert.Contains(t, cmd.Args, "C:\\Programs\\test.exe")
	})

	t.Run("direct mode with no arguments", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: false,
		}
		cmd := service.buildCommand(ctx, program, nil)

		assert.Equal(t, "cmd.exe", cmd.Path)
		assert.Contains(t, cmd.Args, "/c")
		assert.Contains(t, cmd.Args, "start")
		assert.Contains(t, cmd.Args, "/b")
		assert.Contains(t, cmd.Args, "C:\\Programs\\test.exe")
	})

	t.Run("path with spaces in terminal mode", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Program Files\\My App\\test.exe",
			UseTerminal: true,
		}
		cmd := service.buildCommand(ctx, program, []string{"--arg", "value"})

		assert.Equal(t, "cmd.exe", cmd.Path)
		assert.Contains(t, cmd.Args, "C:\\Program Files\\My App\\test.exe")
	})

	t.Run("path with spaces in direct mode", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Program Files\\My App\\test.exe",
			UseTerminal: false,
		}
		cmd := service.buildCommand(ctx, program, []string{"--arg", "value"})

		assert.Equal(t, "cmd.exe", cmd.Path)
		assert.Contains(t, cmd.Args, "C:\\Program Files\\My App\\test.exe")
	})

	t.Run("arguments with special characters", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "C:\\Programs\\test.exe",
			UseTerminal: false,
		}
		args := []string{"--name", "Test & Value", "--path", "C:\\My Files\\data"}
		cmd := service.buildCommand(ctx, program, args)

		for _, arg := range args {
			assert.Contains(t, cmd.Args, arg, "argument %q should be in command", arg)
		}
	})
}

// TestGetTerminalCandidates_Windows tests Windows-specific terminal candidate behavior.
func TestGetTerminalCandidates_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows terminal candidate tests on non-Windows platform")
	}

	candidates := getTerminalCandidates()

	t.Run("cross-platform terminals are present", func(t *testing.T) {
		// First 4 should always be cross-platform terminals
		assert.Equal(t, "wezterm", candidates[0].name)
		assert.Equal(t, "hyper", candidates[1].name)
		assert.Equal(t, "kitty", candidates[2].name)
		assert.Equal(t, "alacritty", candidates[3].name)
	})

	t.Run("macOS terminals are not present", func(t *testing.T) {
		for _, c := range candidates {
			assert.NotEqual(t, "iterm2", c.name, "Windows should not have iterm2")
			assert.NotEqual(t, "apple-terminal", c.name, "Windows should not have apple-terminal")
		}
	})

	t.Run("Linux terminals are present in candidate list", func(t *testing.T) {
		// On Windows, Linux terminals are in the candidate list but won't be available
		var hasGnomeTerminal, hasXterm bool
		for _, c := range candidates {
			if c.name == "gnome-terminal" {
				hasGnomeTerminal = true
			}
			if c.name == "xterm" {
				hasXterm = true
			}
		}
		assert.True(t, hasGnomeTerminal, "gnome-terminal should be in candidate list")
		assert.True(t, hasXterm, "xterm should be in candidate list")
	})
}

// TestNormalizePathCase_Windows tests Windows-specific path case normalization.
func TestNormalizePathCase_Windows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows path case tests on non-Windows platform")
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "uppercase path becomes lowercase",
			input:    "C:\\USERS\\TEST\\FILE.EXE",
			expected: "c:\\users\\test\\file.exe",
		},
		{
			name:     "mixed case path becomes lowercase",
			input:    "C:\\Users\\Test\\File.exe",
			expected: "c:\\users\\test\\file.exe",
		},
		{
			name:     "already lowercase unchanged",
			input:    "c:\\users\\test\\file.exe",
			expected: "c:\\users\\test\\file.exe",
		},
		{
			name:     "path with spaces",
			input:    "C:\\Program Files\\My App\\Test.EXE",
			expected: "c:\\program files\\my app\\test.exe",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizePathCase(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// TestIsPathAllowed_Windows tests Windows-specific path allowlist behavior.
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

		// Different cases should match due to case-insensitive comparison
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

// TestBuildCommand_Unix tests Unix-specific command building.
// These tests verify the current platform behavior.
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
		cmd := service.buildCommand(ctx, program, []string{"arg1", "arg2"})

		assert.Equal(t, "/usr/bin/test", cmd.Path)
		assert.Equal(t, []string{"/usr/bin/test", "arg1", "arg2"}, cmd.Args)
	})

	t.Run("direct mode with no args", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "/usr/bin/test",
			UseTerminal: false,
		}
		cmd := service.buildCommand(ctx, program, nil)

		assert.Equal(t, "/usr/bin/test", cmd.Path)
		assert.Equal(t, []string{"/usr/bin/test"}, cmd.Args)
	})

	t.Run("terminal mode uses terminal emulator", func(t *testing.T) {
		program := &models.ExternalProgram{
			Path:        "/usr/bin/test",
			UseTerminal: true,
		}
		cmd := service.buildCommand(ctx, program, []string{"arg1"})

		// On Unix, terminal mode should use a terminal emulator
		// The exact emulator depends on what's available
		assert.NotNil(t, cmd)
		assert.NotEmpty(t, cmd.Path)
	})
}
