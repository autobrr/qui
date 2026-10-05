// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

func TestSameFile(t *testing.T) {
	t.Parallel()

	boxA := &models.Instance{ID: 1, SSHHost: "box-a", SSHPort: 22}
	boxAAgain := &models.Instance{ID: 2, SSHHost: "box-a", SSHPort: 22}
	boxAOtherPort := &models.Instance{ID: 3, SSHHost: "box-a", SSHPort: 2222}
	boxB := &models.Instance{ID: 4, SSHHost: "box-b", SSHPort: 22}
	local := &models.Instance{ID: 5}

	raw := []byte{1, 2, 3}
	remote := hardlink.RemoteFileID(raw)
	unix := hardlink.UnixFileID(1, 2)
	var identifier [16]byte
	identifier[7] = 2
	windows := hardlink.WindowsFileID(1, identifier)

	tests := []struct {
		name        string
		a, b        hardlink.FileID
		aInst       *models.Instance
		bInst       *models.Instance
		wantMatch   bool
		wantSameKey bool
	}{
		{"unix vs windows, same raw bytes", unix, windows, local, local, false, false},
		{"unix vs remote, same raw bytes", unix, hardlink.RemoteFileID(unix.Bytes()[1:]), local, boxA, false, false},
		{"two local instances share the host", unix, unix, local, &models.Instance{ID: 6}, true, true},
		{"remote, same host and port", remote, remote, boxA, boxAAgain, true, true},
		{"remote, different port", remote, remote, boxA, boxAOtherPort, false, false},
		{"remote, different host", remote, remote, boxA, boxB, false, false},
		{"zero never matches", hardlink.FileID{}, hardlink.FileID{}, local, local, false, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			require.Equal(t, tt.wantMatch, SameFile(tt.a, tt.aInst, tt.b, tt.bInst))
			require.Equal(t, tt.wantSameKey, FileKeyOf(tt.a, tt.aInst) == FileKeyOf(tt.b, tt.bInst))
		})
	}
}
