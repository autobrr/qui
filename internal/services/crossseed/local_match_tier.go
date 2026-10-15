// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"

	"github.com/autobrr/qui/internal/qbittorrent"
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

// contentFiles removes the files that search ignores. A file with an ignored
// extension (nfo, srt) is always a sidecar. An ignored path keyword (proof,
// extras) can be part of the title, as for a movie named Proof, so when every
// remaining file has one, those files stay. A torrent of sidecars only keeps
// all its files.
func contentFiles(files qbt.TorrentFiles, normalizer *stringutils.Normalizer[string, string]) qbt.TorrentFiles {
	var noSidecars, content qbt.TorrentFiles
	for _, file := range files {
		name := normalizer.Normalize(file.Name)
		if slices.ContainsFunc(DefaultIgnoredExtensions, func(ext string) bool { return strings.HasSuffix(name, ext) }) {
			continue
		}
		noSidecars = append(noSidecars, file)
		if !slices.ContainsFunc(DefaultIgnoredPathKeywords, func(keyword string) bool { return strings.Contains(name, keyword) }) {
			content = append(content, file)
		}
	}
	switch {
	case len(content) > 0:
		return content
	case len(noSidecars) > 0:
		return noSidecars
	}
	return files
}

func (d sameDataFiles) sharesPath(other sameDataFiles) bool {
	for p := range other.paths {
		if _, ok := d.paths[p]; ok {
			return true
		}
	}
	return false
}

// localMatchTier decides the match type of a candidate that passed
// localMatchPrefilter, in this order:
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

// localMatchPrefilter selects the candidates whose file lists are worth a
// fetch, from the cached torrent fields alone. nameOrRelease is the tier the
// candidate gets if its file sizes pair with the source. ok is false for a
// candidate that cannot be a cross-seed.
func (s *Service) localMatchPrefilter(
	source *qbt.Torrent,
	sourceRelease *rls.Release,
	candidate *qbittorrent.CrossInstanceTorrentView,
	normalizedContentPath string,
) (nameOrRelease string, ok bool) {
	sourceName := normalizeLowerTrim(source.Name)
	if sourceName != "" && sourceName == normalizeLowerTrim(candidate.Name) {
		return matchTypeName, true
	}

	candidateRelease := s.releaseCache.Parse(candidate.Name)
	matched, mismatchReason := s.matcher().releasesMatchWithReasonAndNames(sourceRelease, candidateRelease, source.Name, candidate.Name, false)
	if matched {
		return matchTypeRelease, true
	}
	// A retitled upload of the same data keeps its own name, so it fails the
	// release match on the title alone.
	if mismatchReason == titleMismatchReason {
		if ok, _ := s.matcher().releasesMatchExceptTitleWithReason(sourceRelease, candidateRelease, false); ok {
			return matchTypeRelease, true
		}
	}

	candidateContentPath := normalizePathForComparison(candidate.ContentPath)
	if normalizedContentPath != "" && normalizedContentPath == candidateContentPath {
		return "", true
	}
	// qui adds an episode cross-seed inside the folder of its season pack.
	if normalizedContentPath != "" && candidateContentPath != "" &&
		(strings.HasPrefix(candidateContentPath, normalizedContentPath+"/") ||
			strings.HasPrefix(normalizedContentPath, candidateContentPath+"/")) {
		return "", true
	}
	// qBittorrent reports the save path as the content path of a multi-file
	// torrent without a subfolder, so its files can sit next to the other
	// torrent's files.
	sourceSavePath := normalizePathForComparison(source.SavePath)
	if sourceSavePath != "" && sourceSavePath == normalizePathForComparison(candidate.SavePath) &&
		(normalizedContentPath == sourceSavePath || candidateContentPath == sourceSavePath) {
		return "", true
	}
	return "", false
}
