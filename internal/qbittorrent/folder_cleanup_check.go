// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"io/fs"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

func (s *folderSnapshot) placement(d fsops.PathDialect) (anchor, folder string, ok bool) {
	content := d.Clean(s.ContentPath)
	anchor = d.Clean(s.SavePath)
	if s.DownloadPath != "" {
		download := d.Clean(s.DownloadPath)
		if pathAtOrUnder(d, content, download) && (!pathAtOrUnder(d, content, anchor) || pathUnder(d, download, anchor)) {
			anchor = download
		}
	}
	if !d.IsAbs(anchor) {
		return "", "", false
	}
	switch s.Layout {
	case layoutNoRootFolder:
		return anchor, anchor, content == anchor
	case layoutSingleFile:
		return anchor, d.Dir(content), pathUnder(d, content, anchor)
	case layoutRootFolder:
		return anchor, content, pathUnder(d, content, anchor)
	}
	return "", "", false
}

func contentLeft(ctx context.Context, backend fsops.Backend, item *pendingFolder) bool {
	item.unreadable = nil
	d := backend.Paths()
	anchor, _, ok := item.snap.placement(d)
	if !ok {
		return false
	}
	if item.snap.Layout != layoutNoRootFolder {
		return entryLeft(ctx, backend, d.Clean(item.snap.ContentPath), item)
	}
	for item.gone < len(item.topLevel) {
		if entryLeft(ctx, backend, d.Join(anchor, d.FromSlash(item.topLevel[item.gone])), item) {
			return true
		}
		item.gone++
	}
	return false
}

func entryLeft(ctx context.Context, backend fsops.Backend, p string, item *pendingFolder) bool {
	info, err := backend.Lstat(ctx, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false
	case err != nil:
		item.unreadable = unreadableAt("lstat", p, err)
		return true
	case !info.IsDir:
		return true
	}
	return holdsFiles(ctx, backend, p, item)
}

func holdsFiles(ctx context.Context, backend fsops.Backend, dir string, item *pendingFolder) bool {
	entries, err := backend.ReadDir(ctx, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		item.unreadable = unreadableAt("readdir", dir, err)
		return true
	}
	var subdirs []string
	for _, e := range entries {
		if e.IsDir && !e.IsSymlink {
			subdirs = append(subdirs, backend.Paths().Join(dir, e.Name))
			continue
		}
		junk, err := junkFile(ctx, backend, dir, e)
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			item.unreadable = unreadableAt("lstat", backend.Paths().Join(dir, e.Name), err)
		}
		if !junk {
			return true
		}
	}
	for _, sub := range subdirs {
		if holdsFiles(ctx, backend, sub, item) {
			return true
		}
	}
	return false
}

func junkFile(ctx context.Context, backend fsops.Backend, dir string, e fsops.DirEntry) (bool, error) {
	if e.IsDir || e.IsSymlink || !isJunkName(e.Name) {
		return false, nil
	}
	info, err := backend.Lstat(ctx, backend.Paths().Join(dir, e.Name))
	return err == nil && info.Mode.IsRegular(), err
}

func unreadableAt(op, p string, err error) *fs.PathError {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr
	}
	return &fs.PathError{Op: op, Path: p, Err: err}
}

func isJunkName(name string) bool {
	switch strings.ToLower(name) {
	case "thumbs.db", "desktop.ini", ".directory", ".ds_store":
		return true
	}
	return strings.HasSuffix(name, "~")
}

type liveMark uint8

const (
	liveAncestor liveMark = 1 << iota
	livePath
	liveContent
)

type cleanupScope struct {
	backend  fsops.Backend
	d        fsops.PathDialect
	stops    []string
	keptKeys map[string]struct{}
	ignored  func(string) bool
	live     map[string]liveMark
}

