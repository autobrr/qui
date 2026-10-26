// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
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
	folderCleanupTick = 3 * time.Second
	// folderCleanupRemoteTick is how often a remote instance's items are
	// checked, since each check costs round trips per torrent over SSH.
	folderCleanupRemoteTick = 30 * time.Second
	folderCleanupDeadline   = 2 * time.Minute
	// folderCleanupProxyPrepareBudget bounds the snapshot a proxied request
	// waits on before it is sent, well inside the timeouts clients use.
	folderCleanupProxyPrepareBudget = 5 * time.Second
	// folderCleanupPrepareBudget bounds it for qui's own operations, well
	// inside the 60 s automations allow for applying their actions.
	folderCleanupPrepareBudget = 30 * time.Second
	// folderCleanupStopWait bounds how long Stop waits for a worker stuck in
	// a filesystem call that ignores its context, so shutdown goes on.
	folderCleanupStopWait = 5 * time.Second
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

	// tick is how often the worker checks its queue, remoteTick how often it
	// checks a remote instance's items, and deadline how long an item waits
	// for its torrent to leave. Tests shorten them before Start.
	tick       time.Duration
	remoteTick time.Duration
	deadline   time.Duration
	stopWait   time.Duration
	capacity   int
	// prepareBudget and proxyPrepareBudget bound how long qui's own
	// operations and proxied requests wait on their snapshots.
	prepareBudget      time.Duration
	proxyPrepareBudget time.Duration

	mu      sync.Mutex
	pending []*pendingFolder
	held    int
	stopped bool

	cancel context.CancelFunc
	done   chan struct{}
}

type backendRequirer interface {
	Require(ctx context.Context, instanceID int, capability models.FilesystemCapability, more ...models.FilesystemCapability) (fsops.Backend, *models.Instance, error)
}

// folderView is the sync manager's cached view of an instance, read on every
// check. Its requests do not grow with the torrent count.
type folderView interface {
	liveTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	// ownRows is the cached rows of hashes alone, keyed by canonicalizeHash.
	ownRows(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.Torrent, error)
	// InstanceFolders reads the preferences and the categories with
	// requests.
	InstanceFolders(ctx context.Context, d fsops.PathDialect, instanceID int) (InstanceFolders, error)
	// hintRefresh asks for one sync, for a torrent the cache still shows
	// where the snapshot left it, or for an item about to reach its
	// deadline.
	hintRefresh(instanceID int)
	// lastSync is when the cache last took a successful sync.
	lastSync(ctx context.Context, instanceID int) (time.Time, error)
}

type ignoreMatcher interface {
	IgnoredPathMatcher(ctx context.Context, instanceID int, d fsops.PathDialect) (func(path string) bool, error)
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
	gone int
	// judgeErr is why the last check could not judge the item's folders, so
	// a drop at the deadline can say cleanup did not run.
	judgeErr error
	// unreadable is the path the last content check could not read, which
	// keeps the item waiting.
	unreadable *fs.PathError
	// atDeadline marks an item judged at the deadline with content still
	// in place.
	atDeadline bool
	// syncAsked is when the item, with content still in place, last asked
	// for a sync ahead of its deadline.
	syncAsked time.Time
	// checked is when the item was queued or last checked on a remote
	// instance, which waits remoteTick between checks.
	checked time.Time
	expires time.Time
}

func (p *pendingFolder) cost() int { return 1 + len(p.relDirs) + len(p.topLevel) }

// asksAgain reports whether p, which asked for a sync on an earlier check,
// asks again on the last tick before its deadline. On a remote instance that
// earlier ask came a remote tick ahead, and a torrent added at these folders
// since would be missing from the scope. It needs no read of the backend.
func (p *pendingFolder) asksAgain(now time.Time, tick time.Duration) bool {
	return !p.syncAsked.IsZero() && now.Sub(p.syncAsked) >= tick && !now.Add(tick).Before(p.expires)
}

