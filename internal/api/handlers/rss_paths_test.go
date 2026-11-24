// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	qbt "github.com/autobrr/go-qbittorrent"
)

// regression: #4474
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
		// regression: #4474
		{"to the root under its own name", `Folder\` + feed, rssStandIn([]byte(feed)), feed, false},
		// regression: #4474
		{"to a folder under its own name", feed, `Archive\` + rssStandIn([]byte(feed)), `Archive\` + feed, false},
		// regression: #4474
		{"to a folder in the short form", feed, rssStandIn([]byte(folder)) + `\` + rssStandIn([]byte(feed)), folder + `\` + feed, false},
		// regression: #4474
		{"to a folder in a folder in the short form", feed, rssStandIn([]byte(folder)) + `\` + rssStandIn([]byte(subfolder)) + `\` + rssStandIn([]byte(feed)), folder + `\` + subfolder + `\` + feed, false},
		// regression: #4474
		{"to a short form in a missing folder", feed, `Missing\` + rssStandIn([]byte(subfolder)) + `\` + rssStandIn([]byte(feed)), "", true},
		// regression: #4474
		{"a real name shaped like a short form", `Folder\#0123abcd`, `Archive\#0123abcd`, `Archive\#0123abcd`, false},
		// regression: #4474
		{"a new plain name", feed, `Archive\Renamed`, `Archive\Renamed`, false},
		// regression: #4474
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

// regression: #4474
func TestRestoreRSSArticleID(t *testing.T) {
	const guid = "https://tracker.example/dl/1?guid=made-up-key"
	feed := []byte(`{"articles":[{"id":"#0123abcd"},{"id":"` + guid + `"}]}`)
	for _, tt := range []struct {
		id    string
		want  string
		fails bool
	}{
		// regression: #4474
		{"#0123abcd", "#0123abcd", false},
		// regression: #4474
		{rssStandIn([]byte(guid)), guid, false},
		// regression: #4474
		{"#89abcdef", "", true},
	} {
		id := tt.id
		err := restoreRSSArticleID(feed, &id)
		if tt.fails {
			_, noMatch := errors.AsType[rssNoMatchError](err)
			assert.True(t, noMatch, tt.id)
			continue
		}
		require.NoError(t, err, tt.id)
		assert.Equal(t, tt.want, id, tt.id)
	}
}
