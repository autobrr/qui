// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/binary"
	"iter"
	"maps"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

// CopyGroup is one Copy group (GLOSSARY.md). ID is keyed, so it does not show
// the instance or hash of the Group representative (ADR 0041).
type CopyGroup struct {
	ID      uint64
	Members []qbittorrent.CopyGroupMember
}

// CopyGroupFiles returns the file lists that it can read of torrents on one
// instance, by normalized hash.
type CopyGroupFiles func(ctx context.Context, instanceID int, torrents []qbittorrent.CrossInstanceTorrentView) (map[string]qbt.TorrentFiles, error)

// CopyLinks is what one FindCopyGroups call found. With it, the next call
// reads and compares only the torrents that changed.
type CopyLinks struct {
	// prints holds each torrent whose links are complete, as the search read it.
	prints map[string]copyGroupsPrint
	// pairs holds the torrents that the search linked.
	pairs [][2]string
	// shared holds the linked torrents that share some files but not all.
	shared [][2]string
	// sizes holds the sizes of the wanted files of each torrent in pairs or
	// shared, so a later call can split the data of a torrent that it does not read.
	sizes map[string][]int64
	// parts holds the Shared parts by instance and hash.
	parts map[int]map[string][]qbittorrent.SharedPart
	// reads holds the read from files of each torrent whose name reads too little.
	reads localReads
}

// without returns the links with the torrents of keys as changed torrents.
func (l *CopyLinks) without(keys iter.Seq[string]) *CopyLinks {
	if l == nil {
		return nil
	}
	next := &CopyLinks{prints: maps.Clone(l.prints), pairs: l.pairs, shared: l.shared, sizes: l.sizes, reads: l.reads}
	for key := range keys {
		delete(next.prints, key)
	}
	return next
}

// FindCopyGroups asks read for the file lists of the torrents that can have a
// copy, and for no other list. A torrent without a list from read stays out of
// every group. An error of read ends the call.
//
// With last, the links of an earlier call, it asks only for the torrents that
// changed since that call and for the torrents that can be their copies. The
// file list of a torrent in last must be the same as in that call. The caller
// keeps the returned links for the next call.
func (s *Service) FindCopyGroups(
	ctx context.Context,
	torrents map[int][]qbittorrent.CrossInstanceTorrentView,
	read CopyGroupFiles,
	last *CopyLinks,
	rules []models.CategoryMappingRule,
	key []byte,
) ([]CopyGroup, *CopyLinks, error) {
	search := newCopyGroupSearch(s, torrents, read, last)
	pathGroups, err := search.readNamesAndPathGroups(ctx)
	if err != nil {
		return nil, nil, err
	}
	reads := search.fileReads(rules)
	idx := s.buildIndexFromViews(search.all, false, reads)
	if idx == nil {
		return nil, search.links(nil, nil, nil, reads), nil
	}
	picks := s.selectAllCandidates(ctx, search.all, idx, false, reads)
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	// The helper skips a pick whose hash it holds, so a pick of the same hash
	// on another instance must not block a shared-path pick.
	for n, instance := range search.instances {
		shared := make([][]*candidatePick, instance.hi-instance.lo)
		search.held.addSharedPathPicks(ctx, instance.id, search.all[instance.lo:instance.hi], search.readable(instance, pathGroups[n]), shared, CrossMatchNeeds{})
		for k := range shared {
			picks[instance.lo+k] = append(picks[instance.lo+k], shared[k]...)
		}
	}
	search.addPackPicks(idx, picks)
	if err := search.readPicks(ctx, picks); err != nil {
		return nil, nil, err
	}
	roots, pairs, shared, err := search.link(ctx, picks)
	if err != nil {
		return nil, nil, err
	}
	return copyGroupsOf(search.all, roots, key), search.links(roots, pairs, shared, reads), nil
}

