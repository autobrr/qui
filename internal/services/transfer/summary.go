// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"

	"github.com/autobrr/qui/internal/models"
)

// Summary is what the Transfer dialog shows before the user starts the
// Transfer.
type Summary struct {
	Preview
	// Free is the free space at the target folder when FreeKnown.
	Free      int64
	FreeKnown bool
	// NoFileAccess is set for a target whose files qui cannot see: a file
	// with other content at the same path can be replaced there.
	NoFileAccess bool
	// Hints is the advice that the log of a Transfer torrent gives.
	Hints []Hint
}

// Summary plans the Transfer as Preview does, with its refusals, and reads
// the free space on the target. It records, claims, and starts nothing. A
// free space that qui cannot read is not known.
func (s *Service) Summary(ctx context.Context, req Request) (*Summary, error) {
	preview, err := s.Preview(ctx, req)
	if err != nil {
		return nil, err
	}
	source, err := s.instance(ctx, req.SourceInstanceID)
	if err != nil {
		return nil, err
	}
	target, err := s.instance(ctx, req.TargetInstanceID)
	if err != nil {
		return nil, err
	}
	targetFiles, err := s.deps.Files.GetBackend(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("read the files of %s: %w", target.Name, err)
	}
	summary := &Summary{Preview: *preview, NoFileAccess: models.FilesystemAccessMode(target) == models.FilesystemModeNone}

	var saves []string
	for _, m := range preview.Members {
		if m.TargetInstanceID == target.ID {
			saves = append(saves, m.Placement.SavePath)
		}
	}
	d := pathsOf(target, targetFiles, saves[0])
	dir := topFolder(d, saves)
	if summary.NoFileAccess {
		summary.Free, summary.FreeKnown, _ = s.clientFreeSpace(ctx, target, d, dir)
	} else {
		// The target folder may not exist yet: the free space of the
		// nearest folder that exists is the free space there.
		for {
			if stat, err := targetFiles.Statfs(ctx, dir); err == nil {
				summary.Free, summary.FreeKnown = stat.BytesAvailable, true
				break
			}
			parent := d.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}

	if preview.Way == WayBitTorrent {
		for _, inst := range s.onTCPAndUTP(ctx, source, target) {
			summary.Hints = append(summary.Hints, Hint{Kind: HintTCPAndUTP, InstanceID: inst.ID})
		}
	}
	return summary, nil
}
