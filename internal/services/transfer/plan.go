// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"cmp"
	"fmt"
	"path"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/go-torrent/metainfo"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/helpertorrent"
)

type file struct {
	offset   int64
	size     int64
	pad      bool
	name     string
	onTarget bool
}

func planFiles(info *metainfo.Info, listed qbt.TorrentFiles) ([]file, error) {
	listed = slices.SortedFunc(slices.Values(listed), func(a, b qbt.TorrentFile) int { return cmp.Compare(a.Index, b.Index) })
	var (
		plan   []file
		offset int64
		next   int
	)
	for _, f := range info.UpvertedFiles() {
		planned := file{offset: offset, size: f.Length, pad: helpertorrent.IsPadding(f)}
		offset += f.Length
		if !planned.pad {
			if next == len(listed) {
				return nil, fmt.Errorf("qBittorrent lists %d files", len(listed))
			}
			l := listed[next]
			next++
			if l.Size != f.Length {
				return nil, fmt.Errorf("%s has %d bytes in qBittorrent and %d in the .torrent", l.Name, l.Size, f.Length)
			}
			if !relativeName(l.Name) {
				return nil, fmt.Errorf("the file name %q is not a relative path", l.Name)
			}
			planned.name = l.Name
		}
		plan = append(plan, planned)
	}
	if next != len(listed) {
		return nil, fmt.Errorf("qBittorrent lists %d files, and the .torrent has %d", len(listed), next)
	}
	return plan, nil
}

func relativeName(name string) bool {
	return name != "" && path.Clean(name) == name && !path.IsAbs(name) && !strings.ContainsRune(name, '\\') &&
		!slices.Contains(strings.Split(name, "/"), "..")
}

type part struct {
	*Member
	position        int
	src             *source
	dir             string
	plan            []file
	proven          []byte
	checked         []byte
	headStartProven bool
	passed          bool
}

func (p *part) rel(f file) string {
	return path.Join(p.dir, f.name)
}

type groupFile struct {
	rel        string
	size       int64
	owner      *part
	name       string
	onTarget   bool
	linked     bool
	hardlinkOf *groupFile
	linksCopy  bool
}

func groupFiles(parts []*part) (files []*groupFile, byRel map[string]*groupFile, err error) {
	byRel = map[string]*groupFile{}
	for _, p := range parts {
		for _, f := range p.plan {
			if f.pad {
				continue
			}
			rel := p.rel(f)
			if have, ok := byRel[rel]; ok {
				if have.size != f.size {
					return nil, nil, fmt.Errorf("%s has %d bytes in %s and %d in %s", rel, have.size, have.owner.Name, f.size, p.Name)
				}
				continue
			}
			byRel[rel] = &groupFile{rel: rel, size: f.size, owner: p, name: f.name}
			files = append(files, byRel[rel])
		}
	}
	return files, byRel, nil
}

func addLayout(info *metainfo.Info, plan []file) qbt.ContentLayout {
	switch {
	case info.IsDir() && commonRoot(names(plan)) == "":
		return qbt.ContentLayoutSubfolderNone
	case !info.IsDir() && inOwnFolder(info.BestName(), names(plan)):
		return qbt.ContentLayoutSubfolderCreate
	}
	return qbt.ContentLayoutOriginal
}

func inOwnFolder(file string, listed []string) bool {
	if len(listed) != 1 {
		return false
	}
	folder, name, nested := strings.Cut(listed[0], "/")
	return nested && name == file && (file == folder || strings.HasPrefix(file, folder+"."))
}

func takenName(info *metainfo.Info, plan []file) string {
	if !info.IsDir() {
		return ""
	}
	folder := info.BestName() + "/"
	if addLayout(info, plan) == qbt.ContentLayoutSubfolderNone {
		folder = ""
	}
	files := info.UpvertedFiles()
	original := make(map[string]int, len(files))
	for i, f := range files {
		if !plan[i].pad {
			original[strings.ToLower(folder+strings.Join(f.BestPath(), "/"))] = i
		}
	}
	for i, f := range plan {
		if other, ok := original[strings.ToLower(f.name)]; ok && other != i {
			return f.name
		}
	}
	return ""
}

func names(plan []file) []string {
	var out []string
	for _, f := range plan {
		if !f.pad {
			out = append(out, f.name)
		}
	}
	return out
}

func groupBytes(d fsops.PathDialect, group []*source) int64 {
	seen := map[string]bool{}
	var total int64
	for _, src := range group {
		for _, f := range src.files {
			at := d.Join(src.torrent.SavePath, d.FromSlash(f.Name))
			if !seen[at] {
				seen[at] = true
				total += f.Size
			}
		}
	}
	return total
}

func diskBytes(files []*groupFile) int64 {
	var total int64
	for _, f := range files {
		if f.hardlinkOf == nil {
			total += f.size
		}
	}
	return total
}