// copyGroupSearch holds the torrents of one FindCopyGroups call and the file
// lists that it asked for.
type copyGroupSearch struct {
	svc       *Service
	all       []qbittorrent.CrossInstanceTorrentView
	instances []copyGroupInstance
	// keys holds the key of each torrent of all, and index its place in all.
	keys  []string
	index map[string]int
	// places holds the place in all of each torrent, for a pick.
	places map[candidateKey]int
	held   *batchFiles
	read   CopyGroupFiles
	asked  []bool
	// last holds the links of the call before this one, or nil.
	last   *CopyLinks
	prints []copyGroupsPrint
	// changed marks each torrent that last does not hold as it is now.
	changed []bool
	// needsFiles marks each torrent whose name reads too little.
	needsFiles []bool
}

// copyGroupInstance is the range of one instance in copyGroupSearch.all.
type copyGroupInstance struct {
	id, lo, hi int
}

func newCopyGroupSearch(s *Service, torrents map[int][]qbittorrent.CrossInstanceTorrentView, read CopyGroupFiles, last *CopyLinks) *copyGroupSearch {
	l := &copyGroupSearch{svc: s, read: read, last: last}
	for _, id := range slices.Sorted(maps.Keys(torrents)) {
		l.instances = append(l.instances, copyGroupInstance{id: id, lo: len(l.all), hi: len(l.all) + len(torrents[id])})
		l.all = append(l.all, torrents[id]...)
	}
	l.asked = make([]bool, len(l.all))
	l.keys = make([]string, len(l.all))
	l.prints = make([]copyGroupsPrint, len(l.all))
	l.changed = make([]bool, len(l.all))
	l.needsFiles = make([]bool, len(l.all))
	l.index = make(map[string]int, len(l.all))
	l.places = make(map[candidateKey]int, len(l.all))
	l.held = &batchFiles{svc: s, byKey: make(map[string]*batchFileData), fetched: make(map[string]qbt.TorrentFiles, len(l.all))}
	m := s.matcher()
	for i := range l.all {
		l.keys[i] = cacheKeyForTorrentFiles(l.all[i].InstanceID, l.all[i].Hash)
		l.index[l.keys[i]] = i
		l.places[candidateKey{instanceID: l.all[i].InstanceID, hash: normalizeHash(l.all[i].Hash)}] = i
		// An entry without a list keeps batchFiles.get away from the client.
		l.held.fetched[l.keys[i]] = nil
		l.prints[i] = newCopyGroupsPrint(l.all[i].Torrent)
		l.changed[i] = last == nil || last.prints[l.keys[i]] != l.prints[i]
		l.needsFiles[i] = nameNeedsFiles(m.readName(l.all[i].Name).release)
	}
	return l
}

// readNamesAndPathGroups asks for the lists of the changed torrents whose name
// reads too little, and of the shared-path groups that hold a changed torrent.
// It returns those groups for each instance. The torrents of a group with no
// changed torrent keep the links of last.
func (l *copyGroupSearch) readNamesAndPathGroups(ctx context.Context) ([][][]int, error) {
	want := make([]bool, len(l.all))
	for i := range l.all {
		want[i] = l.needsFiles[i] && l.changed[i]
	}
	pathGroups := make([][][]int, len(l.instances))
	for n, instance := range l.instances {
		pathGroups[n] = slices.DeleteFunc(sharedPathGroups(l.all[instance.lo:instance.hi]), func(group []int) bool {
			return !slices.ContainsFunc(group, func(i int) bool { return l.changed[instance.lo+i] })
		})
		for _, group := range pathGroups[n] {
			for _, i := range group {
				want[instance.lo+i] = true
			}
		}
	}
	return pathGroups, l.fetch(ctx, want)
}

// fileReads returns the read from files of each torrent whose name reads too
// little: from last for a torrent that did not change, else from its list.
func (l *copyGroupSearch) fileReads(rules []models.CategoryMappingRule) localReads {
	reads := make(localReads)
	for i := range l.all {
		t := l.all[i].Torrent
		switch list := l.held.fetched[l.keys[i]]; {
		case !l.needsFiles[i]:
		case !l.changed[i]:
			if entry, ok := l.last.reads[l.keys[i]]; ok {
				reads[l.keys[i]] = entry
			}
		case len(list) > 0:
			reads[l.keys[i]] = localReadEntry{name: t.Name, category: t.Category, read: l.svc.readLocalFiles(rules, t.Name, t.Category, list), fromFiles: true}
		}
	}
	return reads
}

