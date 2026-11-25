// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"iter"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	localReadTTL         = time.Hour
	localReadFailureTTL  = time.Minute
	localReadFreshAdd    = 10 * time.Minute
	localReadFillTimeout = 2 * time.Minute
	localReadWait        = 60 * time.Second
	localReadFilesMaxAge = 24 * time.Hour
	localReadFetchSlots  = 4
)

type localReadMemo struct {
	mu      sync.Mutex
	entries map[localReadKey]localReadEntry
	filling map[localReadKey]chan struct{}
}

type localReadKey struct {
	instanceID int
	hash       string
}

type localReadEntry struct {
	name, category string
	read           namedRelease
	fromFiles      bool
	expires        time.Time
}

type localTorrent struct {
	hash, name, category string
	addedOn              int64
}

func (e localReadEntry) matches(t localTorrent) bool {
	return e.name == t.name && e.category == t.category
}

type pendingLocalReads struct {
	memo       *localReadMemo
	instanceID int
	reads      localReads
	torrents   []localTorrent
	fills      map[chan struct{}]struct{}
}

type localReads map[string]localReadEntry

func (r localReads) read(m matcher, instanceID int, torrent *qbt.Torrent) namedRelease {
	if entry, ok := r.entry(instanceID, torrent); ok {
		return entry.read
	}
	return m.readName(torrent.Name)
}

func (r localReads) entry(instanceID int, torrent *qbt.Torrent) (localReadEntry, bool) {
	if len(r) == 0 {
		return localReadEntry{}, false
	}
	entry, ok := r[cacheKeyForTorrentFiles(instanceID, torrent.Hash)]
	return entry, ok && entry.name == torrent.Name && entry.category == torrent.Category
}

func (s *Service) readLocalTorrents(ctx context.Context, instanceID int, torrents iter.Seq[*qbt.Torrent]) localReads {
	return s.requestLocalReads(ctx, instanceID, torrents).wait(ctx)
}

func (s *Service) requestLocalReads(ctx context.Context, instanceID int, torrents iter.Seq[*qbt.Torrent]) *pendingLocalReads {
	m := s.matcher()
	var needed []localTorrent
	for torrent := range torrents {
		hash := normalizeHash(torrent.Hash)
		if hash == "" || !nameNeedsFiles(m.readName(torrent.Name).release) {
			continue
		}
		needed = append(needed, localTorrent{hash: hash, name: torrent.Name, category: torrent.Category, addedOn: torrent.AddedOn})
	}

	memo := &s.localReadMemo
	pending := &pendingLocalReads{memo: memo, instanceID: instanceID, reads: make(localReads)}
	if len(needed) == 0 || s.syncManager == nil {
		return pending
	}

	now := time.Now()
	var missing, stale []localTorrent
	var fillDone, refreshDone chan struct{}
	memo.mu.Lock()
	if memo.entries == nil {
		memo.entries = make(map[localReadKey]localReadEntry)
		memo.filling = make(map[localReadKey]chan struct{})
	}
	for _, torrent := range needed {
		key := localReadKey{instanceID: instanceID, hash: torrent.hash}
		entry, ok := memo.entries[key]
		ok = ok && entry.matches(torrent)
		filling := memo.filling[key]
		switch {
		case ok && entry.fromFiles:
			pending.reads[cacheKeyForTorrentFiles(instanceID, torrent.hash)] = entry
			pending.torrents = append(pending.torrents, torrent)
			if now.After(entry.expires) && filling == nil {
				if refreshDone == nil {
					refreshDone = make(chan struct{})
				}
				memo.filling[key] = refreshDone
				stale = append(stale, torrent)
			}
		case ok && now.Before(entry.expires):
		case filling != nil:
			pending.addFill(torrent, filling)
		default:
			if fillDone == nil {
				fillDone = make(chan struct{})
			}
			memo.filling[key] = fillDone
			pending.addFill(torrent, fillDone)
			missing = append(missing, torrent)
		}
	}
	memo.mu.Unlock()

	if len(missing) > 0 {
		go s.fillLocalReads(ctx, instanceID, missing, fillDone, true)
	}
	if len(stale) > 0 {
		go s.fillLocalReads(ctx, instanceID, stale, refreshDone, false)
	}
	return pending
}

func (p *pendingLocalReads) addFill(torrent localTorrent, fill chan struct{}) {
	if p.fills == nil {
		p.fills = make(map[chan struct{}]struct{})
	}
	p.fills[fill] = struct{}{}
	p.torrents = append(p.torrents, torrent)
}

func (p *pendingLocalReads) hasFills() bool {
	return len(p.fills) > 0
}

