// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/services/crossseed"
)

// alignmentPlan describes the renames needed to make an injected torrent's internal
// folder/file names match the files that already exist on disk. In regular (reuse) mode
// qBittorrent stores the torrent using its own info.name and file paths; when those differ
// from the on-disk directory the reporter matched against, qBittorrent reports "Missing Files".
type alignmentPlan struct {
	sourceRoot   string                 // the torrent's top-level folder (empty for rootless torrents)
	targetRoot   string                 // the on-disk directory name we matched against
	fileRenames  []crossseed.FileRename // per-file renames, applied while still under sourceRoot (root-stripped paths for stripRoot plans)
	renameFolder bool                   // rename sourceRoot -> targetRoot after the file renames
	stripRoot    bool                   // add with contentLayout=NoSubfolder so qBittorrent drops sourceRoot from every stored path
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
			plan.fileRenames = append(plan.fileRenames, crossseed.FileRename{OldPath: oldPath, NewPath: newPath})
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

// align renames the just-added torrent to match the on-disk files. It returns the hash that
// qBittorrent lists the torrent under and whether alignment succeeded. The torrent was added
// paused, so if alignment fails it stays paused for inspection. After a success, Settle
// rechecks it and resumes it only at 100%, so bad data is never seeded.
func (i *Injector) align(ctx context.Context, req *InjectRequest, plan alignmentPlan) (string, bool) {
	hash, _, ok := i.aligner.WaitForFiles(ctx, req.InstanceID, []string{req.ParsedTorrent.InfoHash, req.ParsedTorrent.InfoHashV2})
	if !ok {
		log.Warn().
			Int("instanceID", req.InstanceID).
			Str("hash", req.ParsedTorrent.InfoHash).
			Msg("dirscan: torrent never became visible for alignment; leaving torrent paused for inspection")
		return "", false
	}

	// stripRoot plans have no folder rename: the root was already dropped at add time
	// (contentLayout=NoSubfolder), so only the root-stripped file renames apply.
	alignPlan := crossseed.AlignmentPlan{Files: plan.fileRenames}
	if plan.renameFolder {
		alignPlan.FolderFrom, alignPlan.FolderTo = plan.sourceRoot, plan.targetRoot
	}
	if !i.aligner.Align(ctx, req.InstanceID, hash, alignPlan) {
		log.Warn().
			Int("instanceID", req.InstanceID).
			Str("hash", hash).
			Str("torrentRoot", plan.sourceRoot).
			Str("diskRoot", plan.targetRoot).
			Msg("dirscan: alignment incomplete; leaving torrent paused for inspection")
		return hash, false
	}

	log.Info().
		Int("instanceID", req.InstanceID).
		Str("hash", hash).
		Str("torrentRoot", plan.sourceRoot).
		Str("diskRoot", plan.targetRoot).
		Int("fileRenames", len(plan.fileRenames)).
		Bool("folderRenamed", plan.renameFolder).
		Msg("dirscan: aligned torrent content paths to on-disk layout")
	return hash, true
}
