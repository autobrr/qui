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

	"github.com/autobrr/qui/internal/qbittorrent"
)

const (
	// localReadTTL is how long a remembered read is used before a background
	// refresh reads the files again. Callers keep the old read meanwhile.
	localReadTTL = time.Hour
	// localReadFailureTTL is how long a failed file fetch is remembered.
	localReadFailureTTL = time.Minute
	// localReadFreshAdd is the age under which a torrent is read on every call:
	// qBittorrent can return no files for a few seconds after an add.
	localReadFreshAdd = 10 * time.Minute
	// localReadFillTimeout bounds a fill, which outlives the caller that started it.
	localReadFillTimeout = 2 * time.Minute
	// localReadWait is how long a caller waits for a fill before it answers
	// with name reads. It stays under the 120 s that autobrr waits for a check.
	localReadWait = 60 * time.Second
	// localReadFilesMaxAge is the age of the SQLite file lists that a fill
	// accepts for a torrent without a remembered read: file names change only on
	// a rename, and a rename through qui deletes the row.
	localReadFilesMaxAge = 24 * time.Hour
	// localReadFetchSlots is how many of the file-fetch slots of an instance a
	// fill holds, so cross-seed adds still get one.
	localReadFetchSlots = 4
)

// localReadMemo remembers the Read releases that local torrents give from
// their files (nameNeedsFiles). Every Match path that reads local torrents
// reads through it, so a webhook check reads no file list once the reads are
// filled. The zero value is ready to use.
//
// ponytail: an entry of a torrent deleted outside qui stays until a settings
// save or a restart; prune entries that no caller asked for in a day if the
// memo grows.
type localReadMemo struct {
	mu      sync.Mutex
	entries map[localReadKey]localReadEntry
	// filling holds the done channel of the fill that reads a torrent now. A
	// fill stores a read only while it still holds the torrent here: a settings
	// save or a rename through qui takes it away.
	filling map[localReadKey]chan struct{}
}

type localReadKey struct {
	instanceID int
	hash       string
}

type localReadEntry struct {
	// name and category are part of the key: a change misses the entry.
	name, category string
	read           namedRelease
	// fromFiles is false for a remembered failure, which has no read.
	fromFiles bool
	expires   time.Time
}

// localTorrent is what a fill needs of a torrent. It holds no pointer into the
// torrent list of the caller, so that list can be freed while the fill runs.
type localTorrent struct {
	hash, name, category string
	addedOn              int64
}

func (e localReadEntry) matches(t localTorrent) bool {
	return e.name == t.name && e.category == t.category
}

// pendingLocalReads is a request for the reads of one instance. The reads
// that are remembered are in reads at once; wait takes them all again from the
// memo, with the ones that fills read.
type pendingLocalReads struct {
	memo       *localReadMemo
	instanceID int
	reads      localReads
	// torrents are the torrents with a remembered read or a fill.
	torrents []localTorrent
	fills    map[chan struct{}]struct{}
}

// localReads holds the Read releases that a local read took from files,
// keyed by cacheKeyForTorrentFiles.
type localReads map[string]localReadEntry

// read returns the Read release of a local torrent: from its files when the
// local read had them for the same name and category, else from its name. A
// caller can take a new torrent list after the local read.
func (r localReads) read(m matcher, instanceID int, torrent *qbt.Torrent) namedRelease {
	if entry, ok := r[cacheKeyForTorrentFiles(instanceID, torrent.Hash)]; ok && entry.name == torrent.Name && entry.category == torrent.Category {
		return entry.read
	}
	return m.readName(torrent.Name)
}

// readLocalTorrents reads the torrents of one instance whose names need their
// files (nameNeedsFiles), as search reads them. A torrent whose files do not
// load in time keeps its name read.
func (s *Service) readLocalTorrents(ctx context.Context, instanceID int, torrents iter.Seq[*qbt.Torrent]) localReads {
	return s.requestLocalReads(ctx, instanceID, torrents).wait(ctx)
}

