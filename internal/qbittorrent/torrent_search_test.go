// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/assert"
)

func searchedNames(torrents []qbt.Torrent, query string) []string {
	matched := searchTorrents(torrentRefs(torrents), query)
	names := make([]string, 0, len(matched))
	for _, torrent := range matched {
		names = append(names, torrent.Name)
	}
	return names
}

func TestTorrentSearchExcludedTermHidesMatchesOnNameCategoryAndTags(t *testing.T) {
	torrents := []qbt.Torrent{
		{Name: "Some.Movie.2020.1080p.BluRay.x265-GRPA"},
		{Name: "Some.Movie.2020.1080p.BluRay.x264-GRPA"},
		{Name: "Other.Show.S01.1080p.WEB", Category: "x265-encodes"},
		{Name: "Third.Show.S02.720p.HDTV", Tags: "keep, x265"},
	}

	want := []string{"Some.Movie.2020.1080p.BluRay.x264-GRPA"}
	assert.Equal(t, want, searchedNames(torrents, "-x265"))
	assert.Equal(t, want, searchedNames(torrents, "!x265"))
}

func TestTorrentSearchExcludedTermRules(t *testing.T) {
	torrents := []qbt.Torrent{
		{Name: "Some.Movie.2020.mkv", Hash: "0f1e2d3c"},
		{Name: "Some.Movie.2020.1080p.mkv", Hash: "1a2b3c4d"},
		{Name: "Some.Show.S01", Hash: "9a8b7c6d"},
		{Name: "Breaking.Bad.S01.WEB.DL", Hash: "ffac3fff"},
		{Name: "Other.Show.S02.WEB-DL", Hash: "eeeeeeee"},
		{Name: "Third Show S03 WEB DL", Hash: "dddddddd"},
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"wildcard hides a single-file torrent and keeps a folder", "-*.mkv", []string{"Some.Show.S01", "Breaking.Bad.S01.WEB.DL", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
		{"wildcard search minus a word", "*.mkv -1080p", []string{"Some.Movie.2020.mkv"}},
		{"bracket makes a wildcard", "-some.show.s0[1]", []string{"Some.Movie.2020.mkv", "Some.Movie.2020.1080p.mkv", "Breaking.Bad.S01.WEB.DL", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
		{"punctuation-blind words", "-WEB-DL", []string{"Some.Movie.2020.mkv", "Some.Movie.2020.1080p.mkv", "Some.Show.S01"}},
		{"hash is not checked", "-ac3", []string{"Some.Movie.2020.mkv", "Some.Movie.2020.1080p.mkv", "Some.Show.S01", "Breaking.Bad.S01.WEB.DL", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
		{"fuzzy rank is not used", "-bbad", []string{"Some.Movie.2020.mkv", "Some.Movie.2020.1080p.mkv", "Some.Show.S01", "Breaking.Bad.S01.WEB.DL", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
		{"words of one term are not matched apart", "-movie.mkv", []string{"Some.Movie.2020.mkv", "Some.Movie.2020.1080p.mkv", "Some.Show.S01", "Breaking.Bad.S01.WEB.DL", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
		{"a term with only punctuation hides only its text", "-...", []string{"Some.Movie.2020.mkv", "Some.Movie.2020.1080p.mkv", "Some.Show.S01", "Breaking.Bad.S01.WEB.DL", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
		{"each excluded term hides", "show -s02 !s03", []string{"Some.Show.S01"}},
		{"a lone prefix is ignored", "- ! show", []string{"Some.Show.S01", "Other.Show.S02.WEB-DL", "Third Show S03 WEB DL"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, searchedNames(torrents, tt.query))
		})
	}
}

func TestTorrentSearchPlainWordsSearchAsBefore(t *testing.T) {
	torrents := []qbt.Torrent{
		{Name: "Artist - Album (2020) [FLAC]"},
		{Name: "Artist-Album-2020-FLAC"},
		{Name: "Some.Release.this-title-group.mkv"},
		{Name: "Some.Release.1080p.WEB"},
		{Name: "Tom + Jerry"},
		{Name: "Tom.and.Jerry"},
	}

	tests := []struct {
		query string
		want  []string
	}{
		{"Artist - Album", []string{"Artist - Album (2020) [FLAC]", "Artist-Album-2020-FLAC"}},
		{"this-title-group.mkv", []string{"Some.Release.this-title-group.mkv"}},
		{"+1080p", []string{"Some.Release.1080p.WEB"}},
		{"1080p", []string{"Some.Release.1080p.WEB"}},
		{"Tom + Jerry", []string{"Tom + Jerry"}},
	}

	for _, tt := range tests {
		t.Run(tt.query, func(t *testing.T) {
			assert.Equal(t, tt.want, searchedNames(torrents, tt.query))
		})
	}
}

func TestTorrentSearchKeepsAPlainQueryAsTyped(t *testing.T) {
	torrents := []qbt.Torrent{{Name: "Some.Movie.2020", Hash: "ac3ac3ac"}}

	assert.Equal(t, []string{"Some.Movie.2020"}, searchedNames(torrents, "ac3"))
	assert.Empty(t, searchedNames(torrents, "ac3 "))
}

func TestTorrentSearchIncludedWordRules(t *testing.T) {
	torrents := []qbt.Torrent{
		{Name: "Alpha.Beta.Gamma.Delta", Hash: "77770f9a"},
		{Name: "Amélie"},
		{Name: "Abcdefghijkl"},
		{Name: "Nopqrstuvwxyz"},
		{Name: "Some.Film.2019", Category: "movies"},
		{Name: "Other.Film.2018", Category: "WEB-DL"},
	}

	tests := []struct {
		name  string
		query string
		want  []string
	}{
		{"hash with each edge of the hex range", "0f9a", []string{"Alpha.Beta.Gamma.Delta"}},
		{"words in any order", "delta alpha", []string{"Alpha.Beta.Gamma.Delta"}},
		{"fuzzy rank that folds accents", "amelie", []string{"Amélie"}},
		{"fuzzy rank of 9 matches", "acl", []string{"Abcdefghijkl"}},
		{"fuzzy rank of 10 does not match", "nqz", nil},
		{"wildcard on the category alone", "mov*", []string{"Some.Film.2019"}},
		{"punctuation-blind category", "web dl", []string{"Other.Film.2018"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := searchedNames(torrents, tt.query)
			if tt.want == nil {
				assert.Empty(t, got)
				return
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
