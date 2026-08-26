// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	qbsync "github.com/autobrr/qui/internal/qbittorrent"
)

const (
	alignRenameAttempts = 3
	alignVerifyTimeout  = 3 * time.Second
	alignVerifyInterval = 250 * time.Millisecond
	alignRetryDelay     = 300 * time.Millisecond
	// alignAvailabilityTimeout bounds the wait for a just-added torrent to become visible in
	// qBittorrent before the first rename. Matches crossseed's crossSeedRenameWaitTimeout: renames
	// issued before the torrent is registered fail spuriously on slow instances.
	alignAvailabilityTimeout = 15 * time.Second
	// alignmentTimeout bounds the whole detached rename sequence. ponytail: fixed ceiling; a torrent
	// with hundreds of file renames could need more, bump it if that ever shows up in the wild.
	alignmentTimeout = 5 * time.Minute
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
	fileRenames  []fileRename // per-file renames, applied while still under sourceRoot (root-stripped paths for stripRoot plans)
	renameFolder bool         // rename sourceRoot -> targetRoot after the file renames
	stripRoot    bool         // add with contentLayout=NoSubfolder so qBittorrent drops sourceRoot from every stored path
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
// heuristics are needed. searcheeIsDir reports whether the on-disk searchee is a directory (vs a
// single loose file), which decides the qBittorrent save-path layout the torrent must line up with.
func buildAlignmentPlan(req *InjectRequest, searcheeIsDir bool) alignmentPlan {
	var plan alignmentPlan
	if req == nil || req.ParsedTorrent == nil || req.Searchee == nil || req.MatchResult == nil {
		return plan
	}

	sourceRoot := detectTorrentRoot(req.ParsedTorrent.Files)
	rooted := sourceRoot != ""

	// A foldered torrent matched to a single loose file has no on-disk folder to rename the
	// torrent root to. Instead the torrent is added with contentLayout=NoSubfolder, which makes
	// qBittorrent strip the root from every stored path, and the files are aligned rootless-style
	// against the stripped names.
	if rooted && !searcheeIsDir {
		plan.stripRoot = true
		plan.sourceRoot = sourceRoot
		rooted = false
	}

	if rooted {
		targetRoot := filepath.Base(filepath.Clean(req.Searchee.Path))
		if targetRoot == "" || targetRoot == "." || targetRoot == string(filepath.Separator) {
			return plan
		}
		plan.sourceRoot = sourceRoot
		plan.targetRoot = targetRoot
		plan.renameFolder = sourceRoot != targetRoot
	}

	for _, pair := range req.MatchResult.MatchedFiles {
		if pair.SearcheeFile == nil {
			continue
		}
		oldPath := pair.TorrentFile.Path
		if plan.stripRoot {
			oldPath = strings.TrimPrefix(oldPath, plan.sourceRoot+"/")
		}
		desiredRel := filepath.ToSlash(pair.SearcheeFile.RelPath)
		if oldPath == "" || desiredRel == "" {
			continue
		}
		// Rooted: rename within the current source root; the folder rename fixes the root itself.
		// Rootless: the torrent is dropped straight into the save path, so the file must land at the
		// exact on-disk relative path.
		newPath := desiredRel
		if rooted {
			newPath = sourceRoot + "/" + desiredRel
		}
		if oldPath != newPath {
			plan.fileRenames = append(plan.fileRenames, fileRename{oldPath: oldPath, newPath: newPath})
		}
	}

	return plan
}

// searcheePathIsDir reports whether the searchee path is an existing directory. A missing path
// or unavailable backend is treated as not-a-directory so alignment stays conservative.
func searcheePathIsDir(ctx context.Context, backend fsops.Backend, path string) bool {
	if backend == nil {
		return false
	}
	info, err := backend.Stat(ctx, path)
	return err == nil && info.IsDir
}

// alignAndRecheck renames the just-added torrent to match the on-disk files, then triggers a
// recheck and resume-when-complete. Returns whether alignment succeeded. The torrent was added
// force-paused, so if alignment fails it stays paused for inspection; ResumeWhenComplete only
// resumes at 100%, so bad data is never seeded.
func (i *Injector) alignAndRecheck(_ context.Context, req *InjectRequest, plan alignmentPlan) bool {
	hash := req.ParsedTorrent.InfoHash

	// Detach from the run context: once the torrent is added, the rename sequence must finish as a
	// unit. A mid-sequence cancel (user Stop / scheduler) would leave a half-renamed torrent that
	// TorrentExists skips on the next run, stranding it paused forever. The sibling
	// triggerRecheckForPartialLinkTree detaches for the same reason.
	ctx, cancel := context.WithTimeout(context.Background(), alignmentTimeout)
	defer cancel()

	// Wait for the just-added torrent to be visible in qBittorrent before renaming anything.
	// crossseed's aligner (waitForTorrentAvailability) learned this the hard way: on slow
	// instances the add hasn't propagated yet and early renames fail spuriously, stranding a
	// perfectly good match paused.
	if !i.waitForTorrentFiles(ctx, req.InstanceID, strings.ToLower(strings.TrimSpace(hash))) {
		log.Warn().
			Int("instanceID", req.InstanceID).
			Str("hash", hash).
			Msg("dirscan: torrent never became visible for alignment; leaving torrent paused for inspection")
		return false
	}

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
	// to keep it paused. If the recheck can't be scheduled the torrent stays force-paused with no
	// way to resume, so report failure rather than a stranded "success".
	if err := i.triggerRecheckForPausedPartial(req, true); err != nil {
		log.Warn().
			Err(err).
			Int("instanceID", req.InstanceID).
			Str("hash", hash).
			Msg("dirscan: aligned torrent but could not trigger recheck; leaving torrent paused for inspection")
		return false
	}
	return true
}

// applyAlignment renames the torrent's files and folder to match the on-disk layout.
// Files are renamed first (while still under the source root, which does not exist on disk),
// then the root folder is renamed so every path lands on the existing files. stripRoot plans
// have no folder rename: the root was already dropped at add time (contentLayout=NoSubfolder),
// so only the root-stripped file renames apply. Returns false if any rename could not be
// confirmed, in which case the caller leaves the torrent paused.
// ponytail: renames run in plan order with no collision handling. A cross-paired
// identical-size cluster (RAR sets, BDMV) could plan a swap whose first rename targets a
// still-occupied path; that rename fails and the torrent is left paused for inspection.
// Add temp-name shuffling for cycles if a real-world report ever lands.
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

// waitForTorrentFiles blocks until qBittorrent reports a non-empty file list for the torrent,
// proving the just-added torrent is registered and renameable. Returns false if the torrent never
// became visible before ctx or the availability window expired.
func (i *Injector) waitForTorrentFiles(ctx context.Context, instanceID int, canonicalHash string) bool {
	deadline := time.Now().Add(alignAvailabilityTimeout)
	for {
		filesMap, err := i.syncManager.GetTorrentFilesBatch(qbsync.WithForceFilesRefresh(ctx), instanceID, []string{canonicalHash})
		if err == nil && len(filesMap[canonicalHash]) > 0 {
			return true
		}
		if ctx.Err() != nil || !time.Now().Before(deadline) {
			return false
		}
		if !sleepCtx(ctx, alignVerifyInterval) {
			return false
		}
	}
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

		// Only a confirmed rename (renameDone) counts as success. A renamePending (async no-op,
		// old paths still visible) or renameUnknown (file list unreadable) is never accepted: keep
		// polling within the window, then retry the whole call. If the rename is never confirmed
		// across all attempts we return false so the torrent is left paused rather than resumed
		// on top of paths we could not verify.
		deadline := time.Now().Add(alignVerifyTimeout)
		for {
			if i.verifyRename(ctx, instanceID, canonical, oldPath, newPath, folder) == renameDone {
				return true
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
// current files. renameUnknown means the files could not be read; it is never treated as success,
// so the caller keeps polling/retrying and ultimately fails the alignment (leaving the torrent
// paused) when a rename cannot be confirmed.
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
