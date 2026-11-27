// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package infohash

import (
	"errors"
	"strings"
)

const (
	v1Len = 40
	v2Len = 64
)

var ErrInvalid = errors.New("infohash: want 40 or 64 hex characters")

type Hash struct {
	hex string
}

func Parse(s string) (Hash, error) {
	s = strings.TrimSpace(s)
	if len(s) != v1Len && len(s) != v2Len {
		return Hash{}, ErrInvalid
	}
	upper := false
	for i := range len(s) {
		switch c := s[i]; {
		case '0' <= c && c <= '9', 'a' <= c && c <= 'f':
		case 'A' <= c && c <= 'F':
			upper = true
		default:
			return Hash{}, ErrInvalid
		}
	}
	if upper {
		s = strings.ToLower(s)
	}
	return Hash{hex: s}, nil
}

func ParseAll(values []string) []Hash {
	hashes := make([]Hash, len(values))
	for i, value := range values {
		hashes[i], _ = Parse(value)
	}
	return hashes
}

func (h Hash) String() string { return h.hex }

func (h Hash) Upper() string { return strings.ToUpper(h.hex) }

func (h Hash) Is(s string) bool {
	s = strings.TrimSpace(s)
	return h.hex != "" && len(s) == len(h.hex) && strings.EqualFold(s, h.hex)
}

func (h Hash) IsZero() bool { return h.hex == "" }

func (h Hash) IsV2() bool { return len(h.hex) == v2Len }

func (h Hash) TorrentID() Hash {
	if h.IsV2() {
		return Hash{hex: h.hex[:v1Len]}
	}
	return h
}