func (c *FolderCleanup) buildScope(ctx context.Context, instanceID int, instance *models.Instance, backend fsops.Backend, ready []*pendingFolder) (*cleanupScope, map[*pendingFolder]struct{}, error) {
	d := backend.Paths()
	ignored, err := c.ignores.IgnoredPathMatcher(ctx, instanceID, d)
	if err != nil {
		return nil, nil, err
	}
	folders, err := c.view.InstanceFolders(ctx, d, instanceID)
	if err != nil {
		return nil, nil, err
	}
	stops := slices.Concat([]string{folders.DefaultSavePath}, folders.CategorySaveFolders, folders.DownloadFolders)
	if folders.DownloadPath != "" {
		stops = append(stops, folders.DownloadPath)
	}
	for base := range strings.SplitSeq(instance.HardlinkBaseDir, ",") {
		if base = d.Clean(strings.TrimSpace(base)); d.IsAbs(base) {
			stops = append(stops, base)
		}
	}
	kept := folders.MonitoredFolders
	torrents, err := c.view.liveTorrents(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}

	scope := &cleanupScope{
		backend:  backend,
		d:        d,
		stops:    stops,
		keptKeys: make(map[string]struct{}, len(stops)+len(kept)),
		ignored:  ignored,
	}
	for _, p := range slices.Concat(stops, kept) {
		scope.keptKeys[fsops.NormalizePath(d, p)] = struct{}{}
	}
	return scope, scope.indexLive(torrents, ready), nil
}

func (s *cleanupScope) indexLive(torrents []*qbt.Torrent, ready []*pendingFolder) map[*pendingFolder]struct{} {
	d := s.d
	byHash := make(map[string][]*pendingFolder, len(ready))
	for _, item := range ready {
		hash := canonicalizeHash(item.snap.Hash)
		byHash[hash] = append(byHash[hash], item)
	}
	waiting := make(map[*pendingFolder]struct{})

	s.live = make(map[string]liveMark, len(torrents))
	shared := make(map[string]struct{})
	markShared := func(p string) {
		if _, done := shared[p]; !done {
			shared[p] = struct{}{}
			s.markLive(p, 0)
		}
	}
	for _, t := range torrents {
		for _, item := range byHash[canonicalizeHash(t.Hash)] {
			if item.waitsFor(d, t) {
				waiting[item] = struct{}{}
			}
		}
		s.markLive(t.ContentPath, liveContent)
		markShared(t.SavePath)
		if t.DownloadPath != "" {
			markShared(t.DownloadPath)
		}
	}
	return waiting
}

func (p *pendingFolder) waitsFor(d fsops.PathDialect, t *qbt.Torrent) bool {
	return !p.snap.Move || fsops.NormalizePath(d, t.ContentPath) == fsops.NormalizePath(d, p.snap.ContentPath)
}

func (s *cleanupScope) markLive(p string, mark liveMark) {
	key := fsops.NormalizePath(s.d, p)
	if !s.d.IsAbs(key) {
		return
	}
	s.live[key] |= liveAncestor | livePath | mark
	for parent := s.d.Dir(key); parent != key; key, parent = parent, s.d.Dir(parent) {
		if s.live[parent]&liveAncestor != 0 {
			return
		}
		s.live[parent] |= liveAncestor
	}
}

func (s *cleanupScope) protected(dir string) bool {
	key := fsops.NormalizePath(s.d, dir)
	if _, kept := s.keptKeys[key]; kept {
		return true
	}
	return s.live[key]&liveAncestor != 0 || s.ignored(dir)
}

func (s *cleanupScope) sealed(dir string) bool {
	key := fsops.NormalizePath(s.d, dir)
	if _, kept := s.keptKeys[key]; kept {
		return true
	}
	return s.live[key]&livePath != 0 || s.ignored(dir)
}

func (s *cleanupScope) insideSealed(anchor, dir string) bool {
	for ; pathUnder(s.d, dir, anchor); dir = s.d.Dir(dir) {
		if s.sealed(dir) {
			return true
		}
	}
	return false
}

func (s *cleanupScope) junkKept(file string) bool {
	key := fsops.NormalizePath(s.d, file)
	for {
		if s.live[key]&liveContent != 0 {
			return true
		}
		parent := s.d.Dir(key)
		if parent == key {
			return false
		}
		key = parent
	}
}

func (s *cleanupScope) stopFor(anchor string) string {
	anchor = s.d.Clean(anchor)
	best := ""
	for _, stop := range s.stops {
		if len(stop) > len(best) && pathAtOrUnder(s.d, anchor, stop) {
			best = stop
		}
	}
	return best
}

