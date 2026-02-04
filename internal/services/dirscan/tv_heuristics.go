// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/autobrr/qui/pkg/stringutils"
)

type searcheeWorkItem struct {
	searchee *Searchee
	tvGroup  *tvGroupKey
}

type tvGroupKey struct {
	normalizedTitle string
	season          int
}

func buildSearcheeWorkItems(searchee *Searchee, parser *Parser) []searcheeWorkItem {
	tvGroups := groupTVEpisodes(searchee, parser)
	if len(tvGroups) == 0 {
		return []searcheeWorkItem{{searchee: searchee}}
	}

	items := make([]searcheeWorkItem, 0, len(tvGroups))
	for key, group := range tvGroups {
		if group == nil || len(group.episodeFiles) == 0 {
			continue
		}

		if seasonSearchee := group.buildSeasonSearchee(searchee); seasonSearchee != nil {
			k := key
			items = append(items, searcheeWorkItem{searchee: seasonSearchee, tvGroup: &k})
		}

		for _, ep := range group.episodeFiles {
			epSearchee := buildEpisodeSearchee(ep)
			if epSearchee == nil {
				continue
			}
			k := key
			items = append(items, searcheeWorkItem{searchee: epSearchee, tvGroup: &k})
		}
	}

	return items
}

type tvGroup struct {
	displayTitle    string
	normalizedTitle string
	season          int
	episodeFiles    []*ScannedFile
	parentDirs      []string
	mixedSeasonDir  bool
}

func (g *tvGroup) buildSeasonSearchee(original *Searchee) *Searchee {
	if g == nil || original == nil {
		return nil
	}
	if g.mixedSeasonDir {
		return nil
	}
	if g.season <= 0 {
		return nil
	}
	if len(g.episodeFiles) < 2 {
		return nil
	}
	if g.displayTitle == "" {
		return nil
	}

	seasonName := fmt.Sprintf("%s S%02d", g.displayTitle, g.season)

	files := make([]*ScannedFile, 0, len(original.Files))
	for _, f := range original.Files {
		if f == nil {
			continue
		}
		if g.isInGroupDir(f.Path) {
			files = append(files, f)
		}
	}

	if len(files) == 0 {
		return nil
	}

	return &Searchee{
		Name:   seasonName,
		Path:   original.Path,
		Files:  files,
		IsDisc: false,
	}
}

func (g *tvGroup) isInGroupDir(absPath string) bool {
	if g == nil {
		return false
	}
	clean := filepath.Clean(absPath)
	for _, dir := range g.parentDirs {
		dirClean := filepath.Clean(dir)
		if clean == dirClean {
			return true
		}
		if strings.HasPrefix(clean, dirClean+string(filepath.Separator)) {
			return true
		}
	}
	return false
}

func buildEpisodeSearchee(file *ScannedFile) *Searchee {
	if file == nil {
		return nil
	}
	base := filepath.Base(file.Path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" {
		return nil
	}

	sf := *file
	sf.RelPath = base

	return &Searchee{
		Name:   name,
		Path:   filepath.Dir(file.Path),
		Files:  []*ScannedFile{&sf},
		IsDisc: false,
	}
}

func groupTVEpisodes(searchee *Searchee, parser *Parser) map[tvGroupKey]*tvGroup {
	if searchee == nil || parser == nil {
		return nil
	}

	// Track which parent directories contain episodes from multiple seasons.
	seasonsByDir := make(map[string]map[int]struct{})

	groups := make(map[tvGroupKey]*tvGroup)
	for _, f := range searchee.Files {
		key, group, parentDir, ok := buildTVGroupEntry(f, parser, groups)
		if !ok {
			continue
		}

		group.episodeFiles = append(group.episodeFiles, f)
		group.parentDirs = append(group.parentDirs, parentDir)
		recordSeasonInDir(seasonsByDir, parentDir, key.season)
	}

	finalizeTVGroups(groups, seasonsByDir)

	return groups
}

func buildTVGroupEntry(file *ScannedFile, parser *Parser, groups map[tvGroupKey]*tvGroup) (key tvGroupKey, group *tvGroup, parentDir string, ok bool) {
	key, displayTitle, parentDir, ok := parseTVEpisodeKey(file, parser)
	if !ok {
		return tvGroupKey{}, nil, "", false
	}

	group, ok = groups[key]
	if !ok {
		group = &tvGroup{displayTitle: displayTitle, normalizedTitle: key.normalizedTitle, season: key.season}
		groups[key] = group
	}

	return key, group, parentDir, true
}

func parseTVEpisodeKey(file *ScannedFile, parser *Parser) (key tvGroupKey, displayTitle, parentDir string, ok bool) {
	if file == nil || parser == nil || !isVideoPath(file.Path) {
		return tvGroupKey{}, "", "", false
	}

	base := filepath.Base(file.Path)
	name := strings.TrimSuffix(base, filepath.Ext(base))
	if name == "" {
		return tvGroupKey{}, "", "", false
	}

	meta := parser.Parse(name)
	if meta == nil || meta.Release == nil || !meta.IsTV || meta.Season == nil || meta.Episode == nil {
		return tvGroupKey{}, "", "", false
	}

	displayTitle = meta.Title
	if displayTitle == "" {
		displayTitle = name
	}

	season := *meta.Season
	key = tvGroupKey{normalizedTitle: stringutils.NormalizeForMatching(displayTitle), season: season}
	parentDir = filepath.Dir(file.Path)
	return key, displayTitle, parentDir, true
}

func recordSeasonInDir(seasonsByDir map[string]map[int]struct{}, parentDir string, season int) {
	if seasonsByDir == nil || parentDir == "" || season <= 0 {
		return
	}

	seasons, ok := seasonsByDir[parentDir]
	if !ok {
		seasons = make(map[int]struct{})
		seasonsByDir[parentDir] = seasons
	}
	seasons[season] = struct{}{}
}

func finalizeTVGroups(groups map[tvGroupKey]*tvGroup, seasonsByDir map[string]map[int]struct{}) {
	for _, group := range groups {
		if group == nil {
			continue
		}

		uniqueDirs := make(map[string]struct{})
		for _, dir := range group.parentDirs {
			uniqueDirs[dir] = struct{}{}
			if seasons, ok := seasonsByDir[dir]; ok && len(seasons) > 1 {
				group.mixedSeasonDir = true
			}
		}

		group.parentDirs = group.parentDirs[:0]
		for dir := range uniqueDirs {
			group.parentDirs = append(group.parentDirs, dir)
		}
	}
}
