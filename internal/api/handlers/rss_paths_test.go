// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"encoding/json/v2"
	"errors"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbt "github.com/autobrr/go-qbittorrent"
)

func TestRestoreRSSDest(t *testing.T) {
	const feed = "https://tracker.example/rss/made-up-key/feed.xml"
	const folder = "https://tracker.example/folder?key=made-up-folder-key"
	const subfolder = "https://tracker.example/sub?key=made-up-sub-key"
	load := func() (qbt.RSSItems, error) {
		return qbt.RSSItems{"Archive": []byte(`{}`), folder: []byte(`{"` + subfolder + `":{}}`)}, nil
	}
	for _, tt := range []struct {
		name   string
		source string
		dest   string
		want   string
		fails  bool
	}{
		{"to the root under its own name", `Folder\` + feed, rssStandIn([]byte(feed)), feed, false},
		{"to a folder under its own name", feed, `Archive\` + rssStandIn([]byte(feed)), `Archive\` + feed, false},
		{"to a folder in the short form", feed, rssStandIn([]byte(folder)) + `\` + rssStandIn([]byte(feed)), folder + `\` + feed, false},
		{"to a folder in a folder in the short form", feed, rssStandIn([]byte(folder)) + `\` + rssStandIn([]byte(subfolder)) + `\` + rssStandIn([]byte(feed)), folder + `\` + subfolder + `\` + feed, false},
		{"to a short form in a missing folder", feed, `Missing\` + rssStandIn([]byte(subfolder)) + `\` + rssStandIn([]byte(feed)), "", true},
		{"a real name shaped like a short form", `Folder\#0123abcd`, `Archive\#0123abcd`, `Archive\#0123abcd`, false},
		{"a new plain name", feed, `Archive\Renamed`, `Archive\Renamed`, false},
		{"the short form of another name", feed, `Archive\#0123abcd`, "", true},
	} {
		dest := tt.dest
		err := restoreRSSDest(load, tt.source, &dest)
		if tt.fails {
			_, noMatch := errors.AsType[rssNoMatchError](err)
			assert.True(t, noMatch, tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.want, dest, tt.name)
	}
}

func TestRestoreRSSArticleID(t *testing.T) {
	const guid = "https://tracker.example/dl/1?guid=made-up-key"
	const twice = "https://tracker.example/dl/2?guid=made-up-key"
	const sameCodeA, sameCodeB = "https://rss.example.invalid/feed/61522.xml", "https://rss.example.invalid/feed/135103.xml"
	const shownAsItself = "https://tracker.example/dl/3?guid=made-up-key"
	feed := []byte(`{"articles":[{"id":"#0123abcd"},{"id":"` + guid + `"},{"id":"` + twice + `"},{"id":"` + twice + `"},` +
		`{"id":"` + sameCodeA + `"},{"id":"` + sameCodeB + `"},{"id":"` + shownAsItself + `"},{"id":"` + rssStandIn([]byte(shownAsItself)) + `"}]}`)
	for _, tt := range []struct {
		id    string
		want  string
		fails string
	}{
		{"#0123abcd", "#0123abcd", ""},
		{rssStandIn([]byte(guid)), guid, ""},
		{"#89abcdef", "", "matches no article"},
		{rssStandIn([]byte(twice)), twice, ""},
		{rssStandIn([]byte(sameCodeA)), "", "more than one"},
		{rssStandIn([]byte(shownAsItself)), "", "more than one"},
	} {
		id := tt.id
		err := restoreRSSArticleID(feed, &id)
		if tt.fails != "" {
			noMatch, ok := errors.AsType[rssNoMatchError](err)
			require.True(t, ok, tt.id)
			assert.Contains(t, noMatch.Error(), tt.fails, tt.id)
			continue
		}
		require.NoError(t, err, tt.id)
		assert.Equal(t, tt.want, id, tt.id)
	}
}

func TestRestoreRSSPathRefusesAShortFormOfTwoNames(t *testing.T) {
	const sameCodeA, sameCodeB = "https://rss.example.invalid/feed/61522.xml", "https://rss.example.invalid/feed/135103.xml"
	const feed = "https://tracker.example/rss/made-up-key/feed.xml"
	const other = "https://tracker.example/rss/made-up-other-key/feed.xml"
	require.Equal(t, rssStandIn([]byte(sameCodeA)), rssStandIn([]byte(sameCodeB)))
	load := func() (qbt.RSSItems, error) {
		return qbt.RSSItems{
			sameCodeA: []byte(`{}`), sameCodeB: []byte(`{}`),
			feed: []byte(`{}`), rssStandIn([]byte(feed)): []byte(`{}`),
			other: []byte(`{}`),
		}, nil
	}
	for _, tt := range []struct {
		name  string
		path  string
		want  string
		fails bool
	}{
		{"two names with one short form", rssStandIn([]byte(sameCodeA)), "", true},
		{"a name that is the short form of another name", rssStandIn([]byte(feed)), "", true},
		{"a short form of one name", rssStandIn([]byte(other)), other, false},
		{"a full name beside its short form", feed, feed, false},
	} {
		path := tt.path
		_, err := restoreRSSPath(load, &path, false, true)
		if tt.fails {
			noMatch, ok := errors.AsType[rssNoMatchError](err)
			require.True(t, ok, tt.name)
			assert.Contains(t, noMatch.Error(), "more than one", tt.name)
			continue
		}
		require.NoError(t, err, tt.name)
		assert.Equal(t, tt.want, path, tt.name)
	}
}

func TestLogRSSPathsHidesNamesThatHoldALink(t *testing.T) {
	const folder = "https://rss.example.invalid/folder?key=made-up-folder-key"
	const feed = folder + "/feed.xml?key=made-up-feed-key"
	shortFolder, shortFeed := rssStandIn([]byte(folder)), rssStandIn([]byte(feed))

	for _, tt := range []struct {
		name   string
		err    string
		fields []string
		want   map[string]string
	}{
		{"a feed named by its URL", "could not remove RSS item; path: " + feed, []string{"path", feed},
			map[string]string{"error": "could not remove RSS item; path: " + shortFeed, "path": shortFeed}},
		{"a move into a folder named by its URL", "itemPath: " + feed + ", destPath: " + folder + `\` + feed, []string{"itemPath", feed, "destPath", folder + `\` + feed},
			map[string]string{"error": "itemPath: " + shortFeed + ", destPath: " + shortFolder + `\` + shortFeed, "itemPath": shortFeed, "destPath": shortFolder + `\` + shortFeed}},
		{"a name that starts with another name", "itemPath: " + feed + ", destPath: " + folder, []string{"destPath", folder, "itemPath", feed},
			map[string]string{"error": "itemPath: " + shortFeed + ", destPath: " + shortFolder, "itemPath": shortFeed, "destPath": shortFolder}},
		{"plain names", `path: Shows\Feed`, []string{"path", `Shows\Feed`},
			map[string]string{"error": `path: Shows\Feed`, "path": `Shows\Feed`}},
		{"an empty path", "status code: 500", []string{"path", ""},
			map[string]string{"error": "status code: 500", "path": ""}},
	} {
		logs := captureLogs(t)
		logRSSPaths(log.Error(), errors.New(tt.err), tt.fields...).Msg("failed")

		assert.NotContains(t, logs.String(), "made-up", tt.name)
		var line map[string]string
		require.NoError(t, json.Unmarshal(logs.Bytes(), &line), tt.name)
		delete(line, "level")
		delete(line, "message")
		assert.Equal(t, tt.want, line, tt.name)
	}
}
