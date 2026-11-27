// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"context"
	"fmt"
)

type transfers interface {
	RunningFolders(ctx context.Context, instanceID int) ([]string, error)
}

func (s *Service) SetTransfers(t transfers) {
	s.transfers = t
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
