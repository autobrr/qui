// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package pathutil provides cross-platform path sanitization utilities.
package pathutil

import (
	"regexp"
	"strings"
)

// windowsReservedNames contains device names that are reserved on Windows.
// These cannot be used as filenames regardless of extension.
var windowsReservedNames = map[string]bool{
	"CON": true, "PRN": true, "AUX": true, "NUL": true,
	"COM1": true, "COM2": true, "COM3": true, "COM4": true,
	"COM5": true, "COM6": true, "COM7": true, "COM8": true, "COM9": true,
	"LPT1": true, "LPT2": true, "LPT3": true, "LPT4": true,
	"LPT5": true, "LPT6": true, "LPT7": true, "LPT8": true, "LPT9": true,
}

// illegalCharsRegex matches characters that are illegal in Windows filenames.
// These are: < > : " / \ | ? *
var illegalCharsRegex = regexp.MustCompile(`[<>:"/\\|?*]`)

// controlCharsRegex matches ASCII control characters (0x00-0x1F).
var controlCharsRegex = regexp.MustCompile(`[\x00-\x1f]`)

// SanitizePathSegment sanitizes a path segment (directory or file name) to be
// safe for use across platforms (Unix, Windows, macOS).
//
// It performs the following transformations:
//   - Removes characters illegal in Windows: < > : " / \ | ? *
//   - Removes ASCII control characters (0x00-0x1F)
//   - Removes trailing dots and spaces (Windows restriction)
//   - Prefixes Windows reserved names (CON, PRN, etc.) with underscore
//   - Returns "_" if the result would be empty
func SanitizePathSegment(name string) string {
	if name == "" {
		return "_"
	}

	// Remove illegal characters
	result := illegalCharsRegex.ReplaceAllString(name, "")

	// Remove control characters
	result = controlCharsRegex.ReplaceAllString(result, "")

	// Remove trailing dots and spaces (Windows restriction)
	result = strings.TrimRight(result, ". ")

	// Handle empty result
	if result == "" {
		return "_"
	}

	// Check for Windows reserved names (case-insensitive)
	// Check both with and without extension (e.g., "CON.txt" is also reserved)
	upper := strings.ToUpper(result)
	baseName := upper
	if idx := strings.Index(upper, "."); idx > 0 {
		baseName = upper[:idx]
	}
	if windowsReservedNames[baseName] {
		result = "_" + result
	}

	return result
}

// TorrentKey generates a stable, filesystem-safe directory key for a torrent.
// Uses the infohash as the primary identifier, falling back to sanitized name.
// The key is suitable for use as a directory name on any platform.
func TorrentKey(infohash, name string) string {
	// Prefer infohash as it's guaranteed unique and filesystem-safe
	if infohash != "" {
		// Infohash is already hex (a-f0-9), but lowercase it for consistency
		return strings.ToLower(infohash)
	}

	// Fallback to sanitized name
	if name != "" {
		return SanitizePathSegment(name)
	}

	return "_unknown"
}

// IsolationFolderName generates a human-readable isolation folder name for
// hardlink cross-seeding. Format: <sanitized-name>--<shortHash>
//
// This is used when torrents need isolation (e.g., rootless torrents with
// Original layout, or NoSubfolder layout) to prevent file conflicts while
// keeping the folder name readable and unique.
//
// Example: "My.Movie.2024.1080p.BluRay--abcdef12"
func IsolationFolderName(infohash, name string) string {
	sanitizedName := SanitizePathSegment(name)

	// Truncate very long names to keep folder name reasonable
	const maxNameLen = 80
	if len(sanitizedName) > maxNameLen {
		sanitizedName = sanitizedName[:maxNameLen]
		// Remove trailing dots/spaces after truncation
		sanitizedName = strings.TrimRight(sanitizedName, ". ")
	}

	// Use first 8 chars of infohash as short hash
	shortHash := ""
	if len(infohash) >= 8 {
		shortHash = strings.ToLower(infohash[:8])
	} else if infohash != "" {
		shortHash = strings.ToLower(infohash)
	}

	if shortHash == "" {
		return sanitizedName
	}

	return sanitizedName + "--" + shortHash
}
