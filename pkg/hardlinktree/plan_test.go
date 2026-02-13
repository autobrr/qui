// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlinktree

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildPlan_SingleFile(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "movie.mkv", Size: 1000000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/movie.mkv", RelPath: "movie.mkv", Size: 1000000},
	}

	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Movie", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if plan.RootDir != "/dest" {
		t.Errorf("RootDir = %q, want %q", plan.RootDir, "/dest")
	}
	if len(plan.Files) != 1 {
		t.Fatalf("Expected 1 file plan, got %d", len(plan.Files))
	}
	if plan.Files[0].SourcePath != "/downloads/movie.mkv" {
		t.Errorf("SourcePath = %q, want %q", plan.Files[0].SourcePath, "/downloads/movie.mkv")
	}
	expectedTarget := filepath.Join("/dest", "movie.mkv")
	if plan.Files[0].TargetPath != expectedTarget {
		t.Errorf("TargetPath = %q, want %q", plan.Files[0].TargetPath, expectedTarget)
	}
}

func TestBuildPlan_MultipleFiles(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "Show/S01E01.mkv", Size: 500000},
		{Path: "Show/S01E02.mkv", Size: 600000},
		{Path: "Show/S01E03.mkv", Size: 550000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/Show/S01E01.mkv", RelPath: "Show/S01E01.mkv", Size: 500000},
		{AbsPath: "/downloads/Show/S01E02.mkv", RelPath: "Show/S01E02.mkv", Size: 600000},
		{AbsPath: "/downloads/Show/S01E03.mkv", RelPath: "Show/S01E03.mkv", Size: 550000},
	}

	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Show.S01", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if len(plan.Files) != 3 {
		t.Fatalf("Expected 3 file plans, got %d", len(plan.Files))
	}
}

func TestBuildPlan_SubfolderLayout_SingleFile(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "movie.mkv", Size: 1000000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/movie.mkv", RelPath: "movie.mkv", Size: 1000000},
	}

	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutSubfolder, "Movie.2024.mkv", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	// Subfolder layout with single file strips extension from folder name
	expectedTarget := filepath.Join("/dest", "Movie.2024", "movie.mkv")
	if plan.Files[0].TargetPath != expectedTarget {
		t.Errorf("TargetPath = %q, want %q", plan.Files[0].TargetPath, expectedTarget)
	}
}

func TestBuildPlan_SubfolderLayout_MultipleFiles(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "Show/S01E01.mkv", Size: 500000},
		{Path: "Show/S01E02.mkv", Size: 600000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/Show/S01E01.mkv", RelPath: "Show/S01E01.mkv", Size: 500000},
		{AbsPath: "/downloads/Show/S01E02.mkv", RelPath: "Show/S01E02.mkv", Size: 600000},
	}

	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutSubfolder, "Show.S01", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	// Subfolder layout wraps in torrent name folder
	for _, fp := range plan.Files {
		if !filepath.HasPrefix(fp.TargetPath, filepath.Join("/dest", "Show.S01")) {
			t.Errorf("TargetPath %q should start with %q", fp.TargetPath, filepath.Join("/dest", "Show.S01"))
		}
	}
}

func TestBuildPlan_SubfolderLayout_SanitizesIllegalChars(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "movie.mkv", Size: 1000000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/movie.mkv", RelPath: "movie.mkv", Size: 1000000},
	}

	// Torrent name with Windows-illegal characters: < > : " / \ | ? *
	torrentName := "Movie: The Sequel? Part 2 <2024>.mkv"
	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutSubfolder, torrentName, "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	// Folder name should be sanitized to remove illegal characters
	// The file path inside should remain unchanged
	targetPath := plan.Files[0].TargetPath

	// Should NOT contain any Windows-illegal characters in the path
	illegalChars := []string{"<", ">", ":", "\"", "|", "?", "*"}
	for _, c := range illegalChars {
		// Check only the folder segment (between dest and movie.mkv)
		if filepath.Dir(targetPath) != "/dest" && containsChar(filepath.Dir(targetPath), c) {
			t.Errorf("TargetPath folder %q should not contain illegal character %q", filepath.Dir(targetPath), c)
		}
	}

	// Should still contain the file name unchanged
	if filepath.Base(targetPath) != "movie.mkv" {
		t.Errorf("File name should be preserved, got %q", filepath.Base(targetPath))
	}
}

// containsChar checks if a string contains a character
func containsChar(s, char string) bool {
	return strings.Contains(s, char)
}

