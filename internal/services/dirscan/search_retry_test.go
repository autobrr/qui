// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/services/jackett"
)

type passReply struct {
	results []jackett.SearchResult
	covered []int
	err     error
}

func passKey(query string, year int, ids []int) string {
	return fmt.Sprintf("%s|%d|%v", query, year, ids)
}

type scriptedSearcher struct {
	idCap    []int
	replies  map[string]passReply
	requests []jackett.TorznabSearchRequest
}

func (s *scriptedSearcher) SearchWithScope(_ context.Context, req *jackett.TorznabSearchRequest, _ string) error {
	s.requests = append(s.requests, *req)
	key := passKey(req.Query, req.Year, req.IndexerIDs)
	reply, ok := s.replies[key]
	if !ok {
		req.OnAllComplete(nil, fmt.Errorf("unexpected pass %s", key))
		return nil
	}
	if reply.err != nil {
		req.OnAllComplete(nil, reply.err)
		return nil
	}
	req.OnAllComplete(&jackett.SearchResponse{Results: reply.results, RequestedIndexerIDs: req.IndexerIDs, CoveredIndexerIDs: reply.covered}, nil)
	return nil
}

func (s *scriptedSearcher) IndexerIDsWithIDSearchCaps(_ context.Context, req *jackett.TorznabSearchRequest) []int {
	if req.IMDbID == "" && req.TVDbID == "" && req.TMDbID == 0 {
		return nil
	}
	return s.idCap
}

type sentPass struct {
	query      string
	year       int
	indexerIDs []int
	ids        bool
}

func sizedHit(indexerID int, size int64) jackett.SearchResult {
	return jackett.SearchResult{Title: "candidate", IndexerID: indexerID, Size: size}
}

