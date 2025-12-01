// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package domain

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestRedactString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "non-empty string returns redacted",
			input: "secret-password",
			want:  RedactedStr,
		},
		{
			name:  "empty string returns empty",
			input: "",
			want:  "",
		},
		{
			name:  "single character",
			input: "a",
			want:  RedactedStr,
		},
		{
			name:  "whitespace only",
			input: "   ",
			want:  RedactedStr,
		},
		{
			name:  "already redacted string",
			input: RedactedStr,
			want:  RedactedStr,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := RedactString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestIsRedactedString(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  bool
	}{
		{
			name:  "redacted placeholder returns true",
			input: RedactedStr,
			want:  true,
		},
		{
			name:  "empty string returns false",
			input: "",
			want:  false,
		},
		{
			name:  "regular string returns false",
			input: "some-secret",
			want:  false,
		},
		{
			name:  "partial match returns false",
			input: "<redacted",
			want:  false,
		},
		{
			name:  "redacted with extra chars returns false",
			input: RedactedStr + "extra",
			want:  false,
		},
		{
			name:  "similar but different returns false",
			input: "<REDACTED>",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := IsRedactedString(tt.input)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestRedactedStrConstant(t *testing.T) {
	t.Parallel()

	// Ensure the constant has the expected value
	assert.Equal(t, "<redacted>", RedactedStr)
}
