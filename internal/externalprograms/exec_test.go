package externalprograms

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
)

// TestExecute_NilParams tests that Execute handles nil parameters gracefully.
func TestExecute_NilParams(t *testing.T) {
	t.Parallel()

	ctx := context.Background()

	tests := []struct {
		name    string
		program *models.ExternalProgram
		torrent *qbt.Torrent
	}{
		{
			name:    "nil program",
			program: nil,
			torrent: &qbt.Torrent{Hash: "abc123"},
		},
		{
			name:    "nil torrent",
			program: &models.ExternalProgram{ID: 1, Name: "Test", Path: "/bin/echo"},
			torrent: nil,
		},
		{
			name:    "both nil",
			program: nil,
			torrent: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			result := Execute(ctx, tc.program, tc.torrent, DefaultOptions())
			if result.Error == nil {
				t.Error("Execute() expected error for nil params, got nil")
			}
			if result.Started {
				t.Error("Execute() expected Started=false for nil params")
			}
		})
	}
}

// TestExecute_PathAllowlistBlocked tests that Execute blocks disallowed paths.
func TestExecute_PathAllowlistBlocked(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:      1,
		Name:    "Test",
		Path:    "/usr/bin/malicious",
		Enabled: true,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode:      ModeSync,
		AllowList: []string{"/opt/allowed"},
	}

	result := Execute(ctx, program, torrent, opts)
	if result.Error == nil {
		t.Error("Execute() expected error for blocked path, got nil")
	}
	if result.Started {
		t.Error("Execute() expected Started=false for blocked path")
	}
	if !strings.Contains(result.Error.Error(), "not allowed") {
		t.Errorf("Execute() error should mention 'not allowed', got: %v", result.Error)
	}
}

// TestExecute_SyncMode tests synchronous execution.
func TestExecute_SyncMode(t *testing.T) {
	t.Parallel()

	// Skip on Windows as /bin/echo doesn't exist
	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Echo Test",
		Path:         "/bin/echo",
		ArgsTemplate: "hello {hash}",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode:          ModeSync,
		CaptureOutput: true,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	if !result.Completed {
		t.Error("Execute() expected Completed=true in sync mode")
	}
	if result.ExitCode != 0 {
		t.Errorf("Execute() expected ExitCode=0, got %d", result.ExitCode)
	}
	if result.Error != nil {
		t.Errorf("Execute() unexpected error: %v", result.Error)
	}
	if !strings.Contains(result.Stdout, "hello abc123") {
		t.Errorf("Execute() expected stdout to contain 'hello abc123', got: %q", result.Stdout)
	}
}

// TestExecute_SyncMode_NonZeroExit tests sync execution with non-zero exit code.
func TestExecute_SyncMode_NonZeroExit(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:           1,
		Name:         "False Test",
		Path:         "/usr/bin/false",
		ArgsTemplate: "",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode:          ModeSync,
		CaptureOutput: true,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	if !result.Completed {
		t.Error("Execute() expected Completed=true in sync mode")
	}
	if result.ExitCode == 0 {
		t.Errorf("Execute() expected non-zero ExitCode, got %d", result.ExitCode)
	}
	// Non-zero exit should set Error
	if result.Error == nil {
		t.Error("Execute() expected error for non-zero exit")
	}
}

// TestExecute_AsyncMode tests asynchronous execution.
func TestExecute_AsyncMode(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Sleep Test",
		Path:         "/bin/sleep",
		ArgsTemplate: "0.1",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode: ModeAsync,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	// In async mode, Completed should be false as we don't wait
	if result.Completed {
		t.Error("Execute() expected Completed=false in async mode")
	}
	if result.Error != nil {
		t.Errorf("Execute() unexpected error: %v", result.Error)
	}

	// Wait a bit for the process to complete in the background
	time.Sleep(200 * time.Millisecond)
}