func (p *pendingLocalReads) wait(ctx context.Context) localReads {
	if len(p.torrents) == 0 {
		return p.reads
	}
	timer := time.NewTimer(localReadWait)
	defer timer.Stop()
wait:
	for fill := range p.fills {
		select {
		case <-fill:
		case <-timer.C:
			break wait
		case <-ctx.Done():
			break wait
		}
	}

	p.memo.mu.Lock()
	defer p.memo.mu.Unlock()
	for _, torrent := range p.torrents {
		key := cacheKeyForTorrentFiles(p.instanceID, torrent.hash)
		if entry, ok := p.memo.entries[localReadKey{instanceID: p.instanceID, hash: torrent.hash}]; ok && entry.fromFiles {
			p.reads[key] = entry
		} else {
			delete(p.reads, key)
		}
	}
	return p.reads
}

func (s *Service) fillLocalReads(ctx context.Context, instanceID int, torrents []localTorrent, done chan struct{}, firstFill bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), localReadFillTimeout)
	defer cancel()
	ctx = qbittorrent.WithFileFetchLimit(ctx, localReadFetchSlots)
	if firstFill {
		ctx = qbittorrent.WithFilesCacheMaxAge(ctx, localReadFilesMaxAge)
	} else {
		ctx = qbittorrent.WithFilesUntilChangedOrStale(ctx)
	}

	reads := make(map[string]namedRelease, len(torrents))
	settings, err := s.GetAutomationSettings(ctx)
	var fetchErr error
	if err == nil {
		hashes := make([]string, len(torrents))
		for i, torrent := range torrents {
			hashes[i] = torrent.hash
		}
		var filesByHash map[string]qbt.TorrentFiles
		filesByHash, fetchErr = s.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
		if fetchErr != nil {
			log.Debug().Err(fetchErr).Int("instanceID", instanceID).Int("hashCount", len(hashes)).
				Msg("crossseed: some local torrents keep their name read, because their file lists could not be read")
		}
		for _, torrent := range torrents {
			if files := filesByHash[torrent.hash]; len(files) > 0 {
				reads[torrent.hash] = s.readLocalFiles(settings.CategoryMappingRules, torrent.name, torrent.category, files)
			}
		}
	} else {
		log.Debug().Err(err).Int("instanceID", instanceID).
			Msg("crossseed: local torrents keep their name read, because the settings could not be loaded")
	}

	s.heldTitles.filesReadChanged(instanceID, s.localReadMemo.store(instanceID, torrents, reads, done, err == nil, fetchErr))
}

func (memo *localReadMemo) store(instanceID int, torrents []localTorrent, reads map[string]namedRelease, done chan struct{}, settingsLoaded bool, fetchErr error) []string {
	var stored []string
	now := time.Now()
	memo.mu.Lock()
	defer memo.mu.Unlock()
	defer close(done)
	for _, torrent := range torrents {
		key := localReadKey{instanceID: instanceID, hash: torrent.hash}
		if memo.filling[key] != done {
			continue
		}
		delete(memo.filling, key)
		if !settingsLoaded {
			continue
		}
		if read, ok := reads[torrent.hash]; ok {
			memo.entries[key] = localReadEntry{name: torrent.name, category: torrent.category, read: read, fromFiles: true, expires: now.Add(localReadTTL)}
			stored = append(stored, torrent.hash)
			continue
		}
		if fetchErr != nil || now.Sub(time.Unix(torrent.addedOn, 0)) < localReadFreshAdd {
			continue
		}
		entry := memo.entries[key]
		if !entry.matches(torrent) {
			entry = localReadEntry{name: torrent.name, category: torrent.category}
		}
		entry.expires = now.Add(localReadFailureTTL)
		memo.entries[key] = entry
	}
	return stored
}

func (memo *localReadMemo) filesRead(instanceID int, torrent *qbt.Torrent) (namedRelease, bool) {
	memo.mu.Lock()
	defer memo.mu.Unlock()
	entry, ok := memo.entries[localReadKey{instanceID: instanceID, hash: normalizeHash(torrent.Hash)}]
	if !ok || !entry.fromFiles || entry.name != torrent.Name || entry.category != torrent.Category {
		return namedRelease{}, false
	}
	return entry.read, true
}

func (s *Service) readLocalFiles(rules []models.CategoryMappingRule, name, category string, files qbt.TorrentFiles) namedRelease {
	read, _ := s.readRelease(name, files, readPolicy{
		mapContent: func(contentInfo ContentTypeInfo) ContentTypeInfo {
			if ruleInfo, ok := categoryRuleContentType(rules, category); ok {
				return ruleInfo
			}
			return contentInfo
		},
	})
	return read
}

func (s *Service) forgetLocalRead(instanceID int, hash string) {
	s.copyGroups.forget(instanceID, hash)
	memo := &s.localReadMemo
	key := localReadKey{instanceID: instanceID, hash: normalizeHash(hash)}
	memo.mu.Lock()
	delete(memo.entries, key)
	delete(memo.filling, key)
	memo.mu.Unlock()
	s.heldTitles.filesReadChanged(instanceID, []string{hash})
}

func (s *Service) forgetLocalReads() {
	s.copyGroups.forgetAll()
	memo := &s.localReadMemo
	memo.mu.Lock()
	clear(memo.entries)
	clear(memo.filling)
	memo.mu.Unlock()
	s.heldTitles.rebuildAll()
}
