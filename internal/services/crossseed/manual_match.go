// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

// Manual match: the user chooses the target torrent. Candidate discovery and
// the category and content-type gates are bypassed; the recheck is the arbiter
// of a wrong pick.

// manualMatchType marks an add plan whose target was pinned by the user and
// whose files did not validate against the uploaded torrent. Such adds always
// verify before seeding and skip file alignment.
const manualMatchType = "manual"

const (
	manualMatchCoarseLimit   = 60
	manualMatchProposalLimit = 10
)

// findManualTargetCandidate resolves a pinned target hash to a single
// candidate, replacing discovery for Manual match requests.
func (s *Service) findManualTargetCandidate(ctx context.Context, req *FindCandidatesRequest) (*FindCandidatesResponse, error) {
	instanceIDs := normalizeInstanceIDs(req.TargetInstanceIDs)
	if len(instanceIDs) != 1 {
		return nil, fmt.Errorf("%w: manual match requires exactly one target instance", ErrInvalidRequest)
	}
	instanceID := instanceIDs[0]

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	torrents, err := s.syncManager.GetTorrents(ctx, instanceID, qbt.TorrentFilterOptions{Filter: qbt.TorrentFilterAll})
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	want := normalizeHash(req.ManualTargetHash)
	for _, torrent := range torrents {
		if normalizeHash(torrent.Hash) != want {
			continue
		}
		if torrent.Progress < 1 {
			return nil, fmt.Errorf("%w: manual match target %q is not complete", ErrTorrentNotComplete, torrent.Name)
		}
		return &FindCandidatesResponse{
			SourceTorrent: &TorrentInfo{Name: req.TorrentName},
			Candidates: []CrossSeedCandidate{{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Torrents:     []qbt.Torrent{torrent},
				MatchType:    manualMatchType,
			}},
		}, nil
	}
	return nil, fmt.Errorf("%w: manual match target not found in instance", ErrTorrentNotFound)
}

// ManualMatchProposal is one suggested target for a Manual match, ranked by
// file-size overlap with the uploaded torrent.
type ManualMatchProposal struct {
	Hash              string  `json:"hash"`
	Name              string  `json:"name"`
	Size              int64   `json:"size"`
	Category          string  `json:"category"`
	EffectiveSavePath string  `json:"effective_save_path"`
	OverlapBytes      int64   `json:"overlap_bytes"`
	OverlapFraction   float64 `json:"overlap_fraction"`
}

// ManualMatchProposalsResponse carries the ranked target proposals plus the
// prefill values the Manual match dialog needs.
type ManualMatchProposalsResponse struct {
	SourceName      string                `json:"source_name"`
	SourceSize      int64                 `json:"source_size"`
	SourceFileCount int                   `json:"source_file_count"`
	DefaultTags     []string              `json:"default_tags"`
	Proposals       []ManualMatchProposal `json:"proposals"`
}

// ManualMatchProposalsFromBase64 decodes a base64 torrent payload and ranks
// Manual match target proposals for it.
func (s *Service) ManualMatchProposalsFromBase64(ctx context.Context, instanceID int, torrentData, requestedHash string) (*ManualMatchProposalsResponse, error) {
	torrentBytes, err := s.decodeTorrentData(torrentData)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to decode torrent data: %w", ErrInvalidRequest, err)
	}
	return s.ManualMatchProposals(ctx, instanceID, torrentBytes, requestedHash)
}

