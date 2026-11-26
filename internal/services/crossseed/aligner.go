// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"strings"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/qbittorrent"
)

const alignRenameAttempts = 3

type alignerSync interface {
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	RenameTorrentFile(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
	RenameTorrentFolder(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
}

var _ alignerSync = (*qbittorrent.SyncManager)(nil)

type FileRename struct {
	OldPath string
	NewPath string
}

type AlignmentPlan struct {
	Files                []FileRename
	FolderFrom, FolderTo string
}

type Aligner struct {
	sync       alignerSync
	filesWait  time.Duration
	verifyWait time.Duration
	pollEvery  time.Duration
	retryDelay time.Duration
	timeout    time.Duration
}

func NewAligner(sync alignerSync) *Aligner {
	return &Aligner{
		sync:       sync,
		filesWait:  15 * time.Second,
		verifyWait: 3 * time.Second,
		pollEvery:  250 * time.Millisecond,
		retryDelay: 300 * time.Millisecond,
		timeout:    5 * time.Minute,
	}
}

func (a *Aligner) detach(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
}

func (a *Aligner) WaitForFiles(ctx context.Context, instanceID int, hashes []string) (string, qbt.TorrentFiles, bool) {
	ctx, cancel := a.detach(ctx)
	defer cancel()

	unique := dedupeHashes(hashes...)
	lookup := make([]string, 0, len(unique))
	for _, hash := range unique {
		hash = normalizeHash(hash)
		if len(hash) == 64 {
			hash = hash[:40]
		}
		lookup = append(lookup, hash)
	}
	if len(lookup) == 0 {
		return "", nil, false
	}

	deadline := time.Now().Add(a.filesWait)
	for {
		filesByHash, err := a.sync.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), instanceID, lookup)
		if err == nil {
			for _, hash := range lookup {
				if files := filesByHash[hash]; len(files) > 0 {
					return hash, files, true
				}
			}
		}
		if !time.Now().Before(deadline) || !sleepCtx(ctx, a.pollEvery) {
			return "", nil, false
		}
	}
}

func (a *Aligner) Align(ctx context.Context, instanceID int, hash string, plan AlignmentPlan) bool {
	ctx, cancel := a.detach(ctx)
	defer cancel()

	for _, file := range plan.Files {
		if !a.rename(ctx, instanceID, hash, file.OldPath, file.NewPath, false) {
			return false
		}
	}
	if plan.FolderFrom != "" {
		return a.rename(ctx, instanceID, hash, plan.FolderFrom, plan.FolderTo, true)
	}
	return true
}

func (a *Aligner) rename(ctx context.Context, instanceID int, hash, oldPath, newPath string, folder bool) bool {
	var err error
	for attempt := 1; attempt <= alignRenameAttempts; attempt++ {
		if folder {
			err = a.sync.RenameTorrentFolder(ctx, instanceID, hash, oldPath, newPath)
		} else {
			err = a.sync.RenameTorrentFile(ctx, instanceID, hash, oldPath, newPath)
		}

		watch := a.verifyWait
		if err != nil {
			watch = 0
		}
		deadline := time.Now().Add(watch)
		for {
			if a.renameApplied(ctx, instanceID, hash, oldPath, newPath, folder) {
				return true
			}
			if !time.Now().Before(deadline) || !sleepCtx(ctx, a.pollEvery) {
				break
			}
		}

		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Str("torrentHash", hash).
			Str("from", oldPath).
			Str("to", newPath).
			Bool("folder", folder).
			Int("attempt", attempt).
			Msg("Alignment rename not confirmed")
		if attempt < alignRenameAttempts && !sleepCtx(ctx, a.retryDelay) {
			break
		}
	}

	log.Warn().
		Err(err).
		Int("instanceID", instanceID).
		Str("torrentHash", hash).
		Str("from", oldPath).
		Str("to", newPath).
		Bool("folder", folder).
		Msg("Alignment rename failed after retries")
	return false
}

func (a *Aligner) renameApplied(ctx context.Context, instanceID int, hash, oldPath, newPath string, folder bool) bool {
	filesByHash, err := a.sync.GetTorrentFilesBatch(qbittorrent.WithForceFilesRefresh(ctx), instanceID, []string{hash})
	if err != nil {
		return false
	}

	oldFound, newFound := false, false
	for _, f := range filesByHash[normalizeHash(hash)] {
		if f.Name == oldPath || folder && strings.HasPrefix(f.Name, oldPath+"/") {
			oldFound = true
		}
		if f.Name == newPath || folder && strings.HasPrefix(f.Name, newPath+"/") {
			newFound = true
		}
	}
	return newFound && !oldFound
}

func sleepCtx(ctx context.Context, d time.Duration) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}
