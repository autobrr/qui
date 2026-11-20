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

// file is one file of a torrent, in the order of its .torrent.
type file struct {
	offset int64
	size   int64
	// pad is a padding file: zeros that are on no disk.
	pad bool
	// name is the path that qBittorrent reports on the source, relative to
	// the save path. The file keeps it on the target.
	name string
	// onTarget is set when the target had the file before the Transfer. qui
	// checks it there and never writes it.
	onTarget bool
}

// planFiles lines up the files of the .torrent with the names that
// qBittorrent reports, which can differ after a rename.
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

// part is one member of a Transfer as one run reads it on the source.
type part struct {
	*Member
	position int
	src      *source
	// dir is the slash folder of the member below the root of the Transfer,
	// "" for the root itself.
	dir    string
	plan   []file
	proven []byte
	// checked holds the pieces that the last check of the partial files on
	// the target passed.
	checked []byte
	// headStartProven is set when the check of the files that were on the
	// target passed every piece of the member.
	headStartProven bool
	// passed is set once the target has every piece of the Transfer torrent
	// of the member: BitTorrent checked each piece.
	passed bool
}

func (p *part) rel(f file) string {
	return path.Join(p.dir, f.name)
}

// groupFile is a file of the group. The copy writes it once, with the pieces
// of owner, the first member that has it.
type groupFile struct {
	rel   string
	size  int64
	owner *part
	// name is the name of the file in owner.
	name string
	// onTarget is set when the target had the file before the Transfer. qui
	// checks it there and never writes it.
	onTarget bool
	// linked is set when the temporary file is a hardlink to the file on the
	// source. qui never writes it.
	linked bool
	// hardlinkOf is an earlier file of the group that is this file on the
	// source, through a hardlink.
	hardlinkOf *groupFile
	// linksCopy is set when the temporary file is a hardlink to the
	// temporary file of hardlinkOf. qui never writes it.
	linksCopy bool
}

// groupFiles lists each file of the members once, in the order of the copy.
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

// addLayout is the content layout of the add on the target, the one that
// gives the files the names that they have on the source: the names of the
// .torrent, the files of a folder torrent without its folder, or the one
// file in the folder that the layout "Create subfolder" made for it on the
// source. Names that no layout gives get a rename after the add.
func addLayout(info *metainfo.Info, plan []file) qbt.ContentLayout {
	switch {
	case info.IsDir() && commonRoot(names(plan)) == "":
		return qbt.ContentLayoutSubfolderNone
	case !info.IsDir() && inOwnFolder(info.BestName(), names(plan)):
		return qbt.ContentLayoutSubfolderCreate
	}
	return qbt.ContentLayoutOriginal
}

// inOwnFolder reports whether the one file of a torrent is on the source in
// a folder that qBittorrent can make for it: the name of the file without
// an extension. qBittorrent decides what the extension is.
func inOwnFolder(file string, listed []string) bool {
	if len(listed) != 1 {
		return false
	}
	folder, name, nested := strings.Cut(listed[0], "/")
	return nested && name == file && (file == folder || strings.HasPrefix(file, folder+"."))
}

// takenName returns the source name of a file that the .torrent gives
// another file on the target, or "". The rename after the add would move
// the bytes of this file to the other file. Names compare without case, as
// on a target whose filesystem ignores case.
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

// names are the names of the files that are on disk.
func names(plan []file) []string {
	var out []string
	for _, f := range plan {
		if !f.pad {
			out = append(out, f.name)
		}
	}
	return out
}

// groupBytes is the size of the files of the group, a file that two members
// read at one path once.
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

// diskBytes is the size of the files on the source, a file with hardlinks
// once.
func diskBytes(files []*groupFile) int64 {
	var total int64
	for _, f := range files {
		if f.hardlinkOf == nil {
			total += f.size
		}
	}
	return total
}