// ManualMatchProposals ranks same-instance torrents by file-size overlap with
// the uploaded torrent. requestedHash, when set, is always included in the
// result (even at zero overlap) so the dialog can report on an arbitrary pick.
func (s *Service) ManualMatchProposals(ctx context.Context, instanceID int, torrentBytes []byte, requestedHash string) (*ManualMatchProposalsResponse, error) {
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: failed to parse torrent: %w", ErrInvalidRequest, err)
	}

	var sourceTotal int64
	sourceSizes := make(map[int64]int, len(meta.Files))
	for _, f := range meta.Files {
		sourceTotal += f.Size
		sourceSizes[f.Size]++
	}
	if sourceTotal <= 0 {
		return nil, fmt.Errorf("%w: uploaded torrent has no files", ErrInvalidRequest)
	}

	instance, err := s.instanceStore.Get(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get instance: %w", err)
	}
	views, err := s.syncManager.GetCachedInstanceTorrents(ctx, instanceID)
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	sourceTitle := ""
	if release := s.releaseCache.Parse(meta.Name); release != nil {
		sourceTitle = s.stringNormalizer.Normalize(release.Title)
	}
	wantHash := normalizeHash(requestedHash)

	// Coarse pass on cached metadata only: total-size ratio, plus every torrent
	// whose parsed title matches. File lists are loaded only for the shortlist.
	// ponytail: title+size heuristic can miss exotic containment shapes; the
	// manual picker behind the proposals covers those.
	type coarseEntry struct {
		torrent qbt.Torrent
		ratio   float64
		keep    bool
	}
	coarse := make([]coarseEntry, 0, manualMatchCoarseLimit)
	for _, view := range views {
		if view.Torrent == nil {
			continue
		}
		torrent := *view.Torrent
		isRequested := wantHash != "" && normalizeHash(torrent.Hash) == wantHash
		if !isRequested && (torrent.Progress < 1 || s.shouldSkipErroredTorrent(torrent.State)) {
			continue
		}
		entry := coarseEntry{torrent: torrent, keep: isRequested}
		if torrent.Size > 0 {
			entry.ratio = float64(min(torrent.Size, sourceTotal)) / float64(max(torrent.Size, sourceTotal))
		}
		if !entry.keep && sourceTitle != "" {
			if release := s.releaseCache.Parse(torrent.Name); release != nil &&
				s.stringNormalizer.Normalize(release.Title) == sourceTitle {
				entry.keep = true
			}
		}
		coarse = append(coarse, entry)
	}
	slices.SortFunc(coarse, func(a, b coarseEntry) int {
		return cmp.Compare(b.ratio, a.ratio)
	})

	shortlist := make([]qbt.Torrent, 0, manualMatchCoarseLimit)
	for _, entry := range coarse {
		if entry.keep && len(shortlist) < manualMatchCoarseLimit {
			shortlist = append(shortlist, entry.torrent)
		}
	}
	for _, entry := range coarse {
		if len(shortlist) >= manualMatchCoarseLimit {
			break
		}
		if !entry.keep {
			shortlist = append(shortlist, entry.torrent)
		}
	}

	filesByHash := s.batchLoadCandidateFiles(ctx, instanceID, shortlist)
	trackerDomain := ParseTorrentAnnounceDomain(torrentBytes)

	torrentHash := meta.HashV1
	if torrentHash == "" {
		torrentHash = meta.HashV2
	}

	// Effective destination varies only through the target save path (link-mode
	// base-dir selection); cache per save path.
	effectiveBySavePath := make(map[string]string)
	effectiveFor := func(torrent qbt.Torrent) string {
		if path, ok := effectiveBySavePath[torrent.SavePath]; ok {
			return path
		}
		path := s.manualMatchEffectiveSavePath(ctx, instance, torrent.SavePath, torrentHash, meta.Name, trackerDomain, meta.Files)
		effectiveBySavePath[torrent.SavePath] = path
		return path
	}

	proposals := make([]ManualMatchProposal, 0, manualMatchProposalLimit+1)
	for _, torrent := range shortlist {
		overlap := overlapBytesBySize(sourceSizes, filesByHash[normalizeHash(torrent.Hash)])
		isRequested := wantHash != "" && normalizeHash(torrent.Hash) == wantHash
		if overlap == 0 && !isRequested {
			continue
		}
		proposals = append(proposals, ManualMatchProposal{
			Hash:              torrent.Hash,
			Name:              torrent.Name,
			Size:              torrent.Size,
			Category:          torrent.Category,
			EffectiveSavePath: effectiveFor(torrent),
			OverlapBytes:      overlap,
			OverlapFraction:   float64(overlap) / float64(sourceTotal),
		})
	}
	slices.SortFunc(proposals, func(a, b ManualMatchProposal) int {
		return cmp.Or(cmp.Compare(b.OverlapBytes, a.OverlapBytes), strings.Compare(a.Name, b.Name))
	})
	if requestedIdx := slices.IndexFunc(proposals, func(p ManualMatchProposal) bool {
		return wantHash != "" && normalizeHash(p.Hash) == wantHash
	}); requestedIdx >= manualMatchProposalLimit {
		proposals = append(proposals[:manualMatchProposalLimit], proposals[requestedIdx])
	} else if len(proposals) > manualMatchProposalLimit {
		proposals = proposals[:manualMatchProposalLimit]
	}

	defaultTags := []string{}
	if settings, err := s.GetAutomationSettings(ctx); err == nil && settings != nil {
		defaultTags = append(defaultTags, settings.SeededSearchTags...)
	} else if err != nil {
		log.Debug().Err(err).Msg("[CROSSSEED] Manual match proposals: failed to load automation settings for default tags")
	}

	return &ManualMatchProposalsResponse{
		SourceName:      meta.Name,
		SourceSize:      sourceTotal,
		SourceFileCount: len(meta.Files),
		DefaultTags:     defaultTags,
		Proposals:       proposals,
	}, nil
}

