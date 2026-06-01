// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package qbittorrent

import (
	"context"
	"errors"
	"fmt"

	qbt "github.com/autobrr/go-qbittorrent"
)

type mainDataReadMode uint8

const (
	mainDataReadCached mainDataReadMode = iota
	mainDataRead
	mainDataReadFresh
)

type serverStateProvider interface {
	GetServerStateUnchecked() qbt.ServerState
}

type mainDataProvider interface {
	GetData() *qbt.MainData
	GetDataUnchecked() *qbt.MainData
}

func (sm *SyncManager) readMainData(ctx context.Context, instanceID int, mode mainDataReadMode) (*Client, *qbt.SyncManager, *qbt.MainData, error) {
	if sm == nil || sm.clientPool == nil {
		return nil, nil, nil, errors.New("client pool unavailable")
	}

	var (
		client      *Client
		syncManager *qbt.SyncManager
		err         error
	)

	switch mode {
	case mainDataReadCached:
		client, err = sm.clientPool.GetClientOffline(ctx, instanceID)
		if err != nil {
			if errors.Is(err, ErrClientNotFound) {
				return nil, nil, nil, nil
			}
			return nil, nil, nil, err
		}
		if client == nil {
			return nil, nil, nil, nil
		}
		syncManager = client.GetSyncManager()
		if syncManager == nil {
			return client, nil, nil, nil
		}
	case mainDataRead:
		client, syncManager, err = sm.getClientAndSyncManager(ctx, instanceID)
		if err != nil {
			return nil, nil, nil, err
		}
	case mainDataReadFresh:
		client, syncManager, err = sm.getClientAndSyncManager(ctx, instanceID)
		if err != nil {
			return nil, nil, nil, err
		}
		if err := syncManager.Sync(ctx); err != nil {
			return client, syncManager, nil, fmt.Errorf("refresh maindata: %w", err)
		}
	default:
		return nil, nil, nil, fmt.Errorf("unknown maindata read mode %d", mode)
	}

	return client, syncManager, resolveMainData(syncManager, mode), nil
}

func (sm *SyncManager) ReadCachedConnectionStatus(ctx context.Context, instanceID int) string {
	_, syncManager, mainData, err := sm.readMainData(ctx, instanceID, mainDataReadCached)
	if err != nil {
		return ""
	}

	state := resolveServerState(syncManager, mainDataServerState(mainData))
	if state == nil {
		return ""
	}

	return state.ConnectionStatus
}

func (sm *SyncManager) HintMainDataRefresh(instanceID int, reason string) {
	if sm == nil || sm.clientPool == nil {
		return
	}

	sm.syncAfterModification(instanceID, nil, reason)
}

func mainDataServerState(data *qbt.MainData) *qbt.ServerState {
	if data == nil || data.ServerState == (qbt.ServerState{}) {
		return nil
	}

	stateCopy := data.ServerState
	return &stateCopy
}

func resolveMainData(provider mainDataProvider, mode mainDataReadMode) *qbt.MainData {
	if provider == nil {
		return nil
	}

	switch mode {
	case mainDataReadCached, mainDataReadFresh:
		return provider.GetDataUnchecked()
	case mainDataRead:
		return provider.GetData()
	default:
		return nil
	}
}

func resolveServerState(provider serverStateProvider, fallback *qbt.ServerState) *qbt.ServerState {
	if fallback != nil && *fallback != (qbt.ServerState{}) {
		stateCopy := *fallback
		return &stateCopy
	}

	if provider == nil {
		return nil
	}

	state := provider.GetServerStateUnchecked()
	if state == (qbt.ServerState{}) {
		return nil
	}

	stateCopy := state
	return &stateCopy
}
