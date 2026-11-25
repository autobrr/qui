// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/pkg/stringutils"
)

type sameDataFiles struct {
	paths     map[string]struct{}
	sizes     []int64
	filePaths []string
}

func newSameDataFiles(savePath string, files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string], withFilePaths bool) sameDataFiles {
	var data sameDataFiles
	if savePath != "" && withFilePaths {
		data.filePaths = make([]string, len(files))
		for i, file := range files {
			data.filePaths[i] = normalizePathForComparisonInner(savePath + "/" + file.Name)
		}
	}
	content := contentFiles(files, normalizer)
	data.paths = make(map[string]struct{}, len(content))
	i := 0
	for _, file := range content {
		for files[i].Name != file.Name {
			i++
		}
		switch {
		case data.filePaths != nil:
			data.paths[data.filePaths[i]] = struct{}{}
		case savePath != "":
			data.paths[normalizePathForComparisonInner(savePath+"/"+file.Name)] = struct{}{}
		}
		data.sizes = append(data.sizes, file.Size)
	}
	slices.Sort(data.sizes)
	return data
}

func contentFiles(files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) qbt.TorrentFiles {
	var noSidecars, content qbt.TorrentFiles
	for _, file := range files {
		if slices.Contains(DefaultIgnoredExtensions, path.Ext(normalizer.Normalize(file.Name))) {
			continue
		}
		noSidecars = append(noSidecars, file)
		if !shouldIgnoreFile(file.Name, normalizer) {
			content = append(content, file)
		}
	}
	switch {
	case len(content) > 0:
		return content
	case len(noSidecars) == 0:
		return files
	}

	names := make([]string, len(noSidecars))
	for i, file := range noSidecars {
		names[i] = normalizer.Normalize(file.Name)
	}
	var keywords []string
	for _, keyword := range DefaultIgnoredPathKeywords {
		if slices.ContainsFunc(names, func(name string) bool { return !strings.Contains(name, keyword) }) {
			keywords = append(keywords, keyword)
		}
	}
	for i, file := range noSidecars {
		if !slices.ContainsFunc(keywords, func(keyword string) bool { return strings.Contains(names[i], keyword) }) {
			content = append(content, file)
		}
	}
	if len(content) > 0 {
		return content
	}
	return noSidecars
}

func (d sameDataFiles) sharesPath(other sameDataFiles) bool {
	for p := range other.paths {
		if _, ok := d.paths[p]; ok {
			return true
		}
	}
	return false
}

func localMatchTier(sameInstance bool, nameOrRelease string, source, candidate sameDataFiles, linked func() string) string {
	if sameInstance && source.sharesPath(candidate) {
		return matchTypeContentPath
	}
	if nameOrRelease == "" {
		return ""
	}
	if linked != nil {
		if tier := linked(); tier != "" {
			return tier
		}
	}
	if slices.Equal(source.sizes, candidate.sizes) {
		return nameOrRelease
	}
	return ""
}

var localMatchPolicy = matchPolicy{TitleRescue: true, SizeTolerancePercent: defaultSizeMismatchTolerancePercent, SkipSizeCheck: true}