// overlapBytesBySize sums the bytes of the multiset intersection of file sizes.
func overlapBytesBySize(sourceSizes map[int64]int, candidateFiles qbt.TorrentFiles) int64 {
	if len(candidateFiles) == 0 {
		return 0
	}
	remaining := make(map[int64]int, len(sourceSizes))
	maps.Copy(remaining, sourceSizes)
	var overlap int64
	for _, f := range candidateFiles {
		if remaining[f.Size] > 0 {
			remaining[f.Size]--
			overlap += f.Size
		}
	}
	return overlap
}

// manualMatchEffectiveSavePath reports where a Manual match apply would land
// the torrent: the target's save path in regular mode, or the link-mode
// destination directory. Display only; the apply recomputes the real path.
func (s *Service) manualMatchEffectiveSavePath(
	ctx context.Context,
	instance *models.Instance,
	targetSavePath string,
	torrentHash, torrentName, trackerDomain string,
	sourceFiles qbt.TorrentFiles,
) string {
	if instance == nil || (!instance.UseReflinks && !instance.UseHardlinks) {
		return targetSavePath
	}
	baseDir := s.previewLinkBaseDir(ctx, instance, targetSavePath)
	if baseDir == "" {
		return targetSavePath
	}
	treeFiles := make([]hardlinktree.TorrentFile, 0, len(sourceFiles))
	for _, f := range sourceFiles {
		treeFiles = append(treeFiles, hardlinktree.TorrentFile{Path: f.Name, Size: f.Size})
	}
	candidate := CrossSeedCandidate{InstanceID: instance.ID, InstanceName: instance.Name}
	return s.buildHardlinkDestDir(ctx, instance, baseDir, torrentHash, torrentName, candidate, trackerDomain, &CrossSeedRequest{}, treeFiles)
}

// previewLinkBaseDir picks the configured link base directory on the same
// filesystem as samplePath, without the MkdirAll side effect of
// FindMatchingBaseDir. Falls back to the first configured directory.
func (s *Service) previewLinkBaseDir(ctx context.Context, instance *models.Instance, samplePath string) string {
	first := ""
	backend, backendErr := s.getBackendForInstance(ctx, instance.ID)
	for dir := range strings.SplitSeq(instance.HardlinkBaseDir, ",") {
		dir = strings.TrimSpace(dir)
		if dir == "" {
			continue
		}
		if first == "" {
			first = dir
		}
		if backendErr != nil || backend == nil || samplePath == "" {
			continue
		}
		if same, err := backend.SameFilesystem(ctx, samplePath, dir); err == nil && same {
			return dir
		}
	}
	return first
}
