// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"syscall"

	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/services/crossseed"
)

type alignmentPlan struct {
	sourceRoot   string
	targetRoot   string
	fileRenames  []crossseed.FileRename
	renameFolder bool
	stripRoot    bool
}

func (p alignmentPlan) needed() bool {
	return p.renameFolder || len(p.fileRenames) > 0
}

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

func buildAlignmentPlan(d fsops.PathDialect, req *InjectRequest, searcheeIsDir bool) alignmentPlan {
	var plan alignmentPlan
	if req == nil || req.ParsedTorrent == nil || req.Searchee == nil || req.MatchResult == nil {
		return plan
	}

	sourceRoot := detectTorrentRoot(req.ParsedTorrent.Files)
	rooted := sourceRoot != ""

	if rooted && !searcheeIsDir {
		plan.stripRoot = true
		plan.sourceRoot = sourceRoot
		rooted = false
	}

	if rooted {
		targetRoot := d.Base(d.Clean(req.Searchee.Path))
		if targetRoot == "" || targetRoot == "." || targetRoot == d.Separator() {
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
		desiredRel := d.ToSlash(pair.SearcheeFile.RelPath)
		if oldPath == "" || desiredRel == "" {
			continue
		}
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

func searcheeIsDir(ctx context.Context, backend fsops.Backend, path string) (bool, error) {
	info, err := backend.Stat(ctx, path)
	if errors.Is(err, fs.ErrNotExist) || errors.Is(err, syscall.ENOTDIR) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("read searchee: %w", err)
	}
	return info.IsDir, nil
}

func (i *Injector) align(ctx context.Context, req *InjectRequest, plan alignmentPlan) (string, bool) {
	hash, _, ok := i.aligner.WaitForFiles(ctx, req.InstanceID, []string{req.ParsedTorrent.InfoHash, req.ParsedTorrent.InfoHashV2})
	if !ok {
		log.Warn().
			Int("instanceID", req.InstanceID).
			Str("hash", req.ParsedTorrent.InfoHash).
			Msg("dirscan: torrent never became visible for alignment; leaving torrent paused for inspection")
		return "", false
	}

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
