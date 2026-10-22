// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

// placement returns the folder the content sits in, the save path or the
// download path, whichever is deeper and holds the content, and the folder the
// content occupied. ok is false when the paths do not fit together. Paths are
// compared exactly, so a case variant never fits.
func (s *folderSnapshot) placement(d fsops.PathDialect) (anchor, folder string, ok bool) {
	content := d.Clean(s.ContentPath)
	anchor = d.Clean(s.SavePath)
	if s.DownloadPath != "" {
		// A download path above the save path must not lift the climb past it.
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

// contentLeft reports whether qBittorrent has not finished deleting or moving
// the torrent's content out yet. A retry while the content is there costs one
// Lstat, plus one ReadDir when the entry is a folder. A path it cannot read
// counts as content left and is recorded on item.
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

// entryLeft treats a folder that holds only folders and junk as gone, since
// that is what qBittorrent before 5.2.2 leaves behind.
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

// junkFile reports whether e is a regular file with a junk name. A junction
// (which Go on Windows reports as neither a folder nor a symlink), a FIFO or a
// socket can carry such a name too, and counts as a real file. DirEntry does
// not tell them apart, so only a junk name costs an Lstat, whose error is
// returned.
func junkFile(ctx context.Context, backend fsops.Backend, dir string, e fsops.DirEntry) (bool, error) {
	if e.IsDir || e.IsSymlink || !isJunkName(e.Name) {
		return false, nil
	}
	info, err := backend.Lstat(ctx, backend.Paths().Join(dir, e.Name))
	return err == nil && info.Mode.IsRegular(), err
}

// unreadableAt is why p could not be read, with the path once even when err
// already names it.
func unreadableAt(op, p string, err error) *fs.PathError {
	if pathErr, ok := errors.AsType[*fs.PathError](err); ok {
		return pathErr
	}
	return &fs.PathError{Op: op, Path: p, Err: err}
}

// isJunkName is qBittorrent's list from Utils::Fs::smartRemoveEmptyFolderTree,
// matched without regard to case as qBittorrent does.
func isJunkName(name string) bool {
	switch strings.ToLower(name) {
	case "thumbs.db", "desktop.ini", ".directory", ".ds_store":
		return true
	}
	return strings.HasSuffix(name, "~")
}

type liveMark uint8

const (
	// liveAncestor: at or above a live torrent's save, content or download path.
	liveAncestor liveMark = 1 << iota
	// livePath: a live torrent's save, content or download path.
	livePath
	// liveContent: a live torrent's content path.
	liveContent
)

// cleanupScope is what one check of one instance judges folders against. It
// costs a full read of the cached torrents and a preferences request.
type cleanupScope struct {
	backend fsops.Backend
	d       fsops.PathDialect
	// stops set the climb limit; keptKeys folds them and the kept folders.
	stops    []string
	keptKeys map[string]struct{}
	ignored  func(string) bool
	live     map[string]liveMark
}

// buildScope also returns the items whose torrent the cache still shows where
// the snapshot left it.
func (c *FolderCleanup) buildScope(ctx context.Context, instanceID int, instance *models.Instance, backend fsops.Backend, ready []*pendingFolder) (*cleanupScope, map[*pendingFolder]struct{}, error) {
	d := backend.Paths()
	// Ahead of the requests below, so an ignore path that cannot be read does
	// not send them on every tick until the deadline.
	ignored, err := c.ignores.IgnoredPathMatcher(ctx, instanceID, d)
	if err != nil {
		return nil, nil, err
	}
	in, err := c.view.stopFolderInputs(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	nesting, err := c.cachedNesting(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	// SyncManager.CategoryPathRule's rule. The preference comes from this
	// check's read, so a toggle is seen on the next check.
	rule := CategoryPathRule{
		Nest:     nesting.Nests && (nesting.Always || in.UseSubcategories),
		FileName: nesting.FileName,
		Platform: nesting.Platform,
	}
	stops, kept, err := stopFolders(d, in, rule, instance.HardlinkBaseDir)
	if err != nil {
		return nil, nil, err
	}
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

// cachedNesting reuses an instance's version half of the nesting state for
// folderCleanupVersionTTL. An error is not kept, so the next tick asks again.
func (c *FolderCleanup) cachedNesting(ctx context.Context, instanceID int) (nestingSupport, error) {
	expired := func(_ int, m nestingMemo) bool { return time.Since(m.at) >= folderCleanupVersionTTL }
	if m, ok := c.nesting[instanceID]; ok && !expired(instanceID, m) {
		return m.nesting, nil
	}
	// An expired entry is never served, so drop them all, which also drops
	// the entries of instances the worker no longer checks.
	maps.DeleteFunc(c.nesting, expired)
	nesting, err := c.view.categoryNesting(ctx, instanceID)
	if err != nil {
		return nestingSupport{}, err
	}
	if c.nesting == nil {
		c.nesting = make(map[int]nestingMemo)
	}
	c.nesting[instanceID] = nestingMemo{nesting: nesting, at: time.Now()}
	return nesting, nil
}

// stopFolders returns the stop folders, which set the climb limit, and kept
// folders, which are never removed but set no limit. It fails rather than
// return a short list: without the default save path no inheriting category
// resolves, and a missing stop folder can be removed.
func stopFolders(d fsops.PathDialect, in stopInputs, rule CategoryPathRule, hardlinkBaseDirs string) (stops, kept []string, err error) {
	defaultSavePath := d.Clean(in.DefaultSavePath)
	if !d.IsAbs(defaultSavePath) {
		return nil, nil, fmt.Errorf("default save path %q is not absolute", in.DefaultSavePath)
	}
	stops = []string{defaultSavePath}
	tempPath := ""
	if in.TempPathEnabled {
		tempPath = d.Clean(in.TempPath)
		if !d.IsAbs(tempPath) {
			return nil, nil, fmt.Errorf("download path %q is not absolute", in.TempPath)
		}
		stops = append(stops, tempPath)
	}
	for name := range in.Categories {
		stops = append(stops, CategorySavePath(d, name, in.Categories, defaultSavePath, rule))
		if tempPath != "" {
			// qBittorrent's categoryDownloadPath: <download path>/<category>. A
			// category's own download path is not in go-qbittorrent's Category.
			stops = append(stops, CategorySavePath(d, name, nil, tempPath, rule))
		}
	}
	// qBittorrent stops watching a removed monitored folder until it restarts.
	// Neither it nor its custom save path sets the climb limit: a recursive
	// watch saves into <save path>/<sub>. qBittorrent refuses a relative
	// monitored folder and resolves a relative custom path under the default.
	for dir, target := range in.ScanDirs {
		if dir = d.Clean(dir); d.IsAbs(dir) {
			kept = append(kept, dir)
		}
		if target.Mode() == qbt.MonitoredFolderModeCustomPath {
			p := d.Clean(target.CustomPath())
			if !d.IsAbs(p) {
				p = d.Join(defaultSavePath, p)
			}
			kept = append(kept, p)
		}
	}
	for base := range strings.SplitSeq(hardlinkBaseDirs, ",") {
		if base = d.Clean(strings.TrimSpace(base)); d.IsAbs(base) {
			stops = append(stops, base)
		}
	}
	return stops, kept, nil
}

// indexLive marks every live torrent's paths and their ancestors, and returns
// the items whose own row still holds them (waitsFor).
func (s *cleanupScope) indexLive(torrents []qbt.Torrent, ready []*pendingFolder) map[*pendingFolder]struct{} {
	d := s.d
	byHash := make(map[string][]*pendingFolder, len(ready))
	for _, item := range ready {
		hash := canonicalizeHash(item.snap.Hash)
		byHash[hash] = append(byHash[hash], item)
	}
	waiting := make(map[*pendingFolder]struct{})

	s.live = make(map[string]liveMark, len(torrents))
	// Thousands of torrents share a save path; fold each one once.
	shared := make(map[string]struct{})
	markShared := func(p string) {
		if _, done := shared[p]; !done {
			shared[p] = struct{}{}
			s.markLive(p, 0)
		}
	}
	for i := range torrents {
		t := &torrents[i]
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

// waitsFor reports whether t, the item's own row, still shows the torrent
// where the snapshot left it. A move compares content paths, because
// qBittorrent changes an ATM torrent's save path before the files move.
func (p *pendingFolder) waitsFor(d fsops.PathDialect, t *qbt.Torrent) bool {
	return !p.snap.Move || fsops.NormalizePath(d, t.ContentPath) == fsops.NormalizePath(d, p.snap.ContentPath)
}

func (s *cleanupScope) markLive(p string, mark liveMark) {
	key := fsops.NormalizePath(s.d, p)
	if !s.d.IsAbs(key) {
		return
	}
	s.live[key] |= liveAncestor | livePath | mark
	// Once an ancestor is marked, all of its own ancestors already are.
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

// sealed reports whether the walk down must not enter dir: what lies below a
// stop or kept folder, an ignore path or a live torrent's path is not the
// torrent's.
func (s *cleanupScope) sealed(dir string) bool {
	key := fsops.NormalizePath(s.d, dir)
	if _, kept := s.keptKeys[key]; kept {
		return true
	}
	return s.live[key]&livePath != 0 || s.ignored(dir)
}

// insideSealed reports whether dir or a folder between it and anchor is
// sealed, so that the walk down would never have reached dir.
func (s *cleanupScope) insideSealed(anchor, dir string) bool {
	for ; pathUnder(s.d, dir, anchor); dir = s.d.Dir(dir) {
		if s.sealed(dir) {
			return true
		}
	}
	return false
}

// junkKept keeps a junk file at or under a live torrent's content path. That
// is wider than "a file the torrent lists", which would need every live file
// list on every check, and it never deletes more.
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

// stopFor returns the deepest stop folder at or above anchor, or "". It
// compares exactly: on a case-sensitive filesystem a stop folder that matches
// only once case is folded is another folder, and would lift the climb limit.
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

// clean walks down the folders the torrent's files sat in, then climbs from
// its content folder until a protected or non-empty folder. Outside every stop
// folder it never removes the anchor from placement or anything above it.
func (s *cleanupScope) clean(ctx context.Context, item *pendingFolder) {
	d := s.d
	anchor, folder, ok := item.snap.placement(d)
	if !ok {
		log.Debug().Int("instanceID", item.snap.InstanceID).Str("hash", item.snap.Hash).
			Msg("folder cleanup: save and content paths do not fit together, skipped")
		return
	}
	// Below a symlink, folder and everything under it lie in the folder the
	// symlink points to. anchor and the path above it are the user's setting,
	// and remove() never follows a symlink at dir itself.
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
		// A root folder that is a symlink reads as content left until the
		// deadline, and the folder it points to is not the torrent's.
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

// belowLink reports whether a folder between anchor and dir is not a plain
// folder, such as a symlink that replaced one of the torrent's folders. An
// Lstat of dir follows it, and dir is then in the folder the symlink points
// to. A folder whose Lstat fails counts the same. anchor itself may be a
// symlink, since it is the save or download path the user set. A folder that
// is gone counts as plain, so the clean goes on past it.
//
// plain keeps each folder's answer for the rest of the clean, so the check
// reads a folder once however many of the torrent's folders sit below it.
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

// walkDown removes the empty folders below a torrent's root folder, deepest
// first. It never enters a sealed folder, nor a symlink, which keeps its
// folder as a file would.
func (s *cleanupScope) walkDown(ctx context.Context, dir string) {
	entries, err := s.backend.ReadDir(ctx, dir)
	if err != nil {
		// The remove that follows reads dir again and warns.
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

// remove deletes dir when it holds nothing but junk files, and reports whether
// dir is gone. Removes are non-recursive, so a folder that gained a file since
// the read fails safely. A symlink is never removed, since os.Remove would
// delete the user's link: Lstat reports it as not a folder.
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

// pathUnder reports whether child is strictly below parent; both are cleaned
// paths or both keys from fsops.NormalizePath. A root parent already ends in the separator.
// A share root is the exception on Windows: Clean leaves `\\nas\share` bare,
// and Dir of a path below it returns `\\nas\share\`, the same folder.
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
