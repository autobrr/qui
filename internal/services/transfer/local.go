// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func (j *job) linkFiles(ctx context.Context, targetFiles fsops.Backend) error {
	if j.made == nil {
		j.made = map[*groupFile]int64{}
	}
	src, d := j.srcFiles.Paths(), targetFiles.Paths()
	for _, f := range j.files {
		if f.onTarget {
			continue
		}
		source := src.Join(f.owner.src.torrent.SavePath, src.FromSlash(f.name))
		temp := d.Join(j.rootDir, d.FromSlash(j.tempName(f.rel)))
		if _, made := j.made[f]; made {
			f.linked = j.linkedTo(ctx, targetFiles, temp, source)
			continue
		}
		same, err := targetFiles.SameFilesystem(ctx, source, j.rootDir)
		if err != nil || !same {
			continue
		}
		plan, err := hardlinktree.BuildSingleFilePlan(d, j.rootDir, j.tempName(f.rel), source)
		if err != nil {
			return permanent("qui cannot place %s on %s: %v", f.rel, j.target.Name, err)
		}
		if _, err := targetFiles.HardlinkTree(ctx, plan); err != nil {
			j.logf("qui cannot link %s on %s, so it copies the file: %v", f.rel, j.target.Name, err)
			continue
		}
		j.made[f] = f.size
		f.linked = true
	}
	return nil
}

func (j *job) linkedTo(ctx context.Context, targetFiles fsops.Backend, temp, source string) bool {
	t, err := targetFiles.Lstat(ctx, temp)
	if err != nil {
		return false
	}
	s, err := j.srcFiles.Lstat(ctx, source)
	return err == nil && fsops.CompareFiles(j.target, t.FileID, j.source, s.FileID) == fsops.FilesSame
}

func linkedBytes(files []*groupFile) (int, int64) {
	var n int
	var size int64
	for _, f := range files {
		if f.linked {
			n++
			size += f.size
		}
	}
	return n, size
}
