// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseed"
)

const (
	outcomeMovie      = "/data/Example.Movie.2024/Example.Movie.2024.mkv"
	outcomeMovieExtra = "/data/Example.Movie.2024/Example.Movie.2024.srt"
	outcomeEp1        = "/data/Example.Show.S01/Example.Show.S01E01.mkv"
	outcomeEp2        = "/data/Example.Show.S01/Example.Show.S01E02.mkv"
	outcomeShowExtra  = "/data/Example.Show.S01/Example.Show.S01.nfo"
)

var (
	outcomeMovieFiles  = []string{outcomeMovie, outcomeMovieExtra}
	outcomeSeasonFiles = []string{outcomeEp1, outcomeEp2, outcomeShowExtra}
	outcomeIndexers    = []int{1, 2}
)

func fullSearch(files []string, candidates ...candidateReport) workItemReport {
	return workItemReport{files: files, search: itemSearched, requested: outcomeIndexers, answered: outcomeIndexers, candidates: candidates}
}

func partialSearch(files []string, candidates ...candidateReport) workItemReport {
	return workItemReport{files: files, search: itemSearched, requested: outcomeIndexers, answered: []int{1}, candidates: candidates}
}

func episodeSearch(item workItemReport) workItemReport {
	item.episode = true
	return item
}

func addRan(class crossseed.ResultClass, hash string, files ...string) candidateReport {
	return candidateReport{event: candidateAddRan, class: class, matchedFiles: files, hash: hash, indexerID: 1}
}

func existsRefusal(hash string, files ...string) candidateReport {
	return candidateReport{event: candidateRefused, class: crossseed.ClassExists, matchedFiles: files, hash: hash, indexerID: 2}
}

func matchedUpdate(hash string) dirScanFileUpdate {
	return dirScanFileUpdate{status: models.DirScanFileStatusMatched, matchedTorrentHash: hash, matchedIndexerID: new(1)}
}

func inQBittorrentUpdate(hash string) dirScanFileUpdate {
	return dirScanFileUpdate{status: models.DirScanFileStatusInQBittorrent, matchedTorrentHash: hash, matchedIndexerID: new(2)}
}

var (
	noMatchUpdate        = dirScanFileUpdate{status: models.DirScanFileStatusNoMatch, searchedIndexerIDs: outcomeIndexers}
	errorUpdate          = dirScanFileUpdate{status: models.DirScanFileStatusError}
	alreadySeedingUpdate = dirScanFileUpdate{status: models.DirScanFileStatusAlreadySeeding}
)