// NewFolderCleanup returns a stopped cleanup; Start runs its worker.
func NewFolderCleanup(pool backendRequirer, view folderView, ignores ignoreMatcher) *FolderCleanup {
	return &FolderCleanup{
		pool:               pool,
		view:               view,
		ignores:            ignores,
		tick:               folderCleanupTick,
		remoteTick:         folderCleanupRemoteTick,
		deadline:           folderCleanupDeadline,
		stopWait:           folderCleanupStopWait,
		capacity:           folderCleanupCapacity,
		prepareBudget:      folderCleanupPrepareBudget,
		proxyPrepareBudget: folderCleanupProxyPrepareBudget,
	}
}

// SetTickForTest sets how often the worker checks its queue. Call it before
// Start. Product code does not call it.
func (c *FolderCleanup) SetTickForTest(tick time.Duration) { c.tick = tick }

// Start runs the one worker until ctx ends or Stop is called.
func (c *FolderCleanup) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	go c.run(ctx)
}

// Stop drops pending work and waits up to stopWait for the worker to exit.
// Snapshots handed in afterwards are ignored. A worker that has not exited by
// then, such as one inside a hung filesystem call, is left to finish its tick,
// which takes no new work.
func (c *FolderCleanup) Stop() {
	c.drop()
	if c.cancel == nil {
		return
	}
	c.cancel()
	timer := time.NewTimer(c.stopWait)
	defer timer.Stop()
	select {
	case <-c.done:
	case <-timer.C:
		log.Warn().Dur("wait", c.stopWait).Msg("folder cleanup: worker did not stop in time, leaving it to finish its tick")
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

// enqueue hands over torrents that are about to leave their folders, and
// reports whether it queued any. It never blocks on the filesystem.
// alreadyDropped counts torrents the caller left out for lack of room, so the
// one warning carries the full count.
func (c *FolderCleanup) enqueue(snaps []folderSnapshot, alreadyDropped int) bool {
	now := time.Now()
	expires := now.Add(c.deadline)
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
		item.checked = now
		items = append(items, item)
	}

	dropped := alreadyDropped
	queued := false
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		return false
	}
	for _, item := range items {
		if c.held+item.cost() > c.capacity {
			dropped++
			continue
		}
		c.held += item.cost()
		c.pending = append(c.pending, item)
		queued = true
	}
	c.mu.Unlock()

	if dropped > 0 {
		log.Warn().Int("torrents", dropped).
			Msg("folder cleanup: queue full, leaving these torrents' folders to orphan scan")
	}
	return queued
}

