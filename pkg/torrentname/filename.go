// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package torrentname

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/publicsuffix"
)

const (
	maxExportFilenameBytes = 240
	shortTorrentHashLength = 5
	torrentFileExtension   = ".torrent"
)

// TruncateUTF8 preserves multi-byte runes while capping byte length.
func TruncateUTF8(input string, maxBytes int) string {
	if len(input) <= maxBytes {
		return input
	}

	cut := 0
	for cut < len(input) {
		_, size := utf8.DecodeRuneInString(input[cut:])
		if size <= 0 || cut+size > maxBytes {
			break
		}
		cut += size
	}

	return input[:cut]
}

// SanitizeExportFilename returns a filesystem-safe torrent filename with optional tracker tag and hash suffix.
func SanitizeExportFilename(name, fallback, trackerDomain, hash string) string {
	trimmed := strings.TrimSpace(name)
	alt := strings.TrimSpace(fallback)

	if trimmed == "" {
		trimmed = alt
	}

	if trimmed == "" {
		trimmed = "torrent"
	}

	sanitized := strings.Map(func(r rune) rune {
		switch r {
		case '/', '\\', ':', '*', '?', '"', '<', '>', '|':
			return '_'
		case 0:
			return -1
		}

		if r < 32 || r == 127 {
			return -1
		}

		return r
	}, trimmed)

	sanitized = strings.Trim(sanitized, " .")
	if sanitized == "" {
		sanitized = "torrent"
	}

	trackerTag := TrackerTagFromDomain(trackerDomain)
	shortHash := ShortTorrentHash(hash)

	prefix := ""
	if trackerTag != "" {
		prefix = "[" + trackerTag + "] "
	}

	suffix := ""
	if shortHash != "" {
		suffix = " - " + shortHash
	}

	coreBudget := max(maxExportFilenameBytes-len(torrentFileExtension), 0)

	allowedBytes := coreBudget - len(prefix) - len(suffix)
	if allowedBytes < 1 {
		prefix = ""
		allowedBytes = coreBudget - len(suffix)
		if allowedBytes < 1 {
			suffix = ""
			allowedBytes = coreBudget
			if allowedBytes < 1 {
				allowedBytes = 0
			}
		}
	}

	sanitized = TruncateUTF8(sanitized, allowedBytes)
	if sanitized == "" {
		sanitized = "torrent"
	}

	filename := prefix + sanitized + suffix
	if !strings.HasSuffix(strings.ToLower(filename), torrentFileExtension) {
		filename += torrentFileExtension
	}

	return filename
}

// TrackerTagFromDomain converts a tracker domain into a compact tag for filenames.
func TrackerTagFromDomain(domain string) string {
	domain = strings.TrimSpace(domain)
	if domain == "" {
		return ""
	}

	domain = strings.TrimSuffix(domain, ".")
	domain = strings.TrimPrefix(domain, "www.")

	base := domain
	if registrable, err := publicsuffix.EffectiveTLDPlusOne(domain); err == nil {
		base = registrable
	}

	if idx := strings.IndexRune(base, '.'); idx != -1 {
		base = base[:idx]
	}

	base = strings.TrimSpace(base)
	if base == "" {
		return ""
	}

	var builder strings.Builder
	for _, r := range base {
		switch {
		case unicode.IsLetter(r):
			builder.WriteRune(unicode.ToLower(r))
		case unicode.IsDigit(r):
			builder.WriteRune(r)
		case r == '-':
			builder.WriteRune(r)
		}
	}

	tag := strings.Trim(builder.String(), "-")
	return tag
}

// ShortTorrentHash returns a lowercase, trimmed hash snippet suitable for filenames.
func ShortTorrentHash(hash string) string {
	hash = strings.TrimSpace(hash)
	if hash == "" {
		return ""
	}

	var builder strings.Builder
	builder.Grow(shortTorrentHashLength)
	for i := 0; i < len(hash) && builder.Len() < shortTorrentHashLength; i++ {
		c := hash[i]
		switch {
		case '0' <= c && c <= '9':
			builder.WriteByte(c)
		case 'a' <= c && c <= 'f':
			builder.WriteByte(c)
		case 'A' <= c && c <= 'F':
			builder.WriteByte(c + ('a' - 'A'))
		}
	}

	return builder.String()
}
