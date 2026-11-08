// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"context"
	"maps"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
)

// runSession owns the Shared-file groups and the facts of one run, dry run, or preview (ADR 0042).
type runSession struct {
	// ctx is the run context, or the context of the live plan step.
	ctx        context.Context
	session    CrossMatchSession
	instanceID int
	filesRead  filesReader
	evalCtx    *EvalContext
	groups     *crossSeedGroups
	// The Deletion selection sorts torrents in place, so position keeps the order at open.
	torrents []qbt.Torrent
	position map[string]int
	links    map[string][]string
	// groupsOff is set when no reader needs groups, so an ask reads nothing.
	groupsOff  bool
	allGrouped bool
	grouped    map[string]struct{} // torrents asked with Groups
	sharedRead bool
	// files is made once, because cachedTorrentFilesFetcher keeps the map.
	files       map[string]qbt.TorrentFiles
	filesLoaded bool
	// Only a due tag rule that includes cross-seeds lets an expansion override a tag.
	tagGroups bool
}

// openRunSession asks for the whole library when a reader other than the deletes reads groups or facts.
func (s *Service) openRunSession(ctx context.Context, instanceID int, torrents []qbt.Torrent, needs RuleNeeds, evalCtx *EvalContext) error {
	session, err := s.crossMatcher.OpenCrossMatchSession(ctx, instanceID, needs.CrossMatch)
	if err != nil {
		return err
	}
	r := &runSession{
		ctx:        ctx,
		session:    session,
		instanceID: instanceID,
		filesRead:  s.filesReader,
		evalCtx:    evalCtx,
		torrents:   torrents,
		position:   make(map[string]int, len(torrents)),
		links:      make(map[string][]string),
		groupsOff:  !needs.CrossMatch.SameContent,
		grouped:    make(map[string]struct{}),
		files:      make(map[string]qbt.TorrentFiles),
		tagGroups:  needs.TagCrossSeeds,
	}
	for i, t := range torrents {
		r.position[t.Hash] = i
	}
	r.groups = &crossSeedGroups{byHash: make(map[string][]qbt.Torrent), session: r}
	evalCtx.crossSeeds = r.groups

	cm := needs.CrossMatch
	facts := cm.SameExists || cm.SameSeeding || cm.SameTags || cm.OtherExists || cm.OtherSeeding
	if facts || needs.CrossSeedGroupsAll {
		r.resolve(ResolveRequest{All: true, Groups: needs.CrossSeedGroupsAll})
	}
	return nil
}

func (r *runSession) ensureGroups(hash string) {
	if !r.covered(hash) {
		r.resolve(ResolveRequest{Hashes: []string{hash}, Groups: true})
	}
}

// An answer holds whole components, so a member of a known group is covered.
func (r *runSession) covered(hash string) bool {
	if !r.scoped() {
		return true
	}
	if _, done := r.grouped[hash]; done {
		return true
	}
	_, grouped := r.groups.byHash[hash]
	return grouped
}

// resolveDeletes follows the hardlink copies of linkHashes and their groups, so the expansion and the
// FREE_SPACE projection meet no miss. otherShared also asks for the Shared-file check.
func (g *crossSeedGroups) resolveDeletes(hashes, linkHashes []string, hardlinks *HardlinkIndex, otherShared bool) {
	if g == nil || g.session == nil || len(hashes)+len(linkHashes) == 0 {
		return
	}
	r := g.session
	r.resolveGroups(slices.Concat(hashes, linkHashes), otherShared && !r.sharedRead)
	if hardlinks == nil {
		return
	}
	seen := make(map[string]struct{}, len(linkHashes))
	for _, hash := range linkHashes {
		seen[hash] = struct{}{}
	}
	for frontier := linkHashes; len(frontier) > 0; {
		var next []string
		for _, hash := range frontier {
			for _, member := range append(memberHashes(g.byHash[hash]), hash) {
				for _, copyHash := range hardlinks.GetHardlinkCopies(member) {
					if _, ok := seen[copyHash]; !ok {
						seen[copyHash] = struct{}{}
						next = append(next, copyHash)
					}
				}
			}
		}
		r.resolveGroups(next, false)
		frontier = next
	}
}

func memberHashes(members []qbt.Torrent) []string {
	hashes := make([]string, len(members))
	for i, m := range members {
		hashes[i] = m.Hash
	}
	return hashes
}

