// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"maps"
	"slices"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// CrossMatchSession reads only the torrents that a reader asks for and their groups, and keeps what it read (ADR 0042).
type CrossMatchSession interface {
	Resolve(ctx context.Context, req ResolveRequest) *CrossMatchResult
}

// ResolveRequest names the torrents that one Resolve answers.
type ResolveRequest struct {
	// A torrent outside the snapshot is in no set.
	Hashes []string
	All    bool
	// Groups follows each group to its whole component. Without it, only the facts are read.
	Groups bool
	// OtherShared checks the whole library, because a scoped pool can miss a Shared file.
	OtherShared bool
}

// Indexes refer to torrents.
type crossMatchSession struct {
	svc        *Service
	instanceID int
	needs      CrossMatchNeeds
	torrents   []qbittorrent.CrossInstanceTorrentView
	pos        map[string]int
	instances  []*models.Instance
	listErr    error
	files      *batchFiles
	// asked holds the file keys that a fetch asked for, so no round asks twice.
	asked map[string]struct{}

	indexed    bool
	reads      localReads
	picks      [][]*candidatePick // Name and Release picks; path picks join as path groups are read
	reverse    [][]int            // the torrents that pick each torrent
	pathGroups [][]int
	groupsOf   [][]int
	groupRead  []bool
	expanded   []bool
	links      []map[int]struct{} // both ways
	// A classification is final once the path groups are read, because the picks are final then.
	classified []*indexMatch

	otherIndexed bool
	otherIdx     *matchIndex
	others       []qbittorrent.CrossInstanceTorrentView
	offline      []*models.Instance
	otherPicks   [][]*candidatePick
	shared       *CrossMatchResult // the other-instance Shared-file check, once it ran
}

// OpenCrossMatchSession reads no file list. A failed instance list makes every torrent Unknown.
func (s *Service) OpenCrossMatchSession(ctx context.Context, currentInstanceID int, needs CrossMatchNeeds) (CrossMatchSession, error) {
	torrents, instances, listErr, err := s.crossMatchSnapshot(ctx, currentInstanceID)
	if err != nil {
		return nil, err
	}
	c := &crossMatchSession{
		svc:        s,
		instanceID: currentInstanceID,
		needs:      needs,
		torrents:   torrents,
		pos:        make(map[string]int, len(torrents)),
		instances:  instances,
		listErr:    listErr,
		asked:      make(map[string]struct{}),
		files:      newBatchFiles(s, instances, needs.SameContent),
	}
	for i := range torrents {
		c.pos[normalizeHash(torrents[i].Hash)] = i
	}
	return c, nil
}

func (c *crossMatchSession) needsSame() bool {
	n := c.needs
	return n.SameExists || n.SameSeeding || n.SameTags || n.SameContent
}

func (c *crossMatchSession) needsOther() bool {
	return c.needs.OtherExists || c.needs.OtherSeeding
}

func (c *crossMatchSession) Resolve(ctx context.Context, req ResolveRequest) *CrossMatchResult {
	result := &CrossMatchResult{}
	var asked []int
	if req.All {
		asked = indexesOf(len(c.torrents))
	} else {
		for _, hash := range req.Hashes {
			if i, ok := c.pos[normalizeHash(hash)]; ok {
				asked = append(asked, i)
			}
		}
		slices.Sort(asked)
		asked = slices.Compact(asked)
	}
	if len(asked) == 0 && !req.OtherShared {
		return result
	}
	c.index(ctx)

	if c.needsSame() {
		facts := asked
		if req.Groups && c.needs.SameContent {
			c.close(ctx, asked)
			facts = c.addGroups(ctx, asked, result)
		} else {
			c.readFacts(ctx, asked)
		}
		c.addSameFacts(ctx, facts, result)
	}

	checkShared := req.OtherShared && instanceHas(c.instances, c.instanceID, models.CapabilityTrustedIdentity)
	if c.needsOther() || checkShared {
		c.indexOther(ctx)
	}
	if c.needsOther() && c.otherPicks != nil {
		c.fetchOtherFacts(ctx, asked)
		c.svc.addOtherInstanceFacts(ctx, c.instanceID, c.torrents, c.otherPicks, asked, c.needs, c.files, result)
	}
	if checkShared {
		c.otherShared(ctx, result)
	}

	// A peer whose torrents could not be read can hold the only copy.
	c.addUnknown(result, req.OtherShared)
	return result
}

