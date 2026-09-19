// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package pathutil

// IsAbsoluteClientPath reports whether p is absolute for a torrent client on
// any OS. qui and qBittorrent can run on different OSes, so filepath.IsAbs
// would judge the wrong host.
func IsAbsoluteClientPath(p string) bool {
	switch {
	case p == "":
		return false
	case p[0] == '/' || p[0] == '\\':
		return true
	case len(p) >= 3 && isASCIILetter(p[0]) && p[1] == ':' && (p[2] == '\\' || p[2] == '/'):
		return true
	default:
		return false
	}
}

func isASCIILetter(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z')
}
