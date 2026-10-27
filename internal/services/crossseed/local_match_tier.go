// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"path"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

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

// contentFiles removes the files that search ignores. When every file without
// an ignored extension (nfo, srt) is a sample or a bonus video, those files
// stay. A torrent of sidecars only keeps all its files.
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

// localMatchPolicy is the Match policy of the paths that compare two local
// torrents and then pair their file sizes in localMatchTier. That file check
// compares exact sizes, so the verdict skips its own size check.
var localMatchPolicy = matchPolicy{SizeTolerancePercent: defaultSizeMismatchTolerancePercent, SkipSizeCheck: true}

// localMatchPrefilter selects the candidates whose file lists are worth a
// fetch, from the cached torrent fields and the Read releases. nameOrRelease
// is the tier the candidate gets if its file sizes pair with the source. ok is
// false for a candidate that cannot be a cross-seed.
func (s *Service) localMatchPrefilter(
	sameInstance bool,
	source *qbt.Torrent,
	sourceRead namedRelease,
	candidate *qbittorrent.CrossInstanceTorrentView,
	candidateRead namedRelease,
	normalizedContentPath string,
) (nameOrRelease string, ok bool) {
	sourceName := normalizeLowerTrim(source.Name)
	if sourceName != "" && sourceName == normalizeLowerTrim(candidate.Name) {
		return matchTypeName, true
	}

	verdict := s.matcher().matchVerdict(sourceRead, candidateRead,
		matchEvidence{SourceSize: searchSourceSize(source), CandidateSize: searchSourceSize(candidate.Torrent)},
		localMatchPolicy,
	)
	if verdict.Matched {
		return matchTypeRelease, true
	}
	// A retitled upload of the same data keeps its own name, so it fails the
	// release match on the title alone. Local matches adds no torrent, so it
	// rescues the title at any setting and torrent size, and the file sizes
	// decide.
	if !verdict.Strict.matched() && verdict.Strict.differences.first() == differenceTitle {
		if ok, _ := s.matcher().releasesMatchExceptTitleWithReason(sourceRead, candidateRead, false); ok {
			return matchTypeRelease, true
		}
	}

	if !sameInstance {
		return "", false
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
