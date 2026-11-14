// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package pathutil provides cross-platform path sanitization utilities.
package pathutil

import (
	"regexp"
	"strings"
	"unicode/utf8"
)

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

	if IsWindowsDeviceName(result) {
		result = "_" + result
	}

	return result
}

// IsWindowsDeviceName reports whether a path component is a reserved Windows
// device name, ignoring its extension and trailing spaces.
func IsWindowsDeviceName(component string) bool {
	base, _, _ := strings.Cut(component, ".")
	base = strings.TrimRight(base, " ")
	for _, name := range []string{"CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$"} {
		if strings.EqualFold(base, name) {
			return true
		}
	}
	if len(base) < 4 || (!strings.EqualFold(base[:3], "COM") && !strings.EqualFold(base[:3], "LPT")) {
		return false
	}
	switch n := base[3:]; n {
	case "\u00b9", "\u00b2", "\u00b3":
		return true
	default:
		return len(n) == 1 && n[0] >= '1' && n[0] <= '9'
	}
}

// TorrentPathComponent maps one component of a torrent's file path to the name
// libtorrent, and therefore qBittorrent, puts on disk for it.
//
// libtorrent's sanitize_append_path_element replaces an empty component with "_"
// instead of dropping it, on every release line qBittorrent ships (1.1, 1.2, 2.0).
// Go's path.Join drops it, which would place a link tree one directory above where
// qBittorrent looks for the data. Only this rule is mirrored: libtorrent's handling
// of "." and ".." differs between 1.x and 2.0, so copying it would produce wrong
// paths for whichever version we did not target.
func TorrentPathComponent(component string) string {
	if component == "" {
		return "_"
	}
	return component
}

// maxIsolationNameBytes keeps room for "--<shortHash>" in a folder name of
// 255 bytes, the per-name limit of ext4, Btrfs, ZFS, and NTFS. 255 UTF-8 bytes
// are never more than 255 UTF-16 units, so the limit also holds on NTFS.
const maxIsolationNameBytes = 255 - len("--") - 8

// IsolationFolder returns the name of the isolation folder for a torrent: the
// sanitized torrent name, cut at a full character to fit 255 bytes with room
// for the hash of HashedIsolationFolder.
func IsolationFolder(name string) string {
	result := SanitizePathSegment(name)
	if len(result) <= maxIsolationNameBytes {
		return result
	}
	cut := maxIsolationNameBytes
	for cut > 0 && !utf8.RuneStart(result[cut]) {
		cut--
	}
	if result = strings.TrimRight(result[:cut], ". "); result == "" {
		return "_"
	}
	return result
}

// HashedIsolationFolder returns <IsolationFolder>--<shortHash>, where shortHash
// is the first 8 characters of infohash, lowercased. It is the isolation
// folder for a torrent whose plain name is taken. It is also the automations
// template variable {{.IsolationFolderName}}.
func HashedIsolationFolder(infohash, name string) string {
	return IsolationFolder(name) + "--" + strings.ToLower(infohash[:min(len(infohash), 8)])
}
