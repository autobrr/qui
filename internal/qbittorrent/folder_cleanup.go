// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
)

const (
	folderCleanupTick     = 3 * time.Second
	folderCleanupDeadline = 2 * time.Minute
	// folderCleanupVersionTTL is how long the worker reuses what an
	// instance's qBittorrent version says about category nesting. Reading it
	// refreshes qBittorrent's capabilities with a request, which would
	// otherwise go out on every tick that has work, and a later refresh still
	// catches an in-place upgrade. The use_subcategories preference is not
	// reused: it is read on every check.
	folderCleanupVersionTTL = time.Minute
	// folderCleanupCapacity bounds the folders and top-level entries held across
	// the queue. A batch past it is left to orphan scan.
	folderCleanupCapacity = 65536
)

// FolderCleanup removes the leftover folders a torrent leaves after qui deletes
// or moves it. It decides on its own which folders are stop folders, which are
// protected, and when the content is gone; callers only hand it snapshots.
type FolderCleanup struct {
	pool    backendRequirer
	view    folderView
	ignores ignoreMatcher

	// Tick is how often the worker checks its queue, and Deadline how long an
	// item waits for its torrent to leave. They default to the production
	// values. Tests may shorten them before Start; they must not change after.
	Tick     time.Duration
	Deadline time.Duration
	capacity int

	// nesting holds what each instance's qBittorrent version says about
	// category nesting, read and written by the worker alone.
	nesting map[int]nestingMemo

	mu      sync.Mutex
	pending []*pendingFolder
	held    int
	stopped bool

	cancel context.CancelFunc
	done   chan struct{}
}

type backendRequirer interface {
	Require(ctx context.Context, instanceID int, capability models.FilesystemCapability) (fsops.Backend, *models.Instance, error)
}

// folderView is the sync manager's cached view of an instance. No method may
// send a request per torrent: the worker calls them on every check.
type folderView interface {
	liveTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	stopFolderInputs(ctx context.Context, instanceID int) (stopInputs, error)
	// categoryNesting refreshes qBittorrent's capabilities with a request,
	// so the worker keeps its answer for folderCleanupVersionTTL.
	categoryNesting(ctx context.Context, instanceID int) (nestingSupport, error)
	// hintRefresh asks for one sync, for a torrent the cache still shows
	// where the snapshot left it.
	hintRefresh(instanceID int)
}

type ignoreMatcher interface {
	IgnoredPathMatcher(ctx context.Context, instanceID int, d fsops.PathDialect) (func(path string) bool, error)
}

// stopInputs is what qBittorrent reports for the stop folders it owns.
type stopInputs struct {
	DefaultSavePath string
	TempPath        string
	TempPathEnabled bool
	Categories      map[string]qbt.Category
	// UseSubcategories is the use_subcategories preference as read.
	// buildScope replaces it with whether category save paths nest.
	UseSubcategories bool
	// ScanDirs are qBittorrent's monitored folders, each with where it saves
	// the torrents it adds.
	ScanDirs qbt.MonitoredFolders
}

// folderLayout says how a torrent's content sits under its save path.
type folderLayout uint8

const (
	// layoutRootFolder: the content path is the torrent's own folder.
	layoutRootFolder folderLayout = iota
	// layoutSingleFile: the content path is the torrent's only file.
	layoutSingleFile
	// layoutNoRootFolder: the files sit directly in the save path, which is
	// also the content path.
	layoutNoRootFolder
)

// folderSnapshot is one torrent as it was before qui asked qBittorrent to
// delete or move it. Paths are in the backend's dialect, as qBittorrent
// reports them.
type folderSnapshot struct {
	InstanceID   int
	Hash         string
	SavePath     string
	DownloadPath string
	ContentPath  string
	Layout       folderLayout
	// Files holds the slash names qBittorrent lists, read only for
	// layoutNoRootFolder.
	Files          []string
	ContentExisted bool
	// Move is set for a move. The item then waits until the torrent's row
	// shows a new content path, rather than until the row is gone, since the
	// move may not happen at all.
	Move bool
}

