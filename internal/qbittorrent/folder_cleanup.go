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
	folderCleanupTick               = 3 * time.Second
	folderCleanupRemoteTick         = 30 * time.Second
	folderCleanupDeadline           = 2 * time.Minute
	folderCleanupProxyPrepareBudget = 5 * time.Second
	folderCleanupPrepareBudget      = 30 * time.Second
	folderCleanupStopWait           = 5 * time.Second
	folderCleanupCapacity           = 65536
)

type FolderCleanup struct {
	pool    backendRequirer
	view    folderView
	ignores ignoreMatcher

	tick               time.Duration
	remoteTick         time.Duration
	deadline           time.Duration
	stopWait           time.Duration
	capacity           int
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

type folderView interface {
	liveTorrents(ctx context.Context, instanceID int) ([]*qbt.Torrent, error)
	ownRows(ctx context.Context, instanceID int, hashes []string) (map[string]*qbt.Torrent, error)
	InstanceFolders(ctx context.Context, d fsops.PathDialect, instanceID int) (InstanceFolders, error)
	hintRefresh(instanceID int)
	lastSync(ctx context.Context, instanceID int) (time.Time, error)
}

type ignoreMatcher interface {
	IgnoredPathMatcher(ctx context.Context, instanceID int, d fsops.PathDialect) (func(path string) bool, error)
}

type folderLayout uint8

const (
	layoutRootFolder folderLayout = iota
	layoutSingleFile
	layoutNoRootFolder
)

type folderSnapshot struct {
	InstanceID     int
	Hash           string
	SavePath       string
	DownloadPath   string
	ContentPath    string
	Layout         folderLayout
	Files          []string
	ContentExisted bool
	Move           bool
}

type pendingFolder struct {
	snap       folderSnapshot
	relDirs    []string
	topLevel   []string
	gone       int
	judgeErr   error
	unreadable *fs.PathError
	atDeadline bool
	syncAsked  time.Time
	checked    time.Time
	expires    time.Time
}

func (p *pendingFolder) cost() int { return 1 + len(p.relDirs) + len(p.topLevel) }

func (p *pendingFolder) asksAgain(now time.Time, tick time.Duration) bool {
	return !p.syncAsked.IsZero() && now.Sub(p.syncAsked) >= tick && !now.Add(tick).Before(p.expires)
}

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

func (c *FolderCleanup) SetTickForTest(tick time.Duration) { c.tick = tick }

func (c *FolderCleanup) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	go c.run(ctx)
}

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

func (c *FolderCleanup) room() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.stopped {
		return 0
	}
	return c.capacity - c.held
}

func (c *FolderCleanup) enqueue(snaps []folderSnapshot, alreadyDropped int) bool {
	now := time.Now()
	expires := now.Add(c.deadline)
	items := make([]*pendingFolder, 0, len(snaps))
	for _, snap := range snaps {
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

func markUnjudged(instanceID int, err error, items []*pendingFolder) []*pendingFolder {
	log.Debug().Err(err).Int("instanceID", instanceID).Msg("folder cleanup: cannot judge folders yet, will retry")
	for _, item := range items {
		item.judgeErr = err
	}
	return items
}

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
	lead := c.tick
	var waiting []*pendingFolder
	hint, asked := false, false
	if models.FilesystemAccessMode(instance) == models.FilesystemModeRemote {
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
	var synced *time.Time
	var syncErr error
	var unjudged []*pendingFolder
	for _, item := range items {
		item.atDeadline = false
		if !contentLeft(ctx, backend, item) {
			ready = append(ready, item)
			continue
		}
		if item.unreadable != nil {
			retry = append(retry, item)
			continue
		}
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
		retry = append(retry, markUnjudged(instanceID, syncErr, unjudged)...)
	}
	if asked {
		c.view.hintRefresh(instanceID)
	}
	retry = append(retry, waiting...)
	if len(ready) == 0 {
		return retry
	}

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
		if t, ok := rows[canonicalizeHash(item.snap.Hash)]; ok && item.waitsFor(d, t) {
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
	if hint {
		c.view.hintRefresh(instanceID)
	}
	return retry
}
