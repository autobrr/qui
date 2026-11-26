// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"slices"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

type workItemSearch int

const (
	itemSeedsAlready workItemSearch = iota + 1
	itemNoIndexers
	itemSearchFailed
	itemSearchCanceled
	itemSearched
)

type workItemReport struct {
	files      []string
	episode    bool
	search     workItemSearch
	requested  []int
	answered   []int
	candidates []candidateReport
}

func (r workItemReport) searchCovered() bool {
	response := jackett.SearchResponse{RequestedIndexerIDs: r.requested, CoveredIndexerIDs: r.answered}
	return r.search == itemSearched && len(r.requested) > 0 && response.FullyCovered()
}

type candidateEvent int

const (
	candidateDownloadFailed candidateEvent = iota + 1
	candidateRejected
	candidateRefused
	candidateAddRan
)

type candidateReport struct {
	event            candidateEvent
	class            crossseed.ResultClass
	routeUnavailable bool
	settleQueueFull  bool
	matchedFiles     []string
	hash             string
	indexerID        int
}

func (c candidateReport) isMatch() bool {
	return c.event == candidateAddRan || c.inQBittorrent()
}

func (c candidateReport) inQBittorrent() bool {
	return c.event == candidateRefused && c.class == crossseed.ClassExists
}

func (c candidateReport) added() bool {
	return c.event == candidateAddRan && c.class == crossseed.ClassAdded && !c.settleQueueFull
}

func (c candidateReport) waitsForRoute() bool {
	return c.event == candidateRefused && c.class == crossseed.ClassFailed && c.routeUnavailable
}

type searcheeFile struct {
	path  string
	video bool
	final bool
}

func decideSearcheeOutcome(files []searcheeFile, enabledIndexerIDs []int, canceled bool, items []workItemReport) map[string]dirScanFileUpdate {
	j := outcomeJoin{best: make(map[string]rankedUpdate), video: make(map[string]bool, len(files)), unfinished: canceled}
	for _, f := range files {
		j.video[f.path] = f.video
	}
	for _, item := range items {
		j.add(item)
	}

	for _, f := range files {
		switch {
		case j.unfinished:
			j.offer(f.path, false, dirScanFileUpdate{status: models.DirScanFileStatusPending})
		case j.finished && !f.video:
			j.offer(f.path, false, dirScanFileUpdate{status: models.DirScanFileStatusNoMatch})
		}
	}

	decisions := make(map[string]dirScanFileUpdate)
	for _, f := range files {
		d, ok := j.best[f.path]
		if !ok || d.update.status == models.DirScanFileStatusPending {
			continue
		}
		if f.final && statusRank(d.update.status) < statusRank(models.DirScanFileStatusInQBittorrent) {
			continue
		}
		if d.update.status == models.DirScanFileStatusNoMatch {
			d.update.searchedIndexerIDs = enabledIndexerIDs
		}
		decisions[f.path] = d.update
	}
	return decisions
}

func statusRank(status models.DirScanFileStatus) int {
	switch status {
	case models.DirScanFileStatusMatched:
		return 5
	case models.DirScanFileStatusInQBittorrent:
		return 4
	case models.DirScanFileStatusAlreadySeeding:
		return 3
	case models.DirScanFileStatusError:
		return 2
	case models.DirScanFileStatusPending:
		return 1
	default:
		return 0
	}
}

type rankedUpdate struct {
	update  dirScanFileUpdate
	episode bool
}

type outcomeJoin struct {
	best       map[string]rankedUpdate
	video      map[string]bool
	finished   bool
	unfinished bool
}

func (j *outcomeJoin) offer(path string, episode bool, update dirScanFileUpdate) {
	cur, ok := j.best[path]
	rank := statusRank(update.status)
	if ok && (rank < statusRank(cur.update.status) || rank == statusRank(cur.update.status) && (cur.episode || !episode)) {
		return
	}
	j.best[path] = rankedUpdate{update: update, episode: episode}
}

func (j *outcomeJoin) offerAll(item workItemReport, status models.DirScanFileStatus) {
	for _, path := range item.files {
		j.offer(path, item.episode, dirScanFileUpdate{status: status})
	}
}

func (j *outcomeJoin) add(item workItemReport) {
	switch item.search {
	case itemSeedsAlready:
		j.offerAll(item, models.DirScanFileStatusAlreadySeeding)
	case itemSearchFailed:
		j.unfinished = true
		j.offerAll(item, models.DirScanFileStatusError)
	case itemSearched:
		j.addSearched(item)
	case itemNoIndexers, itemSearchCanceled:
	}
}

func (j *outcomeJoin) addSearched(item workItemReport) {
	switch {
	case slices.ContainsFunc(item.candidates, candidateReport.waitsForRoute):
		for _, path := range item.files {
			if j.video[path] {
				j.offer(path, item.episode, dirScanFileUpdate{status: models.DirScanFileStatusPending})
			}
		}
	case item.searchCovered():
		j.finished = true
		j.offerAll(item, models.DirScanFileStatusNoMatch)
	}

	if !slices.ContainsFunc(item.candidates, candidateReport.isMatch) {
		return
	}
	primary := slices.IndexFunc(item.candidates, candidateReport.added)
	status := models.DirScanFileStatusMatched
	if primary < 0 {
		primary = slices.IndexFunc(item.candidates, candidateReport.inQBittorrent)
		status = models.DirScanFileStatusInQBittorrent
	}
	if primary < 0 {
		j.unfinished = true
		j.offerAll(item, models.DirScanFileStatusError)
		return
	}

	j.finished = true
	match := item.candidates[primary]
	update := dirScanFileUpdate{status: status, matchedTorrentHash: match.hash}
	if match.indexerID > 0 {
		update.matchedIndexerID = new(match.indexerID)
	}
	for _, path := range match.matchedFiles {
		j.offer(path, item.episode, update)
	}
}
