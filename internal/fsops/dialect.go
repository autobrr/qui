// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"fmt"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/autobrr/qui/pkg/hardlinktree"
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

// hardlinktree builds plans through its own subset of this interface.
var _ hardlinktree.PathDialect = PathDialect(nil)

var HostPaths PathDialect = hostPaths{}

var SlashPaths PathDialect = slashPaths{}

var WindowsPaths PathDialect = windowsPaths{}

// ClientPaths is the dialect of a path that a qBittorrent reports, for an
// instance whose files qui cannot reach: Windows rules for a path that starts
// with a drive letter or a UNC prefix, and slash rules for any other.
func ClientPaths(savePath string) PathDialect {
	if windowsVolume(savePath) != "" {
		return WindowsPaths
	}
	return SlashPaths
}

type hostPaths struct{}

func (hostPaths) Join(base, name string) string { return filepath.Join(base, name) }
func (hostPaths) Dir(p string) string           { return filepath.Dir(p) }
func (hostPaths) Base(p string) string          { return filepath.Base(p) }
func (hostPaths) Clean(p string) string         { return filepath.Clean(p) }
func (hostPaths) IsAbs(p string) bool           { return filepath.IsAbs(p) }
func (hostPaths) Rel(base, target string) (string, error) {
	return filepath.Rel(shareRootWithSeparator(base), shareRootWithSeparator(target))
}
func (hostPaths) FromSlash(p string) string { return filepath.FromSlash(p) }
func (hostPaths) ToSlash(p string) string   { return filepath.ToSlash(p) }
func (hostPaths) Separator() string         { return string(filepath.Separator) }

// shareRootWithSeparator gives a UNC share root one spelling: filepath.Rel never
// returns for \\host\share against \\host\share\ (Go 1.27.3). A volume the
// separator would change, such as \\host with no share, stays as it is.
func shareRootWithSeparator(p string) string {
	vol := filepath.VolumeName(p)
	if vol == "" {
		return p
	}
	withSeparator := vol + string(filepath.Separator)
	if filepath.Clean(p) == vol && filepath.VolumeName(withSeparator) == vol {
		return withSeparator
	}
	return p
}

type slashPaths struct{}

func (slashPaths) Join(base, name string) string { return path.Join(base, name) }
func (slashPaths) Dir(p string) string           { return path.Dir(p) }
func (slashPaths) Base(p string) string          { return path.Base(p) }
func (slashPaths) Clean(p string) string         { return path.Clean(p) }
func (slashPaths) IsAbs(p string) bool           { return path.IsAbs(p) }

func (slashPaths) Rel(base, target string) (string, error) {
	// filepath.Rel is separator-aware through filepath.Separator, so on a
	// Windows host it would treat "/" as a plain character. Route through a
	// slash-only implementation instead.
	return relSlash(path.Clean(base), path.Clean(target), func(a, b string) bool { return a == b })
}

func (slashPaths) FromSlash(p string) string { return p }
func (slashPaths) ToSlash(p string) string   { return p }
func (slashPaths) Separator() string         { return "/" }

