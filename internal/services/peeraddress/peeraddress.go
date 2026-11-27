// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package peeraddress

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
)

var ErrNoAddress = errors.New("no public peer address")

type Resolver struct {
	instances *models.InstanceStore
	sync      *qbittorrent.SyncManager
}

func New(instances *models.InstanceStore, sync *qbittorrent.SyncManager) *Resolver {
	return &Resolver{instances: instances, sync: sync}
}

func (r *Resolver) PublicPeerAddress(ctx context.Context, instanceID int) (string, error) {
	return r.address(ctx, instanceID, true)
}

func (r *Resolver) LastPublicPeerAddress(ctx context.Context, instanceID int) (string, error) {
	return r.address(ctx, instanceID, false)
}

func (r *Resolver) address(ctx context.Context, instanceID int, sync bool) (string, error) {
	instance, err := r.instances.Get(ctx, instanceID)
	if err != nil {
		return "", err
	}
	if instance.PublicPeerAddress != "" {
		return instance.PublicPeerAddress, nil
	}
	owner, err := r.sync.GetSyncOwner(ctx, instanceID)
	if err != nil {
		return "", err
	}
	if sync {
		if err := owner.Sync(ctx); err != nil {
			return "", fmt.Errorf("refresh maindata: %w", err)
		}
	}
	state := owner.GetServerStateUnchecked()
	host := cmp.Or(state.LastExternalAddressV4, state.LastExternalAddressV6)
	if host == "" {
		return "", ErrNoAddress
	}
	readPreferences := r.sync.GetLastAppPreferences
	if sync {
		readPreferences = r.sync.GetAppPreferences
	}
	prefs, err := readPreferences(ctx, instanceID)
	if err != nil {
		return "", err
	}
	if prefs.ListenPort == 0 {
		return "", ErrNoAddress
	}
	return net.JoinHostPort(host, strconv.Itoa(prefs.ListenPort)), nil
}
