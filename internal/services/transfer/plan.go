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

func diskBytes(plan []file) int64 {
	var total int64
	for _, f := range plan {
		if !f.pad {
			total += f.size
		}
	}
	return total
}
