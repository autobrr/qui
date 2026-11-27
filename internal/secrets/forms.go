// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
	"unicode/utf8"
)

func Origin(full string) string {
	if u, err := url.Parse(full); err == nil && u.Scheme != "" && u.Host != "" {
		return u.Scheme + "://" + u.Host
	}
	if IsPseudoTrackerLabel(full) {
		return full
	}
	return ""
}

func Scheme(full string) string {
	parsed, err := url.Parse(full)
	if err != nil || parsed.Scheme == "" {
		return ""
	}
	return parsed.Scheme + "://"
}

func StandIn(full string) string {
	sum := sha256.Sum256([]byte(full))
	return Origin(full) + "#" + hex.EncodeToString(sum[:4])
}

func IsStandIn(text string) bool {
	origin, hash, found := strings.CutLast(text, "#")
	return found && len(hash) == 8 && strings.Trim(hash, "0123456789abcdef") == "" && Origin(origin) == origin
}

func HoldsLink(text string) bool {
	if strings.Contains(text, "<") {
		return true
	}
	for rest := text; ; {
		before, after, found := strings.Cut(rest, ":")
		if !found {
			break
		}
		if len(before) >= 6 && strings.EqualFold(before[len(before)-6:], "magnet") {
			return true
		}
		rest = after
	}
	for at := 0; ; at += 2 {
		slashes := strings.Index(text[at:], "//")
		if slashes < 0 {
			return false
		}
		at += slashes
		if at > 0 && text[at-1] == ':' {
			return true
		}
		if (at == 0 || !isASCIIAlnum(text[at-1])) && at+2 < len(text) && startsHost(text[at+2]) {
			return true
		}
	}
}

func startsHost(c byte) bool {
	return isASCIIAlnum(c) || c == '[' || c >= utf8.RuneSelf
}

func isASCIIAlnum(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

func IsPseudoTrackerLabel(value string) bool {
	normalized := strings.ToLower(strings.TrimSpace(value))
	normalized = strings.Trim(normalized, "*")
	normalized = strings.TrimSpace(normalized)
	if strings.HasPrefix(normalized, "[") && strings.HasSuffix(normalized, "]") {
		normalized = strings.TrimPrefix(normalized, "[")
		normalized = strings.TrimSuffix(normalized, "]")
		normalized = strings.TrimSpace(normalized)
	}

	switch normalized {
	case "dht", "pex", "lsd":
		return true
	default:
		return false
	}
}