// TestExecute_ContextCancellation tests that context cancellation stops execution.
func TestExecute_ContextCancellation(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Long Sleep",
		Path:         "/bin/sleep",
		ArgsTemplate: "10",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode:          ModeSync,
		CaptureOutput: true,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	// Should be killed by context timeout
	if result.Error == nil {
		t.Error("Execute() expected error from context cancellation")
	}
	if !errors.Is(result.Error, context.DeadlineExceeded) {
		t.Errorf("Execute() expected DeadlineExceeded error, got: %v", result.Error)
	}
}

// TestExecute_InvalidProgram tests execution of non-existent program.
func TestExecute_InvalidProgram(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Non-existent",
		Path:         "/nonexistent/path/to/program",
		ArgsTemplate: "",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode: ModeSync,
	}

	result := Execute(ctx, program, torrent, opts)
	if result.Started {
		t.Error("Execute() expected Started=false for non-existent program")
	}
	if result.Error == nil {
		t.Error("Execute() expected error for non-existent program")
	}
}

// TestDefaultOptions tests that DefaultOptions returns expected defaults.
func TestDefaultOptions(t *testing.T) {
	t.Parallel()

	opts := DefaultOptions()
	if opts.Mode != ModeAsync {
		t.Errorf("DefaultOptions().Mode expected ModeAsync, got %v", opts.Mode)
	}
	if opts.CaptureOutput {
		t.Error("DefaultOptions().CaptureOutput expected false")
	}
	if len(opts.AllowList) != 0 {
		t.Errorf("DefaultOptions().AllowList expected empty, got %v", opts.AllowList)
	}
}

// TestIsPathAllowed tests the path allowlist validation.
func TestIsPathAllowed(t *testing.T) {
	t.Parallel()

	// Create a temp directory for testing
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "script.sh")

	tests := []struct {
		name      string
		path      string
		allowList []string
		want      bool
	}{
		{
			name:      "empty path",
			path:      "",
			allowList: []string{tempDir},
			want:      false,
		},
		{
			name:      "whitespace only path",
			path:      "   ",
			allowList: []string{tempDir},
			want:      false,
		},
		{
			name:      "empty allowlist allows all",
			path:      scriptPath,
			allowList: nil,
			want:      true,
		},
		{
			name:      "empty slice allowlist allows all",
			path:      scriptPath,
			allowList: []string{},
			want:      true,
		},
		{
			name:      "exact path match",
			path:      scriptPath,
			allowList: []string{scriptPath},
			want:      true,
		},
		{
			name:      "directory prefix match",
			path:      scriptPath,
			allowList: []string{tempDir},
			want:      true,
		},
		{
			name:      "path not in allowlist",
			path:      "/some/other/path",
			allowList: []string{tempDir},
			want:      false,
		},
		{
			name:      "similar prefix not matched (no boundary)",
			path:      tempDir + "-backup/script.sh",
			allowList: []string{tempDir},
			want:      false,
		},
		{
			name:      "multiple allowlist entries - first matches",
			path:      scriptPath,
			allowList: []string{tempDir, "/other/path"},
			want:      true,
		},
		{
			name:      "multiple allowlist entries - second matches",
			path:      "/other/path/script.sh",
			allowList: []string{tempDir, "/other/path"},
			want:      true,
		},
		{
			name:      "whitespace in allowlist entries trimmed",
			path:      scriptPath,
			allowList: []string{"  " + tempDir + "  "},
			want:      true,
		},
		{
			name:      "empty allowlist entries skipped",
			path:      scriptPath,
			allowList: []string{"", "  ", tempDir},
			want:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsPathAllowed(tc.path, tc.allowList)
			if got != tc.want {
				t.Errorf("IsPathAllowed(%q, %v) = %v, want %v", tc.path, tc.allowList, got, tc.want)
			}
		})
	}
}

