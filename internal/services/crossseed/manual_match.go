// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"maps"
	"slices"
	"strings"
	"sync"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"
	"golang.org/x/sync/errgroup"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
)

// Manual match: the user chooses the target torrent. Candidate discovery and
// the category and content-type gates are bypassed; the recheck is the arbiter
// of a wrong pick.

// manualMatchType marks an add plan whose target was pinned by the user and
// whose files did not validate against the uploaded torrent. Such adds skip
// file alignment and the link modes (nothing validated to link). Every Manual
// match, validated or not, verifies before seeding.
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
	torrents, err := s.syncManager.GetTorrentRefs(ctx, instanceID, qbittorrent.TorrentRefFilter{})
	if err != nil {
		return nil, fmt.Errorf("failed to get torrents: %w", err)
	}

	want := normalizeHash(req.ManualTargetHash)
	for _, torrent := range torrents {
		if normalizeHash(torrent.Hash) != want {
			continue
		}
		switch s.sourceGate().check(torrent, sourceFilter{}) {
		case sourceIncomplete:
			return nil, fmt.Errorf("%w: manual match target %q is not complete", ErrTorrentNotComplete, torrent.Name)
		case sourceErrored:
			return nil, ErrSourceErrored
		case sourceMovingOrChecking:
			return nil, ErrSourceNotSettled
		default:
		}
		return &FindCandidatesResponse{
			SourceTorrent: &TorrentInfo{Name: req.TorrentName},
			Candidates: []CrossSeedCandidate{{
				InstanceID:   instanceID,
				InstanceName: instance.Name,
				Torrents:     []*qbt.Torrent{torrent},
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
	PackMode                  bool     `json:"pack_mode"`
	PackEpisodeCount          int      `json:"pack_episode_count"`
	AssemblyUnavailableReason string   `json:"assembly_unavailable_reason"`
	ProposalLimit             int      `json:"proposal_limit"`
	ProposalsTruncated        bool     `json:"proposals_truncated"`
	SourceName                string   `json:"source_name"`
	SourceSize                int64    `json:"source_size"`
	SourceFileCount           int      `json:"source_file_count"`
	DefaultTags               []string `json:"default_tags"`
	// PinnedCategory is set when the automation settings pin every cross-seed to
	// one category. The apply then ignores the request category, so the dialog
	// shows this value instead of offering a pick it would discard.
	PinnedCategory string                `json:"pinned_category"`
	Proposals      []ManualMatchProposal `json:"proposals"`
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

	sourceRelease := s.releaseCache.Parse(meta.Name)
	// A pack named only with a Season numeral parses as a movie. Its episode
	// files make it a pack, so a movie sequel stays a movie.
	packRelease := s.matcher().parseSeasonPackName(meta.Name)
	packEpisodeCount := 0
	if isTVSeasonPack(packRelease) {
		packEpisodeCount = len(s.matcher().extractPackEpisodes(meta.Files, packRelease))
	}
	packMode := isTVSeasonPack(sourceRelease) || packEpisodeCount > 0
	proposalLimit := manualMatchProposalLimit
	assemblyUnavailable := ""
	if packMode {
		proposalLimit = min(manualMatchCoarseLimit, packEpisodeCount+manualMatchProposalLimit)
		assemblyUnavailable = manualAssemblyUnavailableReason(instance)
		if packEpisodeCount == 0 {
			assemblyUnavailable = "no_episode_files"
		}
	}
	sourceTitle := ""
	if sourceRelease != nil {
		sourceTitle = s.stringNormalizer.Normalize(sourceRelease.Title)
	}
	wantHash := normalizeHash(requestedHash)

	// Coarse pass on cached metadata only: total-size ratio, plus every torrent
	// whose parsed title matches. File lists are loaded only for the shortlist.
	// ponytail: title+size heuristic can miss exotic containment shapes; the
	// manual picker behind the proposals covers those.
	type coarseEntry struct {
		torrent *qbt.Torrent
		ratio   float64
		keep    bool
	}
	coarse := make([]coarseEntry, 0, manualMatchCoarseLimit)
	var requestedTorrent *qbt.Torrent
	for _, view := range views {
		if view.Torrent == nil {
			continue
		}
		torrent := view.Torrent
		if wantHash != "" && normalizeHash(torrent.Hash) == wantHash {
			// The requested hash claims its shortlist slot outright: the
			// "always included" guarantee must survive a coarse pass crowded
			// with keeps that outrank it.
			requestedTorrent = torrent
			continue
		}
		if s.sourceGate().check(torrent, sourceFilter{}) != sourcePass {
			continue
		}
		entry := coarseEntry{torrent: torrent}
		if torrent.Size > 0 {
			entry.ratio = float64(min(torrent.Size, sourceTotal)) / float64(max(torrent.Size, sourceTotal))
		}
		if sourceTitle != "" {
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

	shortlist := make([]*qbt.Torrent, 0, manualMatchCoarseLimit)
	if requestedTorrent != nil {
		shortlist = append(shortlist, requestedTorrent)
	}
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

	linkMode := instance != nil && (instance.UseReflinks || instance.UseHardlinks)
	linkTargets := make(map[string]*qbt.Torrent)

	proposals := make([]ManualMatchProposal, 0, proposalLimit+1)
	for _, torrent := range shortlist {
		hashKey := normalizeHash(torrent.Hash)
		candidateFiles := filesByHash[hashKey]
		overlap := overlapBytesBySize(sourceSizes, candidateFiles)
		isRequested := wantHash != "" && hashKey == wantHash
		if overlap == 0 && !isRequested {
			continue
		}
		// The apply skips link modes for a pick whose files do not validate,
		// landing it at the target's save path; only a validated pick reaches
		// the link-mode destination. Mirror the apply's validation call so the
		// read-only save path shows what the add will actually do.
		// ponytail: plain parsed releases approximate the apply's release view.
		if linkMode && overlap > 0 &&
			s.matcher().getMatchTypeWithReason(s.releaseCache.Parse(torrent.Name), sourceRelease, candidateFiles, meta.Files, defaultSizeMismatchTolerancePercent).MatchType != "" {
			linkTargets[hashKey] = torrent
		}
		proposals = append(proposals, ManualMatchProposal{
			Hash:              torrent.Hash,
			Name:              torrent.Name,
			Size:              torrent.Size,
			Category:          torrent.Category,
			EffectiveSavePath: cmp.Or(rootlessDestDir(torrent, candidateFiles, false), torrent.SavePath),
			OverlapBytes:      overlap,
			OverlapFraction:   float64(overlap) / float64(sourceTotal),
		})
	}
	slices.SortFunc(proposals, func(a, b ManualMatchProposal) int {
		return cmp.Or(cmp.Compare(b.OverlapBytes, a.OverlapBytes), strings.Compare(a.Name, b.Name))
	})
	proposalsTruncated := len(proposals) > proposalLimit || (packMode && packEpisodeCount > manualMatchCoarseLimit)
	if requestedIdx := slices.IndexFunc(proposals, func(p ManualMatchProposal) bool {
		return wantHash != "" && normalizeHash(p.Hash) == wantHash
	}); requestedIdx >= proposalLimit {
		proposals = append(proposals[:proposalLimit], proposals[requestedIdx])
	} else if len(proposals) > proposalLimit {
		proposals = proposals[:proposalLimit]
	}
	if len(linkTargets) > 0 {
		s.previewLinkSavePaths(ctx, instance, meta, trackerDomain, proposals, linkTargets, filesByHash)
	}

	// Default tag parity with the search-results dialog, which applies
	// "cross-seed" unless the user changes it. The badges stay toggleable.
	defaultTags := []string{"cross-seed"}
	pinnedCategory := ""
	if settings, err := s.GetAutomationSettings(ctx); err == nil && settings != nil {
		for _, tag := range settings.SeededSearchTags {
			if !slices.Contains(defaultTags, tag) {
				defaultTags = append(defaultTags, tag)
			}
		}
		if settings.UseCustomCategory && settings.CustomCategory != "" {
			pinnedCategory = settings.CustomCategory
		}
	} else if err != nil {
		log.Debug().Err(err).Msg("[CROSSSEED] Manual match proposals: failed to load automation settings for default tags")
	}

	return &ManualMatchProposalsResponse{
		PackMode:                  packMode,
		PackEpisodeCount:          packEpisodeCount,
		AssemblyUnavailableReason: assemblyUnavailable,
		ProposalLimit:             proposalLimit,
		ProposalsTruncated:        proposalsTruncated,
		SourceName:                meta.Name,
		SourceSize:                sourceTotal,
		SourceFileCount:           len(meta.Files),
		DefaultTags:               defaultTags,
		PinnedCategory:            pinnedCategory,
		Proposals:                 proposals,
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

const linkPreviewPlans = 4

func (s *Service) previewLinkSavePaths(
	ctx context.Context,
	instance *models.Instance,
	meta TorrentMetadata,
	trackerDomain string,
	proposals []ManualMatchProposal,
	linkTargets map[string]*qbt.Torrent,
	filesByHash map[string]qbt.TorrentFiles,
) {
	direct, err := s.getBackendForInstance(ctx, instance.ID, models.CapabilityWrite)
	if err != nil {
		return
	}
	backend := newCachedFilesystemChecks(direct)
	route, _, _ := s.savedRouteOf(ctx, instance.ID)
	incoming := singleAddTorrent{hash: cmp.Or(meta.HashV1, meta.HashV2), name: meta.Name, files: meta.Files, trackerDomain: trackerDomain}
	var g errgroup.Group
	g.SetLimit(linkPreviewPlans)
	for i := range proposals {
		target, ok := linkTargets[normalizeHash(proposals[i].Hash)]
		if !ok {
			continue
		}
		g.Go(func() error {
			if path, ok := s.linkSavePathPreview(ctx, backend, instance, route, incoming, target, filesByHash[normalizeHash(target.Hash)]); ok {
				proposals[i].EffectiveSavePath = path
			}
			return nil
		})
	}
	_ = g.Wait()
}

type cachedFilesystemChecks struct {
	fsops.Backend
	mu    sync.Mutex
	same  map[[2]string]bool
	stats map[string]statAnswer
}

type statAnswer struct {
	info *fsops.LstatInfo
	err  error
}

func newCachedFilesystemChecks(backend fsops.Backend) *cachedFilesystemChecks {
	return &cachedFilesystemChecks{
		Backend: backend,
		same:    make(map[[2]string]bool),
		stats:   make(map[string]statAnswer),
	}
}

func (c *cachedFilesystemChecks) SameFilesystem(ctx context.Context, sourceFile, dir string) (bool, error) {
	key := [2]string{c.Paths().Dir(sourceFile), dir}
	c.mu.Lock()
	same, ok := c.same[key]
	missing := c.stats[dir]
	c.mu.Unlock()
	if ok {
		return same, nil
	}
	if errors.Is(missing.err, fs.ErrNotExist) {
		return false, missing.err
	}
	same, err := c.Backend.SameFilesystem(ctx, sourceFile, dir)
	if err == nil {
		c.mu.Lock()
		c.same[key] = same
		c.mu.Unlock()
	}
	return same, err
}

func (c *cachedFilesystemChecks) Stat(ctx context.Context, path string) (*fsops.LstatInfo, error) {
	c.mu.Lock()
	answer, ok := c.stats[path]
	c.mu.Unlock()
	if ok {
		return answer.info, answer.err
	}
	info, err := c.Backend.Stat(ctx, path)
	if !errors.Is(err, fsops.ErrConnectionLost) {
		c.mu.Lock()
		c.stats[path] = statAnswer{info, err}
		c.mu.Unlock()
	}
	return info, err
}

func (s *Service) linkSavePathPreview(
	ctx context.Context,
	backend fsops.Backend,
	instance *models.Instance,
	route *candidateRoute,
	incoming singleAddTorrent,
	target *qbt.Torrent,
	targetFiles qbt.TorrentFiles,
) (string, bool) {
	candidate := CrossSeedCandidate{InstanceID: instance.ID, InstanceName: instance.Name, route: route}
	plan, err := s.planSingleAddTree(ctx, backend, instance, candidate, incoming,
		selectExistingSourceFiles(incoming.files, targetFiles), targetFiles, target.SavePath)
	if err != nil {
		return "", false
	}
	if route != nil {
		if mapped, ok := crossseedroute.ToCrossSeed(backend.Paths(), route.rows, plan.BaseDir, plan.SavePath); ok {
			return mapped, true
		}
	}
	return plan.SavePath, true
}
