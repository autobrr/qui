// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"regexp"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
)

// CategoryPathRule is how an instance's qBittorrent turns a category name into
// the folder of a category with no save path.
type CategoryPathRule struct {
	// Nest: the folder resolves under the parent category's save path
	// (qBittorrent 5.0+ with subcategories on).
	Nest bool
	// FileName: qBittorrent 5.3+ names the folder with toValidFileName, whose
	// reserved characters depend on Platform, its build platform.
	FileName bool
	Platform string
}

// qbtInvalidPathChars is the regex from Utils::Fs::toValidPath; slashes are
// absent there because they separate path segments.
var qbtInvalidPathChars = regexp.MustCompile(`[:?"*<>|]+`)

// toValidPath converts a category name into the relative path qBittorrent
// before 5.3 creates for it, so a category called "movies:hd" is protected at
// "movies hd".
func toValidPath(name string) string {
	return qbtInvalidPathChars.ReplaceAllString(name, " ")
}

var windowsDeviceNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³",
}

// toValidFileName mirrors Utils::Fs::toValidFileName with the pad "_" for a
// qBittorrent built for platform.
func toValidFileName(name, platform string) string {
	name = strings.TrimSpace(name)
	if name == "" || name == "." || name == ".." {
		return "_"
	}

	reserved := func(r rune) bool {
		switch {
		case r < 32, r == 127, r == '/':
			return true
		case platform == "windows":
			return strings.ContainsRune(`\<>:"|?*`, r)
		case platform == "macos":
			return r == ':'
		}
		return false
	}
	var b strings.Builder
	for _, r := range name {
		if !reserved(r) {
			b.WriteRune(r)
		} else if !strings.HasSuffix(b.String(), "_") {
			b.WriteByte('_')
		}
	}
	valid := b.String()

	if platform == "windows" {
		valid = strings.TrimRight(valid, ". ")
		base, ext := valid, ""
		if before, after, ok := strings.CutLast(valid, "."); ok {
			base, ext = before, "."+after
		}
		if slices.ContainsFunc(windowsDeviceNames, func(n string) bool { return strings.EqualFold(n, base) }) {
			valid = base + "_1" + ext
		}
	}
	return strings.TrimSpace(valid)
}

// CategorySavePath mirrors SessionImpl::categorySavePath. Returns "" when the
// destination cannot be determined; a depth cap here would silently drop
// protection for a deeply nested category.
func CategorySavePath(d fsops.PathDialect, name string, categories map[string]qbt.Category, defaultSavePath string, rule CategoryPathRule) string {
	savePath := categories[name].SavePath
	if savePath != "" {
		savePath = d.Clean(savePath)
		if d.IsAbs(savePath) {
			return savePath
		}
		if defaultSavePath == "" {
			return ""
		}
		return d.Join(defaultSavePath, savePath)
	}

	if !rule.Nest {
		if defaultSavePath == "" {
			return ""
		}
		// Category names are slash-delimited whatever the host separator is.
		return d.Join(defaultSavePath, d.FromSlash(toValidPath(name)))
	}

	// qBittorrent converts only the last segment and resolves the rest
	// through the parent category.
	parent, folder := defaultSavePath, name
	if before, after, ok := strings.CutLast(name, "/"); ok {
		folder = after
		if before != "" {
			parent = CategorySavePath(d, before, categories, defaultSavePath, rule)
		}
	}
	if parent == "" {
		return ""
	}
	switch {
	case folder == "":
		// A name that ends in "/" adds no segment.
	case rule.FileName:
		folder = toValidFileName(folder, rule.Platform)
	default:
		folder = toValidPath(folder)
	}
	return d.Join(parent, folder)
}
