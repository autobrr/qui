// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"io/fs"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// findHardlinks marks each file of the group that is, on the source, a
// hardlink of a file before it. Only qui's own disk tells, and a file whose
// identity qui cannot read counts as a file of its own.
func (j *job) findHardlinks(ctx context.Context) {
	if !models.FilesystemCapabilitiesOf(j.source).TrustedIdentity {
		return
	}
	d := j.srcFiles.Paths()
	first := map[fsops.FileKey]*groupFile{}
	for _, f := range j.files {
		if f.size == 0 {
			continue
		}
		info, err := j.srcFiles.Lstat(ctx, d.Join(f.owner.SourceSavePath, d.FromSlash(f.name)))
		if err != nil || info.FileIDErr != nil {
			continue
		}
		key, ok := fsops.FileKeyOf(info.FileID, j.source)
		if !ok {
			continue
		}
		if orig, ok := first[key]; ok {
			f.hardlinkOf = orig
		} else {
			first[key] = f
		}
	}
}

// linksCopyLater reports whether qui means to link the file on the target
// to the copy of the file that it is a hardlink of, and has not made its
// temporary file yet.
func (j *job) linksCopyLater(f *groupFile) bool {
	_, made := j.made[f]
	return !made && !f.onTarget && f.hardlinkOf != nil && !f.hardlinkOf.onTarget
}

// linkCopies makes the temporary file of each hardlink of the group a
// hardlink to the temporary file of the file that it links to, so that qui
// copies the bytes once. A file that the target cannot link gets a copy of
// its own, and qui reserves the free space for it.
func (j *job) linkCopies(ctx context.Context) error {
	for _, f := range j.files {
		if !j.linksCopyLater(f) {
			continue
		}
		orig := f.hardlinkOf
		if _, made := j.made[orig]; !made {
			if err := j.createTemp(ctx, orig); err != nil {
				return err
			}
		}
		err := j.root.Link(ctx, j.tempOf(ctx, orig), j.tempName(f.rel))
		if err == nil {
			f.linksCopy = true
			j.made[f] = f.size
			continue
		}
		j.logf("qui cannot link %s on %s, so it copies the file: %v", f.rel, j.target.Name, err)
		if err := j.reserve(ctx, f.size); err != nil {
			return err
		}
		if err := j.createTemp(ctx, f); err != nil {
			return err
		}
	}
	return nil
}

// relink keeps the temporary file of each hardlink that an earlier run
// linked to the temporary file of the file that it links to. It removes the
// other temporary files of hardlinks, so that linkCopies links them again.
func (j *job) relink(ctx context.Context) {
	for _, f := range j.files {
		if _, made := j.made[f]; !made || f.hardlinkOf == nil {
			continue
		}
		if j.sameFile(ctx, j.tempName(f.rel), j.tempName(f.hardlinkOf.rel)) {
			f.linksCopy = true
			j.made[f] = f.size
			continue
		}
		j.remove(ctx, j.tempName(f.rel))
		delete(j.made, f)
	}
}

// tempOf is the name of the temporary file of f, also when a check gave it
// the .!qB name.
func (j *job) tempOf(ctx context.Context, f *groupFile) string {
	name := j.tempName(f.rel)
	if _, err := j.root.Lstat(ctx, name); errors.Is(err, fs.ErrNotExist) {
		return name + incompleteExt
	}
	return name
}

// hardlinkBytes returns the count and the size of the files that qui links
// to the copy of the file that they are a hardlink of.
func (j *job) hardlinkBytes() (int, int64) {
	var n int
	var size int64
	for _, f := range j.files {
		if j.linksCopyLater(f) {
			n++
			size += f.size
		}
	}
	return n, size
}
