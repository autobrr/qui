// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package peeraddress_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/peeraddress"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

type rig struct {
	stub       *qbtstub.Server
	instances  *models.InstanceStore
	instanceID int
	resolver   *peeraddress.Resolver
}

// newRig starts a stub qBittorrent that listens on port 51413 and reports no
// external address yet, and an instance with no public peer address set.
func newRig(t *testing.T) *rig {
	t.Helper()
	stub := qbtstub.New(t, t.TempDir())
	stub.ListenPort = 51413

	db := testdb.NewMigratedSQLite(t, t.Name())
	instances, err := models.NewInstanceStore(db, make([]byte, 32))
	require.NoError(t, err)
	instance, err := instances.Create(t.Context(), "seedbox", stub.URL, "", "", nil, nil, false, new(false))
	require.NoError(t, err)
	clientPool, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clientPool.Close() })

	return &rig{stub: stub, instances: instances, instanceID: instance.ID,
		resolver: peeraddress.New(instances, qbittorrent.NewSyncManager(clientPool, nil))}
}

func TestPublicPeerAddressFollowsTheExternalAddress(t *testing.T) {
	r := newRig(t)
	r.stub.SetExternalAddress("203.0.113.7", "")

	address, err := r.resolver.PublicPeerAddress(t.Context(), r.instanceID)
	require.NoError(t, err)
	require.Equal(t, "203.0.113.7:51413", address)

	r.stub.SetExternalAddress("198.51.100.23", "")

	address, err = r.resolver.PublicPeerAddress(t.Context(), r.instanceID)
	require.NoError(t, err)
	require.Equal(t, "198.51.100.23:51413", address)
}

func TestPublicPeerAddressPicksTheIPFamily(t *testing.T) {
	for _, tc := range []struct {
		name, v4, v6, want string
	}{
		{name: "IPv6 alone", v6: "2001:db8::7", want: "[2001:db8::7]:51413"},
		{name: "IPv4 before IPv6", v4: "203.0.113.7", v6: "2001:db8::7", want: "203.0.113.7:51413"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRig(t)
			r.stub.SetExternalAddress(tc.v4, tc.v6)

			address, err := r.resolver.PublicPeerAddress(t.Context(), r.instanceID)
			require.NoError(t, err)
			require.Equal(t, tc.want, address)
		})
	}
}

func TestPublicPeerAddressWithoutAnAddress(t *testing.T) {
	t.Run("no external address", func(t *testing.T) {
		r := newRig(t)

		_, err := r.resolver.PublicPeerAddress(t.Context(), r.instanceID)
		require.ErrorIs(t, err, peeraddress.ErrNoAddress)
	})
	t.Run("random listen port", func(t *testing.T) {
		r := newRig(t)
		r.stub.ListenPort = 0
		r.stub.SetExternalAddress("203.0.113.7", "")

		_, err := r.resolver.PublicPeerAddress(t.Context(), r.instanceID)
		require.ErrorIs(t, err, peeraddress.ErrNoAddress)
	})
}

func TestPublicPeerAddressReturnsTheAddressThatTheUserSet(t *testing.T) {
	r := newRig(t)
	r.stub.SetExternalAddress("203.0.113.7", "")
	_, err := r.instances.Update(t.Context(), r.instanceID, "seedbox", r.stub.URL, "", "", nil, nil,
		&models.InstanceUpdateParams{PublicPeerAddress: new("192.0.2.10:6881")})
	require.NoError(t, err)

	address, err := r.resolver.PublicPeerAddress(t.Context(), r.instanceID)
	require.NoError(t, err)
	require.Equal(t, "192.0.2.10:6881", address)
}
