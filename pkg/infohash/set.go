// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package infohash

import "slices"

type Set struct {
	hashes []Hash
	seen   map[Hash]struct{}
}

func NewSet(hashes ...Hash) *Set {
	s := &Set{hashes: make([]Hash, 0, len(hashes)), seen: make(map[Hash]struct{}, len(hashes))}
	for _, hash := range hashes {
		s.Add(hash)
	}
	return s
}

func Unique(values []string) []Hash {
	return uniqueAs(values, func(hash Hash) Hash { return hash })
}

func UniqueStrings(values []string) []string {
	return uniqueAs(values, Hash.String)
}

func uniqueAs[T any](values []string, as func(Hash) T) []T {
	seen := make(map[Hash]struct{}, len(values))
	out := make([]T, 0, len(values))
	for _, value := range values {
		hash, _ := Parse(value)
		if _, dup := seen[hash]; dup || hash.IsZero() {
			continue
		}
		seen[hash] = struct{}{}
		out = append(out, as(hash))
	}
	return out
}

func (s *Set) Add(hash Hash) bool {
	if hash.IsZero() || s.Has(hash) {
		return false
	}
	if s.seen == nil {
		s.seen = make(map[Hash]struct{})
	}
	s.seen[hash] = struct{}{}
	s.hashes = append(s.hashes, hash)
	return true
}

func (s *Set) Has(hash Hash) bool {
	_, ok := s.seen[hash]
	return ok
}

func (s *Set) Len() int { return len(s.hashes) }

func (s *Set) Hashes() []Hash { return slices.Clone(s.hashes) }

func (s *Set) Strings() []string {
	out := make([]string, len(s.hashes))
	for i, hash := range s.hashes {
		out[i] = hash.String()
	}
	return out
}
