// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

// fakeReply is one canned pass response, keyed by query, year, and indexers.
type fakeReply struct {
	results []jackett.SearchResult
	covered []int
	partial bool
	jobID   uint64
	err     error
}

func replyKey(query string, year int, ids []int) string {
	return fmt.Sprintf("%s|%d|%v", query, year, ids)
}

// wantRequest pins the wire-visible shape of one pass.
type wantRequest struct {
	query       string
	year        int
	indexerIDs  []int
	skipHistory bool
	season      *int
}

// TestGatherSearchResults drives the retry ladder with a recording fake: which
// passes fire, on which indexers, with which request, and how the covered set
// moves. usable accepts by title so no parsing runs.
func TestGatherSearchResults(t *testing.T) {
	const match, junk = "match", "junk"
	hit := func(indexerID int, title string) jackett.SearchResult {
		return jackett.SearchResult{IndexerID: indexerID, Title: title}
	}
	errPass := errors.New("indexer down")

	tests := []struct {
		name          string
		req           jackett.TorznabSearchRequest
		tagSourcedIDs bool
		altTitle      string
		altSeason     *int
		tvYear        int
		idCap         []int
		replies       map[string]fakeReply
		cancelParent  bool
		wantRequests  []wantRequest
		wantCovered   []int
		wantAnswered  bool
		wantTitles    []string
		wantPartial   bool
		wantJobID     uint64
		wantErr       error
		wantErrText   string
	}{
		{
			name: "year set and nothing usable fires the yearless retry once for the whole search",
			req:  jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2}},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2}): {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1, 2}, jobID: 1},
				replyKey("Movie", 0, []int{1, 2}):    {results: []jackett.SearchResult{hit(2, junk)}, covered: []int{2}, jobID: 2},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1, 2}},
				{query: "Movie", year: 0, indexerIDs: []int{1, 2}},
			},
			wantCovered:  []int{2},
			wantAnswered: true,
			wantTitles:   []string{junk, junk},
			// A retry of every indexer reports outcomes under its own job.
			wantJobID: 2,
		},
		{
			name: "year set with one usable hit skips the yearless retry",
			req:  jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2}},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
			},
			wantRequests: []wantRequest{{query: "Movie", year: 2020, indexerIDs: []int{1, 2}}},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name:     "alternate title targets only the unsatisfied indexer with IDs cleared",
			req:      jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1, 2}, IMDbID: "tt1", OmitQueryForIDs: true},
			altTitle: "Alias",
			replies: map[string]fakeReply{
				replyKey("Show", 0, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match), hit(2, junk)}, covered: []int{1, 2}},
				replyKey("Alias", 0, []int{2}):   {results: []jackett.SearchResult{hit(2, match)}, covered: []int{2}},
			},
			// tagSourcedIDs keeps the title passes on an ID primary.
			tagSourcedIDs: true,
			idCap:         nil,
			wantRequests: []wantRequest{
				{query: "Show", indexerIDs: []int{1, 2}},
				{query: "Alias", indexerIDs: []int{2}, skipHistory: true},
			},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{match, junk, match},
		},
		{
			name:      "alternate title pass of a numeral pack searches its season",
			req:       jackett.TorznabSearchRequest{Query: "Kaiju Squad 100 II", IndexerIDs: []int{1, 2}},
			altTitle:  "Kaiju Squad 100",
			altSeason: new(2),
			replies: map[string]fakeReply{
				replyKey("Kaiju Squad 100 II", 0, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
				replyKey("Kaiju Squad 100", 0, []int{2}):       {results: []jackett.SearchResult{hit(2, match)}, covered: []int{2}},
			},
			wantRequests: []wantRequest{
				{query: "Kaiju Squad 100 II", indexerIDs: []int{1, 2}},
				{query: "Kaiju Squad 100", indexerIDs: []int{2}, skipHistory: true, season: new(2)},
			},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{match, match},
		},
		{
			name:     "failed alternate title pass drops its targets from covered and keeps results",
			req:      jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1, 2}},
			altTitle: "Alias",
			replies: map[string]fakeReply{
				replyKey("Show", 0, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
				replyKey("Alias", 0, []int{2}):   {err: errPass},
			},
			wantRequests: []wantRequest{
				{query: "Show", indexerIDs: []int{1, 2}},
				{query: "Alias", indexerIDs: []int{2}, skipHistory: true},
			},
			wantCovered:  []int{1},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name:     "failed pass with the parent context cancelled returns the context error",
			req:      jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1, 2}},
			altTitle: "Alias",
			replies: map[string]fakeReply{
				replyKey("Show", 0, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
				replyKey("Alias", 0, []int{2}):   {err: errPass},
			},
			cancelParent: true,
			wantRequests: []wantRequest{
				{query: "Show", indexerIDs: []int{1, 2}},
				{query: "Alias", indexerIDs: []int{2}, skipHistory: true},
			},
			wantErr: context.Canceled,
		},
		{
			name:          "tag-sourced IDs rescue the unsatisfied ID-capable indexer by title",
			req:           jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1, 2, 3}, IMDbID: "tt1", OmitQueryForIDs: true},
			tagSourcedIDs: true,
			idCap:         []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Show", 0, []int{1, 2, 3}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2, 3}},
				replyKey("Show", 0, []int{2}):       {covered: nil},
			},
			wantRequests: []wantRequest{
				{query: "Show", indexerIDs: []int{1, 2, 3}},
				{query: "Show", indexerIDs: []int{2}, skipHistory: true},
			},
			// The rescue answered nothing for indexer 2, so it is uncovered.
			wantCovered:  []int{1, 3},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name: "connector pass after the yearless retry searches without the year",
			req:  jackett.TorznabSearchRequest{Query: "Law and Order", Year: 2020, IndexerIDs: []int{1}},
			replies: map[string]fakeReply{
				replyKey("Law and Order", 2020, []int{1}): {covered: []int{1}},
				replyKey("Law and Order", 0, []int{1}):    {covered: []int{1}},
				replyKey("Law & Order", 0, []int{1}):      {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1}, partial: true},
			},
			wantRequests: []wantRequest{
				{query: "Law and Order", year: 2020, indexerIDs: []int{1}},
				{query: "Law and Order", year: 0, indexerIDs: []int{1}},
				{query: "Law & Order", year: 0, indexerIDs: []int{1}, skipHistory: true},
			},
			wantCovered:  []int{1},
			wantAnswered: true,
			wantTitles:   []string{match},
			// A partial retry pass makes the whole search partial.
			wantPartial: true,
		},
		{
			name: "failed yearless retry uncovers every indexer but the primary still answered",
			req:  jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1}},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1}): {covered: []int{1}},
				replyKey("Movie", 0, []int{1}):    {err: errPass},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1}},
				{query: "Movie", year: 0, indexerIDs: []int{1}},
			},
			wantCovered:  []int{},
			wantAnswered: true,
			wantTitles:   []string{},
			wantPartial:  true,
		},
		// regression: #3306
		{
			name:  "yearless retry targets only the indexers that searched by title",
			req:   jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2, 3}, IMDbID: "tt1", OmitQueryForIDs: true},
			idCap: []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2, 3}): {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1, 2, 3}, jobID: 1},
				replyKey("Movie", 0, []int{3}):          {results: []jackett.SearchResult{hit(3, junk)}, covered: []int{3}, jobID: 2},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1, 2, 3}},
				{query: "Movie", year: 0, indexerIDs: []int{3}},
			},
			wantCovered:  []int{1, 2, 3},
			wantAnswered: true,
			wantTitles:   []string{junk, junk},
			// Indexers 1 and 2 have history rows only under the primary job.
			wantJobID: 1,
		},
		// regression: #3306
		{
			name:  "narrowed yearless retry keeps its own job when the primary came from cache",
			req:   jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2, 3}, IMDbID: "tt1", OmitQueryForIDs: true},
			idCap: []int{1, 2},
			replies: map[string]fakeReply{
				// A primary served wholly from cache has no job.
				replyKey("Movie", 2020, []int{1, 2, 3}): {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1, 2, 3}},
				replyKey("Movie", 0, []int{3}):          {results: []jackett.SearchResult{hit(3, junk)}, covered: []int{3}, jobID: 2},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1, 2, 3}},
				{query: "Movie", year: 0, indexerIDs: []int{3}},
			},
			wantCovered:  []int{1, 2, 3},
			wantAnswered: true,
			wantTitles:   []string{junk, junk},
			wantJobID:    2,
		},
		// regression: #3306
		{
			name:  "every indexer searching by ID sends no yearless retry",
			req:   jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2}, IMDbID: "tt1", OmitQueryForIDs: true},
			idCap: []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2}): {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1, 2}},
			},
			wantRequests: []wantRequest{{query: "Movie", year: 2020, indexerIDs: []int{1, 2}}},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{junk},
		},
		// regression: #3306
		{
			name:          "title retry after tag-sourced IDs drops the year when every indexer searched by ID",
			req:           jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2}, IMDbID: "tt1", OmitQueryForIDs: true},
			tagSourcedIDs: true,
			idCap:         []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2}): {covered: []int{1, 2}},
				replyKey("Movie", 0, []int{1, 2}):    {results: []jackett.SearchResult{hit(2, match)}, covered: []int{1, 2}},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1, 2}},
				{query: "Movie", year: 0, indexerIDs: []int{1, 2}, skipHistory: true},
			},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		// regression: #3306
		{
			name:  "failed yearless retry uncovers only the title indexers it targeted",
			req:   jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2, 3}, IMDbID: "tt1", OmitQueryForIDs: true},
			idCap: []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2, 3}): {covered: []int{1, 2, 3}},
				replyKey("Movie", 0, []int{3}):          {err: errPass},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1, 2, 3}},
				{query: "Movie", year: 0, indexerIDs: []int{3}},
			},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{},
			wantPartial:  true,
		},
		{
			name:   "a title-only TV search retries each indexer without a usable result with the source year",
			req:    jackett.TorznabSearchRequest{Query: "Example Show", Season: new(2), IndexerIDs: []int{1, 2}},
			tvYear: 2014,
			replies: map[string]fakeReply{
				replyKey("Example Show", 0, []int{1, 2}):    {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1, 2}},
				replyKey("Example Show", 2014, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match), hit(2, match)}, covered: []int{1, 2}},
			},
			wantRequests: []wantRequest{
				{query: "Example Show", indexerIDs: []int{1, 2}, season: new(2)},
				{query: "Example Show", year: 2014, indexerIDs: []int{1, 2}, skipHistory: true, season: new(2)},
			},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{junk, match, match},
		},
		{
			name:   "a TV search with IDs sends the year only to the indexers that searched by title",
			req:    jackett.TorznabSearchRequest{Query: "Example Show", IndexerIDs: []int{1, 2, 3}, IMDbID: "tt1", OmitQueryForIDs: true},
			tvYear: 2014,
			idCap:  []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Example Show", 0, []int{1, 2, 3}): {covered: []int{1, 2, 3}},
				replyKey("Example Show", 2014, []int{3}):    {results: []jackett.SearchResult{hit(3, match)}, covered: []int{3}},
			},
			wantRequests: []wantRequest{
				{query: "Example Show", indexerIDs: []int{1, 2, 3}},
				{query: "Example Show", year: 2014, indexerIDs: []int{3}, skipHistory: true},
			},
			wantCovered:  []int{1, 2, 3},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name:          "the year retry runs before the title retry after tag-sourced IDs, which sends no year",
			req:           jackett.TorznabSearchRequest{Query: "Example Show", IndexerIDs: []int{1, 2, 3}, IMDbID: "tt1", OmitQueryForIDs: true},
			tagSourcedIDs: true,
			tvYear:        2014,
			idCap:         []int{1, 2},
			replies: map[string]fakeReply{
				replyKey("Example Show", 0, []int{1, 2, 3}): {covered: []int{1, 2, 3}},
				replyKey("Example Show", 2014, []int{3}):    {covered: []int{3}},
				replyKey("Example Show", 0, []int{1, 2}):    {covered: []int{1, 2}},
			},
			wantRequests: []wantRequest{
				{query: "Example Show", indexerIDs: []int{1, 2, 3}},
				{query: "Example Show", year: 2014, indexerIDs: []int{3}, skipHistory: true},
				{query: "Example Show", indexerIDs: []int{1, 2}, skipHistory: true},
			},
			wantCovered:  []int{1, 2, 3},
			wantAnswered: true,
			wantTitles:   []string{},
		},
		{
			name:         "a TV source without a year runs no year retry",
			req:          jackett.TorznabSearchRequest{Query: "Example Show", IndexerIDs: []int{1}},
			replies:      map[string]fakeReply{replyKey("Example Show", 0, []int{1}): {covered: []int{1}}},
			wantRequests: []wantRequest{{query: "Example Show", indexerIDs: []int{1}}},
			wantCovered:  []int{1},
			wantAnswered: true,
			wantTitles:   []string{},
		},
		{
			name:   "an indexer with a usable primary result gets no year retry",
			req:    jackett.TorznabSearchRequest{Query: "Example Show", IndexerIDs: []int{1, 2}},
			tvYear: 2014,
			replies: map[string]fakeReply{
				replyKey("Example Show", 0, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
				replyKey("Example Show", 2014, []int{2}): {covered: []int{2}},
			},
			wantRequests: []wantRequest{
				{query: "Example Show", indexerIDs: []int{1, 2}},
				{query: "Example Show", year: 2014, indexerIDs: []int{2}, skipHistory: true},
			},
			wantCovered:  []int{1, 2},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name:     "the alternate-title and connector retries after the year retry send no year",
			req:      jackett.TorznabSearchRequest{Query: "Law and Order", IndexerIDs: []int{1}},
			tvYear:   2014,
			altTitle: "Alias",
			replies: map[string]fakeReply{
				replyKey("Law and Order", 0, []int{1}):    {covered: []int{1}},
				replyKey("Law and Order", 2014, []int{1}): {covered: []int{1}},
				replyKey("Alias", 0, []int{1}):            {covered: []int{1}},
				replyKey("Law & Order", 0, []int{1}):      {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1}},
			},
			wantRequests: []wantRequest{
				{query: "Law and Order", indexerIDs: []int{1}},
				{query: "Law and Order", year: 2014, indexerIDs: []int{1}, skipHistory: true},
				{query: "Alias", indexerIDs: []int{1}, skipHistory: true},
				{query: "Law & Order", indexerIDs: []int{1}, skipHistory: true},
			},
			wantCovered:  []int{1},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name:   "a failed year retry drops its targets from covered",
			req:    jackett.TorznabSearchRequest{Query: "Example Show", IndexerIDs: []int{1, 2}},
			tvYear: 2014,
			replies: map[string]fakeReply{
				replyKey("Example Show", 0, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
				replyKey("Example Show", 2014, []int{2}): {err: errPass},
			},
			wantRequests: []wantRequest{
				{query: "Example Show", indexerIDs: []int{1, 2}},
				{query: "Example Show", year: 2014, indexerIDs: []int{2}, skipHistory: true},
			},
			wantCovered:  []int{1},
			wantAnswered: true,
			wantTitles:   []string{match},
		},
		{
			name:   "a request that already carries a year runs the yearless retry and no year retry",
			req:    jackett.TorznabSearchRequest{Query: "Example Show", Year: 2014, IndexerIDs: []int{1}},
			tvYear: 2014,
			replies: map[string]fakeReply{
				replyKey("Example Show", 2014, []int{1}): {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1}},
				replyKey("Example Show", 0, []int{1}):    {covered: []int{1}},
			},
			wantRequests: []wantRequest{
				{query: "Example Show", year: 2014, indexerIDs: []int{1}},
				{query: "Example Show", indexerIDs: []int{1}},
			},
			wantCovered:  []int{1},
			wantAnswered: true,
			wantTitles:   []string{junk},
		},
		{
			name:     "a retry answering after a primary nobody answered is an answer",
			req:      jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1}},
			altTitle: "Alias",
			replies: map[string]fakeReply{
				replyKey("Show", 0, []int{1}):  {partial: true},
				replyKey("Alias", 0, []int{1}): {covered: []int{1}},
			},
			wantRequests: []wantRequest{
				{query: "Show", indexerIDs: []int{1}},
				{query: "Alias", indexerIDs: []int{1}, skipHistory: true},
			},
			wantAnswered: true,
			wantTitles:   []string{},
			wantPartial:  true,
		},
		{
			name:         "no indexer answering the primary is not an answer",
			req:          jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1}},
			replies:      map[string]fakeReply{replyKey("Show", 0, []int{1}): {partial: true}},
			wantRequests: []wantRequest{{query: "Show", indexerIDs: []int{1}}},
			wantTitles:   []string{},
			wantPartial:  true,
		},
		{
			name:         "primary error comes back as is",
			req:          jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1}},
			replies:      map[string]fakeReply{replyKey("Show", 0, []int{1}): {err: errPass}},
			wantRequests: []wantRequest{{query: "Show", indexerIDs: []int{1}}},
			wantErr:      errPass,
		},
		{
			name:         "primary deadline error reads as a timeout",
			req:          jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1}},
			replies:      map[string]fakeReply{replyKey("Show", 0, []int{1}): {err: context.DeadlineExceeded}},
			wantRequests: []wantRequest{{query: "Show", indexerIDs: []int{1}}},
			wantErrText:  "search timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []jackett.TorznabSearchRequest
			g := SearchGatherer{
				Search: func(_ context.Context, req *jackett.TorznabSearchRequest) (*jackett.SearchResponse, error) {
					got = append(got, *req)
					reply, ok := tt.replies[replyKey(req.Query, req.Year, req.IndexerIDs)]
					require.True(t, ok, "unexpected pass %q year %d indexers %v", req.Query, req.Year, req.IndexerIDs)
					if reply.err != nil {
						return nil, reply.err
					}
					return &jackett.SearchResponse{Results: reply.results, RequestedIndexerIDs: req.IndexerIDs, CoveredIndexerIDs: reply.covered, Partial: reply.partial, JobID: reply.jobID}, nil
				},
				IDCapIndexers: func(context.Context, *jackett.TorznabSearchRequest) []int { return tt.idCap },
				Usable:        func(r jackett.SearchResult) bool { return r.Title == match },
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelParent {
				cancel()
			}
			req := tt.req
			resp, covered, answered, err := g.Gather(ctx, t.Context(), GatherInput{Req: &req, TagSourcedIDs: tt.tagSourcedIDs, AltTitle: tt.altTitle, AltTitleSeason: tt.altSeason, TVSourceYear: tt.tvYear})

			require.Len(t, got, len(tt.wantRequests))
			for i, want := range tt.wantRequests {
				require.Equal(t, want.query, got[i].Query, "pass %d query", i)
				require.Equal(t, want.year, got[i].Year, "pass %d year", i)
				require.Equal(t, want.indexerIDs, got[i].IndexerIDs, "pass %d indexers", i)
				require.Equal(t, want.skipHistory, got[i].SkipHistory, "pass %d skipHistory", i)
				require.Equal(t, want.season, got[i].Season, "pass %d season", i)
				if want.skipHistory {
					require.Empty(t, got[i].IMDbID, "pass %d keeps IDs", i)
					require.Empty(t, got[i].TVDbID, "pass %d keeps IDs", i)
					require.False(t, got[i].OmitQueryForIDs, "pass %d omits the query", i)
				}
			}

			if tt.wantErr != nil || tt.wantErrText != "" {
				require.Error(t, err)
				if tt.wantErr != nil {
					require.ErrorIs(t, err, tt.wantErr)
				}
				if tt.wantErrText != "" {
					require.EqualError(t, err, tt.wantErrText)
				}
				require.Nil(t, resp)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantCovered, covered)
			require.Equal(t, tt.wantAnswered, answered)
			titles := make([]string, 0, len(resp.Results))
			for _, r := range resp.Results {
				titles = append(titles, r.Title)
			}
			require.Equal(t, tt.wantTitles, titles)
			require.Equal(t, tt.wantPartial, resp.Partial)
			// The decision trace lists every requested indexer, also after a narrowed retry pass.
			require.Equal(t, tt.req.IndexerIDs, resp.RequestedIndexerIDs)
			require.Equal(t, tt.wantJobID, resp.JobID)
		})
	}
}
