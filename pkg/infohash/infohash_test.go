// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package infohash

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	v1Lower = "0123456789abcdef0123456789abcdef01234567"
	v2Lower = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
)

func TestParse(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{name: "v1 lower", input: v1Lower, want: v1Lower},
		{name: "v2 mixed with spaces", input: " 0123456789abcdef0123456789ABCDEF0123456789abcdef0123456789AbCdEf ", want: v2Lower},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := Parse(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.want, h.String())
			assert.Equal(t, strings.ToUpper(tt.want), h.Upper())
			assert.Equal(t, len(tt.want) == 64, h.IsV2())
			assert.False(t, h.IsZero())
		})
	}
}

func TestParseRejects(t *testing.T) {
	tests := []struct {
		name  string
		input string
	}{
		{name: "63 characters", input: v2Lower[:63]},
		{name: "non-hex v1", input: "g" + v1Lower[1:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h, err := Parse(tt.input)
			require.ErrorIs(t, err, ErrInvalid)
			assert.True(t, h.IsZero())
		})
	}
}

func TestTorrentID(t *testing.T) {
	v1 := mustParse(t, v1Lower)
	assert.Equal(t, v1, v1.TorrentID())

	v2 := mustParse(t, strings.ToUpper(v2Lower))
	assert.Equal(t, v2Lower[:40], v2.TorrentID().String())
	assert.False(t, v2.TorrentID().IsV2())
}

func TestParseOfALowercaseOrInvalidValueAllocatesNothing(t *testing.T) {
	for _, input := range []string{v2Lower, ""} {
		allocs := testing.AllocsPerRun(100, func() { _, _ = Parse(input) })
		assert.Zero(t, allocs, input)
	}
}

func TestParseAllKeepsAZeroHashForEachInvalidValue(t *testing.T) {
	got := ParseAll([]string{"", strings.ToUpper(v1Lower), "not a hash", v2Lower})
	assert.Equal(t, []Hash{{}, mustParse(t, v1Lower), {}, mustParse(t, v2Lower)}, got)
}

func FuzzParseRoundTrip(f *testing.F) {
	f.Add(v1Lower)
	f.Add(strings.ToUpper(v2Lower))
	f.Add(" abc ")
	f.Fuzz(func(t *testing.T, input string) {
		h, err := Parse(input)
		if err != nil {
			return
		}
		again, err := Parse(h.String())
		require.NoError(t, err)
		assert.Equal(t, h, again)
		upper, err := Parse(h.Upper())
		require.NoError(t, err)
		assert.Equal(t, h, upper)
		assert.Equal(t, strings.ToLower(strings.TrimSpace(input)), h.String())
	})
}

func mustParse(t *testing.T, s string) Hash {
	t.Helper()
	h, err := Parse(s)
	require.NoError(t, err)
	return h
}

func TestIs(t *testing.T) {
	h := mustParse(t, v1Lower)
	assert.True(t, h.Is(" "+strings.ToUpper(v1Lower)+" "))
	assert.False(t, h.Is(v1Lower[:39]+"8"))
	assert.False(t, h.Is(v1Lower+"00"))
	assert.False(t, Hash{}.Is(""))
}
