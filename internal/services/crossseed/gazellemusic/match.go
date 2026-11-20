// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"context"
	"encoding/hex"
	"errors"
	"path"
	"regexp"
	"sort"
	"strings"
)

type MatchKind string

const maxSearchResults = 20

const (
	MatchHash MatchKind = "hash"
	MatchSize MatchKind = "size"
)

type Match struct {
	Host        string
	SourceFlag  string
	TorrentID   int64
	GroupID     int64
	Size        int64
	Title       string
	Kind        MatchKind
	TargetHash  string
	QueriedHash string
}

// FindMatch distinguishes a completed lookup from a transport or identity failure.
func FindMatch(ctx context.Context, c *Client, torrentBytes []byte, localFiles map[string]int64, totalSize int64) (*Match, error) {
	if torrentBytes != nil {
		if hashes, err := c.TargetHashes(torrentBytes); err == nil {
			for _, targetHash := range hashes {
				res, err := c.SearchByHash(ctx, targetHash)
				if err != nil {
					return nil, err
				}
				if res != nil {
					// OPS names the hash only on a torrent the user snatched or
					// uploaded. The hit itself confirms the queried hash.
					if res.InfoHash == "" {
						res.InfoHash = targetHash
					}
					if !validIdentity(res.TorrentID, res.Size, res.InfoHash) || !strings.EqualFold(res.InfoHash, targetHash) {
						return nil, errors.New("hash lookup returned inconsistent target identity")
					}
					return &Match{Host: c.Host(), SourceFlag: c.SourceFlag(), TorrentID: res.TorrentID, GroupID: res.GroupID, Size: res.Size, Title: res.Title, Kind: MatchHash, TargetHash: strings.ToLower(res.InfoHash), QueriedHash: targetHash}, nil
				}
			}
		}
	}
	var lookupErr error
	for _, fname := range selectSearchFilenames(localFiles, 5) {
		query := makeSearchQuery(fname)
		if query == "" {
			continue
		}
		results, err := c.SearchByFilename(ctx, query)
		if errors.Is(err, ErrAccessDenied) {
			return nil, err
		}
		if err != nil {
			lookupErr = err
			continue
		}
		for _, r := range results {
			if totalSize > 0 && r.Size == totalSize && r.TorrentID > 0 {
				return &Match{Host: c.Host(), SourceFlag: c.SourceFlag(), TorrentID: r.TorrentID, GroupID: r.GroupID, Size: r.Size, Title: r.Title, Kind: MatchSize}, nil
			}
		}
		if len(results) > maxSearchResults {
			continue
		}
		if isMusicFile(fname) {
			break
		}
	}
	return nil, lookupErr
}

func validIdentity(id, size int64, hash string) bool {
	decoded, err := hex.DecodeString(hash)
	return id > 0 && size > 0 && err == nil && len(decoded) == 20
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
	ext := strings.ToLower(path.Ext(filename))
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
	name := path.Base(filename)
	ext := path.Ext(name)
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
