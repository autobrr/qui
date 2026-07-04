// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	qbsync "github.com/autobrr/qui/internal/qbittorrent"
)

const (
	alignRenameAttempts = 3
	alignVerifyTimeout  = 3 * time.Second
	alignVerifyInterval = 250 * time.Millisecond
	alignRetryDelay     = 300 * time.Millisecond
)

type fileRename struct {
	oldPath string
	newPath string
}

// alignmentPlan describes the renames needed to make an injected torrent's internal
// folder/file names match the files that already exist on disk. In regular (reuse) mode
// qBittorrent stores the torrent using its own info.name and file paths; when those differ
// from the on-disk directory the reporter matched against, qBittorrent reports "Missing Files".
type alignmentPlan struct {
	sourceRoot   string       // the torrent's top-level folder (empty for rootless torrents)
	targetRoot   string       // the on-disk directory name we matched against
	fileRenames  []fileRename // per-file renames, applied while still under sourceRoot
	renameFolder bool         // rename sourceRoot -> targetRoot after the file renames
}

func (p alignmentPlan) needed() bool {
	return p.renameFolder || len(p.fileRenames) > 0
}

// detectTorrentRoot returns the common top-level folder shared by every torrent file,
// or "" when the files have no shared root (rootless torrent).
func detectTorrentRoot(files []TorrentFile) string {
	root := ""
	for _, f := range files {
		parts := strings.SplitN(f.Path, "/", 2)
		if len(parts) < 2 || parts[0] == "" {
			return ""
		}
		if root == "" {
			root = parts[0]
			continue
		}
		if parts[0] != root {
			return ""
		}
	}
	return root
}

// buildAlignmentPlan derives the renames required to point a matched torrent at the existing
// on-disk files. It uses the exact torrent->disk file mapping from MatchResult, so no size
// heuristics are needed. Rootless torrents are left alone: they are injected directly into the
// searchee directory and already line up.
func buildAlignmentPlan(req *InjectRequest) alignmentPlan {
	var plan alignmentPlan
	if req == nil || req.ParsedTorrent == nil || req.Searchee == nil || req.MatchResult == nil {
		return plan
	}

	sourceRoot := detectTorrentRoot(req.ParsedTorrent.Files)
	if sourceRoot == "" {
		return plan
	}

	targetRoot := filepath.Base(filepath.Clean(req.Searchee.Path))
	if targetRoot == "" || targetRoot == "." || targetRoot == string(filepath.Separator) {
		return plan
	}

	plan.sourceRoot = sourceRoot
	plan.targetRoot = targetRoot

	for _, pair := range req.MatchResult.MatchedFiles {
		if pair.SearcheeFile == nil {
			continue
		}
		oldPath := pair.TorrentFile.Path
		desiredRel := filepath.ToSlash(pair.SearcheeFile.RelPath)
		if oldPath == "" || desiredRel == "" {
			continue
		}
		// Rename within the current source root; the folder rename below fixes the root itself.
		newPath := sourceRoot + "/" + desiredRel
		if oldPath != newPath {
			plan.fileRenames = append(plan.fileRenames, fileRename{oldPath: oldPath, newPath: newPath})
		}
	}

	plan.renameFolder = sourceRoot != targetRoot
	return plan
}

// alignAndRecheck renames the just-added torrent to match the on-disk files, then triggers a
// recheck and resume-when-complete. Returns whether alignment succeeded. The torrent was added
// force-paused, so if alignment fails it stays paused for inspection; ResumeWhenComplete only
// resumes at 100%, so bad data is never seeded.
func (i *Injector) alignAndRecheck(ctx context.Context, req *InjectRequest, plan alignmentPlan) bool {
	hash := req.ParsedTorrent.InfoHash

	if !i.applyAlignment(ctx, req.InstanceID, hash, plan) {
		// Leave the torrent paused and untouched (no recheck/resume) so the user can inspect it.
		log.Warn().
			Int("instanceID", req.InstanceID).
			Str("hash", hash).
			Str("torrentRoot", plan.sourceRoot).
			Str("diskRoot", plan.targetRoot).
			Msg("dirscan: content path alignment incomplete; leaving torrent paused for inspection")
		return false
	}

	log.Info().
		Int("instanceID", req.InstanceID).
		Str("hash", hash).
		Str("torrentRoot", plan.sourceRoot).
		Str("diskRoot", plan.targetRoot).
		Int("fileRenames", len(plan.fileRenames)).
		Bool("folderRenamed", plan.renameFolder).
		Msg("dirscan: aligned torrent content paths to on-disk layout")

	// Recheck so qBittorrent validates the now-aligned data, then resume unless the user asked
	// to keep it paused.
	i.triggerRecheckForPausedPartial(req, true)
	return true
}

