// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// PROTOTYPE #3895: a throwaway session that tests the plan of #3895. It is
// not production code: no error handling beyond what the tests need.

package crossseed

import (
	"context"
	"maps"
	"slices"

	"github.com/autobrr/go-cache/ttlcache"
	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// CrossMatchSession answers the Shared-file groups and the cross-seed facts
// of the torrents that a reader asks for.
type CrossMatchSession interface {
	Resolve(ctx context.Context, req ResolveRequest) *CrossMatchResult
}

// ResolveRequest names the torrents that a reader asks about.
type ResolveRequest struct {
	Hashes      []string
	All         bool
	Groups      bool
	OtherShared bool
}

type crossMatchSession struct {
	svc        *Service
	instanceID int
	needs      CrossMatchNeeds
	torrents   []qbittorrent.CrossInstanceTorrentView
	pos        map[string]int
	instances  []*models.Instance
	listErr    error
	files      *batchFiles
	// asked holds the file keys that a batch already asked for, so a later
	// round does not ask again (get still retries a miss once, as today).
	asked map[string]struct{}

	indexed    bool
	picks      [][]*candidatePick // Name/Release picks; path picks join as groups are read
	reverse    [][]int            // reverse pick index: j -> the sources that pick j
	pathGroups [][]int
	groupsOf   [][]int
	groupRead  []bool
	expanded   []bool
	links      []map[int]struct{} // undirected

	otherBuilt bool
	otherIdx   *matchIndex
	others     []qbittorrent.CrossInstanceTorrentView
	offline    []*models.Instance
	otherPicks [][]*candidatePick
	reads      localReads
}

// OpenCrossMatchSession reads the torrent snapshot and the instance list. It
// reads no file list.
func (s *Service) OpenCrossMatchSession(ctx context.Context, currentInstanceID int, needs CrossMatchNeeds) (CrossMatchSession, error) {
	torrents, err := s.syncManager.GetCachedInstanceTorrents(ctx, currentInstanceID)
	if err != nil {
		return nil, err
	}
	instances, listErr := s.instanceStore.List(ctx)
	sess := &crossMatchSession{
		svc:        s,
		instanceID: currentInstanceID,
		needs:      needs,
		torrents:   torrents,
		pos:        make(map[string]int, len(torrents)),
		instances:  instances,
		listErr:    listErr,
		asked:      make(map[string]struct{}),
		files: &batchFiles{
			svc:       s,
			instances: instances,
			byKey:     make(map[string]*batchFileData),
			fetched:   make(map[string]qbt.TorrentFiles),
			links:     make(map[string]*localMatchContext),
			linkTiers: make(map[[2]string]localLinkResult),
			folders:   make(map[string]fsops.FileKey),
		},
	}
	for i := range torrents {
		sess.pos[normalizeHash(torrents[i].Hash)] = i
	}
	return sess, nil
}

func (c *crossMatchSession) needsSame() bool {
	n := c.needs
	return n.SameExists || n.SameSeeding || n.SameTags || n.SameContent
}

func (c *crossMatchSession) needsOther() bool {
	return c.needs.OtherExists || c.needs.OtherSeeding
}

// index runs candidate selection for the whole library, on names and the
// local reads, and keeps a reverse pick index. It fetches no file list for
// names that do not need files.
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
	if c.otherBuilt || c.listErr != nil {
		return
	}
	c.otherBuilt = true
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

// addSource adds a torrent of the instance when it has metadata.
func (c *crossMatchSession) addSource(n fileNeed, i int) {
	if c.torrents[i].ContentPath != "" {
		n.add(c.instanceID, c.torrents[i].Hash)
	}
}

func (n fileNeed) addPick(p *candidatePick) {
	if p.entry.view.ContentPath != "" {
		n.add(p.entry.instanceID, p.entry.hash)
	}
}

// fetch reads in one batch per instance the lists that no earlier batch
// asked for, as prefetch does.
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
		filesByHash, _ := c.svc.syncManager.GetTorrentFilesBatch(ctx, instanceID, hashes)
		for hash, files := range filesByHash {
			key := cacheKeyForTorrentFiles(instanceID, hash)
			c.files.fetched[key] = files
			if c.svc.torrentFilesCache != nil {
				c.svc.torrentFilesCache.Set(key, cloneTorrentFiles(files), ttlcache.DefaultTTL)
			}
		}
	}
}

