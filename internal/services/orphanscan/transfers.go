// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"
)

type transfers interface {
	HelperCheck(instanceID int) func(hash string) bool
	RunningFolders(ctx context.Context, instanceID int) ([]string, error)
}

func (s *Service) SetTransfers(t transfers) {
	s.transfers = t
}

func (s *Service) leaveOutHelpers(instanceID int, torrents []*qbt.Torrent) []*qbt.Torrent {
	if s.transfers == nil {
		return torrents
	}
	isHelper := s.transfers.HelperCheck(instanceID)
	if isHelper == nil {
		return torrents
	}
	out := func(t *qbt.Torrent) bool { return isHelper(t.Hash) }
	if !slices.ContainsFunc(torrents, out) {
		return torrents
	}
	return slices.DeleteFunc(slices.Clone(torrents), out)
}

func (s *Service) runningTransferFolders(ctx context.Context, instanceID int) ([]string, error) {
	if s.transfers == nil {
		return nil, nil
	}
	folders, err := s.transfers.RunningFolders(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("read the folders of running Transfers: %w", err)
	}
	return folders, nil
}
