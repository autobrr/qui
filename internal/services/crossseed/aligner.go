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

// alignRenameAttempts is how many times the Aligner sends one rename.
const alignRenameAttempts = 3

// alignerSync is what the Aligner reads from and sends to qBittorrent.
type alignerSync interface {
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
	RenameTorrentFile(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
	RenameTorrentFolder(ctx context.Context, instanceID int, hash, oldPath, newPath string) error
}

var _ alignerSync = (*qbittorrent.SyncManager)(nil)

// FileRename moves one torrent file from OldPath to NewPath.
type FileRename struct {
	OldPath string
	NewPath string
}

// AlignmentPlan is the renames of one Alignment.
type AlignmentPlan struct {
	Files                []FileRename // applied first, under the old root
	FolderFrom, FolderTo string       // empty: no folder rename
}

// Aligner does the Alignment of an added torrent: it renames the files and the
// folder, and reads the file list back to make sure that each rename applied.
type Aligner struct {
	sync       alignerSync
	filesWait  time.Duration // how long WaitForFiles waits for the torrent
	verifyWait time.Duration // how long one rename try is watched
	pollEvery  time.Duration
	retryDelay time.Duration
	timeout    time.Duration // bounds each call after the run context is dropped
}

// NewAligner builds the Aligner.
func NewAligner(sync alignerSync) *Aligner {
	return &Aligner{
		sync:       sync,
		filesWait:  15 * time.Second,
		verifyWait: 3 * time.Second,
		pollEvery:  250 * time.Millisecond,
		retryDelay: 300 * time.Millisecond,
		// ponytail: fixed ceiling; a torrent with hundreds of file renames could need more.
		timeout: 5 * time.Minute,
	}
}

// detach drops the cancel of the run context. Once the torrent is added, a
// cancel in the middle would leave a half-renamed torrent paused, and the next
// run would skip it.
func (a *Aligner) detach(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(ctx), a.timeout)
}

// WaitForFiles waits until qBittorrent lists files for one of hashes, the v1
// and v2 hashes of a just-added torrent. It returns the hash that has the files
// and the stored file names.
func (a *Aligner) WaitForFiles(ctx context.Context, instanceID int, hashes []string) (string, qbt.TorrentFiles, bool) {
	ctx, cancel := a.detach(ctx)
	defer cancel()

	// qBittorrent finds a torrent only by a 40-character id, and lists a v2-only
	// torrent under the first 40 characters of its v2 hash.
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

// Align renames the files of plan, then its folder. It returns false when a
// rename is not confirmed; the caller then leaves the torrent paused.
// ponytail: renames run in plan order with no collision handling. A plan that
// swaps two names fails on its first rename and leaves the torrent paused.
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

// rename sends one rename and watches the file list until it applies. The
// qBittorrent rename calls return before the rename applies and can drop it,
// so each try is watched and a dropped rename is sent again.
func (a *Aligner) rename(ctx context.Context, instanceID int, hash, oldPath, newPath string, folder bool) bool {
	var err error
	for attempt := 1; attempt <= alignRenameAttempts; attempt++ {
		if folder {
			err = a.sync.RenameTorrentFolder(ctx, instanceID, hash, oldPath, newPath)
		} else {
			err = a.sync.RenameTorrentFile(ctx, instanceID, hash, oldPath, newPath)
		}

		// After an API error the rename can still have applied, so read the list once.
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

// renameApplied reports whether the file list shows the new path and not the
// old one. A file list that qui cannot read, or that shows neither path, does
// not confirm the rename: the torrent stays paused rather than resume on paths
// that nothing checked.
func (a *Aligner) renameApplied(ctx context.Context, instanceID int, hash, oldPath, newPath string, folder bool) bool {
	// Force a fresh list: the cached one is from before the rename.
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

// sleepCtx sleeps for d unless ctx ends first. It returns true if the full delay elapsed.
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
