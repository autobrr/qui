// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"context"
	"math"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

const maxSearchResults = 20

type Match struct {
	Host       string
	SourceFlag string
	TorrentID  int64
	GroupID    int64
	Size       int64
	Title      string
	Reason     string // "hash", "size", "filelist"
}

func FindMatch(ctx context.Context, c *Client, torrentBytes []byte, localFiles map[string]int64, totalSize int64) (*Match, error) {
	// 1) Hash-based match (preferred).
	if torrentBytes != nil {
		hashes, err := CalculateHashesWithSources(torrentBytes, []string{c.SourceFlag()})
		if err == nil {
			targetHash := hashes[c.SourceFlag()]
			if targetHash != "" {
				if res, err := c.SearchByHash(ctx, targetHash); err == nil && res != nil {
					return &Match{
						Host:       c.Host(),
						SourceFlag: c.SourceFlag(),
						TorrentID:  res.TorrentID,
						GroupID:    res.GroupID,
						Size:       res.Size,
						Title:      res.Title,
						Reason:     "hash",
					}, nil
				}
			}
		}
	}

	// 2) Filename search + verify by size/filelist.
	searchFiles := selectSearchFilenames(localFiles, 5)
	for _, fname := range searchFiles {
		query := makeSearchQuery(fname)
		if query == "" {
			continue
		}

		results, err := c.SearchByFilename(ctx, query)
		if err != nil {
			continue
		}

		for _, r := range results {
			if r.Size == totalSize && r.TorrentID > 0 {
				return &Match{
					Host:       c.Host(),
					SourceFlag: c.SourceFlag(),
					TorrentID:  r.TorrentID,
					GroupID:    r.GroupID,
					Size:       r.Size,
					Title:      r.Title,
					Reason:     "size",
				}, nil
			}
		}

		if len(results) > maxSearchResults {
			continue
		}

		// Pre-filter by size proximity (within 10% tolerance).
		sizeTolerance := float64(totalSize) * 0.1
		for _, r := range results {
			sizeDiff := math.Abs(float64(r.Size) - float64(totalSize))
			if sizeDiff > sizeTolerance {
				continue
			}

			torrentResp, err := c.GetTorrent(ctx, r.TorrentID)
			if err != nil || torrentResp == nil {
				continue
			}

			remoteFiles := parseFileList(torrentResp.Torrent.FileList)
			if filesConflict(localFiles, remoteFiles) {
				continue
			}

			return &Match{
				Host:       c.Host(),
				SourceFlag: c.SourceFlag(),
				TorrentID:  r.TorrentID,
				GroupID:    r.GroupID,
				Size:       r.Size,
				Title:      r.Title,
				Reason:     "filelist",
			}, nil
		}

		// Stop early if we already tried a music file.
		if isMusicFile(fname) {
			break
		}
	}

	return nil, nil
}

func parseFileList(fileList string) map[string]int64 {
	result := make(map[string]int64)

	for line := range strings.SplitSeq(fileList, "|||") {
		parts := strings.Split(line, "{{{")
		if len(parts) != 2 {
			continue
		}
		fileName := parts[0]
		sizeStr := strings.TrimSuffix(parts[1], "}}}")
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil {
			continue
		}
		fileName = path.Clean(fileName)
		result[fileName] = size
	}

	return result
}

