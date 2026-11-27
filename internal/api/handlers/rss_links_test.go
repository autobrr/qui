// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHoldsRSSLink(t *testing.T) {
	for _, tt := range []struct {
		text string
		link bool
	}{
		{"//tracker.example/dl/1?passkey=made-up-key", true},
		{"Mirror //tracker.example/dl/1?passkey=made-up-key", true},
		{"Mirror\n//tracker.example/dl/1", true},
		{"https://tracker.example/dl/1?passkey=made-up-key", true},
		{"MAGNET:?xt=urn:btih:made-up", true},
		{`<a href="/dl/1">Download</a>`, true},
		{"Show.S01E01.1080p.WEB-DL-GRP", false},
		{"Show // Part 2", false},
		{"AC/DC - Live 1080p/WEB", false},
		{"Ratio 1:2 at 08:00, Size: 4 GiB", false},
		{"/dl/1?passkey=made-up-key", false},
		{"folder//feed", false},
		{":magnet:?xt=urn:btih:made-up", true},
		{"Mirror //", false},
		{"//", false},
		{"//0tracker.example/dl", true},
		{"//9tracker.example/dl", true},
		{"//atracker.example/dl", true},
		{"//ztracker.example/dl", true},
		{"//Atracker.example/dl", true},
		{"//Ztracker.example/dl", true},
		{"///tracker.example/dl", false},
		{"//:tracker.example/dl", false},
		{"//`tracker.example/dl", false},
		{"//{tracker.example/dl", false},
		{"//@tracker.example/dl", false},
		{"//[2001:db8::1]/dl", true},
		{"//\\tracker.example/dl", false},
		{"(//tracker.example/dl?passkey=made-up-key)", true},
		{"Mirror: \"//tracker.example/dl\"", true},
		{"https://_tracker.example/dl?passkey=made-up-key", true},
		{"//ümlaut.example/dl?passkey=made-up-key", true},
		{"//\x80tracker.example/dl", true},
		{"//\x7ftracker.example/dl", false},
	} {
		assert.Equal(t, tt.link, holdsRSSLink([]byte(tt.text)), tt.text)
	}
}

func TestHideRSSLinksHidesEveryFeedURL(t *testing.T) {
	hidden, err := hideRSSLinks([]byte(`{"Feed":{"url":"tracker.example/rss/made-up-key","title":"Feed"}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"Feed":{"url":"#4538a94a","title":"Feed"}}`, string(hidden))
}

func TestHideRSSLinksKeepsNamesAfterOtherValues(t *testing.T) {
	hidden, err := hideRSSLinks([]byte(`{"url":{"Feed":{"url":"https://tracker.example/rss/made-up-key","title":"Feed"}},"link":null,"id":7,"Other":{}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"url":{"Feed":{"url":"https://tracker.example#971a0b65","title":"Feed"}},"link":null,"id":7,"Other":{}}`, string(hidden))
}

func TestHideRSSLinksShowsTheShortFormOfAFeedURLOnly(t *testing.T) {
	hidden, err := hideRSSLinks([]byte(`{"Feed":{"url":"https://tracker.example/rss/made-up-key","articles":[{"url":"https://tracker.example/t/1?key=made-up-key"}]}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"Feed":{"url":"https://tracker.example#971a0b65","articles":[{"url":"https://tracker.example"}]}}`, string(hidden))
}

func TestRestoreRSSFeedURLs(t *testing.T) {
	const feed = "https://tracker.example/rss/made-up-key/feed.xml"
	const sameCodeA, sameCodeB = "https://rss.example.invalid/feed/61522.xml", "https://rss.example.invalid/feed/135103.xml"
	const shapedLikeAShortForm = "https://other.example#0123abcd"
	urls := []string{feed, sameCodeA, sameCodeB, feed, shapedLikeAShortForm}
	for _, tt := range []struct {
		name  string
		feed  string
		want  string
		fails string
	}{
		{"a short form of a URL listed twice", rssStandIn([]byte(feed)), feed, ""},
		{"a full URL shaped like a short form", shapedLikeAShortForm, shapedLikeAShortForm, ""},
		{"a full URL of no feed", "https://new.example/feed.xml", "https://new.example/feed.xml", ""},
		{"a short form of no feed", "https://tracker.example#00000000", "", "The feed https://tracker.example#00000000 matches no RSS feed"},
		{"a short form of two feeds", rssStandIn([]byte(sameCodeA)), "", "The feed https://rss.example.invalid#757efa96 matches more than one RSS feed"},
	} {
		feeds := []string{"Plain", tt.feed}
		err := restoreRSSFeedURLs(feeds, urls, "The feed ")
		if tt.fails != "" {
			noMatch, ok := errors.AsType[rssNoMatchError](err)
			require.True(t, ok, tt.name)
			assert.Equal(t, tt.fails, noMatch.Error(), tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
		assert.Equal(t, []string{"Plain", tt.want}, feeds, tt.name)
	}
}
