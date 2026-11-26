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

type CategoryPathRule struct {
	Nest                 bool
	FileName             bool
	Platform             string
	InheritsDownloadPath bool
}

var qbtInvalidPathChars = regexp.MustCompile(`[:?"*<>|]+`)

func toValidPath(name string) string {
	return qbtInvalidPathChars.ReplaceAllString(name, " ")
}

var windowsDeviceNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9", "COM¹", "COM²", "COM³",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9", "LPT¹", "LPT²", "LPT³",
}

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

func categoryFolder(d fsops.PathDialect, name string, rule CategoryPathRule) string {
	if !rule.Nest {
		return d.FromSlash(toValidPath(name))
	}
	folder := name
	if _, after, found := strings.CutLast(name, "/"); found {
		folder = after
	}
	return CategoryFolderName(folder, rule)
}

func CategoryFolderName(folder string, rule CategoryPathRule) string {
	switch {
	case folder == "":
		return ""
	case rule.FileName:
		return toValidFileName(folder, rule.Platform)
	}
	return toValidPath(folder)
}

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