// requestLocalReads returns the remembered reads of the torrents whose names
// need their files, and starts fills for the ones that are missing. It reads
// torrents only during the call.
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
			// A remembered failure: the name read stands.
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
		// The hourly refresh reads with the normal freshness of the file
		// cache, so a rename made in qBittorrent shows within the hour.
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

// hasFills reports whether wait has fills to wait for.
func (p *pendingLocalReads) hasFills() bool {
	return len(p.fills) > 0
}

// wait waits for the fills of the request, at most localReadWait or until ctx
// ends, and returns the reads. A torrent that is still missing then keeps its
// name read, and so does a torrent whose read qui forgot during the wait.
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

	// A caller can wait on other instances first, so the remembered reads are
	// taken again also without fills.
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

// fillLocalReads reads the files of torrents and remembers the reads. It runs
// under its own context, so a caller that gives up does not stop it, and it
// stores what it read when it stops. acceptOldFiles lets it read SQLite file
// lists up to localReadFilesMaxAge old.
func (s *Service) fillLocalReads(ctx context.Context, instanceID int, torrents []localTorrent, done chan struct{}, acceptOldFiles bool) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), localReadFillTimeout)
	defer cancel()
	ctx = qbittorrent.WithFileFetchLimit(ctx, localReadFetchSlots)
	if acceptOldFiles {
		ctx = qbittorrent.WithFilesCacheMaxAge(ctx, localReadFilesMaxAge)
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
				reads[torrent.hash], _ = s.readRelease(torrent.name, files, readPolicy{
					mapContent: func(contentInfo ContentTypeInfo) ContentTypeInfo {
						if ruleInfo, ok := categoryRuleContentType(settings.CategoryMappingRules, torrent.category); ok {
							return ruleInfo
						}
						return contentInfo
					},
				})
			}
		}
	} else {
		// Without the category mapping rules, a read could differ from the
		// next one, so the fill stores nothing.
		log.Debug().Err(err).Int("instanceID", instanceID).
			Msg("crossseed: local torrents keep their name read, because the settings could not be loaded")
	}

	memo := &s.localReadMemo
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
		if err != nil {
			continue
		}
		if read, ok := reads[torrent.hash]; ok {
			memo.entries[key] = localReadEntry{name: torrent.name, category: torrent.category, read: read, fromFiles: true, expires: now.Add(localReadTTL)}
			continue
		}
		// A fill that stopped early did not try every torrent, and a new torrent
		// can have no files yet: neither counts as a failure.
		if fetchErr != nil || now.Sub(time.Unix(torrent.addedOn, 0)) < localReadFreshAdd {
			continue
		}
		entry := memo.entries[key]
		if !entry.matches(torrent) {
			entry = localReadEntry{name: torrent.name, category: torrent.category}
		}
		// A failed refresh keeps the old read and tries again after a minute.
		entry.expires = now.Add(localReadFailureTTL)
		memo.entries[key] = entry
	}
}

// forgetLocalRead drops the remembered read of one torrent, after qui renamed,
// moved, or deleted it. A fill that reads the torrent now does not store it.
func (s *Service) forgetLocalRead(instanceID int, hash string) {
	memo := &s.localReadMemo
	key := localReadKey{instanceID: instanceID, hash: normalizeHash(hash)}
	memo.mu.Lock()
	defer memo.mu.Unlock()
	delete(memo.entries, key)
	delete(memo.filling, key)
}

// forgetLocalReads drops every remembered read, because the category mapping
// rules change the reads. A fill that runs now stores nothing, and the next
// caller starts a new one.
func (s *Service) forgetLocalReads() {
	memo := &s.localReadMemo
	memo.mu.Lock()
	defer memo.mu.Unlock()
	clear(memo.entries)
	clear(memo.filling)
}
