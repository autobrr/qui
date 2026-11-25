// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"path"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
)

func (s *Service) versionsOf(ctx context.Context, inst *models.Instance, first *source, stay []string) ([]string, error) {
	if len(stay) == 0 {
		return nil, nil
	}
	backend, err := s.deps.Files.GetBackend(ctx, inst.ID)
	if err != nil {
		return nil, fmt.Errorf("read the files of %s: %w", inst.Name, err)
	}
	want := contentKeys(ctx, backend, inst, first.torrent.SavePath, first.files)
	if len(want) == 0 {
		return nil, nil
	}
	var versions []string
	for _, hash := range stay {
		torrent, found, err := s.deps.Sync.HasTorrentByAnyHash(ctx, inst.ID, []string{hash})
		if err != nil {
			return nil, fmt.Errorf("read the torrents of %s: %w", inst.Name, err)
		}
		if !found {
			continue
		}
		files, err := s.deps.Sync.GetTorrentFiles(qbittorrent.WithFilesUntilChanged(ctx), inst.ID, hash)
		if err != nil {
			return nil, fmt.Errorf("read the files of %s: %w", torrent.Name, err)
		}
		if files != nil && maps.Equal(want, contentKeys(ctx, backend, inst, torrent.SavePath, *files)) {
			versions = append(versions, hash)
		}
	}
	return versions, nil
}

func contentKeys(ctx context.Context, backend fsops.Backend, inst *models.Instance, savePath string, files qbt.TorrentFiles) map[fsops.FileKey]struct{} {
	d := backend.Paths()
	keys := make(map[fsops.FileKey]struct{}, len(files))
	for _, f := range files {
		if f.Size == 0 || slices.Contains(crossseed.DefaultIgnoredExtensions, strings.ToLower(path.Ext(f.Name))) {
			continue
		}
		info, err := backend.Lstat(ctx, d.Join(savePath, d.FromSlash(f.Name)))
		if err != nil || info.FileIDErr != nil {
			return nil
		}
		key, ok := fsops.FileKeyOf(info.FileID, inst)
		if !ok {
			return nil
		}
		keys[key] = struct{}{}
	}
	return keys
}

func (j *job) findHardlinks(ctx context.Context) {
	if j.rec.Way == WayBitTorrent || !models.FilesystemCapabilitiesOf(j.source).TrustedIdentity {
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

func (j *job) linksCopyLater(f *groupFile) bool {
	_, made := j.made[f]
	return !made && !f.onTarget && f.hardlinkOf != nil && !f.hardlinkOf.onTarget
}

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
		name := j.tempName(f.rel)
		if err := j.retake(ctx, name); err != nil {
			return err
		}
		err := j.root.Link(ctx, j.tempOf(ctx, orig), name)
		if err == nil {
			f.linksCopy = true
			j.made[f] = f.size
			continue
		}
		if errors.Is(err, fsops.ErrConnectionLost) {
			j.unanswered[name] = true
			return temporary("qui cannot link %s on %s: %w", f.rel, j.target.Name, targetError{err})
		}
		j.logf("qui cannot link %s on %s, so it copies the file: %v", f.rel, j.target.Name, err)
		if _, err := j.reserve(ctx, f.size); err != nil {
			return err
		}
		if err := j.createTemp(ctx, f); err != nil {
			return err
		}
		if err := j.stepWith(ctx, StateCopying, stepOwnCopy, f.rel); err != nil {
			return err
		}
	}
	return nil
}

func (j *job) relink(ctx context.Context) {
	for _, f := range j.files {
		if _, made := j.made[f]; !made || f.hardlinkOf == nil {
			continue
		}
		same, err := j.sameFile(ctx, j.tempName(f.rel), j.tempName(f.hardlinkOf.rel))
		switch {
		case err == nil && same:
			f.linksCopy = true
			j.made[f] = f.size
		case !j.reachedFor(stepOwnCopy, f.rel):
			j.remove(ctx, j.tempName(f.rel))
			delete(j.made, f)
		}
	}
}

func (j *job) tempOf(ctx context.Context, f *groupFile) string {
	name := j.tempName(f.rel)
	if _, err := j.root.Lstat(ctx, name); errors.Is(err, fs.ErrNotExist) {
		return name + incompleteExt
	}
	return name
}

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
