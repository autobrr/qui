// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

import (
	"errors"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/autobrr/qui/pkg/stringutils"
)

var illegalCharsRegex = regexp.MustCompile(`[<>:"/\\|?*]`)

var controlCharsRegex = regexp.MustCompile(`[\x00-\x1f]`)

func SanitizePathSegment(name string) string {
	if name == "" {
		return "_"
	}

	result := illegalCharsRegex.ReplaceAllString(name, "")

	result = controlCharsRegex.ReplaceAllString(result, "")

	result = strings.TrimRight(result, ". ")

	if result == "" {
		return "_"
	}

	if IsWindowsDeviceName(result) {
		result = "_" + result
	}

	return result
}

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

// errFilteredEmptyPath refuses a name that only directional-character removal
// emptied: libtorrent 1.2 and 2.0 place such a name differently.
var errFilteredEmptyPath = errors.New("torrent path is empty after removing directional characters")

// TorrentPathComponent maps one component of a torrent's file path to the name
// libtorrent, and therefore qBittorrent, puts on disk for it. It returns an error
// when the component is not empty but TorrentPathName empties it.
//
// libtorrent's sanitize_append_path_element replaces an empty component with "_"
// instead of dropping it, on every release line qBittorrent ships (1.1, 1.2, 2.0).
// Go's path.Join drops it, which would place a link tree one directory above where
// qBittorrent looks for the data. This and directional-character removal are
// mirrored; dot-component handling differs between libtorrent 1.x and 2.0.
func TorrentPathComponent(component string) (string, error) {
	if component == "" {
		return "_", nil
	}
	name := TorrentPathName(component)
	if name == "" {
		return "", errFilteredEmptyPath
	}
	return name, nil
}

func TorrentFilePath(root string, components []string) (string, error) {
	n := len(root) + len(components)
	for _, c := range components {
		n += len(c)
	}
	var b strings.Builder
	b.Grow(n)
	b.WriteString(root)
	for i, c := range components {
		if i > 0 || root != "" {
			b.WriteByte('/')
		}
		if c == "" {
			b.WriteByte('_')
			continue
		}
		start := b.Len()
		writeTorrentPathName(&b, c)
		if b.Len() == start {
			return "", errFilteredEmptyPath
		}
	}
	return b.String(), nil
}

// TorrentPathName returns a torrent name or path component as qBittorrent shows it:
// malformed UTF-8 replaced and the directional characters libtorrent filters removed.
// A caller must refuse a nonempty name that comes back empty.
func TorrentPathName(name string) string {
	if utf8.ValidString(name) && indexTorrentPathControl(name) < 0 {
		return name
	}
	var b strings.Builder
	b.Grow(len(name))
	writeTorrentPathName(&b, name)
	return b.String()
}

func writeTorrentPathName(b *strings.Builder, name string) {
	if !utf8.ValidString(name) {
		name = stringutils.SanitizeUTF8(name)
	}
	for {
		i := indexTorrentPathControl(name)
		if i < 0 {
			b.WriteString(name)
			return
		}
		b.WriteString(name[:i])
		name = name[i+torrentPathControlLen:]
	}
}

// torrentPathControlLen is the UTF-8 length of every rune isTorrentPathControl reports.
const torrentPathControlLen = 3

func indexTorrentPathControl(name string) int {
	offset := 0
	// Every filtered rune starts with 0xe2.
	for {
		i := strings.IndexByte(name[offset:], '\xe2')
		if i < 0 {
			return -1
		}
		i += offset
		r, size := utf8.DecodeRuneInString(name[i:])
		if isTorrentPathControl(r) {
			return i
		}
		offset = i + size
	}
}

// Both libtorrent 1.2 and 2.0 filter this set in filter_path_character:
// https://github.com/arvidn/libtorrent/blob/RC_1_2/src/torrent_info.cpp
func isTorrentPathControl(r rune) bool {
	switch r {
	case '\u200e', '\u200f', '\u202a', '\u202b', '\u202c', '\u202d', '\u202e':
		return true
	default:
		return false
	}
}

// maxIsolationNameBytes keeps room for "--<shortHash>" in a folder name of
// 255 bytes, the per-name limit of ext4, Btrfs, ZFS, and NTFS. 255 UTF-8 bytes
// are never more than 255 UTF-16 units, so the limit also holds on NTFS.
const maxIsolationNameBytes = 255 - len("--") - 8

func IsolationFolder(name string) string {
	result := SanitizePathSegment(name)
	if len(result) <= maxIsolationNameBytes {
		return result
	}
	cut := maxIsolationNameBytes
	for cut > 0 && !utf8.RuneStart(result[cut]) {
		cut--
	}
	return SanitizePathSegment(result[:cut])
}

// HashedIsolationFolder returns <IsolationFolder>--<shortHash>, where shortHash
// is the first 8 characters of infohash, lowercased. It is the isolation
// folder for a torrent whose plain name is taken. It is also the automations
// template variable {{.IsolationFolderName}}.
func HashedIsolationFolder(infohash, name string) string {
	return IsolationFolder(name) + "--" + strings.ToLower(infohash[:min(len(infohash), 8)])
}
