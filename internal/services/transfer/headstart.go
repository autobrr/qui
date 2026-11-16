// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

// linkName is the hardlink through which a check-only helper checks a file
// that was on the target before the Transfer. qBittorrent's setting "Append
// .!qB extension to incomplete files" can rename only the link, and never
// the file of the user. qui never writes through a link.
func (j *job) linkName(name string) string {
	return fmt.Sprintf("%s.qui-link-%d", name, j.rec.ID)
}

// checkName is the name under which a check-only helper finds a file.
func (j *job) checkName(f file) string {
	if f.onTarget {
		return j.linkName(f.name)
	}
	return j.tempName(f.name)
}

func (j *job) inTheWay(name string) error {
	return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, path.Join(j.savePath(), name))
}

// findOnTarget marks each file that the target already has. A regular file of
// the right size is a head start, and anything else is a conflict, as is any
// file at a name that qui writes.
func (j *job) findOnTarget(ctx context.Context) error {
	for i := range j.plan {
		f := &j.plan[i]
		if f.pad {
			continue
		}
		for _, name := range []string{j.tempName(f.name), j.tempName(f.name) + incompleteExt, j.linkName(f.name), j.linkName(f.name) + incompleteExt} {
			if _, err := j.root.Lstat(ctx, name); !errors.Is(err, fs.ErrNotExist) {
				return j.inTheWay(name)
			}
		}
		info, err := j.root.Lstat(ctx, f.name)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil || !info.Mode.IsRegular() || info.Size != f.size {
			return j.inTheWay(f.name)
		}
		f.onTarget = true
	}
	return nil
}

// copiedBytes is the size of the files that qui copies.
func copiedBytes(plan []file) int64 {
	var total int64
	for _, f := range plan {
		if !f.pad && !f.onTarget {
			total += f.size
		}
	}
	return total
}

// headStart checks the files that were on the target before the copy. A
// file passes when every piece that lies fully inside it passes, so a piece
// that it shares with a missing file does not count against it. A file that
// fails is a conflict, and qui never writes into it or over it.
func (j *job) headStart(ctx context.Context) error {
	if !slices.ContainsFunc(j.plan, func(f file) bool { return f.onTarget }) {
		return nil
	}
	if err := j.step(ctx, StateChecking, "check the files on the target"); err != nil {
		return err
	}
	states, err := j.checkOnTarget(ctx)
	if err != nil {
		return err
	}
	pieceLength := j.src.info.PieceLength
	var files int
	var size int64
	for _, f := range j.plan {
		if !f.onTarget {
			continue
		}
		first, end := (f.offset+pieceLength-1)/pieceLength, (f.offset+f.size)/pieceLength
		if f.offset+f.size == j.src.info.TotalLength() {
			end = int64(len(states))
		}
		for piece := first; piece < end; piece++ {
			if states[piece] != qbt.PieceStateAlreadyDownloaded {
				return permanent("Conflict: the file at %s on %s differs from the torrent.", path.Join(j.savePath(), f.name), j.target.Name)
			}
		}
		files++
		size += f.size
	}
	j.headStartProven = !slices.ContainsFunc(states, func(s qbt.PieceState) bool { return s != qbt.PieceStateAlreadyDownloaded })
	j.logf("Head start: %d files (%s) were on %s before the Transfer and passed the check.", files, formatBytes(size), j.target.Name)
	return j.step(ctx, StateCopying, "copy the missing files")
}

// link makes the hardlink of each file that was on the target. A file that
// qui cannot link is a conflict, whatever the error: it differs between
// filesystems and SFTP servers.
func (j *job) link(ctx context.Context) error {
	for _, f := range j.plan {
		if !f.onTarget {
			continue
		}
		if err := j.root.Link(ctx, f.name, j.linkName(f.name)); err != nil {
			return permanent("Conflict: qui cannot check the file at %s on %s, because it cannot make a hardlink there: %v",
				path.Join(j.savePath(), f.name), j.target.Name, err)
		}
	}
	return nil
}

// unlink removes the hardlinks of the check, also under their .!qB names.
func (j *job) unlink(ctx context.Context) {
	for _, f := range j.plan {
		if f.onTarget {
			j.remove(ctx, j.linkName(f.name), j.linkName(f.name)+incompleteExt)
		}
	}
}

// skips reports whether qui leaves a piece out of the copy, because all its
// bytes are in files that the target already has.
func (j *job) skips(piece int) bool {
	info := j.src.info
	start := int64(piece) * info.PieceLength
	segments := j.segments(start, min(info.PieceLength, info.TotalLength()-start))
	return slices.ContainsFunc(segments, func(s segment) bool { return j.plan[s.file].onTarget }) &&
		!slices.ContainsFunc(segments, func(s segment) bool { return j.writes(s.file) })
}

// writes reports whether qui writes the file on the target.
func (j *job) writes(i int) bool {
	return !j.plan[i].pad && !j.plan[i].onTarget
}