// readGroups reads the path groups of members that no round read yet, adds
// their path picks, and links the torrents that share a file path.
func (c *crossMatchSession) readGroups(ctx context.Context, members []int) {
	var groups [][]int
	for _, m := range members {
		for _, g := range c.groupsOf[m] {
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
	needs.SameContent = true // the session needs the links to find the groups
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

// unreadLink reports whether the pick S->m links them: the list of S or of m
// cannot be read, as classifyCandidates links today.
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

// close follows the links from start in batched rounds until no new torrent
// turns up. Each round fetches the lists of the new members, the members of
// their path groups, their picks, and the torrents that pick them.
func (c *crossMatchSession) close(ctx context.Context, start []int) {
	frontier := slices.DeleteFunc(slices.Clone(start), func(i int) bool { return c.expanded[i] })
	for len(frontier) > 0 && ctx.Err() == nil {
		need := make(fileNeed)
		for _, m := range frontier {
			if len(c.picks[m]) > 0 || len(c.reverse[m]) > 0 { // as prefetch: a lone torrent needs no list
				c.addSource(need, m)
			}
			for _, g := range c.groupsOf[m] {
				if !c.groupRead[g] {
					for _, i := range c.pathGroups[g] {
						c.addSource(need, i)
					}
				}
			}
			for _, p := range c.picks[m] {
				need.addPick(p)
			}
			for _, s := range c.reverse[m] {
				c.addSource(need, s)
			}
		}
		c.fetch(ctx, need)
		c.readGroups(ctx, frontier)
		for _, m := range frontier {
			c.expanded[m] = true
			source := c.torrents[m].Torrent
			res := c.svc.classifyCandidates(ctx, source, c.instanceID, c.picks[m], true, false, c.files)
			for _, hash := range res.unreadCandidates {
				c.link(m, c.pos[hash])
			}
			for _, s := range c.reverse[m] {
				if c.unreadLink(ctx, s, m) {
					c.link(s, m)
				}
			}
		}
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
		frontier = next
	}
}

// component returns the torrents linked to i, i included.
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

func (c *crossMatchSession) Resolve(ctx context.Context, req ResolveRequest) *CrossMatchResult {
	result := &CrossMatchResult{}
	var asked []int
	if req.All {
		asked = make([]int, len(c.torrents))
		for i := range asked {
			asked[i] = i
		}
	} else {
		for _, hash := range req.Hashes {
			// A torrent that is not in the snapshot is in no set, as today.
			if i, ok := c.pos[normalizeHash(hash)]; ok {
				asked = append(asked, i)
			}
		}
	}
	if len(asked) == 0 && !req.OtherShared {
		return result
	}
	c.index(ctx)

	if c.needsSame() {
		var members []int
		if req.Groups && c.needs.SameContent {
			c.close(ctx, asked)
			result.SameInstanceContentByHash = make(map[string][]string)
			done := make(map[int]struct{})
			for _, i := range asked {
				if _, ok := done[i]; ok {
					continue
				}
				comp := c.component(i)
				for _, m := range comp {
					done[m] = struct{}{}
					if len(comp) > 1 { // a torrent with no group has no member files
						members = append(members, m)
					}
					for j := range c.links[m] {
						result.SameInstanceContentByHash[c.torrents[m].Hash] = append(result.SameInstanceContentByHash[c.torrents[m].Hash], c.torrents[j].Hash)
					}
					slices.Sort(result.SameInstanceContentByHash[c.torrents[m].Hash])
				}
			}
			result.MemberFiles = make(map[string]qbt.TorrentFiles, len(members))
			for _, m := range members {
				t := c.torrents[m]
				if data, ok := c.files.get(ctx, c.instanceID, t.Hash, t.SavePath); ok {
					result.MemberFiles[normalizeHash(t.Hash)] = data.files
				}
			}
		} else {
			// The facts need the torrent, its picks and its path groups.
			need := make(fileNeed)
			for _, i := range asked {
				if len(c.picks[i]) > 0 {
					c.addSource(need, i)
				}
				for _, g := range c.groupsOf[i] {
					if !c.groupRead[g] {
						for _, j := range c.pathGroups[g] {
							c.addSource(need, j)
						}
					}
				}
				for _, p := range c.picks[i] {
					need.addPick(p)
				}
			}
			c.fetch(ctx, need)
			c.readGroups(ctx, asked)
		}
		c.sameFacts(ctx, asked, result)
	}

	checkShared := req.OtherShared && instanceHas(c.instances, c.instanceID, models.CapabilityTrustedIdentity)
	if c.needsOther() || checkShared {
		c.indexOther(ctx)
	}
	if c.needsOther() && c.otherPicks != nil {
		c.otherFacts(ctx, asked, result)
	}
	if checkShared {
		c.otherShared(ctx, result)
	}

	// A peer whose torrents could not be read can hold the only copy.
	sameUnknown := c.needsSame() && c.listErr != nil
	otherUnknown := c.needsOther() && (c.listErr != nil || len(c.offline) > 0)
	sharedUnknown := req.OtherShared && (c.listErr != nil || len(result.OfflineLocalInstances) > 0)
	if sameUnknown {
		result.SameInstanceUnknown = make(map[string]struct{})
	}
	if otherUnknown {
		result.OtherInstanceUnknown = make(map[string]struct{})
	}
	if sharedUnknown {
		result.OtherInstanceSharedUnknown = make(map[string]struct{}, len(c.torrents))
		for _, t := range c.torrents {
			result.OtherInstanceSharedUnknown[t.Hash] = struct{}{}
		}
	}
	for _, i := range asked {
		h := c.torrents[i].Hash
		if sameUnknown {
			result.SameInstanceUnknown[h] = struct{}{}
		}
		if otherUnknown {
			result.OtherInstanceUnknown[h] = struct{}{}
		}
	}
	return result
}

func (c *crossMatchSession) sameFacts(ctx context.Context, asked []int, result *CrossMatchResult) {
	needs := c.needs
	result.SameInstanceExists = make(map[string]struct{})
	result.SameInstanceUnknown = make(map[string]struct{})
	if needs.SameSeeding {
		result.SameInstanceSeeding = make(map[string]struct{})
	}
	if needs.SameTags {
		result.SameInstanceTagsByHash = make(map[string][]string)
	}
	for _, i := range asked {
		source := c.torrents[i].Torrent
		m := c.svc.classifyCandidates(ctx, source, c.instanceID, c.picks[i], true, needs.SameTags, c.files)
		if m.unread {
			result.SameInstanceUnknown[source.Hash] = struct{}{}
		}
		if !m.matched {
			continue
		}
		result.SameInstanceExists[source.Hash] = struct{}{}
		if needs.SameSeeding && m.seeding {
			result.SameInstanceSeeding[source.Hash] = struct{}{}
		}
		if needs.SameTags && len(m.memberTags) > 0 {
			result.SameInstanceTagsByHash[source.Hash] = m.memberTags
		}
	}
}

func (c *crossMatchSession) otherFacts(ctx context.Context, asked []int, result *CrossMatchResult) {
	need := make(fileNeed)
	for _, i := range asked {
		if len(c.otherPicks[i]) == 0 {
			continue
		}
		c.addSource(need, i)
		for _, p := range c.otherPicks[i] {
			need.addPick(p)
		}
	}
	c.fetch(ctx, need)
	result.OtherInstanceExists = make(map[string]struct{})
	result.OtherInstanceUnknown = make(map[string]struct{})
	if c.needs.OtherSeeding {
		result.OtherInstanceSeeding = make(map[string]struct{})
	}
	for _, i := range asked {
		source := c.torrents[i].Torrent
		m := c.svc.classifyCandidates(ctx, source, c.instanceID, c.otherPicks[i], false, false, c.files)
		if m.unread {
			result.OtherInstanceUnknown[source.Hash] = struct{}{}
		}
		if m.matched {
			result.OtherInstanceExists[source.Hash] = struct{}{}
			if c.needs.OtherSeeding && m.seeding {
				result.OtherInstanceSeeding[source.Hash] = struct{}{}
			}
		}
	}
}

// otherShared runs the other-instance Shared-file check for the whole
// library, as BuildCrossMatchSets does (copied from it for the prototype).
func (c *crossMatchSession) otherShared(ctx context.Context, result *CrossMatchResult) {
	result.OtherInstanceShared = make(map[string]struct{})
	result.OtherInstanceSharedUnknown = make(map[string]struct{})
	if c.otherIdx != nil {
		untrusted := func(p *candidatePick) bool {
			return !instanceHas(c.instances, p.entry.instanceID, models.CapabilityTrustedIdentity)
		}
		sharedPicks := make([][]*candidatePick, len(c.otherPicks))
		for i := range c.otherPicks {
			trusted := slices.DeleteFunc(slices.Clone(c.otherPicks[i]), untrusted)
			sharedPicks[i] = addSizePicks(c.instances, trusted, c.otherIdx.bySize[searchSourceSize(c.torrents[i].Torrent)])
		}
		sharedGroups := crossPathGroups(c.instances, c.torrents, c.others, func(string) bool { return true })
		groupSources, groupPicks := prefetchGroups(len(c.torrents), sharedGroups)
		need := make(fileNeed)
		for _, group := range groupSources {
			for _, i := range group {
				c.addSource(need, i)
			}
		}
		for _, picks := range [][][]*candidatePick{sharedPicks, groupPicks} {
			for i, sourcePicks := range picks {
				if len(sourcePicks) == 0 || c.torrents[i].ContentPath == "" {
					continue
				}
				c.addSource(need, i)
				for _, p := range sourcePicks {
					need.addPick(p)
				}
			}
		}
		c.fetch(ctx, need)
		c.files.addOtherShared(ctx, c.instanceID, c.torrents, sharedPicks, sharedGroups, result)
	}
	for _, inst := range c.offline {
		if models.FilesystemCapabilitiesOf(inst).Has(models.CapabilityTrustedIdentity) {
			result.OfflineLocalInstances = append(result.OfflineLocalInstances, inst.Name)
		}
	}
}