func TestDecideSearcheeOutcome(t *testing.T) {
	t.Parallel()

	const hashA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const hashB = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"

	tests := []struct {
		name string
		// files defaults to the movie files.
		files    []string
		final    []string
		canceled bool
		items    []workItemReport
		want     map[string]dirScanFileUpdate
	}{
		{
			name:  "a full search with no candidate gives no_match with each enabled indexer",
			items: []workItemReport{fullSearch(outcomeMovieFiles)},
			want:  map[string]dirScanFileUpdate{outcomeMovie: noMatchUpdate, outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "a search that misses an indexer keeps the files pending",
			items: []workItemReport{partialSearch(outcomeMovieFiles)},
		},
		{
			name:  "a search that asked no indexer keeps the files pending",
			items: []workItemReport{{files: outcomeMovieFiles, search: itemSearched}},
		},
		{
			name:  "no available indexer is not a search",
			items: []workItemReport{{files: outcomeMovieFiles, search: itemNoIndexers}},
		},
		{
			name:  "a canceled search keeps the files pending",
			items: []workItemReport{{files: outcomeMovieFiles, search: itemSearchCanceled}},
		},
		{
			name:  "a failed search gives error",
			items: []workItemReport{{files: outcomeMovieFiles, search: itemSearchFailed}},
			want:  map[string]dirScanFileUpdate{outcomeMovie: errorUpdate, outcomeMovieExtra: errorUpdate},
		},
		{
			name:  "a failed search keeps the other files of the Searchee from no_match",
			files: outcomeSeasonFiles,
			items: []workItemReport{fullSearch(outcomeSeasonFiles), episodeSearch(workItemReport{files: []string{outcomeEp1}, search: itemSearchFailed})},
			want:  map[string]dirScanFileUpdate{outcomeEp1: errorUpdate},
		},
		{
			name:     "a run canceled before a Work item started gives no no_match",
			canceled: true,
			items:    []workItemReport{fullSearch(outcomeMovieFiles)},
		},
		{
			name:  "files that seed already get already_seeding",
			items: []workItemReport{{files: outcomeMovieFiles, search: itemSeedsAlready}},
			want:  map[string]dirScanFileUpdate{outcomeMovie: alreadySeedingUpdate, outcomeMovieExtra: alreadySeedingUpdate},
		},
		{
			name:  "already_seeding does not replace a final row",
			final: []string{outcomeMovie},
			items: []workItemReport{{files: outcomeMovieFiles, search: itemSeedsAlready}},
			want:  map[string]dirScanFileUpdate{outcomeMovieExtra: alreadySeedingUpdate},
		},
		{
			name:  "error does not replace a final row",
			final: []string{outcomeMovie},
			items: []workItemReport{{files: outcomeMovieFiles, search: itemSearchFailed}},
			want:  map[string]dirScanFileUpdate{outcomeMovieExtra: errorUpdate},
		},
		{
			name:  "no_match does not replace a final row",
			final: []string{outcomeMovie},
			items: []workItemReport{fullSearch(outcomeMovieFiles)},
			want:  map[string]dirScanFileUpdate{outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "an add gives matched for the matched files, and the extras of the finished Searchee get no_match",
			items: []workItemReport{partialSearch(outcomeMovieFiles, addRan(crossseed.ClassAdded, hashA, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: matchedUpdate(hashA), outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "matched replaces a final row",
			final: []string{outcomeMovie},
			items: []workItemReport{fullSearch(outcomeMovieFiles, addRan(crossseed.ClassAdded, hashA, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: matchedUpdate(hashA), outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "an exists refusal gives in_qbittorrent for the matched files",
			items: []workItemReport{fullSearch(outcomeMovieFiles, existsRefusal(hashA, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: inQBittorrentUpdate(hashA), outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "the first add wins over an exists refusal before it",
			items: []workItemReport{fullSearch(outcomeMovieFiles, existsRefusal(hashA, outcomeMovie), addRan(crossseed.ClassAdded, hashB, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: matchedUpdate(hashB), outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "a failed add beside an add gives matched",
			items: []workItemReport{fullSearch(outcomeMovieFiles, addRan(crossseed.ClassFailed, hashA, outcomeMovie), addRan(crossseed.ClassAdded, hashB, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: matchedUpdate(hashB), outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "a rejected match gives no decision from the candidate",
			items: []workItemReport{fullSearch(outcomeMovieFiles, candidateReport{event: candidateRejected})},
			want:  map[string]dirScanFileUpdate{outcomeMovie: noMatchUpdate, outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "a blocked refusal gives no decision from the candidate",
			items: []workItemReport{fullSearch(outcomeMovieFiles, candidateReport{event: candidateRefused, class: crossseed.ClassSkipped})},
			want:  map[string]dirScanFileUpdate{outcomeMovie: noMatchUpdate, outcomeMovieExtra: noMatchUpdate},
		},
		{
			// Known bug #2736: the next scan never tries the download again.
			name:  "a failed download after a full search gives no_match (#2736)",
			items: []workItemReport{fullSearch(outcomeMovieFiles, candidateReport{event: candidateDownloadFailed})},
			want:  map[string]dirScanFileUpdate{outcomeMovie: noMatchUpdate, outcomeMovieExtra: noMatchUpdate},
		},
		{
			// Known bug #3664: the next scan never tries the match again.
			name:  "a Pre-add check error gives no_match (#3664)",
			items: []workItemReport{fullSearch(outcomeMovieFiles, candidateReport{event: candidateRefused, class: crossseed.ClassFailed})},
			want:  map[string]dirScanFileUpdate{outcomeMovie: noMatchUpdate, outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "an unavailable instance of the cross-seed route keeps the files pending",
			items: []workItemReport{fullSearch(outcomeMovieFiles, candidateReport{event: candidateRefused, class: crossseed.ClassFailed, routeUnavailable: true})},
		},
		{
			name: "a match beside a wait for the route finishes the extras of the Searchee",
			items: []workItemReport{fullSearch(outcomeMovieFiles,
				existsRefusal(hashA, outcomeMovie),
				candidateReport{event: candidateRefused, class: crossseed.ClassFailed, routeUnavailable: true},
			)},
			want: map[string]dirScanFileUpdate{outcomeMovie: inQBittorrentUpdate(hashA), outcomeMovieExtra: noMatchUpdate},
		},
		{
			name:  "an episode that waits for the route stays pending after a full season search",
			files: outcomeSeasonFiles,
			items: []workItemReport{
				fullSearch(outcomeSeasonFiles),
				episodeSearch(fullSearch([]string{outcomeEp1}, candidateReport{event: candidateRefused, class: crossseed.ClassFailed, routeUnavailable: true})),
			},
			want: map[string]dirScanFileUpdate{outcomeEp2: noMatchUpdate, outcomeShowExtra: noMatchUpdate},
		},
		{
			// Known bug #2736: the episode search missed indexer 2, but the season search counts for the episode file.
			name:  "a full season search and an episode search that misses an indexer give no_match for the episode (#2736)",
			files: outcomeSeasonFiles,
			items: []workItemReport{fullSearch(outcomeSeasonFiles), episodeSearch(partialSearch([]string{outcomeEp1}))},
			want:  map[string]dirScanFileUpdate{outcomeEp1: noMatchUpdate, outcomeEp2: noMatchUpdate, outcomeShowExtra: noMatchUpdate},
		},
		{
			name:  "a stranded add is a failed add and gives error",
			items: []workItemReport{fullSearch(outcomeMovieFiles, addRan(crossseed.ClassStranded, hashA, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: errorUpdate, outcomeMovieExtra: errorUpdate},
		},
		{
			name:  "an add with the class skipped is a failed add and gives error",
			items: []workItemReport{fullSearch(outcomeMovieFiles, addRan(crossseed.ClassSkipped, hashA, outcomeMovie))},
			want:  map[string]dirScanFileUpdate{outcomeMovie: errorUpdate, outcomeMovieExtra: errorUpdate},
		},
		{
			name: "an add left paused by a full Settle queue is a failed add and gives error",
			items: []workItemReport{fullSearch(outcomeMovieFiles, candidateReport{
				event: candidateAddRan, class: crossseed.ClassAdded, settleQueueFull: true, matchedFiles: []string{outcomeMovie}, hash: hashA, indexerID: 1,
			})},
			want: map[string]dirScanFileUpdate{outcomeMovie: errorUpdate, outcomeMovieExtra: errorUpdate},
		},
		{
			name:  "a failed add keeps the other files of the Searchee from no_match",
			files: outcomeSeasonFiles,
			items: []workItemReport{fullSearch(outcomeSeasonFiles), episodeSearch(fullSearch([]string{outcomeEp1}, addRan(crossseed.ClassFailed, hashA, outcomeEp1)))},
			want:  map[string]dirScanFileUpdate{outcomeEp1: errorUpdate},
		},
		{
			name:  "an episode error does not replace an in_qbittorrent season",
			files: outcomeSeasonFiles,
			items: []workItemReport{
				fullSearch(outcomeSeasonFiles, existsRefusal(hashA, outcomeEp1, outcomeEp2)),
				episodeSearch(workItemReport{files: []string{outcomeEp1}, search: itemSearchFailed}),
			},
			want: map[string]dirScanFileUpdate{outcomeEp1: inQBittorrentUpdate(hashA), outcomeEp2: inQBittorrentUpdate(hashA)},
		},
		{
			name:  "files that seed already do not replace an in_qbittorrent season",
			files: outcomeSeasonFiles,
			items: []workItemReport{
				fullSearch(outcomeSeasonFiles, existsRefusal(hashA, outcomeEp1, outcomeEp2)),
				episodeSearch(workItemReport{files: []string{outcomeEp1}, search: itemSeedsAlready}),
			},
			want: map[string]dirScanFileUpdate{outcomeEp1: inQBittorrentUpdate(hashA), outcomeEp2: inQBittorrentUpdate(hashA), outcomeShowExtra: noMatchUpdate},
		},
		{
			name:  "files that seed already replace a season error",
			files: outcomeSeasonFiles,
			items: []workItemReport{
				{files: outcomeSeasonFiles, search: itemSearchFailed},
				episodeSearch(workItemReport{files: []string{outcomeEp1}, search: itemSeedsAlready}),
			},
			want: map[string]dirScanFileUpdate{outcomeEp1: alreadySeedingUpdate, outcomeEp2: errorUpdate, outcomeShowExtra: errorUpdate},
		},
		{
			name:  "an episode add replaces a season error",
			files: outcomeSeasonFiles,
			items: []workItemReport{
				{files: outcomeSeasonFiles, search: itemSearchFailed},
				episodeSearch(fullSearch([]string{outcomeEp1}, addRan(crossseed.ClassAdded, hashA, outcomeEp1))),
			},
			want: map[string]dirScanFileUpdate{outcomeEp1: matchedUpdate(hashA), outcomeEp2: errorUpdate, outcomeShowExtra: errorUpdate},
		},
		{
			name:  "an episode in qBittorrent wins over its season in qBittorrent",
			files: outcomeSeasonFiles,
			items: []workItemReport{
				fullSearch(outcomeSeasonFiles, existsRefusal(hashA, outcomeEp1, outcomeEp2)),
				episodeSearch(fullSearch([]string{outcomeEp1}, existsRefusal(hashB, outcomeEp1))),
			},
			want: map[string]dirScanFileUpdate{outcomeEp1: inQBittorrentUpdate(hashB), outcomeEp2: inQBittorrentUpdate(hashA), outcomeShowExtra: noMatchUpdate},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			paths := tt.files
			if paths == nil {
				paths = outcomeMovieFiles
			}
			files := make([]searcheeFile, 0, len(paths))
			for _, path := range paths {
				files = append(files, searcheeFile{path: path, video: isVideoPath(path), final: slices.Contains(tt.final, path)})
			}
			want := tt.want
			if want == nil {
				want = map[string]dirScanFileUpdate{}
			}

			require.Equal(t, want, decideSearcheeOutcome(files, outcomeIndexers, tt.canceled, tt.items))
			reversed := slices.Clone(tt.items)
			slices.Reverse(reversed)
			require.Equal(t, want, decideSearcheeOutcome(files, outcomeIndexers, tt.canceled, reversed), "the order of the Work items changed the result")
		})
	}
}
