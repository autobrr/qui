// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package gazellemusic

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"path"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"
)

type MatchKind string

const (
	MatchHash     MatchKind = "hash"
	MatchSize     MatchKind = "size"
	MatchFileList MatchKind = "filelist"
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
	RemoteFiles map[string]int64
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
				meta, err := selectedMetadata(ctx, c, r)
				if err != nil {
					return nil, err
				}
				match := discoveryMatch(c, r, meta, MatchSize)
				remote := parseFileList(meta.Torrent.FileList)
				if fileTotal(remote) == totalSize && !filesConflict(localFiles, remote) {
					match.Kind = MatchFileList
					match.RemoteFiles = remote
				}
				return match, nil
			}
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

func selectedMetadata(ctx context.Context, c *Client, r TorrentSearchResult) (*TorrentResponse, error) {
	meta, err := c.GetTorrent(ctx, r.TorrentID)
	if err != nil {
		return nil, err
	}
	if meta == nil || !validIdentity(meta.Torrent.ID, meta.Torrent.Size, meta.Torrent.InfoHash) || meta.Torrent.ID != r.TorrentID || meta.Torrent.Size != r.Size {
		return nil, fmt.Errorf("selected torrent %d returned inconsistent target metadata", r.TorrentID)
	}
	return meta, nil
}

func discoveryMatch(c *Client, r TorrentSearchResult, meta *TorrentResponse, kind MatchKind) *Match {
	return &Match{Host: c.Host(), SourceFlag: c.SourceFlag(), TorrentID: r.TorrentID, GroupID: r.GroupID, Size: meta.Torrent.Size, Title: r.Title, Kind: kind, TargetHash: strings.ToLower(meta.Torrent.InfoHash)}
}

func fileTotal(files map[string]int64) int64 {
	var total int64
	for _, size := range files {
		if size < 0 || size > math.MaxInt64-total {
			return 0
		}
		total += size
	}
	return total
}

func parseFileList(fileList string) map[string]int64 {
	result := make(map[string]int64, strings.Count(fileList, "|||")+1)

	for line := range strings.SplitSeq(fileList, "|||") {
		fileName, sizeField, found := strings.Cut(line, "{{{")
		if !found || strings.Contains(sizeField, "{{{") {
			return nil
		}
		sizeStr := strings.TrimSuffix(sizeField, "}}}")
		size, err := strconv.ParseInt(sizeStr, 10, 64)
		if err != nil || size < 0 || !safeFileListPath(fileName) {
			return nil
		}
		if _, duplicate := result[fileName]; duplicate {
			return nil
		}
		result[fileName] = size
	}

	return result
}

func filesConflict(localFiles, remoteFiles map[string]int64) bool {
	return len(localFiles) == 0 || len(localFiles) != len(remoteFiles) || FileListFingerprint(localFiles) != FileListFingerprint(remoteFiles)
}

// FileListFingerprint uses the same root and name normalization as discovery,
// retaining multiplicity and nested paths in a length-delimited snapshot.
func FileListFingerprint(files map[string]int64) [32]byte {
	type fileSig struct {
		size int64
		name string // relative path, then normalized with the root folder stripped when consistent
	}

	sigs := make([]fileSig, 0, len(files))
	for name, size := range files {
		n := strings.TrimPrefix(path.Clean(strings.ReplaceAll(name, "\\", "/")), "./")
		sigs = append(sigs, fileSig{size: size, name: strings.TrimPrefix(n, "/")})
	}

	rootPrefix := func() string {
		root := ""
		for _, sig := range sigs {
			first, _, nested := strings.Cut(sig.name, "/")
			if !nested || first == "" {
				return ""
			}
			if root == "" {
				root = first
				continue
			}
			if root != first {
				return ""
			}
		}
		if root == "" {
			return ""
		}
		return root + "/"
	}()

	normalize := func(n string) string {
		if rootPrefix != "" {
			if trimmed, ok := strings.CutPrefix(n, rootPrefix); ok {
				n = trimmed
			}
		}
		var out strings.Builder
		out.Grow(len(n))
		joined := false
		for part := range strings.SplitSeq(n, "/") {
			if part == "" || part == "." {
				continue
			}
			if joined {
				out.WriteByte('/')
			}
			joined = true
			out.WriteString(normalizeFilenameForCompare(part))
		}
		return out.String()
	}

	snapshotLen := 8
	for i := range sigs {
		sigs[i].name = normalize(sigs[i].name)
		snapshotLen += 16 + len(sigs[i].name)
	}
	slices.SortFunc(sigs, func(a, b fileSig) int {
		return cmp.Or(strings.Compare(a.name, b.name), cmp.Compare(a.size, b.size))
	})
	snapshot := make([]byte, 0, snapshotLen)
	snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(len(sigs)))
	for _, sig := range sigs {
		snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(len(sig.name)))
		snapshot = append(snapshot, sig.name...)
		snapshot = binary.BigEndian.AppendUint64(snapshot, uint64(sig.size))
	}
	return sha256.Sum256(snapshot)
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

// normalizeFilenameForCompare drops the runes that zeroWidthChars matches and
// turns each run of spaces, dots, underscores, and hyphens into one space.
// Bytes of invalid UTF-8 stay as they are.
func normalizeFilenameForCompare(name string) string {
	name = strings.TrimSpace(strings.ToLower(name))
	var b strings.Builder
	b.Grow(len(name))
	inSpace := false
	for i := 0; i < len(name); {
		r, size := utf8.DecodeRuneInString(name[i:])
		chunk := name[i : i+size]
		i += size
		switch {
		case r <= 0x1F || r == 0x7F || (r >= 0x200B && r <= 0x200D) || r == 0xFEFF:
		case r == ' ' || r == '.' || r == '_' || r == '-':
			if !inSpace {
				b.WriteByte(' ')
			}
			inSpace = true
		default:
			inSpace = false
			b.WriteString(chunk)
		}
	}
	return b.String()
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

func safeFileListPath(name string) bool {
	if name == "" || name != path.Clean(name) || path.IsAbs(name) || strings.ContainsAny(name, "\\\x00") {
		return false
	}
	if len(name) >= 2 && name[1] == ':' && ((name[0] >= 'a' && name[0] <= 'z') || (name[0] >= 'A' && name[0] <= 'Z')) {
		return false
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == ".." || part == "." {
			return false
		}
	}
	return true
}
