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
	"syscall"
	"time"
	"unicode/utf8"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/text/unicode/norm"

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
// Lstat, plus one ReadDir when the entry is a folder.
func contentLeft(ctx context.Context, backend fsops.Backend, item *pendingFolder) bool {
	d := backend.Paths()
	anchor, _, ok := item.snap.placement(d)
	if !ok {
		return false
	}
	if item.snap.Layout != layoutNoRootFolder {
		return entryLeft(ctx, backend, d.Clean(item.snap.ContentPath))
	}
	for item.gone < len(item.topLevel) {
		if entryLeft(ctx, backend, d.Join(anchor, d.FromSlash(item.topLevel[item.gone]))) {
			return true
		}
		item.gone++
	}
	return false
}

// entryLeft treats a folder that holds only folders and junk as gone, since
// that is what qBittorrent before 5.2.2 leaves behind.
func entryLeft(ctx context.Context, backend fsops.Backend, p string) bool {
	info, err := backend.Lstat(ctx, p)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return false
	case err != nil, !info.IsDir:
		return true
	}
	return holdsFiles(ctx, backend, p)
}

func holdsFiles(ctx context.Context, backend fsops.Backend, dir string) bool {
	entries, err := backend.ReadDir(ctx, dir)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	var subdirs []string
	for _, e := range entries {
		switch {
		case e.IsDir && !e.IsSymlink:
			subdirs = append(subdirs, backend.Paths().Join(dir, e.Name))
		case !junkFile(ctx, backend, dir, e):
			return true
		}
	}
	for _, sub := range subdirs {
		if holdsFiles(ctx, backend, sub) {
			return true
		}
	}
	return false
}

// junkFile reports whether e is a regular file with a junk name. A junction
// (which Go on Windows reports as neither a folder nor a symlink), a FIFO or a
// socket can carry such a name too, and counts as a real file. DirEntry does
// not tell them apart, so only a junk name costs an Lstat.
func junkFile(ctx context.Context, backend fsops.Backend, dir string, e fsops.DirEntry) bool {
	if e.IsDir || e.IsSymlink || !isJunkName(e.Name) {
		return false
	}
	info, err := backend.Lstat(ctx, backend.Paths().Join(dir, e.Name))
	return err == nil && info.Mode.IsRegular()
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

// cleanupScope is what one check of one instance judges folders against. It is
// built only when some item's content is gone, because it costs a full read of
// the cached torrents and, at most once a minute per instance while the
// request succeeds, a WebAPI version request.
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
	in, err := c.view.stopFolderInputs(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	nesting, err := c.cachedNesting(ctx, instanceID)
	if err != nil {
		return nil, nil, err
	}
	// CategorySavePathsNest's rule. The preference comes from this check's
	// read, so a toggle made in qui's UI, which drops the cached preferences,
	// is seen on the next check, and any other toggle once that cache expires.
	in.UseSubcategories = nesting.Nests && (nesting.Always || in.UseSubcategories)
	stops, kept, err := stopFolders(d, in, instance.HardlinkBaseDir)
	if err != nil {
		return nil, nil, err
	}
	ignored, err := c.ignores.IgnoredPathMatcher(ctx, instanceID, d)
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
		scope.keptKeys[foldKey(d, p)] = struct{}{}
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
func stopFolders(d fsops.PathDialect, in stopInputs, hardlinkBaseDirs string) (stops, kept []string, err error) {
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
		stops = append(stops, CategorySavePath(d, name, in.Categories, defaultSavePath, in.UseSubcategories))
		if tempPath != "" {
			// qBittorrent's categoryDownloadPath: <download path>/<category>. A
			// category's own download path is not in go-qbittorrent's Category.
			stops = append(stops, CategorySavePath(d, name, nil, tempPath, in.UseSubcategories))
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
// the items that must wait for their torrent's row. A torrent's own row counts
// like any other: the cache cannot tell a delete it has not caught up with
// from one qBittorrent ignored. A deleted torrent waits until its row is gone.
// A moved torrent waits while its row shows the old content path, since
// qBittorrent changes an ATM torrent's save path at once but its content path
// only once the files have moved.
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
			if !item.snap.Move || foldKey(d, t.ContentPath) == foldKey(d, item.snap.ContentPath) {
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

func (s *cleanupScope) markLive(p string, mark liveMark) {
	key := foldKey(s.d, p)
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
	key := foldKey(s.d, dir)
	if _, kept := s.keptKeys[key]; kept {
		return true
	}
	return s.live[key]&liveAncestor != 0 || s.ignored(dir)
}

// sealed reports whether the walk down must not enter dir: what lies below a
// stop or kept folder, an ignore path or a live torrent's path is not the
// torrent's.
func (s *cleanupScope) sealed(dir string) bool {
	key := foldKey(s.d, dir)
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
	key := foldKey(s.d, file)
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

	switch {
	case item.snap.Layout == layoutNoRootFolder:
		for _, rel := range item.relDirs {
			if dir := d.Join(anchor, d.FromSlash(rel)); !s.protected(dir) && !s.insideSealed(anchor, dir) {
				s.remove(ctx, dir)
			}
		}
	case item.snap.Layout == layoutRootFolder && !s.sealed(folder):
		s.walkDown(ctx, folder)
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

// walkDown removes the empty folders below a torrent's root folder, deepest
// first. It never enters a sealed folder, nor a symlink, which keeps its
// folder as a file would.
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
		if !junkFile(ctx, s.backend, dir, e) || s.junkKept(file) {
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
	case isDirNotEmpty(err):
		log.Debug().Str("dir", dir).Msg("folder cleanup: kept folder, not empty")
		return false
	}
	return s.failed(ctx, err, dir)
}

func isDirNotEmpty(err error) bool {
	return errors.Is(err, syscall.ENOTEMPTY) || strings.Contains(strings.ToLower(err.Error()), "not empty")
}

func (s *cleanupScope) failed(ctx context.Context, err error, p string) bool {
	if ctx.Err() == nil {
		log.Warn().Err(err).Str("path", p).Msg("folder cleanup: could not remove folder")
	}
	return false
}

// foldKey is orphan scan's normalizePath: a macOS or Windows volume
// bind-mounted into Linux is case-insensitive. It is for matching protected
// folders and junk only, where a fold keeps more; the climb compares exactly.
func foldKey(d fsops.PathDialect, p string) string {
	p = d.Clean(p)
	if isASCII(p) {
		// Already NFC, and lowering cannot unclean it.
		return strings.ToLower(p)
	}
	return nfcClean(d, strings.ToLower(nfcClean(d, p)))
}

func isASCII(s string) bool {
	for i := range len(s) {
		if s[i] >= utf8.RuneSelf {
			return false
		}
	}
	return true
}

func nfcClean(d fsops.PathDialect, p string) string {
	p = d.Clean(p)
	if utf8.ValidString(p) && !norm.NFC.IsNormalString(p) {
		p = norm.NFC.String(p)
	}
	return p
}

// pathUnder reports whether child is strictly below parent; both are cleaned
// paths or both keys from foldKey. A root parent already ends in the separator.
func pathUnder(d fsops.PathDialect, child, parent string) bool {
	if len(child) <= len(parent) || !strings.HasPrefix(child, parent) {
		return false
	}
	sep := d.Separator()
	return strings.HasSuffix(parent, sep) || strings.HasPrefix(child[len(parent):], sep)
}

func pathAtOrUnder(d fsops.PathDialect, child, parent string) bool {
	return child == parent || pathUnder(d, child, parent)
}