func TestSearchSearcheeRetries(t *testing.T) {
	t.Parallel()

	const (
		minSize = 900
		maxSize = 1100
	)
	errPass := errors.New("indexer timed out")

	tests := []struct {
		name         string
		searchee     string
		files        []string
		arrTVDbID    int
		arrTitles    []string
		idCap        []int
		replies      map[string]passReply
		wantPasses   []sentPass
		wantCovered  []int
		wantResults  int
		wantCanceled bool
	}{
		{
			name:      "a hit in the size band on one tracker leaves the alternate-title retry to the other",
			searchee:  "Example.Show.S01E02.1080p.WEB-DL",
			arrTitles: []string{"Beispiel Serie"},
			replies: map[string]passReply{
				passKey("Example Show", 0, []int{1, 2}): {results: []jackett.SearchResult{sizedHit(1, 1000)}, covered: []int{1, 2}},
				passKey("Beispiel Serie", 0, []int{2}):  {results: []jackett.SearchResult{sizedHit(2, 1000)}, covered: []int{2}},
			},
			wantPasses: []sentPass{
				{query: "Example Show", indexerIDs: []int{1, 2}},
				{query: "Beispiel Serie", indexerIDs: []int{2}},
			},
			wantCovered: []int{1, 2},
			wantResults: 2,
		},
		{
			name:     "a movie with only out-of-band hits retries without the year",
			searchee: "Example.Movie.2024.1080p.WEB-DL",
			replies: map[string]passReply{
				passKey("Example Movie", 2024, []int{1, 2}): {results: []jackett.SearchResult{sizedHit(1, 50)}, covered: []int{1, 2}},
				passKey("Example Movie", 0, []int{1, 2}):    {results: []jackett.SearchResult{sizedHit(1, 1000)}, covered: []int{1, 2}},
			},
			wantPasses: []sentPass{
				{query: "Example Movie", year: 2024, indexerIDs: []int{1, 2}},
				{query: "Example Movie", indexerIDs: []int{1, 2}},
			},
			wantCovered: []int{1, 2},
			wantResults: 2,
		},
		{
			name:     "a show spelled with and gets the ampersand retry",
			searchee: "Law.and.Order.S01E02.1080p.WEB-DL",
			replies: map[string]passReply{
				passKey("Law and Order", 0, []int{1, 2}): {covered: []int{1, 2}},
				passKey("Law & Order", 0, []int{1, 2}):   {results: []jackett.SearchResult{sizedHit(2, 1000)}, covered: []int{1, 2}},
			},
			wantPasses: []sentPass{
				{query: "Law and Order", indexerIDs: []int{1, 2}},
				{query: "Law & Order", indexerIDs: []int{1, 2}},
			},
			wantCovered: []int{1, 2},
			wantResults: 1,
		},
		{
			name:     "a folder-name ID gets a title retry on the trackers that searched by ID",
			searchee: "Example Movie (2024) {imdb-tt1234567} [WEBDL-1080p]",
			idCap:    []int{1},
			replies: map[string]passReply{
				passKey("Example Movie", 2024, []int{1, 2}): {covered: []int{1, 2}},
				passKey("Example Movie", 0, []int{2}):       {covered: []int{2}},
				passKey("Example Movie", 0, []int{1}):       {results: []jackett.SearchResult{sizedHit(1, 1000)}, covered: []int{1}},
			},
			wantPasses: []sentPass{
				{query: "Example Movie", year: 2024, indexerIDs: []int{1, 2}, ids: true},
				{query: "Example Movie", indexerIDs: []int{2}, ids: true},
				{query: "Example Movie", indexerIDs: []int{1}},
			},
			wantCovered: []int{1, 2},
			wantResults: 1,
		},
		{
			name:     "a TV folder with a year gets the year retry",
			searchee: "Example.Show.2014.S02.1080p.WEB-DL",
			replies: map[string]passReply{
				passKey("Example Show", 0, []int{1, 2}):    {results: []jackett.SearchResult{sizedHit(1, 50)}, covered: []int{1, 2}},
				passKey("Example Show", 2014, []int{1, 2}): {results: []jackett.SearchResult{sizedHit(1, 1000)}, covered: []int{1, 2}},
			},
			wantPasses: []sentPass{
				{query: "Example Show", indexerIDs: []int{1, 2}},
				{query: "Example Show", year: 2014, indexerIDs: []int{1, 2}},
			},
			wantCovered: []int{1, 2},
			wantResults: 2,
		},
		{
			name:     "a season folder takes the year retry from its episode files",
			searchee: "Example Show S02",
			files:    []string{"/data/Example.Show.2014.S02E01.1080p.WEB-DL.mkv", "/data/Example.Show.2014.S02E02.1080p.WEB-DL.mkv"},
			replies: map[string]passReply{
				passKey("Example Show", 0, []int{1, 2}):    {covered: []int{1, 2}},
				passKey("Example Show", 2014, []int{1, 2}): {results: []jackett.SearchResult{sizedHit(2, 1000)}, covered: []int{1, 2}},
			},
			wantPasses: []sentPass{
				{query: "Example Show", indexerIDs: []int{1, 2}},
				{query: "Example Show", year: 2014, indexerIDs: []int{1, 2}},
			},
			wantCovered: []int{1, 2},
			wantResults: 1,
		},
		{
			name:      "a failed retry uncovers only the trackers it targeted",
			searchee:  "Example.Show.S01E02.1080p.WEB-DL",
			arrTitles: []string{"Beispiel Serie"},
			replies: map[string]passReply{
				passKey("Example Show", 0, []int{1, 2}): {results: []jackett.SearchResult{sizedHit(1, 1000)}, covered: []int{1, 2}},
				passKey("Beispiel Serie", 0, []int{2}):  {err: errPass},
			},
			wantPasses: []sentPass{
				{query: "Example Show", indexerIDs: []int{1, 2}},
				{query: "Beispiel Serie", indexerIDs: []int{2}},
			},
			wantCovered: []int{1},
			wantResults: 1,
		},
		{
			name:      "an arr ID search runs no title retry",
			searchee:  "Example.Show.S01E02.1080p.WEB-DL",
			arrTVDbID: 12345,
			arrTitles: []string{"Beispiel Serie"},
			idCap:     []int{1, 2},
			replies: map[string]passReply{
				passKey("Example Show", 0, []int{1, 2}): {covered: []int{1, 2}},
			},
			wantPasses:  []sentPass{{query: "Example Show", indexerIDs: []int{1, 2}, ids: true}},
			wantCovered: []int{1, 2},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := zerolog.New(io.Discard)
			searcher := &scriptedSearcher{idCap: tt.idCap, replies: tt.replies}
			parser := NewParser(nil)
			svc := &Service{parser: parser, searcher: NewSearcher(searcher, parser)}
			meta := parser.Parse(tt.searchee)
			tagSourcedIDs := meta.HasExternalIDs()
			if tt.arrTVDbID > 0 {
				meta.SetExternalIDs("", 0, tt.arrTVDbID)
			}

			searchee := &Searchee{Name: tt.searchee}
			for _, file := range tt.files {
				searchee.Files = append(searchee.Files, &ScannedFile{Path: file, Size: 500})
			}
			in := svc.searchInput(meta, []int{1, 2}, nil, tt.arrTitles, tagSourcedIDs)
			in.TVSourceYear = svc.searcheeTVYear(fsops.SlashPaths, searchee, meta)
			resp, covered, err := svc.searchSearchee(t.Context(), searchee, in, minSize, maxSize, nil, &l)
			require.NoError(t, err)
			require.NotNil(t, resp)

			sent := make([]sentPass, 0, len(searcher.requests))
			for _, req := range searcher.requests {
				sent = append(sent, sentPass{query: req.Query, year: req.Year, indexerIDs: req.IndexerIDs, ids: req.IMDbID != "" || req.TVDbID != ""})
			}
			require.Equal(t, tt.wantPasses, sent)
			require.Equal(t, tt.wantCovered, covered)
			require.Len(t, resp.Results, tt.wantResults)
		})
	}
}