func newPendingFolder(snap folderSnapshot, expires time.Time) (*pendingFolder, error) {
	item := &pendingFolder{snap: snap, expires: expires}
	if snap.Layout == layoutNoRootFolder {
		top, err := topLevelNames(snap.Files)
		if err != nil {
			return nil, err
		}
		if len(top) == 0 {
			return nil, errors.New("no file list")
		}
		item.topLevel = top
		dirs := make(map[string]struct{})
		for _, name := range snap.Files {
			clean, _ := cleanTorrentFileName(name)
			for dir := path.Dir(clean); dir != "."; dir = path.Dir(dir) {
				dirs[dir] = struct{}{}
			}
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

// topLevelNames returns the distinct slash names directly in the save path, in
// file order. It fails on a name cleanTorrentFileName refuses.
func topLevelNames(files []string) ([]string, error) {
	var top []string
	seen := make(map[string]struct{})
	for _, name := range files {
		clean, ok := cleanTorrentFileName(name)
		if !ok {
			return nil, fmt.Errorf("unsafe file name %q", name)
		}
		first, _, _ := strings.Cut(clean, "/")
		if _, dup := seen[first]; !dup {
			seen[first] = struct{}{}
			top = append(top, first)
		}
	}
	return top, nil
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
	ticker := time.NewTicker(c.tick)
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

// runTick keeps the batch counted in held while it works unlocked, so an
// enqueue meanwhile sees the room the batch may take back, and releases the
// cost of each item that does not come back for a retry.
func (c *FolderCleanup) runTick(ctx context.Context) {
	c.mu.Lock()
	batch := c.pending
	c.pending = nil
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

	// One instant decides both whether an item cleans at the deadline and
	// whether it is dropped, so no item passes the deadline between the two.
	now := time.Now()
	var retry []*pendingFolder
	for _, id := range order {
		if ctx.Err() != nil {
			return
		}
		retry = append(retry, c.checkInstance(ctx, id, byInstance[id], now)...)
	}

	unjudged := make(map[int]int)
	judgeErr := make(map[int]error)
	unreadable := make(map[int][]*fs.PathError)
	retry = slices.DeleteFunc(retry, func(item *pendingFolder) bool {
		if now.Before(item.expires) {
			return false
		}
		id := item.snap.InstanceID
		if item.judgeErr != nil {
			unjudged[id]++
			judgeErr[id] = item.judgeErr
			return true
		}
		if item.unreadable != nil {
			unreadable[id] = append(unreadable[id], item.unreadable)
			return true
		}
		log.Debug().Int("instanceID", item.snap.InstanceID).Str("hash", item.snap.Hash).Str("contentPath", item.snap.ContentPath).
			Msg("folder cleanup: torrent still in its folder at the deadline, dropped")
		return true
	})
	// Each failure was logged at Debug as it may pass. One that lasted to the
	// deadline, such as an ignore path the host cannot read, turns cleanup off.
	for _, id := range order {
		if n := unjudged[id]; n > 0 {
			log.Warn().Err(judgeErr[id]).Int("instanceID", id).Int("torrents", n).
				Msg("folder cleanup: could not judge folders before the deadline, leaving these torrents' folders to orphan scan")
		}
		if u := unreadable[id]; len(u) > 0 {
			examples := make([]string, 0, min(len(u), 3))
			for _, e := range u[:min(len(u), 3)] {
				examples = append(examples, e.Path)
			}
			log.Warn().Err(u[0].Err).Int("instanceID", id).Int("torrents", len(u)).Strs("paths", examples).
				Msg("folder cleanup: could not read the content before the deadline, leaving these torrents' folders in place")
		}
	}

	released := 0
	for _, item := range batch {
		released += item.cost()
	}
	for _, item := range retry {
		released -= item.cost()
	}
	c.mu.Lock()
	if !c.stopped {
		c.held -= released
		c.pending = append(retry, c.pending...)
	}
	c.mu.Unlock()
}

// markUnjudged records err on items whose folders could not be judged, to retry.
func markUnjudged(instanceID int, err error, items []*pendingFolder) []*pendingFolder {
	log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: cannot judge folders yet, will retry")
	for _, item := range items {
		item.judgeErr = err
	}
	return items
}

// checkInstance cleans the items whose content is gone, and at the deadline
// those whose content could be read once a fresh sync has landed, and returns
// the ones to check again.
func (c *FolderCleanup) checkInstance(ctx context.Context, instanceID int, items []*pendingFolder, now time.Time) []*pendingFolder {
	for _, item := range items {
		item.judgeErr = nil
	}
	backend, instance, err := c.pool.Require(ctx, instanceID, models.CapabilityWrite)
	if errors.Is(err, fsops.ErrNotCapable) {
		return nil
	}
	if err != nil {
		return markUnjudged(instanceID, err, items)
	}
	// lead is the longest time to the item's next check.
	lead := c.tick
	// waiting are the items this check leaves for a later one.
	var waiting []*pendingFolder
	// hint asks for a sync at the end of the check, and asked records an
	// item's request for one ahead of its deadline, which goes out before
	// the check removes anything on this instance, so the sync lands
	// before the next tick.
	hint, asked := false, false
	if models.FilesystemAccessMode(instance) == models.FilesystemModeRemote {
		// The instance is checked once one of its items is due, and then
		// all of them are, so it costs at most one check per remoteTick.
		// That check runs on the first tick after it is due. Between two
		// checks only the items at their deadline are checked, so batches
		// queued a few seconds apart do not each check the rest again.
		lead = c.remoteTick + c.tick
		if slices.ContainsFunc(items, func(item *pendingFolder) bool { return !now.Before(item.checked.Add(c.remoteTick)) }) {
			for _, item := range items {
				item.checked = now
			}
		} else {
			var expired []*pendingFolder
			for _, item := range items {
				if now.Before(item.expires) {
					if item.asksAgain(now, c.tick) {
						item.syncAsked = now
						asked = true
					}
					waiting = append(waiting, item)
				} else {
					expired = append(expired, item)
				}
			}
			if len(expired) == 0 {
				if asked {
					c.view.hintRefresh(instanceID)
				}
				return items
			}
			items = expired
		}
	}

	var ready, retry []*pendingFolder
	// synced is read once, and only for an item at its deadline.
	var synced *time.Time
	var syncErr error
	var unjudged []*pendingFolder
	for _, item := range items {
		item.atDeadline = false
		if !contentLeft(ctx, backend, item) {
			ready = append(ready, item)
			continue
		}
		// Content can stay for good, such as a file the user added in one
		// of the torrent's folders, which holdsFiles finds at any depth. At
		// the deadline the item is judged anyway, so its row and the scope
		// still protect, and the clean removes only the folders that are
		// empty. A path the check could not read keeps the item waiting, and
		// the deadline warns about it.
		if item.unreadable != nil {
			retry = append(retry, item)
			continue
		}
		// While content is there nothing reads the item's row, so nothing
		// asks for a sync, and the cache may predate a torrent added at
		// these folders since. One check ahead of the deadline the item asks
		// for one, again on the last tick if that check came earlier, and the
		// deadline judges only against a sync that landed after the last ask.
		// Without one the item is dropped as before, which is also what
		// happens when an overrunning tick makes the ticker skip that last
		// tick before the deadline.
		if now.Before(item.expires) {
			if item.syncAsked.IsZero() && !now.Add(lead).Before(item.expires) || item.asksAgain(now, c.tick) {
				item.syncAsked = now
				asked = true
			}
			retry = append(retry, item)
			continue
		}
		if synced == nil {
			var t time.Time
			t, syncErr = c.view.lastSync(ctx, instanceID)
			synced = &t
		}
		if syncErr != nil {
			unjudged = append(unjudged, item)
			continue
		}
		if item.syncAsked.IsZero() || !synced.After(item.syncAsked) {
			log.Debug().Int("instanceID", instanceID).Str("hash", item.snap.Hash).
				Msg("folder cleanup: no sync since the deadline approached, not cleaning")
			retry = append(retry, item)
			continue
		}
		item.atDeadline = true
		ready = append(ready, item)
	}
	if len(unjudged) > 0 {
		// The deadline drop then warns that the folders went unjudged.
		retry = append(retry, markUnjudged(instanceID, syncErr, unjudged)...)
	}
	if asked {
		c.view.hintRefresh(instanceID)
	}
	retry = append(retry, waiting...)
	if len(ready) == 0 {
		return retry
	}

	// The usual first check after a delete finds the torrent's own row still
	// cached. That alone decides it waits, so the scope, a full read of the
	// cached torrents and two requests, is built only for the rest.
	hashes := make([]string, len(ready))
	for i, item := range ready {
		hashes[i] = item.snap.Hash
	}
	rows, err := c.view.ownRows(ctx, instanceID, hashes)
	if err != nil {
		return append(retry, markUnjudged(instanceID, err, ready)...)
	}
	d := backend.Paths()
	judge := make([]*pendingFolder, 0, len(ready))
	for _, item := range ready {
		if t, ok := rows[canonicalizeHash(item.snap.Hash)]; ok && item.waitsFor(d, &t) {
			hint = true
			retry = append(retry, item)
			continue
		}
		judge = append(judge, item)
	}
	if len(judge) > 0 {
		scope, waiting, err := c.buildScope(ctx, instanceID, instance, backend, judge)
		if err != nil {
			retry = append(retry, markUnjudged(instanceID, err, judge)...)
		} else {
			// The full read can be newer than the row read above, so it decides.
			for _, item := range judge {
				if _, wait := waiting[item]; wait {
					hint = true
					retry = append(retry, item)
					continue
				}
				if item.atDeadline {
					log.Debug().Int("instanceID", instanceID).Str("hash", item.snap.Hash).Str("contentPath", item.snap.ContentPath).
						Msg("folder cleanup: content still there at the deadline, removing only empty folders")
				}
				scope.clean(ctx, item)
			}
		}
	}
	// A row still in place asks for a sync even after the request above,
	// which may have landed before the reads that found the row. A second
	// hint only restarts the debounce if the first sync has not started.
	if hint {
		c.view.hintRefresh(instanceID)
	}
	return retry
}
