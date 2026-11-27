// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"bytes"
	"fmt"
	"path"
	"path/filepath"
	"strings"

	"github.com/autobrr/go-torrent/metainfo"

	"github.com/autobrr/qui/pkg/pathutil"
	"github.com/autobrr/qui/pkg/stringutils"
)

type MatchMode string

const (
	MatchModeStrict   MatchMode = "strict"
	MatchModeFlexible MatchMode = "flexible"
)

type MatchResult struct {
	IsMatch bool

	MatchedFiles []MatchedFilePair

	UnmatchedSearcheeFiles []*ScannedFile

	UnmatchedTorrentFiles []TorrentFile

	MatchRatio float64

	SizeMatchRatio float64

	IsPerfectMatch bool

	IsPartialMatch bool
}

type MatchedFilePair struct {
	SearcheeFile *ScannedFile
	TorrentFile  TorrentFile
}

type TorrentFile struct {
	Path   string
	Size   int64
	Offset int64
}

type Matcher struct {
	mode MatchMode
}

func NewMatcher(mode MatchMode, _ float64) *Matcher {
	if mode == "" {
		mode = MatchModeStrict
	}
	return &Matcher{
		mode: mode,
	}
}

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

	switch m.mode {
	case MatchModeStrict:
		m.matchStrict(searchee, torrentFiles, result)
	case MatchModeFlexible:
		m.matchFlexible(searchee, torrentFiles, result)
	default:
		m.matchStrict(searchee, torrentFiles, result)
	}

	totalSearcheeFiles := len(searchee.Files)
	totalTorrentFiles := len(torrentFiles)
	matchedCount := len(result.MatchedFiles)

	if totalSearcheeFiles > 0 {
		result.MatchRatio = float64(matchedCount) / float64(totalSearcheeFiles)
	}

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

	allSearcheeFilesMatched := matchedCount == totalSearcheeFiles
	allTorrentFilesMatched := matchedCount == totalTorrentFiles
	result.IsPerfectMatch = allSearcheeFilesMatched && allTorrentFilesMatched
	result.IsPartialMatch = matchedCount > 0 && !result.IsPerfectMatch
	result.IsMatch = matchedCount > 0

	return result
}

func (m *Matcher) matchStrict(searchee *Searchee, torrentFiles []TorrentFile, result *MatchResult) {
	torrentByName := make(map[string][]TorrentFile)
	for _, tf := range torrentFiles {
		key := normalizeFileName(tf.Path)
		torrentByName[key] = append(torrentByName[key], tf)
	}

	matchedTorrentFiles := make(map[int]bool)

	for _, sf := range searchee.Files {
		key := normalizeFileName(sf.RelPath)
		candidates := torrentByName[key]

		matched := false
		for i, tf := range candidates {
			tfIdx := findTorrentFileIndex(torrentFiles, tf)
			if matchedTorrentFiles[tfIdx] {
				continue
			}

			if sizesMatchExactly(sf.Size, tf.Size) {
				result.MatchedFiles = append(result.MatchedFiles, MatchedFilePair{
					SearcheeFile: sf,
					TorrentFile:  tf,
				})
				matchedTorrentFiles[tfIdx] = true
				torrentByName[key] = append(candidates[:i], candidates[i+1:]...)
				matched = true
				break
			}
		}

		if !matched {
			result.UnmatchedSearcheeFiles = append(result.UnmatchedSearcheeFiles, sf)
		}
	}

	for i, tf := range torrentFiles {
		if !matchedTorrentFiles[i] {
			result.UnmatchedTorrentFiles = append(result.UnmatchedTorrentFiles, tf)
		}
	}
}

func (m *Matcher) matchFlexible(searchee *Searchee, torrentFiles []TorrentFile, result *MatchResult) {
	torrentBySize := buildTorrentSizeMap(torrentFiles)

	matchedTorrentFiles := make(map[int]bool)

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

	collectUnmatchedTorrentFiles(torrentFiles, matchedTorrentFiles, result)
}

func buildTorrentSizeMap(torrentFiles []TorrentFile) map[int64][]TorrentFile {
	m := make(map[int64][]TorrentFile)
	for _, tf := range torrentFiles {
		m[tf.Size] = append(m[tf.Size], tf)
	}
	return m
}

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

func (m *Matcher) findSizeCandidates(
	sf *ScannedFile,
	torrentFiles []TorrentFile,
	torrentBySize map[int64][]TorrentFile,
	matchedTorrentFiles map[int]bool,
) []TorrentFile {
	var candidates []TorrentFile
	for size, files := range torrentBySize {
		if !sizesMatchExactly(sf.Size, size) {
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

func selectBestCandidate(sf *ScannedFile, candidates []TorrentFile) *TorrentFile {
	if len(candidates) == 1 {
		return &candidates[0]
	}

	sfKey := normalizeFileName(sf.RelPath)
	for i := range candidates {
		if normalizeFileName(candidates[i].Path) == sfKey {
			return &candidates[i]
		}
	}

	return &candidates[0]
}

func collectUnmatchedTorrentFiles(torrentFiles []TorrentFile, matched map[int]bool, result *MatchResult) {
	for i, tf := range torrentFiles {
		if !matched[i] {
			result.UnmatchedTorrentFiles = append(result.UnmatchedTorrentFiles, tf)
		}
	}
}

func sizesMatchExactly(size1, size2 int64) bool {
	return size1 == size2
}

func normalizeFileName(path string) string {
	name := filepath.Base(path)
	ext := strings.ToLower(filepath.Ext(name))
	base := strings.TrimSuffix(name, ext)

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

	return normalized + " " + ext
}

func findTorrentFileIndex(files []TorrentFile, target TorrentFile) int {
	for i, f := range files {
		if f.Path == target.Path && f.Size == target.Size {
			return i
		}
	}
	return -1
}

func CalculateTotalSize(searchee *Searchee) int64 {
	var total int64
	for _, f := range searchee.Files {
		total += f.Size
	}
	return total
}

type ParsedTorrent struct {
	Name        string
	InfoHash    string
	InfoHashV2  string
	Files       []TorrentFile
	TotalSize   int64
	PieceLength int64
	PieceCount  int
}

func ParseTorrentBytes(data []byte) (*ParsedTorrent, error) {
	mi, err := metainfo.Load(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("parse torrent: %w", err)
	}

	info, err := mi.UnmarshalInfo()
	if err != nil {
		return nil, fmt.Errorf("unmarshal info: %w", err)
	}

	name := pathutil.TorrentPathName(info.BestName())
	root := name
	if root == "" {
		if root, err = pathutil.TorrentPathComponent(info.BestName()); err != nil {
			return nil, err
		}
	}

	parsed := &ParsedTorrent{
		Name:        name,
		InfoHash:    mi.HashInfoBytes().HexString(),
		PieceLength: info.PieceLength,
		PieceCount:  info.NumPieces(),
	}
	if info.HasV2() {
		parsed.InfoHashV2 = metainfo.HashV2Bytes(mi.InfoBytes).HexString()
	}

	var offset int64
	if len(info.Files) == 0 {
		parsed.Files = []TorrentFile{{
			Path:   name,
			Size:   info.Length,
			Offset: 0,
		}}
		parsed.TotalSize = info.Length
	} else {
		parsed.Files = make([]TorrentFile, 0, len(info.Files))
		for i := range info.Files {
			f := &info.Files[i]
			filePath, err := pathutil.TorrentFilePath(root, f.BestPath())
			if err != nil {
				return nil, err
			}
			tf := TorrentFile{
				Path:   path.Clean(filePath),
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
