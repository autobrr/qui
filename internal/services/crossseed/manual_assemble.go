// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/releases"
)

// ManualAssembleRequest selects episode torrents from one instance.
// An empty selection asks the check endpoint to suggest targets.
type ManualAssembleRequest struct {
	InstanceID   int      `json:"instance_id"`
	TorrentData  string   `json:"torrent_data"`
	TargetHashes []string `json:"target_hashes"`
	Category     string   `json:"category"`
	Tags         []string `json:"tags"`
}

type ManualAssembleTarget struct {
	Hash   string `json:"hash"`
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type ManualAssembleResponse struct {
	Ready           bool                   `json:"ready"`
	Applied         bool                   `json:"applied"`
	Reason          string                 `json:"reason"`
	Message         string                 `json:"message"`
	Targets         []ManualAssembleTarget `json:"targets"`
	MatchedEpisodes int                    `json:"matched_episodes"`
	TotalEpisodes   int                    `json:"total_episodes"`
	Coverage        float64                `json:"coverage"`
	LinkedBytes     int64                  `json:"linked_bytes"`
	MissingBytes    int64                  `json:"missing_bytes"`
	Destination     string                 `json:"destination"`
	DefaultCategory string                 `json:"default_category"`
	LinkMode        string                 `json:"link_mode"`
}

func manualAssemblyUnavailableReason(inst *models.Instance) string {
	if len(filterLinkEligible([]*models.Instance{inst})) != 0 {
		return ""
	}
	switch {
	case !inst.HasLocalFilesystemAccess:
		return "no_filesystem_access"
	case !inst.UseHardlinks && !inst.UseReflinks:
		return "no_link_mode"
	default:
		return "no_base_directory"
	}
}

type manualAssemblePlan struct {
	response *ManualAssembleResponse
	prep     *seasonPackPrep
	instance *models.Instance
	build    *seasonPackPlanBuild
	episodes map[episodeIdentity]episodeMatch
}

// CheckManualAssemble resolves the selected files without creating links or adding a torrent.
func (s *Service) CheckManualAssemble(ctx context.Context, req *ManualAssembleRequest) (*ManualAssembleResponse, error) {
	plan, err := s.planManualAssemble(ctx, req)
	if err != nil {
		return nil, err
	}
	return plan.response, nil
}

func (s *Service) ApplyManualAssemble(ctx context.Context, req *ManualAssembleRequest) (*ManualAssembleResponse, error) {
	if len(normalizedHashes(req.TargetHashes...)) < 2 {
		return nil, fmt.Errorf("%w: assembly requires at least two target hashes", ErrInvalidRequest)
	}
	planned, err := s.planManualAssemble(ctx, req)
	if err != nil {
		return nil, err
	}
	resp, prep, inst := planned.response, planned.prep, planned.instance
	if !resp.Ready {
		s.recordApplyRun(ctx, prep.meta.Name, resp.Reason, resp.Message, inst.ID, resp.MatchedEpisodes, resp.TotalEpisodes, resp.Coverage, resp.LinkMode)
		return resp, nil
	}
	if _, found, err := s.syncManager.HasTorrentByAnyHash(ctx, inst.ID, collectHashes(prep.meta)); err != nil {
		return nil, err
	} else if found {
		resp.Ready, resp.Reason = false, "already_exists"
		s.recordApplyRun(ctx, prep.meta.Name, resp.Reason, "", inst.ID, resp.MatchedEpisodes, resp.TotalEpisodes, resp.Coverage, resp.LinkMode)
		return resp, nil
	}
	if err := s.createSeasonPackTree(ctx, inst, planned.build, resp.LinkMode); err != nil {
		resp.Ready, resp.Reason, resp.Message = false, "link_failed", err.Error()
		s.recordApplyRun(ctx, prep.meta.Name, resp.Reason, resp.Message, inst.ID, resp.MatchedEpisodes, resp.TotalEpisodes, resp.Coverage, resp.LinkMode)
		return resp, nil
	}
	result, err := s.addSeasonPack(ctx, prep, inst, planned.build, planned.episodes, req.Category, req.Tags, resp.LinkMode, prep.meta.Name)
	if err != nil {
		return nil, err
	}
	resp.Applied, resp.Reason, resp.Message = result.Applied, result.Reason, result.Message
	return resp, nil
}

func (s *Service) planManualAssemble(ctx context.Context, req *ManualAssembleRequest) (*manualAssemblePlan, error) {
	if req.InstanceID <= 0 {
		return nil, fmt.Errorf("%w: instance_id must be a positive integer", ErrInvalidRequest)
	}
	torrentBytes, err := s.decodeTorrentData(req.TorrentData)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid torrent data: %w", ErrInvalidRequest, err)
	}
	meta, err := ParseTorrentMetadataWithInfo(torrentBytes)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid torrent: %w", ErrInvalidRequest, err)
	}
	packRelease := s.releaseCache.Parse(meta.Name)
	if !isTVSeasonPack(packRelease) {
		return nil, fmt.Errorf("%w: upload is not a season pack", ErrInvalidRequest)
	}
	inst, err := s.instanceStore.Get(ctx, req.InstanceID)
	if err != nil {
		return nil, err
	}
	if inst == nil {
		return nil, fmt.Errorf("%w: instance not found", ErrInvalidRequest)
	}
	settings, err := s.GetAutomationSettings(ctx)
	if err != nil {
		return nil, err
	}
	prep := &seasonPackPrep{
		manual: true, rejected: make(map[string]string), settings: settings,
		meta: meta, torrentBytes: torrentBytes, packRelease: packRelease,
		packEpisodes: extractPackEpisodes(meta.Files, packRelease),
	}
	prep.totalEpisodes = len(prep.packEpisodes)
	resp := &ManualAssembleResponse{
		Targets: []ManualAssembleTarget{}, TotalEpisodes: prep.totalEpisodes,
		LinkMode: determineLinkMode([]*models.Instance{inst}, inst.ID),
	}
	planned := &manualAssemblePlan{response: resp, prep: prep, instance: inst}
	for _, file := range meta.Files {
		resp.MissingBytes += file.Size
	}
	if prep.totalEpisodes == 0 {
		resp.Reason = "no_episode_files"
		return planned, nil
	}
	cached, err := s.syncManager.GetCachedInstanceTorrents(ctx, inst.ID)
	if err != nil {
		return nil, err
	}
	torrents := make(map[string]qbt.Torrent, len(cached))
	for _, view := range cached {
		if view.Torrent != nil {
			torrents[normalizeHash(view.Hash)] = *view.Torrent
		}
	}
	hashes := normalizedHashes(req.TargetHashes...)
	var candidates map[episodeIdentity][]episodeMatch
	if len(hashes) == 0 {
		proposals, err := s.ManualMatchProposals(ctx, inst.ID, torrentBytes, "")
		if err != nil {
			return nil, err
		}
		// Use the apply's file pairing, including its ignored sidecars.
		usableFiles := func(files qbt.TorrentFiles) qbt.TorrentFiles {
			return slices.DeleteFunc(slices.Clone(files), func(file qbt.TorrentFile) bool {
				return shouldIgnoreFile(file.Name, seasonPackNormalizer(s))
			})
		}
		uploadFiles := usableFiles(meta.Files)
		for _, proposal := range proposals.Proposals {
			files, err := s.syncManager.GetTorrentFilesBatch(ctx, inst.ID, []string{proposal.Hash})
			if err != nil {
				return nil, err
			}
			match := s.getMatchTypeWithReason(s.releaseCache.Parse(proposal.Name), packRelease, files[normalizeHash(proposal.Hash)], meta.Files, defaultSizeMismatchTolerancePercent)
			_, unmatched := matchSourceFilesToCandidates(uploadFiles, usableFiles(files[normalizeHash(proposal.Hash)]))
			if match.MatchType != "" && len(uploadFiles) > 0 && len(unmatched) == 0 {
				resp.Targets = append(resp.Targets, ManualAssembleTarget{Hash: proposal.Hash, Name: proposal.Name})
				resp.DefaultCategory = proposal.Category
				return planned, nil
			}
		}
		candidates = s.matchEpisodeCandidatesDetailed(cached, packRelease, prep.packEpisodes, settings, nil)
	} else {
		candidates = make(map[episodeIdentity][]episodeMatch)
		for _, hash := range hashes {
			torrent, found := torrents[hash]
			if !found {
				prep.rejected[hash] = "target_not_found"
				continue
			}
			if torrent.Progress < 1 {
				prep.rejected[hash] = "incomplete"
				continue
			}
			parsed := s.releaseCache.Parse(torrent.Name)
			if parsed == nil {
				prep.rejected[hash] = "no_episode_identity"
				continue
			}
			id := episodeIdentity{series: parsed.Series, episode: parsed.Episode}
			if releases.IsEpisodeRange(parsed) {
				eps := parsed.SeriesEpisodes()
				id = episodeIdentity{series: eps[0][0], episode: eps[0][1]}
			} else if id.episode <= 0 {
				prep.rejected[hash] = "no_episode_identity"
				continue
			}
			clone := *parsed
			id.series = cmp.Or(id.series, packRelease.Series)
			clone.Series = id.series
			candidates[id] = append(candidates[id], episodeMatch{
				manual: true, torrentHash: hash, contentPath: torrent.ContentPath,
				category: torrent.Category, release: &clone,
			})
		}
	}
	if reason := manualAssemblyUnavailableReason(inst); reason != "" {
		resp.Reason = reason
		return planned, nil
	}
	plan, episodes, planErr := s.planSeasonPack(ctx, prep, inst, candidates, resp.LinkMode, "")
	if planErr != nil && !errors.Is(planErr, errLayoutMismatch) {
		return nil, planErr
	}
	selected := make(map[string]struct{}, len(episodes))
	for _, episode := range episodes {
		selected[normalizeHash(episode.torrentHash)] = struct{}{}
	}
	if len(hashes) == 0 {
		hashes = slices.Sorted(maps.Keys(selected))
	}
	for _, hash := range hashes {
		reason := prep.rejected[hash]
		if _, ok := selected[hash]; !ok && reason == "" {
			reason = "not_paired"
		}
		resp.Targets = append(resp.Targets, ManualAssembleTarget{Hash: hash, Name: torrents[hash].Name, Reason: reason})
	}
	resp.MatchedEpisodes = len(episodes)
	resp.Coverage = float64(len(episodes)) / float64(prep.totalEpisodes)
	resp.DefaultCategory = s.resolveSeasonPackCategory(ctx, prep, "", episodes)
	if plan != nil {
		resp.LinkedBytes = plan.linkedBytes
		resp.MissingBytes = plan.totalBytes - plan.linkedBytes
		resp.Destination = plan.plan.RootDir
	}
	if planErr != nil {
		resp.Reason, resp.Message = "layout_mismatch", planErr.Error()
	} else {
		resp.Ready = true
	}
	planned.build, planned.episodes = plan, episodes
	return planned, nil
}
