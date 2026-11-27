// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

type space struct {
	free, freed int64
	known       bool
}

func (j *job) freeSpace(ctx context.Context) (space, error) {
	read := space{freed: j.svc.freedOn(j.target.ID), known: true}
	if !j.blind {
		stat, err := j.dstFiles.Statfs(ctx, j.rootDir)
		if err != nil {
			return space{}, temporary("qui cannot read the free space on %s: %w", j.target.Name, targetError{err})
		}
		read.free = stat.BytesAvailable
		return read, nil
	}
	free, known, err := j.svc.clientFreeSpace(ctx, j.target, j.rootPaths, j.rootDir)
	if err != nil || !known {
		return space{}, err
	}
	read.free = free
	return read, nil
}

func (s *Service) clientFreeSpace(ctx context.Context, target *models.Instance, d fsops.PathDialect, dir string) (free int64, known bool, err error) {
	free, err = s.deps.Sync.GetFreeSpaceAtPath(ctx, target.ID, dir)
	if !errors.Is(err, qbittorrent.ErrFreeSpaceAtPathUnsupported) {
		if err != nil {
			return 0, false, temporary("qui cannot read the free space on %s: %v", target.Name, err)
		}
		return free, true, nil
	}
	prefs, err := s.deps.Sync.GetAppPreferences(ctx, target.ID)
	if err != nil {
		return 0, false, temporary("qui cannot read the default save path of %s: %v", target.Name, err)
	}
	if d.Clean(prefs.SavePath) != dir {
		return 0, false, nil
	}
	if free, err = s.deps.Sync.GetFreeSpace(ctx, target.ID); err != nil {
		return 0, false, temporary("qui cannot read the free space on %s: %v", target.Name, err)
	}
	return free, true, nil
}

func (j *job) need() int64 {
	var need int64
	for _, f := range j.files {
		size, made := j.made[f]
		switch {
		case f.onTarget || f.linked || f.linksCopy || j.linksCopyLater(f):
		case made:
			need += f.size - size
		default:
			need += f.size
		}
	}
	return need
}

func (j *job) reserve(ctx context.Context, bytes int64) (bool, error) {
	read, err := j.freeSpace(ctx)
	if err != nil || !read.known {
		return read.known, err
	}
	return true, j.claim(bytes, read)
}

func (j *job) claim(bytes int64, read space) error {
	left := read.free - j.reserved
	if left < bytes {
		return permanent("Not enough free space on %s: not enough free space on the target: the Transfer needs %s, and %s is free",
			j.target.Name, formatBytes(bytes), formatBytes(max(left, 0)))
	}
	if others, ok := j.svc.reserve(j.target.ID, bytes, read, j.reserved); !ok {
		return temporary("Not enough free space on %s for now: the Transfer needs %s, and %s is free, but other Transfers to %s take %s of it.",
			j.target.Name, formatBytes(bytes), formatBytes(left), j.target.Name, formatBytes(others))
	}
	j.reserved += bytes
	return nil
}

func (j *job) unclaim(bytes int64) {
	j.svc.unreserve(j.target.ID, bytes)
	j.reserved -= bytes
}
