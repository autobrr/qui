// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/anacrolix/torrent/metainfo"
	"github.com/autobrr/qui/pkg/stringutils"
)

// MatchMode defines how strictly files are compared.
type MatchMode string

const (
	// MatchModeStrict requires files to match by name AND size.
	MatchModeStrict MatchMode = "strict"
	// MatchModeFlexible allows files to match by size only.
	MatchModeFlexible MatchMode = "flexible"
)

// MatchResult represents the result of matching a searchee against a torrent.
type MatchResult struct {
	// IsMatch is true if the match criteria were met
	IsMatch bool

	// MatchedFiles contains the matched file pairs
	MatchedFiles []MatchedFilePair

	// UnmatchedSearcheeFiles contains searchee files that weren't matched
	UnmatchedSearcheeFiles []*ScannedFile

	// UnmatchedTorrentFiles contains torrent files that weren't matched
	UnmatchedTorrentFiles []TorrentFile

	// MatchRatio is the ratio of matched files (0.0-1.0)
	MatchRatio float64

	// SizeMatchRatio is the ratio of matched size (0.0-1.0)
	SizeMatchRatio float64

	// IsPerfectMatch is true if all files matched
	IsPerfectMatch bool

	// IsPartialMatch is true if some but not all files matched
	IsPartialMatch bool
}

// MatchedFilePair represents a matched pair of searchee file and torrent file.
type MatchedFilePair struct {
	SearcheeFile *ScannedFile
	TorrentFile  TorrentFile
}

// TorrentFile represents a file within a torrent.
type TorrentFile struct {
	Path   string // Relative path within the torrent
	Size   int64  // Size in bytes
	Offset int64  // Byte offset in the torrent's byte stream (for piece calculation)
}

// Matcher handles matching searchees against torrent file lists.
type Matcher struct {
	mode             MatchMode
	sizeTolerancePct float64
}

// NewMatcher creates a new matcher.
func NewMatcher(mode MatchMode, sizeTolerancePct float64) *Matcher {
	if mode == "" {
		mode = MatchModeStrict
	}
	if sizeTolerancePct <= 0 {
		sizeTolerancePct = 0 // Exact size match
	}
	return &Matcher{
		mode:             mode,
		sizeTolerancePct: sizeTolerancePct,
	}
}

// Match attempts to match a searchee's files against a torrent's files.
func (m *Matcher) Match(searchee *Searchee, torrentFiles []TorrentFile) *MatchResult {
	result := &MatchResult{
		MatchedFiles:           make([]MatchedFilePair, 0),
		UnmatchedSearcheeFiles: make([]*ScannedFile, 0),
		UnmatchedTorrentFiles:  make([]TorrentFile, 0),
	}

	if len(searchee.Files) == 0 || len(torrentFiles) == 0 {
		result.UnmatchedSearcheeFiles = searchee.Files
		result.UnmatchedTorrentFiles = torrentFiles
		return result
	}

	// Try matching based on mode
	switch m.mode {
	case MatchModeStrict:
		m.matchStrict(searchee, torrentFiles, result)
	case MatchModeFlexible:
		m.matchFlexible(searchee, torrentFiles, result)
	default:
		m.matchStrict(searchee, torrentFiles, result)
	}

	// Calculate ratios
	totalSearcheeFiles := len(searchee.Files)
	totalTorrentFiles := len(torrentFiles)
	matchedCount := len(result.MatchedFiles)

	if totalSearcheeFiles > 0 {
		result.MatchRatio = float64(matchedCount) / float64(totalSearcheeFiles)
	}

	// Calculate size ratio
	var matchedSize, totalSearcheeSize int64
	for _, f := range searchee.Files {
		totalSearcheeSize += f.Size
	}
	for _, mp := range result.MatchedFiles {
		matchedSize += mp.SearcheeFile.Size
	}
	if totalSearcheeSize > 0 {
		result.SizeMatchRatio = float64(matchedSize) / float64(totalSearcheeSize)
	}

	// Determine match type
	// Perfect match requires all searchee files matched AND all torrent files matched
	allSearcheeFilesMatched := matchedCount == totalSearcheeFiles
	allTorrentFilesMatched := matchedCount == totalTorrentFiles
	result.IsPerfectMatch = allSearcheeFilesMatched && allTorrentFilesMatched
	result.IsPartialMatch = matchedCount > 0 && !result.IsPerfectMatch
	// IsMatch indicates we have at least one file-level match and can consider this
	// candidate for injection. Acceptance (perfect vs partial) is decided elsewhere.
	result.IsMatch = matchedCount > 0

	return result
}

