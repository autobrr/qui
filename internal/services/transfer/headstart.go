// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
)

func (j *job) linkName(name string) string {
	return fmt.Sprintf("%s.qui-link-%d", name, j.rec.ID)
}

func (j *job) checkName(f file) string {
	if f.onTarget {
		return j.linkName(f.name)
	}
	return j.tempName(f.name)
}

func (j *job) inTheWay(rel string) error {
	return permanent("Conflict: a file is in the way on %s at %s.", j.target.Name, j.targetPath(rel))
}

func cannotLook(instance string, err error) error {
	return temporary("qui cannot look for a file in the way on %s: %w", instance, err)
}

func (j *job) findOnTarget(ctx context.Context) error {
	for _, f := range j.files {
		for _, name := range []string{j.tempName(f.rel), j.tempName(f.rel) + incompleteExt, j.linkName(f.rel), j.linkName(f.rel) + incompleteExt} {
			_, err := j.root.Lstat(ctx, name)
			switch {
			case errors.Is(err, fs.ErrNotExist):
			case err != nil:
				return cannotLook(j.target.Name, err)
			default:
				return j.inTheWay(name)
			}
		}
		info, err := j.root.Lstat(ctx, f.rel)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			continue
		case err != nil:
			return cannotLook(j.target.Name, err)
		case !info.Mode.IsRegular() || info.Size != f.size:
			return j.inTheWay(f.rel)
		}
		f.onTarget = true
	}
	j.markOnTarget()
	return nil
}

func (j *job) markOnTarget() {
	for _, p := range j.parts {
		for i := range p.plan {
			if f := &p.plan[i]; !f.pad {
				f.onTarget = j.byRel[p.rel(*f)].onTarget
			}
		}
	}
}

func (p *part) hasOnTarget() bool {
	return slices.ContainsFunc(p.plan, func(f file) bool { return f.onTarget })
}

func (j *job) headStart(ctx context.Context) error {
	if !slices.ContainsFunc(j.files, func(f *groupFile) bool { return f.onTarget }) {
		return nil
	}
	if err := j.step(ctx, StateChecking, "check the files on the target"); err != nil {
		return err
	}
	for _, p := range j.parts {
		if !p.hasOnTarget() {
			continue
		}
		states, err := j.checkOnTarget(ctx, p)
		if err != nil {
			return err
		}
		info := p.src.info
		for _, f := range p.plan {
			if !f.onTarget {
				continue
			}
			first, end := (f.offset+info.PieceLength-1)/info.PieceLength, (f.offset+f.size)/info.PieceLength
			if f.offset+f.size == info.TotalLength() {
				end = int64(len(states))
			}
			for piece := first; piece < end; piece++ {
				if states[piece] != qbt.PieceStateAlreadyDownloaded {
					return j.differs(p, f)
				}
			}
		}
		if j.rec.Way == WayBitTorrent {
			if err := j.sameEdges(ctx, p, states); err != nil {
				return err
			}
		}
		p.headStartProven = !slices.ContainsFunc(states, func(s qbt.PieceState) bool { return s != qbt.PieceStateAlreadyDownloaded })
	}
	var files int
	var size int64
	for _, f := range j.files {
		if f.onTarget {
			files++
			size += f.size
		}
	}
	j.logf("Head start: %d files (%s) were on %s before the Transfer and passed the check.", files, formatBytes(size), j.target.Name)
	return j.step(ctx, StateCopying, "copy the missing files")
}

func (j *job) differs(p *part, f file) error {
	return permanent("Conflict: the file at %s on %s differs from the torrent.", j.targetPath(p.rel(f)), j.target.Name)
}

func (j *job) sameEdges(ctx context.Context, p *part, states []qbt.PieceState) error {
	length := p.src.info.PieceLength
	for _, f := range p.plan {
		if !f.onTarget {
			continue
		}
		end := f.offset + f.size
		for from := f.offset; from < end; from = (from/length + 1) * length {
			if states[from/length] == qbt.PieceStateAlreadyDownloaded {
				continue
			}
			if j.srcFiles == nil {
				return permanent("Conflict: qui cannot check a part of the file at %s on %s, because it cannot read the files on %s.",
					j.targetPath(p.rel(f)), j.target.Name, j.source.Name)
			}
			to := min((from/length+1)*length, end)
			same, err := j.sameBytes(ctx, p, f, from-f.offset, to-from)
			if err != nil {
				return err
			}
			if !same {
				return j.differs(p, f)
			}
		}
	}
	return nil
}

func (j *job) sameBytes(ctx context.Context, p *part, f file, offset, n int64) (bool, error) {
	paths := j.srcFiles.Paths()
	theirs, ours := make([]byte, n), make([]byte, n)
	if err := readAt(ctx, j.srcFiles, paths.Join(p.src.torrent.SavePath, paths.FromSlash(f.name)), offset, ours); err != nil {
		return false, temporary("qui cannot read %s on %s: %v", f.name, j.source.Name, err)
	}
	if err := readAt(ctx, j.dstFiles, j.targetPath(p.rel(f)), offset, theirs); err != nil {
		return false, temporary("qui cannot read %s on %s: %v", j.targetPath(p.rel(f)), j.target.Name, err)
	}
	return bytes.Equal(theirs, ours), nil
}

func readAt(ctx context.Context, files fsops.Backend, name string, offset int64, into []byte) error {
	r, err := files.OpenRead(ctx, name, offset)
	if err != nil {
		return err
	}
	defer r.Close()
	_, err = io.ReadFull(r, into)
	return err
}

func (j *job) link(ctx context.Context, p *part) error {
	for _, f := range p.plan {
		if !f.onTarget {
			continue
		}
		err := j.root.Link(ctx, p.rel(f), j.linkName(p.rel(f)))
		if errors.Is(err, fsops.ErrConnectionLost) {
			return temporary("qui cannot link %s on %s: %w", p.rel(f), j.target.Name, targetError{err})
		}
		if err != nil {
			return permanent("Conflict: qui cannot check the file at %s on %s, because it cannot make a hardlink there: %v",
				j.targetPath(p.rel(f)), j.target.Name, err)
		}
	}
	return nil
}

func (j *job) unlink(ctx context.Context, p *part) {
	for _, f := range p.plan {
		if f.onTarget {
			j.remove(ctx, j.linkName(p.rel(f)), j.linkName(p.rel(f))+incompleteExt)
		}
	}
}