// scoped reports whether the run session reads groups only for the torrents that a reader asks
// for, so a reader asks before its loop.
func (g *crossSeedGroups) scoped() bool {
	return g != nil && g.session != nil && g.session.scoped()
}

func (r *runSession) scoped() bool {
	return !r.groupsOff && !r.allGrouped
}

func (g *crossSeedGroups) setContext(ctx context.Context) {
	if g != nil && g.session != nil {
		g.session.ctx = ctx
	}
}

// With a run session, only a due tag rule that includes cross-seeds reads the group.
func (g *crossSeedGroups) tagOwnerMembers(hash string) []qbt.Torrent {
	if g != nil && g.session != nil && !g.session.tagGroups {
		return nil
	}
	return g.members(hash)
}

func (r *runSession) resolveGroups(hashes []string, otherShared bool) {
	var ask []string
	for _, hash := range hashes {
		if !r.covered(hash) {
			ask = append(ask, hash)
		}
	}
	if len(ask) == 0 && !otherShared {
		return
	}
	r.resolve(ResolveRequest{Hashes: ask, Groups: len(ask) > 0, OtherShared: otherShared})
}

func (r *runSession) resolve(req ResolveRequest) {
	result := r.session.Resolve(r.ctx, req)
	if req.OtherShared {
		r.sharedRead = true
	}
	if req.Groups {
		if req.All {
			r.allGrouped = true
		}
		for _, hash := range req.Hashes {
			r.grouped[hash] = struct{}{}
		}
	}
	r.merge(result)

	starts := slices.Collect(maps.Keys(result.SameInstanceContentByHash))
	if req.All {
		starts = slices.Collect(maps.Keys(r.links))
	}
	added := r.addGroups(slices.Concat(req.Hashes, starts))
	if r.filesLoaded {
		r.readMissingFiles(r.ctx, added)
	}
}

func (r *runSession) merge(result *CrossMatchResult) {
	ev := r.evalCtx
	mergeSet(&ev.SameInstanceCrossSeedHashSet, result.SameInstanceExists)
	mergeSet(&ev.SameInstanceCrossSeedSeedingHashSet, result.SameInstanceSeeding)
	mergeSet(&ev.CrossInstanceHashSet, result.OtherInstanceExists)
	mergeSet(&ev.CrossInstanceSeedingHashSet, result.OtherInstanceSeeding)
	mergeSet(&ev.SameInstanceCrossSeedUnknown, result.SameInstanceUnknown)
	mergeSet(&ev.CrossInstanceUnknown, result.OtherInstanceUnknown)
	mergeSet(&ev.CrossInstanceSharedHashSet, result.OtherInstanceShared)
	mergeSet(&ev.CrossInstanceSharedUnknown, result.OtherInstanceSharedUnknown)
	if result.SameInstanceTagsByHash != nil {
		if ev.SameInstanceCrossSeedTagsByHash == nil {
			ev.SameInstanceCrossSeedTagsByHash = make(map[string][]string, len(result.SameInstanceTagsByHash))
		}
		maps.Copy(ev.SameInstanceCrossSeedTagsByHash, result.SameInstanceTagsByHash)
	}
	if result.OfflineLocalInstances != nil {
		ev.OfflineLocalInstances = result.OfflineLocalInstances
	}
	touched := make(map[string]struct{})
	for a, next := range result.SameInstanceContentByHash {
		for _, b := range next {
			if a == b {
				continue
			}
			r.links[a] = append(r.links[a], b)
			r.links[b] = append(r.links[b], a)
			touched[a], touched[b] = struct{}{}, struct{}{}
		}
	}
	for hash := range touched {
		slices.Sort(r.links[hash])
		r.links[hash] = slices.Compact(r.links[hash])
	}
	maps.Copy(r.files, result.MemberFiles)
}

func mergeSet(dst *map[string]struct{}, src map[string]struct{}) {
	if src == nil {
		return
	}
	if *dst == nil {
		*dst = make(map[string]struct{}, len(src))
	}
	maps.Copy(*dst, src)
}

