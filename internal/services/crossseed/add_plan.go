// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/moistari/rls"
)

type addPlanLayoutRank int

const (
	addPlanNoAlignment addPlanLayoutRank = iota
	addPlanFilenameAlignment
	addPlanRootAlignment
	addPlanShapeChange
	addPlanRootAndFilenameAlignment
)

type crossSeedAddPlan struct {
	torrent        qbt.Torrent
	files          qbt.TorrentFiles
	matchType      string
	matchScore     int
	sourceRoot     string
	candidateRoot  string
	contentLayout  string
	layoutRank     addPlanLayoutRank
	hasRootFolder  bool
	fileCount      int
	needsRootAlign bool
	needsFileAlign bool
	forceRecheck   bool
}

func buildCrossSeedAddPlan(
	torrent qbt.Torrent,
	files qbt.TorrentFiles,
	matchType string,
	sourceRelease *rls.Release,
	candidateRelease *rls.Release,
	sourceFiles qbt.TorrentFiles,
) crossSeedAddPlan {
	sourceRoot := detectCommonRoot(sourceFiles)
	candidateRoot := detectCommonRoot(files)
	needsRootAlign := sourceRoot != candidateRoot
	needsFileAlign := filesNeedRenaming(sourceFiles, files)
	sourceHasRoot := sourceRoot != ""
	candidateHasRoot := candidateRoot != ""

	// isEpisodeInPack deliberately keeps contentLayout from becoming "Subfolder"
	// when matchType is partial-in-pack, isTVEpisode(sourceRelease) is true, and
	// isTVSeasonPack(candidateRelease) is true. In that sourceRoot == "" and
	// candidateRoot != "" shape, the episode maps into the season-pack content path
	// instead of a qBittorrent-created subfolder, so classifying it as Subfolder
	// would misrepresent an episode-to-season-pack mapping.
	isEpisodeInPack := matchType == "partial-in-pack" &&
		isTVEpisode(sourceRelease) &&
		isTVSeasonPack(candidateRelease)
	isRootlessSingleFileIntoFolder := sourceRoot == "" && candidateRoot != "" && len(sourceFiles) == 1 && !isEpisodeInPack

	contentLayout := "Original"
	if sourceRoot == "" && candidateRoot != "" && !isEpisodeInPack && !isRootlessSingleFileIntoFolder {
		contentLayout = "Subfolder"
	} else if sourceRoot != "" && candidateRoot == "" {
		contentLayout = "NoSubfolder"
	}

	return crossSeedAddPlan{
		torrent:        torrent,
		files:          files,
		matchType:      matchType,
		matchScore:     matchTypePriority(matchType),
		sourceRoot:     sourceRoot,
		candidateRoot:  candidateRoot,
		contentLayout:  contentLayout,
		layoutRank:     rankAddPlanLayout(sourceHasRoot, candidateHasRoot, needsRootAlign, needsFileAlign),
		hasRootFolder:  candidateHasRoot,
		fileCount:      len(files),
		needsRootAlign: needsRootAlign,
		needsFileAlign: needsFileAlign,
		forceRecheck:   needsRootAlign || needsFileAlign,
	}
}

func rankAddPlanLayout(sourceHasRoot, candidateHasRoot, needsRootAlign, needsFileAlign bool) addPlanLayoutRank {
	if !needsRootAlign && !needsFileAlign {
		return addPlanNoAlignment
	}

	sameShape := sourceHasRoot == candidateHasRoot
	if sameShape && !needsRootAlign {
		return addPlanFilenameAlignment
	}
	if sameShape && !needsFileAlign {
		return addPlanRootAlignment
	}
	if sameShape {
		return addPlanRootAndFilenameAlignment
	}
	if !needsFileAlign {
		return addPlanShapeChange
	}
	return addPlanRootAndFilenameAlignment
}

func (p crossSeedAddPlan) betterThan(other crossSeedAddPlan, hasOther bool) bool {
	if !hasOther {
		return true
	}
	if p.matchScore != other.matchScore {
		return p.matchScore > other.matchScore
	}
	if p.layoutRank != other.layoutRank {
		return p.layoutRank < other.layoutRank
	}
	if p.hasRootFolder != other.hasRootFolder {
		return p.hasRootFolder
	}
	return p.fileCount > other.fileCount
}
