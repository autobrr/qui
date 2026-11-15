// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"slices"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
	"github.com/autobrr/qui/internal/services/jackett"
)

// workItemSearch is what happened to one Work item before its candidates.
type workItemSearch int

const (
	itemSeedsAlready workItemSearch = iota + 1
	// itemNoIndexers is not a search.
	itemNoIndexers
	itemSearchFailed
	// itemSearchCanceled is a canceled search, or a Work item that another run finished first.
	itemSearchCanceled
	itemSearched
)

// workItemReport is what the search step, the match step, and the add step
// report for one Work item.
type workItemReport struct {
	files []string
	// episode marks the search for one episode of a season.
	episode bool
	search  workItemSearch
	// requested and answered are the indexers of an itemSearched search.
	requested  []int
	answered   []int
	candidates []candidateReport
}

// searchCovered reports whether an itemSearched search asked at least one
// indexer and every requested indexer answered.
func (r workItemReport) searchCovered() bool {
	response := jackett.SearchResponse{RequestedIndexerIDs: r.requested, CoveredIndexerIDs: r.answered}
	return r.search == itemSearched && len(r.requested) > 0 && response.FullyCovered()
}

// candidateEvent is what happened to one candidate that Dir Scan tried after a search.
type candidateEvent int

const (
	// candidateDownloadFailed is a failed .torrent download or parse.
	candidateDownloadFailed candidateEvent = iota + 1
	candidateRejected
	candidateRefused
	candidateAddRan
)

type candidateReport struct {
	event candidateEvent
	// class is the Result class of a refusal or of an add.
	class crossseed.ResultClass
	// routeUnavailable marks an error refusal that an unavailable instance of
	// the cross-seed route caused.
	routeUnavailable bool
	// settleQueueFull marks an added torrent that stays paused because the
	// Settle queue was full.
	settleQueueFull bool
	// matchedFiles, hash, and indexerID describe the matched torrent of a
	// refusal or an add.
	matchedFiles []string
	hash         string
	indexerID    int
}

// isMatch reports whether the candidate counts as a found match: the torrent
// is in qBittorrent already, or an add ran.
func (c candidateReport) isMatch() bool {
	return c.event == candidateAddRan || c.inQBittorrent()
}

func (c candidateReport) inQBittorrent() bool {
	return c.event == candidateRefused && c.class == crossseed.ClassExists
}

// added reports whether the add counts as added. A torrent that stays paused
// because the Settle queue was full is a failed add in Dir Scan.
func (c candidateReport) added() bool {
	return c.event == candidateAddRan && c.class == crossseed.ClassAdded && !c.settleQueueFull
}

func (c candidateReport) waitsForRoute() bool {
	return c.event == candidateRefused && c.class == crossseed.ClassFailed && c.routeUnavailable
}

// searcheeFile is one file of a Searchee, with what its stored row says now.
type searcheeFile struct {
	path  string
	video bool
	final bool
}

// decideSearcheeOutcome returns the Searchee outcome of one run: the update to
// write for each file, keyed by path. A file without an entry keeps its stored
// row. canceled reports a run that stopped before a Work item started. The
// order of the Work items does not change the result.
func decideSearcheeOutcome(files []searcheeFile, enabledIndexerIDs []int, canceled bool, items []workItemReport) map[string]dirScanFileUpdate {
	j := outcomeJoin{best: make(map[string]rankedUpdate), video: make(map[string]bool, len(files)), unfinished: canceled}
	for _, f := range files {
		j.video[f.path] = f.video
	}
	for _, item := range items {
		j.add(item)
	}

	// A Searchee that is not finished gives no file no_match. A finished one
	// also finishes its non-video extras, which no episode search covers.
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
		// Only a match replaces a final row.
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

// statusRank orders the decisions for one file. Pending leaves the stored row,
// and it outranks no_match.
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
	best  map[string]rankedUpdate
	video map[string]bool
	// finished is set by a full search or a found match.
	finished bool
	// unfinished is set by a failed search, a Work item whose adds all
	// failed, or a canceled run. It wins over finished.
	unfinished bool
}

// offer keeps the update with the higher rank. On a tie, an episode search
// wins over a season search, because it matched the exact file.
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
		// The extras of a finished Searchee still get no_match.
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
	// The first add wins, then the first torrent in qBittorrent.
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