// index selects the candidates of the whole library from names and local reads.
func (c *crossMatchSession) index(ctx context.Context) {
	if c.indexed {
		return
	}
	c.indexed = true
	n := len(c.torrents)
	c.reads = c.svc.readLocalTorrents(ctx, c.instanceID, viewTorrents(c.torrents))
	c.picks = make([][]*candidatePick, n)
	c.reverse = make([][]int, n)
	c.groupsOf = make([][]int, n)
	c.expanded = make([]bool, n)
	c.links = make([]map[int]struct{}, n)
	c.classified = make([]*indexMatch, n)
	if !c.needsSame() {
		return
	}
	if idx := c.svc.buildIndexFromViews(c.torrents, c.needs.SameSeeding, c.reads); idx != nil {
		c.picks = c.svc.selectAllCandidates(ctx, c.torrents, idx, true, c.reads)
		c.pathGroups = sharedPathGroups(c.torrents)
	}
	for i, picks := range c.picks {
		for _, p := range picks {
			if j, ok := c.pos[p.entry.hash]; ok {
				c.reverse[j] = append(c.reverse[j], i)
			}
		}
	}
	c.groupRead = make([]bool, len(c.pathGroups))
	for g, group := range c.pathGroups {
		for _, i := range group {
			c.groupsOf[i] = append(c.groupsOf[i], g)
		}
	}
}

func (c *crossMatchSession) indexOther(ctx context.Context) {
	if c.otherIndexed || c.listErr != nil {
		return
	}
	c.otherIndexed = true
	c.otherIdx, c.others, c.offline = c.svc.buildOtherInstanceIndex(ctx, c.instances, c.instanceID, c.needs.OtherSeeding, c.reads)
	if c.otherIdx != nil {
		c.otherPicks = c.svc.selectAllCandidates(ctx, c.torrents, c.otherIdx, false, c.reads)
	}
}

type fileNeed map[int]map[string]struct{}

func (n fileNeed) add(instanceID int, hash string) {
	if n[instanceID] == nil {
		n[instanceID] = make(map[string]struct{})
	}
	n[instanceID][normalizeHash(hash)] = struct{}{}
}

func (c *crossMatchSession) addTorrent(n fileNeed, i int) {
	if c.torrents[i].ContentPath != "" {
		n.add(c.instanceID, c.torrents[i].Hash)
	}
}

// addSource follows the rules of prefetch.
func (c *crossMatchSession) addSource(n fileNeed, i int, picks []*candidatePick) {
	if len(picks) == 0 || c.torrents[i].ContentPath == "" {
		return
	}
	c.addTorrent(n, i)
	for _, p := range picks {
		if p.entry.view.ContentPath != "" {
			n.add(p.entry.instanceID, p.entry.hash)
		}
	}
}

func (c *crossMatchSession) addUnreadGroups(n fileNeed, i int) {
	for _, g := range c.groupsOf[i] {
		if !c.groupRead[g] {
			for _, j := range c.pathGroups[g] {
				c.addTorrent(n, j)
			}
		}
	}
}

// fetch follows prefetch, and skips the keys that an earlier fetch asked for.
func (c *crossMatchSession) fetch(ctx context.Context, need fileNeed) {
	for instanceID, set := range need {
		var hashes []string
		for hash := range set {
			key := cacheKeyForTorrentFiles(instanceID, hash)
			if _, done := c.asked[key]; done {
				continue
			}
			c.asked[key] = struct{}{}
			hashes = append(hashes, hash)
		}
		if len(hashes) == 0 {
			continue
		}
		slices.Sort(hashes)
		filesByHash, err := c.svc.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
		if err != nil {
			log.Debug().Err(err).Int("instanceID", instanceID).Msg("crossseed: some cross-match file lists could not be read")
		}
		for hash, files := range filesByHash {
			key := cacheKeyForTorrentFiles(instanceID, hash)
			c.files.fetched[key] = files
			if c.svc.torrentFilesCache != nil {
				c.svc.torrentFilesCache.Set(key, cloneTorrentFiles(files), ttlcache.DefaultTTL)
			}
		}
	}
}

