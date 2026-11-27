// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package secrets_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/secrets"
)

func TestOrigin(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, in, want string }{
		{"passkey in the path", "https://tracker.example.invalid/abc123xyz/announce", "https://tracker.example.invalid"},
		{"passkey in the query", "http://tracker.example.invalid/announce?passkey=abc", "http://tracker.example.invalid"},
		{"passkey in the user info", "https://user:abc@tracker.example.invalid/announce", "https://tracker.example.invalid"},
		{"port stays", "udp://tracker.example.invalid:1337/abc/announce", "udp://tracker.example.invalid:1337"},
		{"DHT row stays", "** [DHT] **", "** [DHT] **"},
		{"no scheme can hold a passkey", "tracker.example.invalid/abc/announce", ""},
		{"no host can hold a passkey", "https:///abc/announce", ""},
		{"not a URL can hold a passkey", "https://tracker.example.invalid:abc/announce", ""},
		{"an IPv6 zone keeps its escape once", "http://[fe80::1%25eth0]:8080/abc/announce", "http://[fe80::1%eth0]:8080"},
		{"a second pass empties an IPv6 zone", "http://[fe80::1%eth0]:8080", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tc.want, secrets.Origin(tc.in))
		})
	}
}

func TestScheme(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ in, want string }{
		{"discord://made-up-token@channel", "discord://"},
		{"https://hooks.example.invalid/services/made-up-token", "https://"},
		{"no scheme at all", ""},
		{"://made-up-token", ""},
		{"https://bad host/%zz", ""},
	} {
		assert.Equal(t, tc.want, secrets.Scheme(tc.in), tc.in)
	}
}

func TestStandIn(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "https://tracker.example#971a0b65", secrets.StandIn("https://tracker.example/rss/made-up-key"))
	assert.Equal(t, "#4538a94a", secrets.StandIn("tracker.example/rss/made-up-key"))
	assert.Equal(t, secrets.StandIn("https://rss.example.invalid/feed/61522.xml"), secrets.StandIn("https://rss.example.invalid/feed/135103.xml"))
}

func TestIsStandIn(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		text string
		want bool
	}{
		{"https://tracker.example#971a0b65", true},
		{"#4538a94a", true},
		{"** [DHT] **#4538a94a", true},
		{"https://tracker.example#971A0B65", false},
		{"https://tracker.example#971a0b6", false},
		{"https://tracker.example#971a0b650", false},
		{"https://tracker.example/rss#971a0b65", false},
		{"Feed#971a0b65", false},
		{"https://tracker.example", false},
		{"Plain name", false},
	} {
		assert.Equal(t, tc.want, secrets.IsStandIn(tc.text), tc.text)
	}
	assert.True(t, secrets.IsStandIn(secrets.StandIn("https://tracker.example/dl/1?passkey=made-up-key")))
}

func TestHoldsLink(t *testing.T) {
	t.Parallel()
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
		assert.Equal(t, tt.link, secrets.HoldsLink(tt.text), tt.text)
	}
}

func TestIsPseudoTrackerLabel(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		value string
		want  bool
	}{
		{"** [DHT] **", true},
		{"** [PeX] **", true},
		{"** [LSD] **", true},
		{" lsd ", true},
		{"[dht]", true},
		{"dht.tracker.example", false},
		{"https://tracker.example/announce", false},
		{"", false},
	} {
		assert.Equal(t, tc.want, secrets.IsPseudoTrackerLabel(tc.value), tc.value)
	}
}
