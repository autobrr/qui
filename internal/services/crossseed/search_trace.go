// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"maps"
	"slices"
	"sort"
	"sync"

	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/redact"
)

// Statuses for SearchIndexerOutcome.Status.
const (
	// searchIndexerStatusSearched means the indexer answered every pass of the search.
	searchIndexerStatusSearched = "searched"
	// searchIndexerStatusNotCovered means the indexer missed at least one pass
	// (rate limited or a retry pass failed) and stays eligible for the next run.
	searchIndexerStatusNotCovered = "not_covered"
	// searchIndexerStatusError means the indexer reported an error during a search pass.
	searchIndexerStatusError = "error"
	// searchIndexerStatusExcluded means content filtering excluded the indexer.
	searchIndexerStatusExcluded = "excluded"
)

// maxTraceRejectedPerReason caps the rejected-candidate detail per reason;
// RejectionCounts still reports the full totals.
const maxTraceRejectedPerReason = 5

// SearchRejectedCandidate is one Torznab result the search classifier rejected.
type SearchRejectedCandidate struct {
	Indexer   string `json:"indexer"`
	IndexerID int    `json:"indexerId"`
	Title     string `json:"title"`
	Size      int64  `json:"size"`
	Reason    string `json:"reason"`
}

// SearchIndexerOutcome reports how one indexer fared during the Torznab passes.
type SearchIndexerOutcome struct {
	IndexerID int    `json:"indexerId"`
	Status    string `json:"status"`
	Detail    string `json:"detail,omitempty"`
	// Candidates is the number of raw results this indexer contributed before filtering.
	Candidates int `json:"candidates"`
}

// SearchDecisionTrace explains why a search accepted or rejected candidates.
// It travels with the response for the manual search dialog and is never persisted.
// Every count covers the Torznab passes only. Gazelle results reach the response
// without passing through this funnel.
type SearchDecisionTrace struct {
	SourceSize          int64                     `json:"sourceSize"`
	TolerancePercent    float64                   `json:"tolerancePercent"`
	TotalResults        int                       `json:"totalResults"`
	SizeFiltered        int                       `json:"sizeFiltered"`
	ReleaseFiltered     int                       `json:"releaseFiltered"`
	LateContentFiltered int                       `json:"lateContentFiltered"`
	DuplicateFiltered   int                       `json:"duplicateFiltered"`
	FinalMatches        int                       `json:"finalMatches"`
	RejectionCounts     map[string]int            `json:"rejectionCounts,omitempty"`
	RejectedCandidates  []SearchRejectedCandidate `json:"rejectedCandidates,omitempty"`
	Indexers            []SearchIndexerOutcome    `json:"indexers,omitempty"`
}

// searchTraceRejections samples rejected candidates for the trace. Counts live
// in the caller's releaseFilterReasons map (plus the size-filter counter);
// runningCount is that count after the current rejection.
type searchTraceRejections struct {
	candidates []SearchRejectedCandidate
}

func (r *searchTraceRejections) add(res jackett.SearchResult, reason string, runningCount int) {
	if runningCount > maxTraceRejectedPerReason {
		return
	}
	r.candidates = append(r.candidates, SearchRejectedCandidate{
		Indexer:   res.Indexer,
		IndexerID: res.IndexerID,
		Title:     res.Title,
		Size:      res.Size,
		Reason:    reason,
	})
}

// searchIndexerErrors captures per-indexer failures from the jackett
// per-indexer completion callback, which fires from scheduler goroutines.
type searchIndexerErrors struct {
	mu   sync.Mutex
	errs map[int]string
}

// record keeps the first error per indexer, redacted for API exposure.
func (e *searchIndexerErrors) record(_ uint64, indexerID int, err error) {
	if err == nil || indexerID <= 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.errs == nil {
		e.errs = make(map[int]string)
	}
	if _, ok := e.errs[indexerID]; !ok {
		e.errs[indexerID] = redact.String(err.Error())
	}
}

func (e *searchIndexerErrors) snapshot() map[int]string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return maps.Clone(e.errs)
}

// buildSearchIndexerOutcomes derives one outcome row per indexer involved in
// the search: the requested set plus content-excluded indexers.
func buildSearchIndexerOutcomes(requested, covered []int, errs map[int]string, excluded map[int]string, candidates map[int]int) []SearchIndexerOutcome {
	if len(requested) == 0 && len(excluded) == 0 {
		return nil
	}

	requestedSet := make(map[int]struct{}, len(requested))
	outcomes := make([]SearchIndexerOutcome, 0, len(requested)+len(excluded))
	for _, id := range requested {
		requestedSet[id] = struct{}{}
		out := SearchIndexerOutcome{IndexerID: id, Candidates: candidates[id]}
		switch {
		case excluded[id] != "":
			out.Status = searchIndexerStatusExcluded
			out.Detail = excluded[id]
		case errs[id] != "":
			out.Status = searchIndexerStatusError
			out.Detail = errs[id]
		case slices.Contains(covered, id):
			out.Status = searchIndexerStatusSearched
		default:
			out.Status = searchIndexerStatusNotCovered
		}
		outcomes = append(outcomes, out)
	}

	for id, reason := range excluded {
		if _, ok := requestedSet[id]; ok {
			continue
		}
		outcomes = append(outcomes, SearchIndexerOutcome{
			IndexerID: id,
			Status:    searchIndexerStatusExcluded,
			Detail:    reason,
		})
	}

	sort.Slice(outcomes, func(i, j int) bool { return outcomes[i].IndexerID < outcomes[j].IndexerID })
	return outcomes
}
