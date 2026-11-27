// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets_test

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/secrets"
)

func TestRestore(t *testing.T) {
	t.Parallel()
	const feed = "https://tracker.example/rss/made-up-key/feed.xml"
	const sameCodeA, sameCodeB = "https://rss.example.invalid/feed/61522.xml", "https://rss.example.invalid/feed/135103.xml"
	const shapedLikeAStandIn = "https://other.example#0123abcd"
	errRead := errors.New("qBittorrent cannot be read")
	saved := []string{feed, sameCodeA, sameCodeB, feed, shapedLikeAStandIn}
	hidden := secrets.Audience{}

	for _, tc := range []struct {
		name     string
		audience secrets.Audience
		sent     string
		saved    []string
		readErr  error
		want     string
		wantErr  error
		wantRead int
	}{
		{name: "a reader that sees the kind sends a literal value", audience: secrets.NewAudience(secrets.TrackerPasskey), sent: secrets.StandIn(feed), want: secrets.StandIn(feed)},
		{name: "a value that is not a Stand-in", audience: hidden, sent: "https://new.example/feed.xml", want: "https://new.example/feed.xml"},
		{name: "a read of the saved values that fails", audience: hidden, sent: secrets.StandIn(feed), readErr: errRead, wantErr: errRead, wantRead: 1},
		{name: "the Stand-in of one saved value", audience: hidden, sent: secrets.StandIn(feed), saved: []string{feed, sameCodeA}, want: feed, wantRead: 1},
		{name: "the Stand-in of a value saved twice", audience: hidden, sent: secrets.StandIn(feed), saved: saved, want: feed, wantRead: 1},
		{name: "a saved value shaped like a Stand-in", audience: hidden, sent: shapedLikeAStandIn, saved: saved, want: shapedLikeAStandIn, wantRead: 1},
		{name: "the Stand-in of two saved values", audience: hidden, sent: secrets.StandIn(sameCodeA), saved: saved, wantErr: secrets.ErrAmbiguous, wantRead: 1},
		{name: "a saved name that is the Stand-in of another saved value", audience: hidden, sent: secrets.StandIn(feed), saved: []string{feed, secrets.StandIn(feed)}, wantErr: secrets.ErrAmbiguous, wantRead: 1},
		{name: "a Stand-in of no saved value", audience: hidden, sent: "https://tracker.example#00000000", saved: saved, wantErr: secrets.ErrNoMatch, wantRead: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			reads := 0
			got, err := secrets.Restore(tc.audience, secrets.TrackerPasskey, tc.sent, func() ([]string, error) {
				reads++
				return tc.saved, tc.readErr
			})
			assert.Equal(t, tc.wantRead, reads)
			if tc.wantErr != nil {
				require.ErrorIs(t, err, tc.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
		})
	}
}

func TestRestoreAsksTheKindOfItsCall(t *testing.T) {
	t.Parallel()
	const feed = "https://tracker.example/rss/made-up-key/feed.xml"
	saved := func() ([]string, error) { return []string{feed}, nil }

	got, err := secrets.Restore(secrets.NewAudience(secrets.NotificationToken), secrets.TrackerPasskey, secrets.StandIn(feed), saved)
	require.NoError(t, err)
	assert.Equal(t, feed, got)

	got, err = secrets.Restore(secrets.NewAudience(secrets.NotificationToken), secrets.NotificationToken, secrets.StandIn(feed), saved)
	require.NoError(t, err)
	assert.Equal(t, secrets.StandIn(feed), got)
}