// TestIsPathAllowed_Symlinks tests allowlist validation with symlinks.
func TestIsPathAllowed_Symlinks(t *testing.T) {
	t.Parallel()

	// Skip on Windows as symlink behavior differs
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink test on Windows")
	}

	tempDir := t.TempDir()
	realDir := filepath.Join(tempDir, "real")
	linkDir := filepath.Join(tempDir, "link")
	scriptPath := filepath.Join(realDir, "script.sh")

	// Create real directory
	if err := os.MkdirAll(realDir, 0o755); err != nil {
		t.Fatalf("Failed to create real dir: %v", err)
	}

	// Create script file
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho test"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("Failed to create script: %v", err)
	}

	// Create symlink
	if err := os.Symlink(realDir, linkDir); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	linkScript := filepath.Join(linkDir, "script.sh")

	tests := []struct {
		name      string
		path      string
		allowList []string
		want      bool
	}{
		{
			name:      "allow real path, access via symlink",
			path:      linkScript,
			allowList: []string{realDir},
			want:      true, // Should resolve symlink
		},
		{
			name:      "allow symlink path, access via symlink",
			path:      linkScript,
			allowList: []string{linkDir},
			want:      true,
		},
		{
			name:      "allow real path, access via real",
			path:      scriptPath,
			allowList: []string{realDir},
			want:      true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := IsPathAllowed(tc.path, tc.allowList)
			if got != tc.want {
				t.Errorf("IsPathAllowed(%q, %v) = %v, want %v", tc.path, tc.allowList, got, tc.want)
			}
		})
	}
}

// TestExecute_StderrCapture tests that stderr is captured in sync mode.
func TestExecute_StderrCapture(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	// Check if bash is available
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found, skipping test")
	}

	ctx := context.Background()

	// Create a temp script that writes to stderr
	tempDir := t.TempDir()
	scriptPath := filepath.Join(tempDir, "stderr_test.sh")
	if err := os.WriteFile(scriptPath, []byte("#!/bin/bash\necho 'error message' >&2\n"), 0o755); err != nil { //nolint:gosec
		t.Fatalf("Failed to create script: %v", err)
	}

	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Stderr Test",
		Path:         scriptPath,
		ArgsTemplate: "",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode:          ModeSync,
		CaptureOutput: true,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	if !strings.Contains(result.Stderr, "error message") {
		t.Errorf("Execute() expected stderr to contain 'error message', got: %q", result.Stderr)
	}
}

// TestExecute_DurationTracking tests that execution duration is tracked.
func TestExecute_DurationTracking(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Duration Test",
		Path:         "/bin/sleep",
		ArgsTemplate: "0.1",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{Hash: "abc123", Name: "Test Torrent"}

	opts := ExecuteOptions{
		Mode: ModeSync,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	// Duration should be at least 100ms for sleep 0.1
	if result.Duration < 100*time.Millisecond {
		t.Errorf("Execute() expected Duration >= 100ms, got %v", result.Duration)
	}
	// But not too long
	if result.Duration > 500*time.Millisecond {
		t.Errorf("Execute() expected Duration < 500ms, got %v", result.Duration)
	}
}

// TestExecute_ArgumentSubstitution tests that torrent data is substituted in arguments.
func TestExecute_ArgumentSubstitution(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("Skipping on Windows")
	}

	ctx := context.Background()
	program := &models.ExternalProgram{
		ID:           1,
		Name:         "Args Test",
		Path:         "/bin/echo",
		ArgsTemplate: "{hash} {name} {category}",
		Enabled:      true,
		UseTerminal:  false,
	}
	torrent := &qbt.Torrent{
		Hash:     "deadbeef",
		Name:     "My Torrent",
		Category: "movies",
	}

	opts := ExecuteOptions{
		Mode:          ModeSync,
		CaptureOutput: true,
	}

	result := Execute(ctx, program, torrent, opts)
	if !result.Started {
		t.Errorf("Execute() expected Started=true, got false. Error: %v", result.Error)
	}
	expected := "deadbeef My Torrent movies"
	if !strings.Contains(result.Stdout, expected) {
		t.Errorf("Execute() expected stdout to contain %q, got: %q", expected, result.Stdout)
	}
}