// readGroups links the torrents that share a file path.
func (c *crossMatchSession) readGroups(ctx context.Context, torrents []int) {
	var groups [][]int
	for _, i := range torrents {
		for _, g := range c.groupsOf[i] {
			if !c.groupRead[g] {
				c.groupRead[g] = true
				groups = append(groups, c.pathGroups[g])
			}
		}
	}
	if len(groups) == 0 {
		return
	}
	needs := c.needs
	needs.SameContent = true // addSharedPathPicks returns the links only with SameContent
	shared := c.files.addSharedPathPicks(ctx, c.instanceID, c.torrents, groups, c.picks, needs)
	for hash, next := range shared {
		for _, other := range next {
			c.link(c.pos[normalizeHash(hash)], c.pos[normalizeHash(other)])
		}
	}
}

func (c *crossMatchSession) link(i, j int) {
	if i == j {
		return
	}
	for _, pair := range [2][2]int{{i, j}, {j, i}} {
		if c.links[pair[0]] == nil {
			c.links[pair[0]] = make(map[int]struct{})
		}
		c.links[pair[0]][pair[1]] = struct{}{}
	}
}

// Call classify only after the path groups of i are read.
func (c *crossMatchSession) classify(ctx context.Context, i int) indexMatch {
	if m := c.classified[i]; m != nil {
		return *m
	}
	m := c.svc.classifyCandidates(ctx, c.torrents[i].Torrent, c.instanceID, c.picks[i], true, c.needs.SameTags, c.files)
	c.classified[i] = &m
	return m
}

// unreadLink links a pick whose list cannot be read, as classifyCandidates does.
func (c *crossMatchSession) unreadLink(ctx context.Context, source, m int) bool {
	s, t := c.torrents[source], c.torrents[m]
	if s.ContentPath == "" || t.ContentPath == "" {
		return false
	}
	if _, ok := c.files.get(ctx, c.instanceID, s.Hash, s.SavePath); !ok {
		return true
	}
	_, ok := c.files.get(ctx, c.instanceID, t.Hash, t.SavePath)
	return !ok
}

// close follows the links from start in batched rounds, with one fetch per round.
func (c *crossMatchSession) close(ctx context.Context, start []int) {
	frontier := slices.DeleteFunc(slices.Clone(start), func(i int) bool { return c.expanded[i] })
	for len(frontier) > 0 && ctx.Err() == nil {
		need := make(fileNeed)
		for _, m := range frontier {
			c.addSource(need, m, c.picks[m])
			if len(c.reverse[m]) > 0 {
				c.addTorrent(need, m)
			}
			c.addUnreadGroups(need, m)
			for _, s := range c.reverse[m] {
				c.addTorrent(need, s)
			}
		}
		c.fetch(ctx, need)
		c.readGroups(ctx, frontier)
		for _, m := range frontier {
			c.linkUnread(ctx, m)
		}
		frontier = c.nextFrontier(frontier)
	}
}

// linkUnread links m to each pick and picker when one of the two lists cannot be read.
func (c *crossMatchSession) linkUnread(ctx context.Context, m int) {
	c.expanded[m] = true
	for _, hash := range c.classify(ctx, m).unreadCandidates {
		c.link(m, c.pos[hash])
	}
	for _, s := range c.reverse[m] {
		if c.unreadLink(ctx, s, m) {
			c.link(s, m)
		}
	}
}

func (c *crossMatchSession) nextFrontier(frontier []int) []int {
	seen := make(map[int]struct{})
	var next []int
	for _, m := range frontier {
		for j := range c.links[m] {
			if _, dup := seen[j]; !dup && !c.expanded[j] {
				seen[j] = struct{}{}
				next = append(next, j)
			}
		}
	}
	slices.Sort(next)
	return next
}

func (c *crossMatchSession) component(i int) []int {
	seen := map[int]struct{}{i: {}}
	queue := []int{i}
	for len(queue) > 0 {
		m := queue[0]
		queue = queue[1:]
		for j := range c.links[m] {
			if _, ok := seen[j]; !ok {
				seen[j] = struct{}{}
				queue = append(queue, j)
			}
		}
	}
	return slices.Sorted(maps.Keys(seen))
}