func TestBuildPlan_NoSubfolderLayout(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "Show/S01E01.mkv", Size: 500000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/Show/S01E01.mkv", RelPath: "Show/S01E01.mkv", Size: 500000},
	}

	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutNoSubfolder, "Show.S01", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	// NoSubfolder strips the first directory component
	expectedTarget := filepath.Join("/dest", "S01E01.mkv")
	if plan.Files[0].TargetPath != expectedTarget {
		t.Errorf("TargetPath = %q, want %q", plan.Files[0].TargetPath, expectedTarget)
	}
}

func TestBuildPlan_SizeMatching(t *testing.T) {
	// Files with same names but different sizes
	candidateFiles := []TorrentFile{
		{Path: "file1.mkv", Size: 1000},
		{Path: "file2.mkv", Size: 2000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/file2.mkv", RelPath: "file2.mkv", Size: 2000},
		{AbsPath: "/downloads/file1.mkv", RelPath: "file1.mkv", Size: 1000},
	}

	plan, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Test", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	if len(plan.Files) != 2 {
		t.Fatalf("Expected 2 file plans, got %d", len(plan.Files))
	}

	// Verify size-based matching
	for _, fp := range plan.Files {
		if filepath.Base(fp.TargetPath) == "file1.mkv" && fp.SourcePath != "/downloads/file1.mkv" {
			t.Errorf("file1.mkv should map to /downloads/file1.mkv, got %s", fp.SourcePath)
		}
		if filepath.Base(fp.TargetPath) == "file2.mkv" && fp.SourcePath != "/downloads/file2.mkv" {
			t.Errorf("file2.mkv should map to /downloads/file2.mkv, got %s", fp.SourcePath)
		}
	}
}

func TestBuildPlan_NoCandidateFiles(t *testing.T) {
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/movie.mkv", RelPath: "movie.mkv", Size: 1000000},
	}

	_, err := BuildPlan(nil, existingFiles, LayoutOriginal, "Movie", "/dest")
	if err == nil {
		t.Error("Expected error for empty candidate files")
	}
}

func TestBuildPlan_NoExistingFiles(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "movie.mkv", Size: 1000000},
	}

	_, err := BuildPlan(candidateFiles, nil, LayoutOriginal, "Movie", "/dest")
	if err == nil {
		t.Error("Expected error for empty existing files")
	}
}

func TestBuildPlan_NoMatchingFile(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "movie.mkv", Size: 1000000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/other.mkv", RelPath: "other.mkv", Size: 2000000}, // Different size
	}

	_, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Movie", "/dest")
	if err == nil {
		t.Error("Expected error when no matching file for size")
	}
}

func TestBuildPlan_EmptyDestDir(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "movie.mkv", Size: 1000000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/movie.mkv", RelPath: "movie.mkv", Size: 1000000},
	}

	_, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Movie", "")
	if err == nil {
		t.Error("Expected error for empty destination directory")
	}
}

func TestBuildPlan_RejectsPathTraversal(t *testing.T) {
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/movie.mkv", RelPath: "movie.mkv", Size: 1000000},
	}

	tests := []struct {
		name          string
		candidatePath string
		wantError     bool
	}{
		{
			name:          "normal path",
			candidatePath: "movie.mkv",
			wantError:     false,
		},
		{
			name:          "nested path",
			candidatePath: "Folder/movie.mkv",
			wantError:     false,
		},
		{
			name:          "parent directory escape",
			candidatePath: "../outside/movie.mkv",
			wantError:     true,
		},
		{
			name:          "parent directory only",
			candidatePath: "..",
			wantError:     true,
		},
		{
			name:          "nested parent escape",
			candidatePath: "Folder/../../outside/movie.mkv",
			wantError:     true,
		},
		{
			name:          "absolute path unix",
			candidatePath: "/etc/passwd",
			wantError:     true,
		},
		{
			name:          "current directory",
			candidatePath: ".",
			wantError:     true,
		},
		{
			name:          "empty path",
			candidatePath: "",
			wantError:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidateFiles := []TorrentFile{
				{Path: tt.candidatePath, Size: 1000000},
			}

			_, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Test", "/dest")
			if tt.wantError && err == nil {
				t.Errorf("BuildPlan should reject path %q but didn't", tt.candidatePath)
			}
			if !tt.wantError && err != nil {
				t.Errorf("BuildPlan should accept path %q but got error: %v", tt.candidatePath, err)
			}
		})
	}
}

