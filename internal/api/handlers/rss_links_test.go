// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// regression: #4474
func TestHoldsRSSLink(t *testing.T) {
	for _, tt := range []struct {
		text string
		link bool
	}{
		// regression: #4474
		{"//tracker.example/dl/1?passkey=made-up-key", true},
		// regression: #4474
		{"Mirror //tracker.example/dl/1?passkey=made-up-key", true},
		// regression: #4474
		{"Mirror\n//tracker.example/dl/1", true},
		// regression: #4474
		{"https://tracker.example/dl/1?passkey=made-up-key", true},
		// regression: #4474
		{"MAGNET:?xt=urn:btih:made-up", true},
		// regression: #4474
		{`<a href="/dl/1">Download</a>`, true},
		// regression: #4474
		{"Show.S01E01.1080p.WEB-DL-GRP", false},
		// regression: #4474
		{"Show // Part 2", false},
		// regression: #4474
		{"AC/DC - Live 1080p/WEB", false},
		// regression: #4474
		{"Ratio 1:2 at 08:00, Size: 4 GiB", false},
		// regression: #4474
		{"/dl/1?passkey=made-up-key", false},
		// regression: #4474
		{"folder//feed", false},
		// regression: #4474
		{":magnet:?xt=urn:btih:made-up", true},
		// regression: #4474
		{"Mirror //", false},
		// regression: #4474
		{"//", false},
		// regression: #4474
		{"//0tracker.example/dl", true},
		// regression: #4474
		{"//9tracker.example/dl", true},
		// regression: #4474
		{"//atracker.example/dl", true},
		// regression: #4474
		{"//ztracker.example/dl", true},
		// regression: #4474
		{"//Atracker.example/dl", true},
		// regression: #4474
		{"//Ztracker.example/dl", true},
		// regression: #4474
		{"///tracker.example/dl", false},
		// regression: #4474
		{"//:tracker.example/dl", false},
		// regression: #4474
		{"//`tracker.example/dl", false},
		// regression: #4474
		{"//{tracker.example/dl", false},
		// regression: #4474
		{"//@tracker.example/dl", false},
		// regression: #4474
		{"//[tracker.example/dl", false},
	} {
		assert.Equal(t, tt.link, holdsRSSLink([]byte(tt.text)), tt.text)
	}
}

// regression: #4474
func TestHideRSSLinksHidesEveryFeedURL(t *testing.T) {
	hidden, err := hideRSSLinks([]byte(`{"Feed":{"url":"tracker.example/rss/made-up-key","title":"Feed"}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"Feed":{"url":"","title":"Feed"}}`, string(hidden))
}

// regression: #4474
func TestHideRSSLinksKeepsNamesAfterOtherValues(t *testing.T) {
	hidden, err := hideRSSLinks([]byte(`{"url":{"Feed":{"url":"https://tracker.example/rss/made-up-key","title":"Feed"}},"link":null,"id":7,"Other":{}}`))
	require.NoError(t, err)
	assert.JSONEq(t, `{"url":{"Feed":{"url":"https://tracker.example","title":"Feed"}},"link":null,"id":7,"Other":{}}`, string(hidden))
}
