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

type PathDialect interface {
	Join(base, name string) string
	Dir(p string) string
	Base(p string) string
	Clean(p string) string
	IsAbs(p string) bool
	Rel(base, target string) (string, error)
	FromSlash(p string) string
	ToSlash(p string) string
	Separator() string
}

var _ hardlinktree.PathDialect = PathDialect(nil)

var HostPaths PathDialect = hostPaths{}

var SlashPaths PathDialect = slashPaths{}

var WindowsPaths PathDialect = windowsPaths{}

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

type windowsPaths struct{}

func (windowsPaths) Join(base, name string) string {
	switch {
	case base == "" && name == "":
		return ""
	case base == "":
		return windowsPaths{}.Clean(name)
	case isWindowsSeparator(base[len(base)-1]):
		name = strings.TrimLeft(name, `\/`)
	case base[len(base)-1] != ':':
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