func filesConflict(localFiles, remoteFiles map[string]int64) bool {
	if len(localFiles) != len(remoteFiles) {
		return true
	}

	type fileSig struct {
		size int64
		name string // normalized relative path (root folder stripped when consistent)
	}

	singleRoot := func(files map[string]int64) string {
		root := ""
		for name := range files {
			n := strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
			n = strings.TrimPrefix(n, "/")
			if n == "" || n == "." {
				return ""
			}
			parts := strings.Split(n, "/")
			if len(parts) < 2 {
				return ""
			}
			if parts[0] == "" {
				return ""
			}
			if root == "" {
				root = parts[0]
				continue
			}
			if root != parts[0] {
				return ""
			}
		}
		return root
	}

	localRoot := singleRoot(localFiles)
	remoteRoot := singleRoot(remoteFiles)

	normalize := func(name string, root string) string {
		n := strings.ReplaceAll(name, "\\", "/")
		n = strings.TrimPrefix(path.Clean(n), "./")
		n = strings.TrimPrefix(n, "/")
		if root != "" {
			prefix := root + "/"
			if trimmed, ok := strings.CutPrefix(n, prefix); ok {
				n = trimmed
			}
		}
		if n == "." {
			n = ""
		}
		parts := strings.Split(n, "/")
		out := make([]string, 0, len(parts))
		for _, part := range parts {
			if part == "" || part == "." {
				continue
			}
			out = append(out, normalizeFilenameForCompare(part))
		}
		return strings.Join(out, "/")
	}

	localSigs := make([]fileSig, 0, len(localFiles))
	for name, size := range localFiles {
		localSigs = append(localSigs, fileSig{
			size: size,
			name: normalize(name, localRoot),
		})
	}
	remoteSigs := make([]fileSig, 0, len(remoteFiles))
	for name, size := range remoteFiles {
		remoteSigs = append(remoteSigs, fileSig{
			size: size,
			name: normalize(name, remoteRoot),
		})
	}

	sort.Slice(localSigs, func(i, j int) bool {
		if localSigs[i].size != localSigs[j].size {
			return localSigs[i].size < localSigs[j].size
		}
		return localSigs[i].name < localSigs[j].name
	})
	sort.Slice(remoteSigs, func(i, j int) bool {
		if remoteSigs[i].size != remoteSigs[j].size {
			return remoteSigs[i].size < remoteSigs[j].size
		}
		return remoteSigs[i].name < remoteSigs[j].name
	})

	for i := range localSigs {
		if localSigs[i] != remoteSigs[i] {
			return true
		}
	}

	return false
}

var musicExtensions = map[string]bool{
	".flac": true,
	".mp3":  true,
	".dsf":  true,
	".dff":  true,
	".m4a":  true,
	".ogg":  true,
	".opus": true,
	".wav":  true,
	".aiff": true,
}

func isMusicFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	return musicExtensions[ext]
}

func selectSearchFilenames(files map[string]int64, maxCount int) []string {
	type fileEntry struct {
		name string
		size int64
	}

	entries := make([]fileEntry, 0, len(files))
	for name, size := range files {
		entries = append(entries, fileEntry{name: name, size: size})
	}

	sort.Slice(entries, func(i, j int) bool {
		iMusic := isMusicFile(entries[i].name)
		jMusic := isMusicFile(entries[j].name)
		if iMusic != jMusic {
			return iMusic
		}
		return len(entries[i].name) > len(entries[j].name)
	})

	if maxCount <= 0 {
		maxCount = 5
	}

	result := make([]string, 0, maxCount)
	for i := 0; i < len(entries) && len(result) < maxCount; i++ {
		result = append(result, entries[i].name)
	}
	return result
}

var (
	garbledChars   = regexp.MustCompile(`[^\p{L}\p{N}\s\-_.'()\[\]{}]`)
	multipleSpaces = regexp.MustCompile(`\s+`)
	zeroWidthChars = regexp.MustCompile("[\x00-\x1F\x7F\u200B-\u200D\uFEFF]")
)

func normalizeFilenameForCompare(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	name = zeroWidthChars.ReplaceAllString(name, "")

	// Common torrent name separators; keep extensions but make formatting differences match.
	name = strings.NewReplacer(
		".", " ",
		"_", " ",
		"-", " ",
	).Replace(name)

	name = multipleSpaces.ReplaceAllString(name, " ")
	return name
}

var genericFilenames = map[string]bool{
	"cover":    true,
	"folder":   true,
	"front":    true,
	"back":     true,
	"cd":       true,
	"disc":     true,
	"disk":     true,
	"artwork":  true,
	"booklet":  true,
	"inlay":    true,
	"inside":   true,
	"outside":  true,
	"scan":     true,
	"scans":    true,
	"thumb":    true,
	"albumart": true,
}

func makeSearchQuery(filename string) string {
	name := filepath.Base(filename)
	ext := filepath.Ext(name)
	if ext != "" {
		name = strings.TrimSuffix(name, ext)
	}

	name = zeroWidthChars.ReplaceAllString(name, "")
	name = garbledChars.ReplaceAllString(name, " ")
	name = multipleSpaces.ReplaceAllString(name, " ")
	name = strings.TrimSpace(name)

	if genericFilenames[strings.ToLower(name)] {
		return ""
	}
	return name
}
