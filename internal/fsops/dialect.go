// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"
)

// PathDialect is the path grammar of a backend's filesystem. A path that goes
// to or comes from a Backend is manipulated with that backend's dialect, never
// with the host's filepath: a remote unix path on a Windows host is still
// slash-delimited and still absolute. Ext is absent on purpose, it reads the
// same under both grammars.
type PathDialect interface {
	// Join takes two parts, which is every backend-path join in qui; a
	// variadic slice would escape through the interface call on each file.
	Join(base, name string) string
	Dir(p string) string
	Base(p string) string
	Clean(p string) string
	IsAbs(p string) bool
	Rel(base, target string) (string, error)
	// FromSlash and ToSlash convert between slash-delimited names, which is
	// how qBittorrent reports every file, and the dialect's own form.
	FromSlash(p string) string
	ToSlash(p string) string
	Separator() string
}

// HostPaths is the dialect of the host filesystem: path/filepath.
var HostPaths PathDialect = hostPaths{}

// SlashPaths is the dialect of a slash-delimited POSIX filesystem regardless of
// the host: path. FromSlash and ToSlash are the identity.
var SlashPaths PathDialect = slashPaths{}

type hostPaths struct{}

func (hostPaths) Join(base, name string) string           { return filepath.Join(base, name) }
func (hostPaths) Dir(p string) string                     { return filepath.Dir(p) }
func (hostPaths) Base(p string) string                    { return filepath.Base(p) }
func (hostPaths) Clean(p string) string                   { return filepath.Clean(p) }
func (hostPaths) IsAbs(p string) bool                     { return filepath.IsAbs(p) }
func (hostPaths) Rel(base, target string) (string, error) { return filepath.Rel(base, target) }
func (hostPaths) FromSlash(p string) string               { return filepath.FromSlash(p) }
func (hostPaths) ToSlash(p string) string                 { return filepath.ToSlash(p) }
func (hostPaths) Separator() string                       { return string(filepath.Separator) }

type slashPaths struct{}

func (slashPaths) Join(base, name string) string { return path.Join(base, name) }
func (slashPaths) Dir(p string) string           { return path.Dir(p) }
func (slashPaths) Base(p string) string          { return path.Base(p) }
func (slashPaths) Clean(p string) string         { return path.Clean(p) }
func (slashPaths) IsAbs(p string) bool           { return path.IsAbs(p) }

// Rel is filepath.Rel's contract on slash paths: both absolute or both
// relative, lexical only, error when target cannot be made relative to base.
func (slashPaths) Rel(base, target string) (string, error) {
	// filepath.Rel is separator-aware through filepath.Separator, so on a
	// Windows host it would treat "/" as a plain character. Route through a
	// slash-only implementation instead.
	return relSlash(path.Clean(base), path.Clean(target))
}

func (slashPaths) FromSlash(p string) string { return p }
func (slashPaths) ToSlash(p string) string   { return p }
func (slashPaths) Separator() string         { return "/" }

// relSlash is filepath.Rel for slash paths; base and target are already clean.
func relSlash(base, target string) (string, error) {
	if base == target {
		return ".", nil
	}
	if path.IsAbs(base) != path.IsAbs(target) {
		return "", fmt.Errorf("Rel: can't make %s relative to %s", target, base)
	}
	if base == "." {
		base = ""
	}
	if target == "." {
		target = ""
	}
	bs := splitSlash(base)
	ts := splitSlash(target)
	n := 0
	for n < len(bs) && n < len(ts) && bs[n] == ts[n] {
		n++
	}
	if slices.Contains(bs[n:], "..") {
		return "", fmt.Errorf("Rel: can't make %s relative to %s", target, base)
	}
	parts := slices.Repeat([]string{".."}, len(bs)-n)
	parts = append(parts, ts[n:]...)
	return strings.Join(parts, "/"), nil
}

func splitSlash(p string) []string {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}
