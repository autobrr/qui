// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/autobrr/rls"
	"github.com/rs/zerolog/log"
)

func (s *Service) catalogAddPlan(candidate CrossSeedCandidate, incomingRelease *rls.Release, incoming qbt.TorrentFiles, filesByHash map[string]qbt.TorrentFiles, req *CrossSeedRequest) (*crossSeedAddPlan, string) {
	p := req.SearchDecision
	if candidate.InstanceID != p.SourceInstanceID || len(candidate.Torrents) != 1 || normalizeHash(candidate.Torrents[0].Hash) != p.SourceHash {
		return nil, "file-evidence source binding mismatch"
	}
	torrent := candidate.Torrents[0]
	if s.sourceGate().check(torrent, req.sourceFilter) != sourcePass {
		return nil, "bound source incomplete or ineligible"
	}
	files := filesByHash[p.SourceHash]
	if err := validateCatalogSource(p, torrent, files); err != nil {
		return nil, err.Error()
	}
	read, err := s.readCatalogRelease(torrent.Name, files, p.Catalog.Family)
	if err != nil {
		return nil, err.Error()
	}
	incomingRoot := detectCommonRoot(incoming)
	pairs, err := completeCatalogPairing(incoming, files, incomingRoot, detectCommonRoot(files))
	if err != nil {
		return nil, err.Error()
	}
	matchType := "exact"
	for _, pair := range pairs {
		if pair.sourcePath != pair.candidatePath {
			matchType = "size"
			break
		}
	}
	plan := buildCrossSeedAddPlan(*torrent, files, matchType, incomingRelease, read.release, incoming)
	plan.filePairs = pairs
	plan.contentLayout, plan.savePathOverride = "Original", ""
	return &plan, ""
}

func (s *Service) alignCatalogFiles(ctx context.Context, instanceID int, hash, hashV2 string, plan *crossSeedAddPlan) (bool, string) {
	active, visible, ok := s.aligner.WaitForFiles(ctx, instanceID, dedupeHashes(hash, hashV2))
	if !ok {
		return false, active
	}
	alignment, err := catalogAlignment(plan, visible)
	if err != nil {
		log.Debug().
			Err(err).
			Int("instanceID", instanceID).
			Str("torrentHash", active).
			Msg("Catalog file alignment rejected the added file table, leaving the torrent paused")
		return false, active
	}
	return s.aligner.Align(ctx, instanceID, active, alignment), active
}

func catalogAlignment(plan *crossSeedAddPlan, visible qbt.TorrentFiles) (AlignmentPlan, error) {
	if len(visible) != len(plan.filePairs) {
		return AlignmentPlan{}, errors.New("added file count changed")
	}
	byIndex := make(map[int]qbt.TorrentFile, len(visible))
	for _, file := range visible {
		if _, duplicate := byIndex[file.Index]; duplicate {
			return AlignmentPlan{}, errors.New("duplicate added file index")
		}
		byIndex[file.Index] = file
	}
	var alignment AlignmentPlan
	sourceRoot, targetRoot := detectCommonRoot(visible), detectCommonRoot(plan.files)
	rootOnly := sourceRoot != "" && targetRoot != "" && sourceRoot != targetRoot
	for _, pair := range plan.filePairs {
		expected := plan.sourceFiles[pair.sourceIndex]
		file, ok := byIndex[expected.Index]
		if !ok || file.Size != expected.Size || file.Name != pair.sourcePath {
			return AlignmentPlan{}, errors.New("added file identity changed")
		}
		if rootOnly && adjustPathForRootRename(pair.candidatePath, targetRoot, sourceRoot) != file.Name {
			rootOnly = false
		}
		if file.Name != pair.candidatePath {
			alignment.Files = append(alignment.Files, FileRename{OldPath: file.Name, NewPath: pair.candidatePath})
		}
	}
	if rootOnly {
		return AlignmentPlan{FolderFrom: sourceRoot, FolderTo: targetRoot}, nil
	}
	return alignment, nil
}
