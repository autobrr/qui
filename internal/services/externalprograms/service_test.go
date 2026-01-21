// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package externalprograms

import (
	"context"
	"errors"
	"path/filepath"
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

func (m *mockProgramStore) GetByID(ctx context.Context, id int) (*models.ExternalProgram, error) {
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

func (m *mockActivityStore) Create(ctx context.Context, activity *models.AutomationActivity) error {
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
	var s *Service = nil

	result := s.Execute(context.Background(), ExecuteRequest{
		ProgramID:  1,
		Torrent:    &qbt.Torrent{Hash: "abc123"},
		InstanceID: 1,
	})

	assert.False(t, result.Success)
	assert.Error(t, result.Error)
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
	assert.Error(t, result.Error)
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
	assert.Error(t, result.Error)
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
	assert.Error(t, result.Error)
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
	assert.Error(t, result.Error)
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
	assert.Error(t, result.Error)
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
	var s *Service = nil
	// This would panic, so we test that a properly initialized service handles nil config
	s = &Service{config: nil}
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

	assert.Equal(t, "", data["hash"])
	assert.Equal(t, "", data["name"])
	assert.Equal(t, "", data["save_path"])
	assert.Equal(t, "", data["category"])
	assert.Equal(t, "", data["tags"])
	assert.Equal(t, "", data["state"])
	assert.Equal(t, "0", data["size"])
	assert.Equal(t, "0.00", data["progress"])
	assert.Equal(t, "", data["content_path"])
	assert.Equal(t, "", data["comment"])
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
				RuleID:     intPtr(42),
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
		assert.Nil(t, result.Error)
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
func intPtr(i int) *int {
	return &i
}