func relSlash(base, target string, same func(a, b string) bool) (string, error) {
	if same(base, target) {
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
	for n < len(bs) && n < len(ts) && same(bs[n], ts[n]) {
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

// windowsPaths follows path/filepath on Windows for drive and UNC paths. Both /
// and \ separate, and each method returns \. Device paths (\\?\, \\.\) are
// outside it.
type windowsPaths struct{}

func (windowsPaths) Join(base, name string) string {
	switch {
	case base == "" && name == "":
		return ""
	case base == "":
		return windowsPaths{}.Clean(name)
	case isWindowsSeparator(base[len(base)-1]):
		// More separators after base could make a UNC prefix of it.
		name = strings.TrimLeft(name, `\/`)
	case base[len(base)-1] != ':':
		// A drive without a separator (C:) gets none: the path stays
		// relative to the current folder of that drive.
		name = `\` + name
	}
	return windowsPaths{}.Clean(base + name)
}

func (windowsPaths) Dir(p string) string {
	vol := windowsVolume(p)
	i := len(vol) + strings.LastIndexAny(p[len(vol):], `\/`)
	dir := windowsPaths{}.Clean(p[len(vol) : i+1])
	vol = windowsPaths{}.FromSlash(vol)
	if dir == "." && len(vol) > 2 {
		// A UNC share has no parent.
		return vol
	}
	return vol + dir
}

func (windowsPaths) Base(p string) string {
	if p == "" {
		return "."
	}
	p = strings.TrimRight(p, `\/`)
	p = p[len(windowsVolume(p)):]
	if p = p[strings.LastIndexAny(p, `\/`)+1:]; p == "" {
		return `\`
	}
	return p
}

func (windowsPaths) Clean(p string) string {
	vol := windowsVolume(p)
	rest := windowsPaths{}.ToSlash(p[len(vol):])
	vol = windowsPaths{}.FromSlash(vol)
	if rest == "" && isUNC(vol) {
		return vol
	}
	cleaned := windowsPaths{}.FromSlash(path.Clean(rest))
	if vol != "" || strings.HasPrefix(p, cleaned) {
		return vol + cleaned
	}
	if first, _, _ := strings.Cut(cleaned, `\`); strings.Contains(first, ":") {
		return `.\` + cleaned
	}
	if strings.HasPrefix(cleaned, `\??`) {
		return `\.` + cleaned
	}
	return cleaned
}

func (windowsPaths) IsAbs(p string) bool {
	vol := windowsVolume(p)
	if isUNC(vol) {
		return true
	}
	return vol != "" && len(p) > len(vol) && isWindowsSeparator(p[len(vol)])
}

// Rel compares names without case, as Windows does. A share root reads the
// same with and without its trailing separator.
func (windowsPaths) Rel(base, target string) (string, error) {
	d := windowsPaths{}
	baseVol, targetVol := d.FromSlash(windowsVolume(base)), d.FromSlash(windowsVolume(target))
	if !strings.EqualFold(baseVol, targetVol) {
		return "", fmt.Errorf("Rel: can't make %s relative to %s", target, base)
	}
	rest := func(p, vol string) string {
		r := d.ToSlash(d.Clean(p)[len(vol):])
		if r == "" && isUNC(vol) {
			return "/"
		}
		return r
	}
	rel, err := relSlash(rest(base, baseVol), rest(target, targetVol), strings.EqualFold)
	if err != nil {
		return "", fmt.Errorf("Rel: can't make %s relative to %s", target, base)
	}
	return d.FromSlash(rel), nil
}

func (windowsPaths) FromSlash(p string) string { return strings.ReplaceAll(p, "/", `\`) }
func (windowsPaths) ToSlash(p string) string   { return strings.ReplaceAll(p, `\`, "/") }
func (windowsPaths) Separator() string         { return `\` }

// windowsVolume is the drive (C:) or the UNC share (\\host\share) at the start
// of p, as it is written there, or "". A share with a .. part is no volume.
func windowsVolume(p string) string {
	switch {
	case len(p) >= 2 && p[1] == ':' && !isWindowsSeparator(p[0]):
		return p[:2]
	case len(p) >= 2 && isWindowsSeparator(p[0]) && isWindowsSeparator(p[1]):
		vol, separators := p, 0
		for i := 2; i < len(p) && vol == p; i++ {
			if isWindowsSeparator(p[i]) {
				if separators++; separators == 2 {
					vol = p[:i]
				}
			}
		}
		if strings.Contains(windowsPaths{}.ToSlash(vol)+"/", "/../") {
			return ""
		}
		return vol
	}
	return ""
}

func isUNC(vol string) bool {
	return len(vol) >= 2 && isWindowsSeparator(vol[0]) && isWindowsSeparator(vol[1])
}

func isWindowsSeparator(c byte) bool {
	return c == '\\' || c == '/'
}
