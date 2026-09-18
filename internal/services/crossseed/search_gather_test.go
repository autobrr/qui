// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

// fakeReply is one canned pass response, keyed by query, year, and indexers.
type fakeReply struct {
	results []jackett.SearchResult
	covered []int
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
		idCap         []int
		replies       map[string]fakeReply
		cancelParent  bool
		waitExpired   bool
		wantRequests  []wantRequest
		wantCovered   []int
		wantTitles    []string
		wantErr       error
		wantErrText   string
	}{
		{
			name: "year set and nothing usable fires the yearless retry once for the whole search",
			req:  jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2}},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2}): {results: []jackett.SearchResult{hit(1, junk)}, covered: []int{1, 2}},
				replyKey("Movie", 0, []int{1, 2}):    {results: []jackett.SearchResult{hit(2, junk)}, covered: []int{2}},
			},
			wantRequests: []wantRequest{
				{query: "Movie", year: 2020, indexerIDs: []int{1, 2}},
				{query: "Movie", year: 0, indexerIDs: []int{1, 2}},
			},
			wantCovered: []int{2},
			wantTitles:  []string{junk, junk},
		},
		{
			name: "year set with one usable hit skips the yearless retry",
			req:  jackett.TorznabSearchRequest{Query: "Movie", Year: 2020, IndexerIDs: []int{1, 2}},
			replies: map[string]fakeReply{
				replyKey("Movie", 2020, []int{1, 2}): {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1, 2}},
			},
			wantRequests: []wantRequest{{query: "Movie", year: 2020, indexerIDs: []int{1, 2}}},
			wantCovered:  []int{1, 2},
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
			wantCovered: []int{1, 2},
			wantTitles:  []string{match, junk, match},
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
			wantCovered: []int{1},
			wantTitles:  []string{match},
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
			wantCovered: []int{1, 3},
			wantTitles:  []string{match},
		},
		{
			name: "connector pass after the yearless retry searches without the year",
			req:  jackett.TorznabSearchRequest{Query: "Law and Order", Year: 2020, IndexerIDs: []int{1}},
			replies: map[string]fakeReply{
				replyKey("Law and Order", 2020, []int{1}): {covered: []int{1}},
				replyKey("Law and Order", 0, []int{1}):    {covered: []int{1}},
				replyKey("Law & Order", 0, []int{1}):      {results: []jackett.SearchResult{hit(1, match)}, covered: []int{1}},
			},
			wantRequests: []wantRequest{
				{query: "Law and Order", year: 2020, indexerIDs: []int{1}},
				{query: "Law and Order", year: 0, indexerIDs: []int{1}},
				{query: "Law & Order", year: 0, indexerIDs: []int{1}, skipHistory: true},
			},
			wantCovered: []int{1},
			wantTitles:  []string{match},
		},
		{
			name:         "primary error comes back as is",
			req:          jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1}},
			replies:      map[string]fakeReply{replyKey("Show", 0, []int{1}): {err: errPass}},
			wantRequests: []wantRequest{{query: "Show", indexerIDs: []int{1}}},
			wantErr:      errPass,
		},
		{
			name:         "primary error after the wait deadline reads as a timeout",
			req:          jackett.TorznabSearchRequest{Query: "Show", IndexerIDs: []int{1}},
			replies:      map[string]fakeReply{replyKey("Show", 0, []int{1}): {err: context.DeadlineExceeded}},
			waitExpired:  true,
			wantRequests: []wantRequest{{query: "Show", indexerIDs: []int{1}}},
			wantErrText:  "search timed out",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []jackett.TorznabSearchRequest
			g := searchGatherer{
				search: func(_ context.Context, req *jackett.TorznabSearchRequest) (*jackett.SearchResponse, error) {
					got = append(got, *req)
					reply, ok := tt.replies[replyKey(req.Query, req.Year, req.IndexerIDs)]
					require.True(t, ok, "unexpected pass %q year %d indexers %v", req.Query, req.Year, req.IndexerIDs)
					if reply.err != nil {
						return nil, reply.err
					}
					return &jackett.SearchResponse{Results: reply.results, CoveredIndexerIDs: reply.covered}, nil
				},
				idCapIndexers: func(context.Context, *jackett.TorznabSearchRequest) []int { return tt.idCap },
				usable:        func(r jackett.SearchResult) bool { return r.Title == match },
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancelParent {
				cancel()
			}
			waitCtx := t.Context()
			if tt.waitExpired {
				expired, cancelWait := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancelWait()
				waitCtx = expired
			}

			req := tt.req
			resp, covered, err := g.gather(ctx, waitCtx, gatherInput{req: &req, tagSourcedIDs: tt.tagSourcedIDs, altTitle: tt.altTitle})

			require.Len(t, got, len(tt.wantRequests))
			for i, want := range tt.wantRequests {
				require.Equal(t, want.query, got[i].Query, "pass %d query", i)
				require.Equal(t, want.year, got[i].Year, "pass %d year", i)
				require.Equal(t, want.indexerIDs, got[i].IndexerIDs, "pass %d indexers", i)
				require.Equal(t, want.skipHistory, got[i].SkipHistory, "pass %d skipHistory", i)
				if want.skipHistory {
					require.Empty(t, got[i].IMDbID, "pass %d keeps IDs", i)
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
			titles := make([]string, 0, len(resp.Results))
			for _, r := range resp.Results {
				titles = append(titles, r.Title)
			}
			require.Equal(t, tt.wantTitles, titles)
		})
	}
}