// readPicks drops the picks that need no compare, and asks for the lists of
// the others. A torrent is no copy of itself, and two torrents that did not
// change keep their link of last.
func (l *copyGroupSearch) readPicks(ctx context.Context, picks [][]*candidatePick) error {
	want := make([]bool, len(l.all))
	for i := range picks {
		picks[i] = slices.DeleteFunc(picks[i], func(p *candidatePick) bool {
			c := l.places[p.entry.key()]
			return c == i || !l.changed[i] && !l.changed[c]
		})
		for _, p := range picks[i] {
			want[i], want[l.places[p.entry.key()]] = true, true
		}
	}
	return l.fetch(ctx, want)
}

// addPackPicks gives each single episode the season packs of its release as
// picks. A pack is no copy of an episode, so the pick has no name tier: it can
// only share files. An episode and a pack that did not change keep their link
// of last.
func (l *copyGroupSearch) addPackPicks(idx *matchIndex, picks [][]*candidatePick) {
	m := l.svc.matcher()
	policy := localMatchPolicy
	policy.IndividualEpisodes = true
	// A torrent with two Title keys meets a pack two times.
	picked := map[[2]int]struct{}{}
	for key, episodes := range idx.byTitleKey {
		if key.episode == 0 {
			continue
		}
		packs := idx.byTitleKey[titleEpisodeKey{title: key.title, date: key.date}]
		for _, episode := range episodes {
			e := l.places[episode.key()]
			for _, pack := range packs {
				p := l.places[pack.key()]
				if _, done := picked[[2]int{e, p}]; done || !l.changed[e] && !l.changed[p] {
					continue
				}
				picked[[2]int{e, p}] = struct{}{}
				if isTVEpisode(episode.read.release) && isTVSeasonPack(pack.read.release) &&
					m.matchVerdict(pack.read, episode.read, matchEvidence{}, policy).Matched {
					picks[e] = append(picks[e], &candidatePick{entry: pack.candidateEntry})
				}
			}
		}
	}
}

// links returns what the next call starts from, with the Shared parts. roots
// holds the root of the Copy group of each torrent.
func (l *copyGroupSearch) links(roots []int, pairs, shared [][2]string, reads localReads) *CopyLinks {
	next := &CopyLinks{prints: make(map[string]copyGroupsPrint, len(l.all)), pairs: pairs, shared: shared, sizes: map[string][]int64{}, reads: reads}
	for i := range l.all {
		// A torrent whose list did not read is a changed torrent in the next call.
		if !l.unread(i) {
			next.prints[l.keys[i]] = l.prints[i]
		}
	}
	for _, pair := range slices.Concat(pairs, shared) {
		for _, key := range pair {
			if _, ok := next.sizes[key]; !ok {
				next.sizes[key] = l.wantedSizes(key)
			}
		}
	}
	next.parts = sharedParts(l.all, roots, l.index, shared, next.sizes)
	return next
}

// wantedSizes returns the sizes of the files of a torrent that qBittorrent
// downloads: from its list if the call read it, else from last.
func (l *copyGroupSearch) wantedSizes(key string) []int64 {
	list := l.held.fetched[key]
	if len(list) == 0 {
		return l.last.sizes[key]
	}
	sizes := make([]int64, 0, len(list))
	for _, f := range list {
		if f.Priority != 0 {
			sizes = append(sizes, f.Size)
		}
	}
	return sizes
}

// unread reports whether the call asked for the list of the torrent and got
// none. Such a torrent stays out of every group (ADR 0041).
func (l *copyGroupSearch) unread(i int) bool {
	return l.asked[i] && len(l.held.fetched[l.keys[i]]) == 0
}

// readable leaves out the torrents without a file list. Such a torrent links
// to no torrent, and addSharedPathPicks pairs it with each member of its groups.
func (l *copyGroupSearch) readable(instance copyGroupInstance, pathGroups [][]int) [][]int {
	for g, group := range pathGroups {
		pathGroups[g] = slices.DeleteFunc(group, func(i int) bool {
			return len(l.held.fetched[l.keys[instance.lo+i]]) == 0
		})
	}
	return pathGroups
}

