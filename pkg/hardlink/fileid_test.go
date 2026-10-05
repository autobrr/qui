// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package hardlink

import (
	"crypto/sha256"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFileIDRoundTripsThroughBytes(t *testing.T) {
	t.Parallel()

	var identifier [16]byte
	for i := range identifier {
		identifier[i] = byte(i + 1)
	}
	tests := []struct {
		name string
		id   FileID
		kind Kind
	}{
		{"unix", UnixFileID(0x0102030405060708, 42), KindUnix},
		{"windows", WindowsFileID(7, identifier), KindWindows},
		{"remote", RemoteFileID([]byte{9, 8, 7}), KindRemote},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			b := tt.id.Bytes()
			require.Len(t, b, 25)
			require.Equal(t, byte(tt.kind), b[0])
			require.Equal(t, tt.id, FileIDFromBytes(b))
			require.False(t, tt.id.IsZero())

			h := sha256.New()
			tt.id.WriteToHash(h)
			want := sha256.Sum256(b)
			require.Equal(t, want[:], h.Sum(nil))
		})
	}
}

func TestFileIDKindsNeverCollide(t *testing.T) {
	t.Parallel()

	// Same raw bytes under different tags are different files.
	unix := UnixFileID(1, 2)
	var identifier [16]byte
	identifier[7] = 2
	windows := WindowsFileID(1, identifier)
	remote := RemoteFileID(unix.Bytes()[1:])

	require.Equal(t, unix.Bytes()[1:], windows.Bytes()[1:])
	require.Equal(t, unix.Bytes()[1:], remote.Bytes()[1:])
	require.NotEqual(t, unix, windows)
	require.NotEqual(t, unix, remote)
	require.True(t, unix.Less(windows))
	require.True(t, windows.Less(remote))
	require.True(t, UnixFileID(1, 1).Less(unix))
	require.False(t, unix.Less(unix))
}

func TestFileIDZero(t *testing.T) {
	t.Parallel()

	require.True(t, (FileID{}).IsZero())
	require.True(t, RemoteFileID(nil).IsZero())
	require.True(t, FileIDFromBytes([]byte{1, 2, 3}).IsZero(), "a pre-tag row decodes to no identity")
	require.True(t, FileIDFromBytes(make([]byte, 16)).IsZero(), "a pre-tag unix row decodes to no identity")
	require.False(t, UnixFileID(0, 0).IsZero(), "a real dev 0 ino 0 is still an identity")
}