// matchStrict matches files by both name and size.
func (m *Matcher) matchStrict(searchee *Searchee, torrentFiles []TorrentFile, result *MatchResult) {
	// Build a map of torrent files by normalized name
	torrentByName := make(map[string][]TorrentFile)
	for _, tf := range torrentFiles {
		key := normalizeFileName(tf.Path)
		torrentByName[key] = append(torrentByName[key], tf)
	}

	// Track which torrent files have been matched
	matchedTorrentFiles := make(map[int]bool)

	// Try to match each searchee file
	for _, sf := range searchee.Files {
		key := normalizeFileName(sf.RelPath)
		candidates := torrentByName[key]

		matched := false
		for i, tf := range candidates {
			// Skip already matched files
			tfIdx := findTorrentFileIndex(torrentFiles, tf)
			if matchedTorrentFiles[tfIdx] {
				continue
			}

			// Check size match
			if m.sizesMatch(sf.Size, tf.Size) {
				result.MatchedFiles = append(result.MatchedFiles, MatchedFilePair{
					SearcheeFile: sf,
					TorrentFile:  tf,
				})
				matchedTorrentFiles[tfIdx] = true
				// Remove from candidates to avoid duplicate matching
				torrentByName[key] = append(candidates[:i], candidates[i+1:]...)
				matched = true
				break
			}
		}

		if !matched {
			result.UnmatchedSearcheeFiles = append(result.UnmatchedSearcheeFiles, sf)
		}
	}

	// Collect unmatched torrent files
	for i, tf := range torrentFiles {
		if !matchedTorrentFiles[i] {
			result.UnmatchedTorrentFiles = append(result.UnmatchedTorrentFiles, tf)
		}
	}
}

// matchFlexible matches files by size only, using names to disambiguate ties.
func (m *Matcher) matchFlexible(searchee *Searchee, torrentFiles []TorrentFile, result *MatchResult) {
	// Build a map of torrent files by size
	torrentBySize := buildTorrentSizeMap(torrentFiles)

	// Track which torrent files have been matched
	matchedTorrentFiles := make(map[int]bool)

	// Try to match each searchee file
	for _, sf := range searchee.Files {
		match := m.findFlexibleMatch(sf, torrentFiles, torrentBySize, matchedTorrentFiles)
		if match == nil {
			result.UnmatchedSearcheeFiles = append(result.UnmatchedSearcheeFiles, sf)
			continue
		}

		tfIdx := findTorrentFileIndex(torrentFiles, *match)
		result.MatchedFiles = append(result.MatchedFiles, MatchedFilePair{
			SearcheeFile: sf,
			TorrentFile:  *match,
		})
		matchedTorrentFiles[tfIdx] = true
	}

	// Collect unmatched torrent files
	collectUnmatchedTorrentFiles(torrentFiles, matchedTorrentFiles, result)
}

// buildTorrentSizeMap creates a map of torrent files grouped by size.
func buildTorrentSizeMap(torrentFiles []TorrentFile) map[int64][]TorrentFile {
	m := make(map[int64][]TorrentFile)
	for _, tf := range torrentFiles {
		m[tf.Size] = append(m[tf.Size], tf)
	}
	return m
}

// findFlexibleMatch finds the best matching torrent file for a searchee file using size-only matching.
func (m *Matcher) findFlexibleMatch(
	sf *ScannedFile,
	torrentFiles []TorrentFile,
	torrentBySize map[int64][]TorrentFile,
	matchedTorrentFiles map[int]bool,
) *TorrentFile {
	candidates := m.findSizeCandidates(sf, torrentFiles, torrentBySize, matchedTorrentFiles)
	if len(candidates) == 0 {
		return nil
	}
	return selectBestCandidate(sf, candidates)
}

// findSizeCandidates finds all unmatched torrent files with matching size.
func (m *Matcher) findSizeCandidates(
	sf *ScannedFile,
	torrentFiles []TorrentFile,
	torrentBySize map[int64][]TorrentFile,
	matchedTorrentFiles map[int]bool,
) []TorrentFile {
	var candidates []TorrentFile
	for size, files := range torrentBySize {
		if !m.sizesMatch(sf.Size, size) {
			continue
		}
		for _, tf := range files {
			tfIdx := findTorrentFileIndex(torrentFiles, tf)
			if !matchedTorrentFiles[tfIdx] {
				candidates = append(candidates, tf)
			}
		}
	}
	return candidates
}

