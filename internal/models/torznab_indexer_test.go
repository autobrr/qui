// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseTorznabBackend(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		value   string
		want    TorznabBackend
		wantErr bool
	}{
		{
			name:  "jackett",
			value: "jackett",
			want:  TorznabBackendJackett,
		},
		{
			name:  "prowlarr",
			value: "prowlarr",
			want:  TorznabBackendProwlarr,
		},
		{
			name:  "native",
			value: "native",
			want:  TorznabBackendNative,
		},
		{
			name:  "empty defaults to jackett",
			value: "",
			want:  TorznabBackendJackett,
		},
		{
			name:    "invalid backend",
			value:   "invalid",
			wantErr: true,
		},
		{
			name:    "uppercase not supported",
			value:   "JACKETT",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := ParseTorznabBackend(tt.value)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid torznab backend")
				return
			}

			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestMustTorznabBackend(t *testing.T) {
	t.Parallel()

	t.Run("valid backend", func(t *testing.T) {
		t.Parallel()

		// Should not panic
		backend := MustTorznabBackend("jackett")
		assert.Equal(t, TorznabBackendJackett, backend)
	})

	t.Run("empty defaults to jackett", func(t *testing.T) {
		t.Parallel()

		backend := MustTorznabBackend("")
		assert.Equal(t, TorznabBackendJackett, backend)
	})

	t.Run("invalid backend panics", func(t *testing.T) {
		t.Parallel()

		assert.Panics(t, func() {
			MustTorznabBackend("invalid")
		})
	})
}

func TestTorznabBackendConstants(t *testing.T) {
	t.Parallel()

	// Ensure constants have expected values
	assert.Equal(t, TorznabBackend("jackett"), TorznabBackendJackett)
	assert.Equal(t, TorznabBackend("prowlarr"), TorznabBackendProwlarr)
	assert.Equal(t, TorznabBackend("native"), TorznabBackendNative)
}