// The walk starts at the member first in the run list, so a group has one order whatever the ask.
func (r *runSession) addGroups(starts []string) []string {
	byHash := r.groups.byHash
	var groups [][]string
	need := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, start := range starts {
		if _, done := seen[start]; done || len(r.links[start]) == 0 {
			continue
		}
		if _, grouped := byHash[start]; grouped {
			continue
		}
		component := r.walk(start, seen)
		first := ""
		for _, hash := range component {
			if p, ok := r.position[hash]; ok && (first == "" || p < r.position[first]) {
				first = hash
			}
		}
		if first == "" {
			continue
		}
		var members []string
		for _, hash := range r.walk(first, make(map[string]struct{})) {
			if _, ok := r.position[hash]; ok {
				members = append(members, hash)
				need[hash] = struct{}{}
			}
		}
		groups = append(groups, members)
	}
	if len(groups) == 0 {
		return nil
	}
	torrentByHash := make(map[string]qbt.Torrent, len(need))
	for _, t := range r.torrents {
		if _, ok := need[t.Hash]; ok {
			torrentByHash[t.Hash] = t
		}
	}
	var added []string
	for _, hashes := range groups {
		members := make([]qbt.Torrent, len(hashes))
		for i, hash := range hashes {
			members[i] = torrentByHash[hash]
		}
		for _, hash := range hashes {
			byHash[hash] = members
		}
		added = append(added, hashes...)
	}
	return added
}

func (r *runSession) walk(start string, seen map[string]struct{}) []string {
	seen[start] = struct{}{}
	queue := []string{start}
	for i := 0; i < len(queue); i++ {
		for _, next := range r.links[queue[i]] {
			if _, ok := seen[next]; !ok {
				seen[next] = struct{}{}
				queue = append(queue, next)
			}
		}
	}
	return queue
}

// loadFiles reads through filesReader only the members without a list and the hardlink groups.
func (r *runSession) loadFiles(ctx context.Context, hardlinks *HardlinkIndex) {
	r.evalCtx.CrossSeedFilesByHash = r.files
	r.filesLoaded = true
	hashes := slices.Collect(maps.Keys(r.groups.byHash))
	if hardlinks != nil {
		for _, group := range hardlinks.DeleteSafeGroupBySignature {
			hashes = append(hashes, group...)
		}
	}
	r.readMissingFiles(ctx, hashes)
}

func (r *runSession) readMissingFiles(ctx context.Context, hashes []string) {
	var missing []string
	for _, hash := range hashes {
		if _, ok := r.files[hash]; !ok {
			missing = append(missing, hash)
		}
	}
	if len(missing) == 0 || r.filesRead == nil {
		return
	}
	slices.Sort(missing)
	filesByHash, err := r.filesRead.GetTorrentFilesBatch(ctx, r.instanceID, slices.Compact(missing))
	if err != nil {
		log.Warn().Err(err).Int("instanceID", r.instanceID).
			Msg("automations: some cross-seed files could not be loaded")
	}
	maps.Copy(r.files, filesByHash)
}

// freeSpaceReadAheadBatch is the first read-ahead batch of a FREE_SPACE delete rule.
const freeSpaceReadAheadBatch = 32

// freeSpaceReadAhead reads the groups of the torrents that a FREE_SPACE delete rule can claim next.
// Each claim changes the FREE_SPACE value, so a batch holds every torrent in the rule's scope.
type freeSpaceReadAhead struct {
	groups      *crossSeedGroups
	hardlinks   *HardlinkIndex // nil when the expansion follows no hardlink copy
	otherShared bool
	size        int
	next        int // the first position in sort order that no batch holds
}

func newFreeSpaceReadAhead(groups *crossSeedGroups, hardlinks *HardlinkIndex, otherShared bool) *freeSpaceReadAhead {
	if groups == nil || groups.session == nil {
		return nil
	}
	return &freeSpaceReadAhead{groups: groups, hardlinks: hardlinks, otherShared: otherShared, size: freeSpaceReadAheadBatch}
}

// ask must get the same sorted torrents and inScope on each call, with i ascending.
func (a *freeSpaceReadAhead) ask(torrents []qbt.Torrent, i int, inScope func(*qbt.Torrent) bool) {
	if a == nil || i < a.next {
		return
	}
	var batch []string
	for ; i < len(torrents) && len(batch) < a.size; i++ {
		if inScope(&torrents[i]) {
			batch = append(batch, torrents[i].Hash)
		}
	}
	a.next = i
	a.size *= 2
	a.groups.resolveDeletes(nil, batch, a.hardlinks, a.otherShared)
}
