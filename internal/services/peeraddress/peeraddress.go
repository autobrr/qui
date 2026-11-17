// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package peeraddress gives the public peer address of an instance: the
// address at which other BitTorrent clients reach its qBittorrent.
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

// ErrNoAddress means that the user set no public peer address and that
// qBittorrent reports no external address, or a random listen port.
var ErrNoAddress = errors.New("no public peer address")

// Resolver reads the public peer address of an instance.
type Resolver struct {
	instances *models.InstanceStore
	sync      *qbittorrent.SyncManager
}

// New returns a Resolver that reads the instances from instances and asks
// qBittorrent through sync.
func New(instances *models.InstanceStore, sync *qbittorrent.SyncManager) *Resolver {
	return &Resolver{instances: instances, sync: sync}
}

// PublicPeerAddress returns the host:port at which other BitTorrent clients
// reach the qBittorrent of the instance: the address that the user set, or
// else the external address (IPv4 before IPv6) and the listen port that
// qBittorrent reports. It syncs the instance first, so each call sees a new
// external address. Without an address it returns ErrNoAddress.
func (r *Resolver) PublicPeerAddress(ctx context.Context, instanceID int) (string, error) {
	return r.address(ctx, instanceID, true)
}

// LastPublicPeerAddress is PublicPeerAddress from the last sync of the
// instance and the preferences that qui read last. It sends no sync/maindata
// request, and it reads the preferences from qBittorrent only while qui has
// none, so a list of the instances does not wait for qBittorrent. Before the
// first sync it returns ErrNoAddress.
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
