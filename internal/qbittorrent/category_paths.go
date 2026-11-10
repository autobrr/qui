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
	// InheritsDownloadPath: a category with no download path option takes
	// its parent's, and builds its folder from its name (qBittorrent 5.0+).
	// Before 5.0, no option means the global download path itself.
	InheritsDownloadPath bool
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

	parent := defaultSavePath
	if p := parentCategory(name, rule); p != "" {
		parent = CategorySavePath(d, p, categories, defaultSavePath, rule)
	}
	if parent == "" {
		return ""
	}
	return d.Join(parent, categoryFolder(d, name, rule))
}

// parentCategory returns the category whose folder name's folder sits in, or
// "" for the default folder.
func parentCategory(name string, rule CategoryPathRule) string {
	if !rule.Nest {
		return ""
	}
	before, _, found := strings.CutLast(name, "/")
	if !found {
		return ""
	}
	return before
}

// categoryFolder is the folder qBittorrent names for a category inside its
// parent's folder. Without nesting that is the whole name, whose slashes
// still nest folders.
func categoryFolder(d fsops.PathDialect, name string, rule CategoryPathRule) string {
	if !rule.Nest {
		// Category names are slash-delimited whatever the host separator is.
		return d.FromSlash(toValidPath(name))
	}
	// qBittorrent converts only the last segment and resolves the rest
	// through the parent category.
	folder := name
	if _, after, found := strings.CutLast(name, "/"); found {
		folder = after
	}
	switch {
	case folder == "":
		// A name that ends in "/" adds no segment.
		return ""
	case rule.FileName:
		return toValidFileName(folder, rule.Platform)
	}
	return toValidPath(folder)
}

// CategoryDownloadPath mirrors SessionImpl::categoryDownloadPath: where an
// automatically managed torrent in the category downloads before it is
// complete. ok is false when the category has no download path, so its
// torrents download straight to the save path. ok with "" means the path
// cannot be determined, because it rests on a downloadPath that is not
// absolute.
func CategoryDownloadPath(d fsops.PathDialect, name string, categories map[string]qbt.Category, downloadPath string, downloadPathEnabled bool, rule CategoryPathRule) (string, bool) {
	if downloadPath != "" {
		downloadPath = d.Clean(downloadPath)
	}
	if !d.IsAbs(downloadPath) {
		downloadPath = ""
	}
	under := func(base, p string) string {
		if p != "" && d.IsAbs(p) {
			return p
		}
		if base == "" {
			return ""
		}
		return d.Join(base, p)
	}

	if !rule.InheritsDownloadPath {
		option := categories[name].DownloadPath
		if option == nil {
			return downloadPath, downloadPathEnabled
		}
		if !option.Enabled {
			return "", false
		}
		if option.Path == "" {
			return under(downloadPath, d.FromSlash(toValidPath(name))), true
		}
		return under(downloadPath, d.Clean(option.Path)), true
	}

	// A category with no option takes the nearest ancestor's, path included,
	// and the top one the global setting.
	option := &qbt.CategoryDownloadPath{Enabled: downloadPathEnabled}
	for n := name; n != ""; n = parentCategory(n, rule) {
		if o := categories[n].DownloadPath; o != nil {
			option = o
			break
		}
	}
	if !option.Enabled {
		return "", false
	}

	// The folder sits under the parent's folder, built the same way, where a
	// parent with no option or one turned off still has its named folder. The
	// global downloadPath is the base even when it is turned off.
	var place func(n, p string) string
	place = func(n, p string) string {
		if p == "" {
			p = categoryFolder(d, n, rule)
		} else if p = d.Clean(p); d.IsAbs(p) {
			return p
		}
		base := downloadPath
		if parent := parentCategory(n, rule); parent != "" {
			o := categories[parent].DownloadPath
			if o != nil && o.Enabled {
				base = place(parent, o.Path)
			} else {
				base = place(parent, "")
			}
		}
		return under(base, p)
	}
	return place(name, option.Path), true
}