func (s *cleanupScope) clean(ctx context.Context, item *pendingFolder) {
	d := s.d
	anchor, folder, ok := item.snap.placement(d)
	if !ok {
		log.Debug().Int("instanceID", item.snap.InstanceID).Str("hash", item.snap.Hash).
			Msg("folder cleanup: save and content paths do not fit together, skipped")
		return
	}
	plain := make(map[string]bool)
	if s.belowLink(ctx, anchor, folder, plain) {
		return
	}

	switch {
	case item.snap.Layout == layoutNoRootFolder:
		for _, rel := range item.relDirs {
			dir := d.Join(anchor, d.FromSlash(rel))
			if !s.protected(dir) && !s.insideSealed(anchor, dir) && !s.belowLink(ctx, anchor, dir, plain) {
				s.remove(ctx, dir)
			}
		}
	case item.snap.Layout == layoutRootFolder && !s.sealed(folder):
		if info, err := s.backend.Lstat(ctx, folder); err == nil && info.IsDir {
			s.walkDown(ctx, folder)
		}
	}

	limit := anchor
	if stop := s.stopFor(anchor); stop != "" {
		limit = stop
	}
	for dir := folder; pathUnder(d, dir, limit); dir = d.Dir(dir) {
		if s.protected(dir) {
			log.Debug().Str("dir", dir).Msg("folder cleanup: kept protected folder")
			return
		}
		if !s.remove(ctx, dir) {
			return
		}
	}
}

func (s *cleanupScope) belowLink(ctx context.Context, anchor, dir string, plain map[string]bool) bool {
	for p := s.d.Dir(dir); pathUnder(s.d, p, anchor); p = s.d.Dir(p) {
		ok, known := plain[p]
		if !known {
			info, err := s.backend.Lstat(ctx, p)
			ok = errors.Is(err, fs.ErrNotExist) || err == nil && info.IsDir
			plain[p] = ok
			if !ok {
				if err != nil {
					s.failed(ctx, err, p)
				} else {
					log.Debug().Str("dir", dir).Str("above", p).Msg("folder cleanup: kept folder below one that is not a plain folder")
				}
			}
		}
		if !ok {
			return true
		}
	}
	return false
}

func (s *cleanupScope) walkDown(ctx context.Context, dir string) {
	entries, err := s.backend.ReadDir(ctx, dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		child := s.d.Join(dir, e.Name)
		if !e.IsDir || e.IsSymlink || s.sealed(child) {
			continue
		}
		s.walkDown(ctx, child)
		if !s.protected(child) {
			s.remove(ctx, child)
		}
	}
}

func (s *cleanupScope) remove(ctx context.Context, dir string) bool {
	info, err := s.backend.Lstat(ctx, dir)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return true
	case err != nil:
		return s.failed(ctx, err, dir)
	case !info.IsDir:
		log.Debug().Str("dir", dir).Msg("folder cleanup: kept, not a plain folder")
		return false
	}

	entries, err := s.backend.ReadDir(ctx, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return s.failed(ctx, err, dir)
	}
	junk := make([]string, 0, len(entries))
	for _, e := range entries {
		file := s.d.Join(dir, e.Name)
		if isJunk, _ := junkFile(ctx, s.backend, dir, e); !isJunk || s.junkKept(file) {
			log.Debug().Str("dir", dir).Msg("folder cleanup: kept folder, not empty")
			return false
		}
		junk = append(junk, file)
	}
	for _, file := range junk {
		if err := s.backend.Remove(ctx, file, fsops.RemoveOptions{}); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return s.failed(ctx, err, file)
		}
	}

	err = s.backend.Remove(ctx, dir, fsops.RemoveOptions{})
	switch {
	case err == nil:
		log.Debug().Str("dir", dir).Msg("folder cleanup: removed folder")
		return true
	case errors.Is(err, fs.ErrNotExist):
		return true
	case fsops.IsDirNotEmpty(err):
		log.Debug().Str("dir", dir).Msg("folder cleanup: kept folder, not empty")
		return false
	}
	return s.failed(ctx, err, dir)
}

func (s *cleanupScope) failed(ctx context.Context, err error, p string) bool {
	if ctx.Err() == nil {
		log.Warn().Err(err).Str("path", p).Msg("folder cleanup: could not remove folder")
	}
	return false
}

func pathUnder(d fsops.PathDialect, child, parent string) bool {
	if len(child) <= len(parent) || !strings.HasPrefix(child, parent) {
		return false
	}
	sep := d.Separator()
	rest := child[len(parent):]
	return strings.HasSuffix(parent, sep) || (strings.HasPrefix(rest, sep) && rest != sep)
}

func pathAtOrUnder(d fsops.PathDialect, child, parent string) bool {
	return child == parent || child == parent+d.Separator() || pathUnder(d, child, parent)
}