// fetch asks read one time for each torrent of want.
func (l *copyGroupSearch) fetch(ctx context.Context, want []bool) error {
	for _, instance := range l.instances {
		var torrents []qbittorrent.CrossInstanceTorrentView
		for i := instance.lo; i < instance.hi; i++ {
			if want[i] && !l.asked[i] {
				l.asked[i] = true
				torrents = append(torrents, l.all[i])
			}
		}
		if len(torrents) == 0 {
			continue
		}
		files, err := l.read(ctx, instance.id, torrents)
		if err != nil {
			return err
		}
		for _, t := range torrents {
			l.held.fetched[cacheKeyForTorrentFiles(instance.id, t.Hash)] = files[normalizeHash(t.Hash)]
		}
	}
	return nil
}

// link joins the pairs of last that did not change and whose lists the call
// did not fail to read, and the picks with equal file sizes. It returns the
// root of each torrent, each pair that it joined, and each pair of torrents
// that share some files but not all.
func (l *copyGroupSearch) link(ctx context.Context, picks [][]*candidatePick) ([]int, [][2]string, [][2]string, error) {
	parent := make([]int, len(l.all))
	for i := range parent {
		parent[i] = i
	}
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	var pairs, shared [][2]string
	if l.last != nil {
		pairs = l.kept(l.last.pairs)
		for _, pair := range pairs {
			parent[root(l.index[pair[1]])] = root(l.index[pair[0]])
		}
		shared = l.kept(l.last.shared)
	}
	for i := range l.all {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		if len(picks[i]) == 0 {
			continue
		}
		source := l.all[i]
		sourceData, ok := l.held.get(ctx, source.InstanceID, source.Hash, source.SavePath)
		if !ok {
			continue
		}
		for _, p := range picks[i] {
			candidateData, ok := l.held.get(ctx, p.entry.instanceID, p.entry.hash, p.entry.savePath)
			if !ok {
				continue
			}
			c := l.places[p.entry.key()]
			pair := [2]string{l.keys[i], l.keys[c]}
			switch {
			case slices.Equal(sourceData.data.sizes, candidateData.data.sizes) &&
				localMatchTier(p.entry.instanceID == source.InstanceID, p.nameOrRelease, sourceData.data, candidateData.data, nil) != "":
				parent[root(c)] = root(i)
				pairs = append(pairs, pair)
			case sharesFiles(sourceData.files, candidateData.files):
				shared = append(shared, pair)
			}
		}
	}
	for i := range parent {
		parent[i] = root(i)
	}
	return parent, pairs, shared, nil
}

// kept returns the pairs of last whose torrents did not change and whose
// lists the call did not fail to read.
func (l *copyGroupSearch) kept(last [][2]string) [][2]string {
	var kept [][2]string
	for _, pair := range last {
		a, okA := l.index[pair[0]]
		b, okB := l.index[pair[1]]
		if okA && okB && !l.changed[a] && !l.changed[b] && !l.unread(a) && !l.unread(b) {
			kept = append(kept, pair)
		}
	}
	return kept
}

// sharesFiles reports whether two lists have a wanted file of the same size.
func sharesFiles(a, b qbt.TorrentFiles) bool {
	sizes := make(map[int64]struct{}, len(a))
	for _, f := range a {
		if f.Priority != 0 {
			sizes[f.Size] = struct{}{}
		}
	}
	return slices.ContainsFunc(b, func(f qbt.TorrentFile) bool {
		_, ok := sizes[f.Size]
		return ok && f.Priority != 0
	})
}

func copyGroupsOf(all []qbittorrent.CrossInstanceTorrentView, roots []int, key []byte) []CopyGroup {
	members := make(map[int][]int)
	for i, r := range roots {
		members[r] = append(members[r], i)
	}
	var groups []CopyGroup
	for i := range all {
		group := members[i]
		if len(group) < 2 {
			continue
		}
		rep := all[slices.MinFunc(group, func(a, b int) int {
			return qbittorrent.CompareRepresentative(all[a].InstanceID, all[a].Torrent, all[b].InstanceID, all[b].Torrent)
		})]
		copyGroup := CopyGroup{ID: copyGroupID(key, rep.InstanceID, rep.Hash)}
		for _, member := range group {
			copyGroup.Members = append(copyGroup.Members, qbittorrent.CopyGroupMember{InstanceID: all[member].InstanceID, Hash: normalizeHash(all[member].Hash)})
		}
		groups = append(groups, copyGroup)
	}
	return groups
}