// nestingSupport is what an instance's qBittorrent version says about
// category save paths, the half of CategorySavePathsNest that costs a request.
type nestingSupport struct {
	// Nests: an empty category save path can resolve under the parent
	// category's save path (qBittorrent 5.0+).
	Nests bool
	// Always: qBittorrent 5.2+ dropped use_subcategories and always nests.
	Always bool
}

type nestingMemo struct {
	nesting nestingSupport
	at      time.Time
}

// pendingFolder is a snapshot reduced to the folders it can touch, so the queue
// holds folders rather than a large torrent's file names.
type pendingFolder struct {
	snap folderSnapshot
	// relDirs are the slash folders the torrent's files sat in, deepest first.
	relDirs []string
	// topLevel are the slash names directly in the save path, in file order.
	topLevel []string
	// gone counts the leading topLevel entries already seen gone, so a retry
	// stats only what was still there.
	gone    int
	expires time.Time
}

func (p *pendingFolder) cost() int { return 1 + len(p.relDirs) + len(p.topLevel) }

// NewFolderCleanup returns a stopped cleanup; Start runs its worker.
func NewFolderCleanup(pool backendRequirer, view folderView, ignores ignoreMatcher) *FolderCleanup {
	return &FolderCleanup{
		pool:     pool,
		view:     view,
		ignores:  ignores,
		Tick:     folderCleanupTick,
		Deadline: folderCleanupDeadline,
		capacity: folderCleanupCapacity,
	}
}

// Start runs the one worker until ctx ends or Stop is called.
func (c *FolderCleanup) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	go c.run(ctx)
}

// Stop drops pending work and waits for the worker to exit. Snapshots handed in
// afterwards are ignored.
func (c *FolderCleanup) Stop() {
	c.drop()
	if c.cancel != nil {
		c.cancel()
		<-c.done
	}
}

func (c *FolderCleanup) drop() {
	c.mu.Lock()
	c.stopped = true
	c.pending = nil
	c.held = 0
	c.mu.Unlock()
}

// room is how many more torrents the queue could take, at one folder each.
func (c *FolderCleanup) room() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return 0
	}
	return c.capacity - c.held
}

// enqueue hands over torrents that are about to leave their folders. It never
// blocks on the filesystem. alreadyDropped counts torrents the caller left out
// for lack of room, so the one warning carries the full count.
func (c *FolderCleanup) enqueue(snaps []folderSnapshot, alreadyDropped int) {
	expires := time.Now().Add(c.Deadline)
	items := make([]*pendingFolder, 0, len(snaps))
	for _, snap := range snaps {
		// Content that was missing before the operation may be an unmounted
		// disk; its empty mount point must stay.
		if !snap.ContentExisted {
			continue
		}
		item, err := newPendingFolder(snap, expires)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", snap.InstanceID).Str("hash", snap.Hash).
				Msg("folder cleanup: skipped torrent")
			continue
		}
		items = append(items, item)
	}

	dropped := alreadyDropped
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return
	}
	for _, item := range items {
		if c.held+item.cost() > c.capacity {
			dropped++
			continue
		}
		c.held += item.cost()
		c.pending = append(c.pending, item)
	}
	c.mu.Unlock()

	if dropped > 0 {
		log.Warn().Int("torrents", dropped).
			Msg("folder cleanup: queue full, leaving these torrents' folders to orphan scan")
	}
}

func newPendingFolder(snap folderSnapshot, expires time.Time) (*pendingFolder, error) {
	item := &pendingFolder{snap: snap, expires: expires}
	if snap.Layout == layoutNoRootFolder {
		dirs := make(map[string]struct{})
		top := make(map[string]struct{})
		for _, name := range snap.Files {
			clean, ok := cleanTorrentFileName(name)
			if !ok {
				return nil, fmt.Errorf("unsafe file name %q", name)
			}
			first, _, _ := strings.Cut(clean, "/")
			if _, seen := top[first]; !seen {
				top[first] = struct{}{}
				item.topLevel = append(item.topLevel, first)
			}
			for dir := path.Dir(clean); dir != "."; dir = path.Dir(dir) {
				dirs[dir] = struct{}{}
			}
		}
		if len(item.topLevel) == 0 {
			return nil, errors.New("no file list")
		}
		for dir := range dirs {
			item.relDirs = append(item.relDirs, dir)
		}
		slices.SortFunc(item.relDirs, func(a, b string) int {
			if n := strings.Count(b, "/") - strings.Count(a, "/"); n != 0 {
				return n
			}
			return strings.Compare(a, b)
		})
	}
	item.snap.Files = nil
	return item, nil
}