func TestSearchSearcheeSkipsAFinishedItem(t *testing.T) {
	t.Parallel()

	l := zerolog.New(io.Discard)
	searcher := &scriptedSearcher{}
	parser := NewParser(nil)
	svc := &Service{parser: parser, searcher: NewSearcher(searcher, parser)}
	in := svc.searchInput(parser.Parse("Example.Show.S01E02.1080p.WEB-DL"), []int{1}, nil, nil, false)

	resp, covered, err := svc.searchSearchee(t.Context(), &Searchee{Name: "Example.Show.S01E02.1080p.WEB-DL"}, in, 0, 0, func() bool { return false }, &l)
	require.NoError(t, err)
	require.Nil(t, resp)
	require.Nil(t, covered)
	require.Empty(t, searcher.requests)
}

func TestSearcheeTVYear(t *testing.T) {
	t.Parallel()

	parser := NewParser(nil)
	svc := &Service{parser: parser}
	episodes := []*ScannedFile{{Path: "/data/Example.Show.2014.S02E01.1080p.WEB-DL.mkv", Size: 500}}
	year := func(name string, files []*ScannedFile) int {
		return svc.searcheeTVYear(fsops.SlashPaths, &Searchee{Name: name, Files: files}, parser.Parse(name))
	}

	require.Equal(t, 2014, year("Example.Show.2014.S02.1080p.WEB-DL", nil))
	require.Equal(t, 2014, year("Example Show S02", episodes))
	require.Zero(t, year("Example Show S02", nil))
	require.Zero(t, year("Example.Movie.2024.1080p.WEB-DL", nil))
}

func TestInSizeBand(t *testing.T) {
	t.Parallel()

	inBand := inSizeBand(900, 1100)
	require.True(t, inBand(sizedHit(1, 1000)))
	require.False(t, inBand(sizedHit(1, 50)))
	require.False(t, inBand(sizedHit(1, 5000)))

	unknownSize := inSizeBand(0, 0)
	require.True(t, unknownSize(sizedHit(1, 50)))
	require.True(t, unknownSize(sizedHit(1, 5000)))
}