// applyAlignment renames the torrent's files and folder to match the on-disk layout.
// Files are renamed first (while still under the source root, which does not exist on disk),
// then the root folder is renamed so every path lands on the existing files. Returns false if
// any rename could not be confirmed, in which case the caller leaves the torrent paused.
func (i *Injector) applyAlignment(ctx context.Context, instanceID int, hash string, plan alignmentPlan) bool {
	for _, fr := range plan.fileRenames {
		if !i.renameTorrentPath(ctx, instanceID, hash, fr.oldPath, fr.newPath, false) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("from", fr.oldPath).
				Str("to", fr.newPath).
				Msg("dirscan: failed to rename torrent file during alignment")
			return false
		}
	}

	if plan.renameFolder {
		if !i.renameTorrentPath(ctx, instanceID, hash, plan.sourceRoot, plan.targetRoot, true) {
			log.Warn().
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("from", plan.sourceRoot).
				Str("to", plan.targetRoot).
				Msg("dirscan: failed to rename torrent folder during alignment")
			return false
		}
	}

	return true
}

type renameStatus int

const (
	renamePending renameStatus = iota
	renameDone
	renameUnknown
)

// renameTorrentPath issues a file or folder rename and verifies it landed, retrying because
// qBittorrent's rename API is async and can return 200 OK while silently doing nothing.
func (i *Injector) renameTorrentPath(ctx context.Context, instanceID int, hash, oldPath, newPath string, folder bool) bool {
	canonical := strings.ToLower(strings.TrimSpace(hash))

	for attempt := 1; attempt <= alignRenameAttempts; attempt++ {
		if ctx.Err() != nil {
			return false
		}

		var err error
		if folder {
			err = i.syncManager.RenameTorrentFolder(ctx, instanceID, hash, oldPath, newPath)
		} else {
			err = i.syncManager.RenameTorrentFile(ctx, instanceID, hash, oldPath, newPath)
		}

		if err != nil {
			// The API errored, but the rename may still have been applied. Only treat a
			// confirmed rename as success; otherwise retry.
			if i.verifyRename(ctx, instanceID, canonical, oldPath, newPath, folder) == renameDone {
				return true
			}
			log.Debug().
				Err(err).
				Int("instanceID", instanceID).
				Str("hash", hash).
				Str("from", oldPath).
				Str("to", newPath).
				Int("attempt", attempt).
				Msg("dirscan: rename API call failed, retrying")
			if attempt < alignRenameAttempts && sleepCtx(ctx, alignRetryDelay) {
				continue
			}
			return false
		}

		deadline := time.Now().Add(alignVerifyTimeout)
		for {
			switch i.verifyRename(ctx, instanceID, canonical, oldPath, newPath, folder) {
			case renameDone, renameUnknown:
				return true
			case renamePending:
			}
			if ctx.Err() != nil || !time.Now().Before(deadline) {
				break
			}
			time.Sleep(alignVerifyInterval)
		}

		if attempt < alignRenameAttempts && !sleepCtx(ctx, alignRetryDelay) {
			return false
		}
	}

	return false
}

// verifyRename reports whether qBittorrent has applied a rename by inspecting the torrent's
// current files. renameUnknown means the files could not be read (best-effort success).
func (i *Injector) verifyRename(ctx context.Context, instanceID int, canonicalHash, oldPath, newPath string, folder bool) renameStatus {
	filesMap, err := i.syncManager.GetTorrentFilesBatch(qbsync.WithForceFilesRefresh(ctx), instanceID, []string{canonicalHash})
	if err != nil {
		return renameUnknown
	}
	files, ok := filesMap[canonicalHash]
	if !ok || len(files) == 0 {
		return renameUnknown
	}

	if folder {
		// Require both that the old root is gone and the new root is present, so a rename that
		// silently did nothing (e.g. the torrent's actual root differed) is not read as success.
		oldRemains, newPresent := false, false
		for _, f := range files {
			if f.Name == oldPath || strings.HasPrefix(f.Name, oldPath+"/") {
				oldRemains = true
			}
			if f.Name == newPath || strings.HasPrefix(f.Name, newPath+"/") {
				newPresent = true
			}
		}
		if !oldRemains && newPresent {
			return renameDone
		}
		return renamePending
	}

	oldExists, newExists := false, false
	for _, f := range files {
		switch f.Name {
		case oldPath:
			oldExists = true
		case newPath:
			newExists = true
		}
	}
	if newExists && !oldExists {
		return renameDone
	}
	return renamePending
}

// sleepCtx sleeps for d unless ctx is cancelled first. Returns true if the full delay elapsed.
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