func copyGroupID(key []byte, instanceID int, hash string) uint64 {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(cacheKeyForTorrentFiles(instanceID, hash)))
	return binary.BigEndian.Uint64(mac.Sum(nil))
}

// sharedParts splits the data of each set of torrents that the Copy groups
// of roots and the pairs of shared link, when shared holds a pair of the set,
// into the parts that the same torrents hold. Inside a set, files of equal
// size hold the same data. Only the torrents of one instance share a part.
func sharedParts(all []qbittorrent.CrossInstanceTorrentView, roots []int, index map[string]int, shared [][2]string, sizes map[string][]int64) map[int]map[string][]qbittorrent.SharedPart {
	if len(shared) == 0 {
		return nil
	}
	parent := slices.Clone(roots)
	root := func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, pair := range shared {
		parent[root(index[pair[1]])] = root(index[pair[0]])
	}
	split := make(map[int]bool, len(shared))
	for _, pair := range shared {
		split[root(index[pair[0]])] = true
	}
	type setOnInstance struct{ root, instanceID int }
	members := map[setOnInstance][]int{}
	var order []setOnInstance
	for i := range all {
		key := setOnInstance{root(i), all[i].InstanceID}
		if !split[key.root] {
			continue
		}
		if _, ok := members[key]; !ok {
			order = append(order, key)
		}
		members[key] = append(members[key], i)
	}

	parts := map[int]map[string][]qbittorrent.SharedPart{}
	var nextID uint64
	for _, key := range order {
		set := members[key]
		if len(set) < 2 {
			continue
		}
		lists := make([][]int64, len(set))
		for n, i := range set {
			lists[n] = sizes[cacheKeyForTorrentFiles(all[i].InstanceID, all[i].Hash)]
		}
		held, partSizes := splitIntoParts(lists)
		if parts[key.instanceID] == nil {
			parts[key.instanceID] = map[string][]qbittorrent.SharedPart{}
		}
		for n, i := range set {
			list := make([]qbittorrent.SharedPart, len(held[n]))
			for k, part := range held[n] {
				list[k] = qbittorrent.SharedPart{ID: nextID + uint64(part), Size: partSizes[part]}
			}
			parts[key.instanceID][normalizeHash(all[i].Hash)] = list
		}
		nextID += uint64(len(partSizes))
	}
	return parts
}

// splitIntoParts groups the files of size lists by the lists that hold them.
// It returns the parts of each list and the size of each part.
func splitIntoParts(lists [][]int64) ([][]int, []int64) {
	counts := make([]map[int64]int, len(lists))
	var sizes []int64
	for n, list := range lists {
		counts[n] = make(map[int64]int, len(list))
		for _, size := range list {
			counts[n][size]++
		}
		sizes = append(sizes, list...)
	}
	slices.Sort(sizes)
	split := partSplit{held: make([][]int, len(lists)), partOf: map[string]int{}}
	holders := make([]byte, len(lists))
	for _, size := range slices.Compact(sizes) {
		most := 0
		for n := range lists {
			most = max(most, counts[n][size])
		}
		// The k-th file of this size is one file in each list that holds more than k.
		for k := range most {
			for n := range lists {
				holders[n] = '0'
				if counts[n][size] > k {
					holders[n] = '1'
				}
			}
			split.add(holders, size)
		}
	}
	return split.held, split.sizes
}

// partSplit holds the parts that splitIntoParts found, by the lists that hold them.
type partSplit struct {
	held   [][]int
	sizes  []int64
	partOf map[string]int
}

func (p *partSplit) add(holders []byte, size int64) {
	part, ok := p.partOf[string(holders)]
	if !ok {
		part = len(p.sizes)
		p.partOf[string(holders)] = part
		p.sizes = append(p.sizes, 0)
		for n, holder := range holders {
			if holder == '1' {
				p.held[n] = append(p.held[n], part)
			}
		}
	}
	p.sizes[part] += size
}