// addGroups adds the file lists of the groups with two or more torrents. It returns the asked
// torrents and the members of their groups, which the closure classified.
func (c *crossMatchSession) addGroups(ctx context.Context, asked []int, result *CrossMatchResult) []int {
	result.SameInstanceContentByHash = make(map[string][]string)
	result.MemberFiles = make(map[string]qbt.TorrentFiles)
	done := make(map[int]struct{})
	members := slices.Clone(asked)
	for _, i := range asked {
		if _, ok := done[i]; ok || len(c.links[i]) == 0 {
			continue
		}
		for _, m := range c.component(i) {
			done[m] = struct{}{}
			members = append(members, m)
			t := c.torrents[m]
			next := make([]string, 0, len(c.links[m]))
			for j := range c.links[m] {
				next = append(next, c.torrents[j].Hash)
			}
			slices.Sort(next)
			result.SameInstanceContentByHash[t.Hash] = next
			if data, ok := c.files.get(ctx, c.instanceID, t.Hash, t.SavePath); ok {
				result.MemberFiles[normalizeHash(t.Hash)] = data.files
			}
		}
	}
	slices.Sort(members)
	return slices.Compact(members)
}

// readFacts reads the torrents, their path groups and their picks, but not the groups.
func (c *crossMatchSession) readFacts(ctx context.Context, asked []int) {
	need := make(fileNeed)
	for _, i := range asked {
		c.addSource(need, i, c.picks[i])
		c.addUnreadGroups(need, i)
	}
	c.fetch(ctx, need)
	c.readGroups(ctx, asked)
}

func (c *crossMatchSession) addSameFacts(ctx context.Context, asked []int, result *CrossMatchResult) {
	result.SameInstanceExists = make(map[string]struct{})
	result.SameInstanceUnknown = make(map[string]struct{})
	if c.needs.SameSeeding {
		result.SameInstanceSeeding = make(map[string]struct{})
	}
	if c.needs.SameTags {
		result.SameInstanceTagsByHash = make(map[string][]string)
	}
	for _, i := range asked {
		if ctx.Err() != nil {
			break
		}
		source := c.torrents[i].Torrent
		m := c.classify(ctx, i)
		if m.unread {
			result.SameInstanceUnknown[source.Hash] = struct{}{}
		}
		if !m.matched {
			continue
		}
		result.SameInstanceExists[source.Hash] = struct{}{}
		if c.needs.SameSeeding && m.seeding {
			result.SameInstanceSeeding[source.Hash] = struct{}{}
		}
		if c.needs.SameTags && len(m.memberTags) > 0 {
			result.SameInstanceTagsByHash[source.Hash] = m.memberTags
		}
	}
}

func (c *crossMatchSession) fetchOtherFacts(ctx context.Context, asked []int) {
	need := make(fileNeed)
	for _, i := range asked {
		c.addSource(need, i, c.otherPicks[i])
	}
	c.fetch(ctx, need)
}

// otherShared runs once per session.
func (c *crossMatchSession) otherShared(ctx context.Context, result *CrossMatchResult) {
	if c.shared == nil {
		c.shared = &CrossMatchResult{}
		var sharedPicks [][]*candidatePick
		var sharedGroups []crossPathGroup
		if c.otherIdx != nil {
			sharedPicks, sharedGroups = otherSharedCandidates(c.instances, c.torrents, c.otherPicks, c.otherIdx, c.others)
			groupSources, groupPicks := prefetchGroups(len(c.torrents), sharedGroups)
			need := make(fileNeed)
			for _, group := range groupSources {
				for _, i := range group {
					c.addTorrent(need, i)
				}
			}
			for _, picks := range [][][]*candidatePick{sharedPicks, groupPicks} {
				for i, sourcePicks := range picks {
					c.addSource(need, i, sourcePicks)
				}
			}
			c.fetch(ctx, need)
		}
		c.files.addOtherSharedResult(ctx, c.instanceID, c.torrents, sharedPicks, sharedGroups, c.offline, c.shared)
	}
	result.OtherInstanceShared = maps.Clone(c.shared.OtherInstanceShared)
	result.OtherInstanceSharedUnknown = maps.Clone(c.shared.OtherInstanceSharedUnknown)
	result.OfflineLocalInstances = slices.Clone(c.shared.OfflineLocalInstances)
}

func (c *crossMatchSession) addUnknown(result *CrossMatchResult, otherShared bool) {
	addUnknownSets(result, c.torrents,
		c.needsSame() && c.listErr != nil,
		c.needsOther() && (c.listErr != nil || len(c.offline) > 0),
		otherShared && (c.listErr != nil || len(result.OfflineLocalInstances) > 0))
}