// selectBestCandidate selects the best matching torrent file from candidates, preferring name match.
func selectBestCandidate(sf *ScannedFile, candidates []TorrentFile) *TorrentFile {
	if len(candidates) == 1 {
		return &candidates[0]
	}

	// Try to find a name match among candidates
	sfKey := normalizeFileName(sf.RelPath)
	for i := range candidates {
		if normalizeFileName(candidates[i].Path) == sfKey {
			return &candidates[i]
		}
	}

	// If no name match, use first candidate
	return &candidates[0]
}

// collectUnmatchedTorrentFiles adds unmatched torrent files to the result.
func collectUnmatchedTorrentFiles(torrentFiles []TorrentFile, matched map[int]bool, result *MatchResult) {
	for i, tf := range torrentFiles {
		if !matched[i] {
			result.UnmatchedTorrentFiles = append(result.UnmatchedTorrentFiles, tf)
		}
	}
}

// sizesMatch checks if two sizes match within the configured tolerance.
func (m *Matcher) sizesMatch(size1, size2 int64) bool {
	if m.sizeTolerancePct <= 0 {
		return size1 == size2
	}

	// Calculate tolerance based on the larger size
	larger := max(size1, size2)

	tolerance := float64(larger) * (m.sizeTolerancePct / 100.0)
	diff := size1 - size2
	if diff < 0 {
		diff = -diff
	}

	return float64(diff) <= tolerance
}

// normalizeFileName normalizes a file path for comparison.
func normalizeFileName(path string) string {
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, ext)

	// Strip TRaSH naming tags if present (e.g. "{tmdb-12345}", "{imdb-tt...}", "[tvdb-123]").
	if cleaned, _ := extractTRaSHMetadata(base); cleaned != "" {
		base = cleaned
	}

	base = strings.NewReplacer(
		".", " ",
		"_", " ",
		"-", " ",
		"[", " ",
		"]", " ",
		"(", " ",
		")", " ",
		"{", " ",
		"}", " ",
	).Replace(base)
	base = cleanExtraSpaces(base)

	normalized := stringutils.NormalizeForMatching(base)
	ext = strings.TrimPrefix(ext, ".")
	if ext == "" {
		return normalized
	}

	// Keep extension as part of strict matching so that e.g. ".mkv" and ".mp4" don't collide.
	return normalized + " " + ext
}

// findTorrentFileIndex finds the index of a torrent file in the slice.
func findTorrentFileIndex(files []TorrentFile, target TorrentFile) int {
	for i, f := range files {
		if f.Path == target.Path && f.Size == target.Size {
			return i
		}
	}
	return -1
}

// CalculateTotalSize calculates the total size of a searchee.
func CalculateTotalSize(searchee *Searchee) int64 {
	var total int64
	for _, f := range searchee.Files {
		total += f.Size
	}
	return total
}

// ParsedTorrent holds parsed torrent metadata.
type ParsedTorrent struct {
	Name        string
	InfoHash    string
	Files       []TorrentFile
	TotalSize   int64
	PieceLength int64
	PieceCount  int
}

// ParseTorrentBytes parses a .torrent file and extracts file information.
func ParseTorrentBytes(data []byte) (*ParsedTorrent, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse torrent: %w", err)
	}

	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("unmarshal info: %w", err)
	}

	parsed := &ParsedTorrent{
		Name:        info.BestName(),
		InfoHash:    mi.HashInfoBytes().HexString(),
		PieceLength: info.PieceLength,
		PieceCount:  info.NumPieces(),
	}

	// Build file list with byte offsets
	var offset int64
	if len(info.Files) == 0 {
		// Single-file torrent
		parsed.Files = []TorrentFile{{
			Path:   info.BestName(),
			Size:   info.Length,
			Offset: 0,
		}}
		parsed.TotalSize = info.Length
	} else {
		// Multi-file torrent
		parsed.Files = make([]TorrentFile, 0, len(info.Files))
		root := info.BestName()
		for i := range info.Files {
			f := &info.Files[i]
			pathParts := f.BestPath()
			// For multi-file torrents, qBittorrent's "Original" layout places files under the
			// top-level folder named by the torrent's info.name.
			//
			// The torrent file list itself typically does NOT include that folder in each file path,
			// so we include it here to reflect the on-disk paths qBittorrent will expect.
			if root != "" && (len(pathParts) == 0 || pathParts[0] != root) {
				pathParts = append([]string{root}, pathParts...)
			}
			tf := TorrentFile{
				Path:   path.Join(pathParts...),
				Size:   f.Length,
				Offset: offset,
			}
			parsed.Files = append(parsed.Files, tf)
			offset += f.Length
		}
		parsed.TotalSize = offset
	}

	return parsed, nil
}
