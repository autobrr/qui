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

// sameDataFiles holds the files of a torrent that decide whether another
// torrent holds the same data. Files that search ignores (nfo, srt, samples,
// and the rest of the ignore list) are left out, as contentFiles describes.
type sameDataFiles struct {
	paths map[string]struct{} // save path joined with the file name, normalized
	sizes []int64             // sorted
}

func newSameDataFiles(savePath string, files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) sameDataFiles {
	files = contentFiles(files, normalizer)
	data := sameDataFiles{paths: make(map[string]struct{}, len(files))}
	for _, file := range files {
		if savePath != "" {
			data.paths[normalizePathForComparison(savePath+"/"+file.Name)] = struct{}{}
		}
		data.sizes = append(data.sizes, file.Size)
	}
	slices.Sort(data.sizes)
	return data
}

// contentFiles removes the files that search ignores. When every file without
// an ignored extension (nfo, srt) is ignored, an ignore word that every file
// has is part of the title, so only the other words mark a sidecar. When every
// file still has one, those files stay. A torrent of sidecars only keeps all
// its files.
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

	// Every file has a keyword. Drop the keywords that every file has, as
	// for a show named Extras in an Extras/ folder.
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

// localMatchTier decides the match type of a candidate that the batch
// matcher selected, in this order:
//
//  1. Content: on the same instance, a source file has the same path on disk.
//  2. Hardlink or Reflink: linked returns the tier. Only a Name or Release
//     candidate is checked. A nil linked skips this tier.
//  3. Name or Release: nameOrRelease is set and the file sizes pair one to one.
//
// Any other candidate is not a cross-seed and gets "".
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

// localMatchPolicy is the Match policy of the paths that compare two local
// torrents and then pair their file sizes in localMatchTier. That file check
// compares exact sizes, so the verdict skips its own size check. These paths
// add nothing to qBittorrent, so equal file sizes or a link decide, and they
// rescue a title at any title rescue setting of searches.
var localMatchPolicy = matchPolicy{TitleRescue: true, SizeTolerancePercent: defaultSizeMismatchTolerancePercent, SkipSizeCheck: true}