func TestBuildPlan_Deterministic(t *testing.T) {
	candidateFiles := []TorrentFile{
		{Path: "Show/S01E03.mkv", Size: 550000},
		{Path: "Show/S01E01.mkv", Size: 500000},
		{Path: "Show/S01E02.mkv", Size: 600000},
	}
	existingFiles := []ExistingFile{
		{AbsPath: "/downloads/Show/S01E02.mkv", RelPath: "Show/S01E02.mkv", Size: 600000},
		{AbsPath: "/downloads/Show/S01E03.mkv", RelPath: "Show/S01E03.mkv", Size: 550000},
		{AbsPath: "/downloads/Show/S01E01.mkv", RelPath: "Show/S01E01.mkv", Size: 500000},
	}

	plan1, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Show.S01", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	plan2, err := BuildPlan(candidateFiles, existingFiles, LayoutOriginal, "Show.S01", "/dest")
	if err != nil {
		t.Fatalf("BuildPlan error: %v", err)
	}

	// Plans should be identical
	if len(plan1.Files) != len(plan2.Files) {
		t.Fatalf("Plan lengths differ: %d vs %d", len(plan1.Files), len(plan2.Files))
	}

	for i := range plan1.Files {
		if plan1.Files[i].SourcePath != plan2.Files[i].SourcePath ||
			plan1.Files[i].TargetPath != plan2.Files[i].TargetPath {
			t.Errorf("Plans differ at index %d", i)
		}
	}
}

func TestNormalizeFileKey(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected string
	}{
		{
			name:     "simple file",
			path:     "movie.mkv",
			expected: "movie.mkv",
		},
		{
			name:     "with path",
			path:     "Show/S01E01.mkv",
			expected: "s01e01.mkv",
		},
		{
			name:     "with punctuation",
			path:     "Movie.2024.1080p.BluRay.mkv",
			expected: "movie20241080pbluray.mkv", // strips non-alphanumeric (periods, etc.)
		},
		{
			name:     "sidecar file with video extension",
			path:     "movie.mkv.nfo",
			expected: "movie.nfo",
		},
		{
			name:     "srt subtitle",
			path:     "movie.mkv.srt",
			expected: "movie.srt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeFileKey(tt.path)
			if result != tt.expected {
				t.Errorf("normalizeFileKey(%q) = %q, want %q", tt.path, result, tt.expected)
			}
		})
	}
}

func TestHasCommonRootFolder(t *testing.T) {
	tests := []struct {
		name     string
		files    []TorrentFile
		expected bool
	}{
		{
			name:     "empty files",
			files:    []TorrentFile{},
			expected: false,
		},
		{
			name: "single file at root",
			files: []TorrentFile{
				{Path: "movie.mkv", Size: 1000},
			},
			expected: false,
		},
		{
			name: "single file in folder",
			files: []TorrentFile{
				{Path: "Movie/movie.mkv", Size: 1000},
			},
			expected: true,
		},
		{
			name: "multiple files at root",
			files: []TorrentFile{
				{Path: "video.mkv", Size: 1000},
				{Path: "subs.srt", Size: 100},
			},
			expected: false,
		},
		{
			name: "multiple files in same folder",
			files: []TorrentFile{
				{Path: "Movie/video.mkv", Size: 1000},
				{Path: "Movie/subs.srt", Size: 100},
			},
			expected: true,
		},
		{
			name: "multiple files in different folders",
			files: []TorrentFile{
				{Path: "Movie/video.mkv", Size: 1000},
				{Path: "Extras/trailer.mkv", Size: 500},
			},
			expected: false,
		},
		{
			name: "nested folders with common root",
			files: []TorrentFile{
				{Path: "Show/Season 1/S01E01.mkv", Size: 1000},
				{Path: "Show/Season 1/S01E02.mkv", Size: 1000},
				{Path: "Show/Season 2/S02E01.mkv", Size: 1000},
			},
			expected: true,
		},
		{
			name: "mixed root and folder",
			files: []TorrentFile{
				{Path: "readme.txt", Size: 100},
				{Path: "Movie/video.mkv", Size: 1000},
			},
			expected: false,
		},
		{
			name: "forward slash path separator",
			files: []TorrentFile{
				{Path: "Folder/file1.mkv", Size: 1000},
				{Path: "Folder/file2.mkv", Size: 1000},
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := HasCommonRootFolder(tt.files)
			if result != tt.expected {
				t.Errorf("HasCommonRootFolder() = %v, want %v", result, tt.expected)
			}
		})
	}
}
