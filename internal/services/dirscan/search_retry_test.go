// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/jackett"
)

// recordingSearcher answers each search with the next scripted response, and
// records the request it answered so a test can assert what was sent.
type recordingSearcher struct {
	responses []*jackett.SearchResponse
	errs      []error
	requests  []jackett.TorznabSearchRequest
}

func (s *recordingSearcher) SearchWithScope(_ context.Context, req *jackett.TorznabSearchRequest, _ string) error {
	s.requests = append(s.requests, *req)
	pass := len(s.requests) - 1

	if pass < len(s.errs) && s.errs[pass] != nil {
		req.OnAllComplete(nil, s.errs[pass])
		return nil
	}
	var response *jackett.SearchResponse
	if pass < len(s.responses) {
		response = s.responses[pass]
	}
	req.OnAllComplete(response, nil)
	return nil
}

func retryTestService(searcher *recordingSearcher) *Service {
	parser := NewParser(nil)
	return &Service{parser: parser, searcher: NewSearcher(searcher, parser)}
}

func response(size int64, covered ...int) *jackett.SearchResponse {
	resp := &jackett.SearchResponse{RequestedIndexerIDs: []int{1, 2}, CoveredIndexerIDs: covered}
	if size > 0 {
		resp.Results = []jackett.SearchResult{{Title: "candidate", Size: size, IndexerID: 1}}
	}
	return resp
}

func TestSearchWithRetries(t *testing.T) {
	t.Parallel()

	const (
		minSize = 900
		maxSize = 1100
	)

	tests := []struct {
		name          string
		searcheeName  string
		arrTitles     []string
		responses     []*jackett.SearchResponse
		errs          []error
		wantPasses    int
		wantQueries   []string
		wantYears     []int
		wantCovered   []int
		wantResultLen int
	}{
		{
			name:          "a result in the size band stops after the first pass",
			searcheeName:  "Example.Movie.2024.1080p.WEB-DL",
			responses:     []*jackett.SearchResponse{response(1000, 1, 2)},
			wantPasses:    1,
			wantQueries:   []string{"Example Movie"},
			wantYears:     []int{2024},
			wantCovered:   []int{1, 2},
			wantResultLen: 1,
		},
		{
			name:          "only out-of-band results retry without the year",
			searcheeName:  "Example.Movie.2024.1080p.WEB-DL",
			responses:     []*jackett.SearchResponse{response(50, 1, 2), response(1000, 1, 2)},
			wantPasses:    2,
			wantQueries:   []string{"Example Movie", "Example Movie"},
			wantYears:     []int{2024, 0},
			wantCovered:   []int{1, 2},
			wantResultLen: 2,
		},
		{
			name:          "an indexer missing from the retry is no longer covered",
			searcheeName:  "Example.Movie.2024.1080p.WEB-DL",
			responses:     []*jackett.SearchResponse{response(0, 1, 2), response(0, 1)},
			wantPasses:    2,
			wantYears:     []int{2024, 0},
			wantCovered:   []int{1},
			wantResultLen: 0,
		},
		{
			name:          "a failed retry leaves the searchee uncovered",
			searcheeName:  "Example.Movie.2024.1080p.WEB-DL",
			responses:     []*jackett.SearchResponse{response(0, 1, 2)},
			errs:          []error{nil, errors.New("indexer timed out")},
			wantPasses:    2,
			wantYears:     []int{2024, 0},
			wantCovered:   nil,
			wantResultLen: 0,
		},
		{
			name:          "a retry that answers with nothing leaves the searchee uncovered",
			searcheeName:  "Example.Movie.2024.1080p.WEB-DL",
			responses:     []*jackett.SearchResponse{response(0, 1, 2)},
			wantPasses:    2,
			wantYears:     []int{2024, 0},
			wantCovered:   nil,
			wantResultLen: 0,
		},
		{
			name: "an ID-driven search never retries",
			// The alternate title would produce a retry pass on its own, so this
			// row fails if the ID gate stops working.
			searcheeName:  "Example Movie (2024) {imdb-tt1234567}",
			arrTitles:     []string{"Ejemplo Pelicula"},
			responses:     []*jackett.SearchResponse{response(0, 1, 2)},
			wantPasses:    1,
			wantCovered:   []int{1, 2},
			wantResultLen: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := zerolog.New(io.Discard)
			searcher := &recordingSearcher{responses: tt.responses, errs: tt.errs}
			svc := retryTestService(searcher)
			meta := svc.parser.Parse(tt.searcheeName)
			searchee := &Searchee{Name: tt.searcheeName}

			resp, err := svc.searchWithRetries(context.Background(), searchee, meta, []int{1, 2}, nil, tt.arrTitles, minSize, maxSize, &l)
			require.NoError(t, err)
			require.NotNil(t, resp)

			require.Len(t, searcher.requests, tt.wantPasses)
			for i, wantYear := range tt.wantYears {
				require.Equal(t, wantYear, searcher.requests[i].Year, "year of pass %d", i)
			}
			for i, wantQuery := range tt.wantQueries {
				require.Equal(t, wantQuery, searcher.requests[i].Query, "query of pass %d", i)
			}
			require.Equal(t, tt.wantCovered, resp.CoveredIndexerIDs)
			require.Len(t, resp.Results, tt.wantResultLen)
		})
	}
}

func TestRetrySearchPasses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		searchee   string
		arrTitles  []string
		wantLabels []string
		wantQuery  string
	}{
		{
			name:       "a movie with a year drops the year first",
			searchee:   "Example.Movie.2024.1080p.WEB-DL",
			wantLabels: []string{"yearless"},
		},
		{
			name:       "an arr alternate title becomes a second pass",
			searchee:   "Example.Movie.2024.1080p.WEB-DL",
			arrTitles:  []string{"Ejemplo Pelicula"},
			wantLabels: []string{"yearless", "alternate-title"},
			wantQuery:  "Ejemplo Pelicula",
		},
		{
			name:       "an alternate title alone retries a show with no year",
			searchee:   "Example.Show.S01E02.1080p.WEB-DL",
			arrTitles:  []string{"Beispiel Serie"},
			wantLabels: []string{"alternate-title"},
			wantQuery:  "Beispiel Serie",
		},
		{
			name:      "an arr title equal to the primary query is not a retry",
			searchee:  "Example.Show.S01E02.1080p.WEB-DL",
			arrTitles: []string{"Example Show"},
		},
		{
			name:      "an embedded ID skips every retry",
			searchee:  "Example Movie (2024) {imdb-tt1234567}",
			arrTitles: []string{"Ejemplo Pelicula"},
		},
	}

	parser := NewParser(nil)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			passes := retrySearchPasses(parser.Parse(tt.searchee), tt.arrTitles)

			var labels []string
			for _, pass := range passes {
				labels = append(labels, pass.label)
			}
			require.Equal(t, tt.wantLabels, labels)

			if tt.wantQuery != "" {
				require.Equal(t, tt.wantQuery, passes[len(passes)-1].query)
			}
		})
	}
}

func TestHasResultInSizeBand(t *testing.T) {
	t.Parallel()

	outOfBand := []jackett.SearchResult{{Size: 50}, {Size: 5000}}
	inBand := []jackett.SearchResult{{Size: 50}, {Size: 5000}, {Size: 1000}}

	require.False(t, hasResultInSizeBand(outOfBand, 900, 1100))
	require.True(t, hasResultInSizeBand(inBand, 900, 1100))

	// An unknown searchee size means no band, so anything can match.
	require.True(t, hasResultInSizeBand(outOfBand, 0, 0))
}
