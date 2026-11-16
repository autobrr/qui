// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/services/transfer"
)

// A push copies from qui's own disk to a target over SFTP. A target with
// fsync@openssh.com confirms each flush, and qui's own check proves the
// copy. Without it, a check on the target proves the copy.
func TestTransferPushesToATargetOverSFTP(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fsync bool
		proof string
		adds  int
	}{
		{name: "with fsync", fsync: true, proof: "Proof: qui checked every piece, and homelab confirmed each flush.", adds: 1},
		{name: "without fsync", proof: "Proof: the check on homelab passed all 5 pieces.", adds: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRigOn(t, onDisk, overSFTP)
			r.dstSSH.SetFsync(tc.fsync)
			s := newShow(t, showName)
			r.seed(s)

			record := r.run(r.request(s))

			require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
			require.Equal(t, transfer.WaySFTP, record.Way)
			require.Equal(t, contents(s, showName), tree(t, r.dstDir), "the target has the files, and no temporary file")
			require.Contains(t, logOf(record), "Copying 70 B over SFTP from seedbox to homelab.")
			require.Contains(t, logOf(record), tc.proof)
			require.Equal(t, tc.adds, r.dst.Writes("torrents/add"))
			require.True(t, r.dst.Has(s.hash))
			require.False(t, r.src.Has(s.hash))
			require.Empty(t, tree(t, r.srcDir))
		})
	}
}

// A relay copies between two instances over SFTP, through qui.
func TestTransferRelaysBetweenTwoSFTPSides(t *testing.T) {
	r := newRigOn(t, overSFTP, overSFTP)
	r.dstSSH.SetFsync(true)
	s := newShow(t, showName)
	r.seed(s)

	record := r.run(r.request(s))

	require.Equal(t, transfer.StateDone, record.State, record.FailureReason)
	require.Equal(t, transfer.WaySFTP, record.Way)
	require.Equal(t, contents(s, showName), tree(t, r.dstDir))
	require.Contains(t, logOf(record), "Copying 70 B over SFTP from seedbox to homelab.")
	require.Contains(t, logOf(record), "Proof: qui checked every piece, and homelab confirmed each flush.")
	require.True(t, r.dst.Has(s.hash))
	require.False(t, r.src.Has(s.hash))
	require.Empty(t, tree(t, r.srcDir))
}