// cleanTorrentFileName validates a torrent-internal slash name. qBittorrent
// reports these from torrent metadata, so a name that is absolute, drive
// qualified, UNC, carries a backslash or climbs with ".." is refused on every
// OS rather than joined to a save path.
func cleanTorrentFileName(name string) (string, bool) {
	if name == "" || strings.ContainsRune(name, '\\') || strings.HasPrefix(name, "/") || hasDriveLetter(name) {
		return "", false
	}
	clean := path.Clean(name)
	if clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return "", false
	}
	return clean, true
}

func hasDriveLetter(name string) bool {
	return len(name) >= 2 && name[1] == ':' && ('a' <= name[0]|0x20 && name[0]|0x20 <= 'z')
}

func (c *FolderCleanup) run(ctx context.Context) {
	defer close(c.done)
	ticker := time.NewTicker(c.Tick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			c.drop()
			return
		case <-ticker.C:
			c.runTick(ctx)
		}
	}
}

func (c *FolderCleanup) runTick(ctx context.Context) {
	c.mu.Lock()
	batch := c.pending
	c.pending = nil
	c.held = 0
	c.mu.Unlock()
	if len(batch) == 0 {
		return
	}

	byInstance := make(map[int][]*pendingFolder)
	var order []int
	for _, item := range batch {
		id := item.snap.InstanceID
		if _, ok := byInstance[id]; !ok {
			order = append(order, id)
		}
		byInstance[id] = append(byInstance[id], item)
	}

	var retry []*pendingFolder
	for _, id := range order {
		if ctx.Err() != nil {
			return
		}
		retry = append(retry, c.checkInstance(ctx, id, byInstance[id])...)
	}

	now := time.Now()
	retry = slices.DeleteFunc(retry, func(item *pendingFolder) bool {
		if now.Before(item.expires) {
			return false
		}
		log.Debug().Int("instanceID", item.snap.InstanceID).Str("hash", item.snap.Hash).Str("contentPath", item.snap.ContentPath).
			Msg("folder cleanup: torrent still in its folder at the deadline, dropped")
		return true
	})

	c.mu.Lock()
	if !c.stopped {
		for _, item := range retry {
			c.held += item.cost()
		}
		c.pending = append(retry, c.pending...)
	}
	c.mu.Unlock()
}

// checkInstance cleans the items whose content is gone and returns the ones to
// check again.
func (c *FolderCleanup) checkInstance(ctx context.Context, instanceID int, items []*pendingFolder) []*pendingFolder {
	backend, instance, err := c.pool.Require(ctx, instanceID, models.CapabilityWrite)
	if errors.Is(err, fsops.ErrNotCapable) {
		return nil
	}
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: backend unavailable, will retry")
		return items
	}
	// Remote instances wait for their own ticket, even once they can write.
	if models.FilesystemAccessMode(instance) == models.FilesystemModeRemote {
		return nil
	}

	var ready, retry []*pendingFolder
	for _, item := range items {
		if contentLeft(ctx, backend, item) {
			retry = append(retry, item)
		} else {
			ready = append(ready, item)
		}
	}
	if len(ready) == 0 {
		return retry
	}

	scope, waiting, err := c.buildScope(ctx, instanceID, instance, backend, ready)
	if err != nil {
		log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: cannot judge folders yet, will retry")
		return append(retry, ready...)
	}
	if len(waiting) > 0 {
		c.view.hintRefresh(instanceID)
	}
	for _, item := range ready {
		if _, wait := waiting[item]; wait {
			retry = append(retry, item)
			continue
		}
		scope.clean(ctx, item)
	}
	return retry
}
