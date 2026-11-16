// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseed"
)

// matchSharedPath is the match type of the delete dialog that puts a torrent
// in the Shared-file group: it reads a file at the same path. Over SSH the
// dialog finds no hardlink, so a cross-seed on links stays on the source, and
// its links keep the data.
const matchSharedPath = "content_path"

// CrossSeeder adds a torrent through the existing routed cross-seed add:
// on the Cross-seed instance of the instance that holds the matched torrent.
type CrossSeeder interface {
	CrossSeed(ctx context.Context, req *crossseed.CrossSeedRequest) (*crossseed.CrossSeedResponse, error)
}

// RouteReader reads the route of a Source instance to its Cross-seed
// instance, nil when it has none.
type RouteReader interface {
	Get(ctx context.Context, sourceInstanceID int) (*models.CrossSeedRoute, error)
}

// groupOf returns the Shared-file group of first on its instance, first
// first, each member read fresh and checked like first.
func (s *Service) groupOf(ctx context.Context, instanceID int, first *source) ([]*source, error) {
	group := []*source{first}
	seen := map[string]bool{strings.ToLower(first.torrent.Hash): true}
	for frontier := []string{first.torrent.Hash}; len(frontier) > 0; {
		res, err := s.deps.Groups.FindDeleteMatches(ctx, map[int][]string{instanceID: frontier})
		if err != nil {
			return nil, fmt.Errorf("find the torrents that share the files: %w", err)
		}
		frontier = nil
		for _, m := range res.Matches {
			hash := strings.ToLower(m.Hash)
			if m.InstanceID != instanceID || seen[hash] || m.MatchType != matchSharedPath {
				continue
			}
			seen[hash] = true
			member, err := s.readSource(ctx, instanceID, hash)
			if err != nil {
				return nil, err
			}
			if member == nil {
				continue
			}
			if err := member.movable(); err != nil {
				return nil, refuse("%s shares the files of this torrent: %v", m.Name, err)
			}
			group = append(group, member)
			frontier = append(frontier, hash)
		}
	}
	return group, nil
}

// members places each torrent of the group on the target. The group keeps
// its folders: placement.SavePath is the target folder of the top folder of
// the group. At a target with a Cross-seed instance, a group whose torrents
// hold the same files sends all but the Group representative there.
func (s *Service) members(ctx context.Context, from, target *models.Instance, group []*source, placement Placement) ([]Member, error) {
	backend, err := s.deps.Files.GetBackend(ctx, from.ID)
	if err != nil {
		return nil, fmt.Errorf("read the files of %s: %w", from.Name, err)
	}
	targetFiles, err := s.deps.Files.GetBackend(ctx, target.ID)
	if err != nil {
		return nil, fmt.Errorf("read the files of %s: %w", target.Name, err)
	}
	d, td := backend.Paths(), targetFiles.Paths()
	saves := make([]string, len(group))
	for i, src := range group {
		saves[i] = src.torrent.SavePath
	}
	top := topFolder(d, saves)

	var crossSeed *models.Instance
	if len(group) > 1 && sameFiles(d, group) {
		if crossSeed, err = s.crossSeedInstance(ctx, target.ID); err != nil {
			return nil, err
		}
	}
	representative := slices.MinFunc(group, func(a, b *source) int {
		return qbittorrent.CompareRepresentative(from.ID, &a.torrent, from.ID, &b.torrent)
	})

	members := make([]Member, 0, len(group))
	for i, src := range group {
		rel, err := d.Rel(top, d.Clean(src.torrent.SavePath))
		if err != nil {
			return nil, fmt.Errorf("place %s: %w", src.torrent.Name, err)
		}
		m := Member{
			Hash:             strings.ToLower(src.torrent.Hash),
			Name:             src.torrent.Name,
			SourceSavePath:   src.torrent.SavePath,
			TargetInstanceID: target.ID,
			Placement:        placement,
		}
		m.Placement.SavePath = td.Join(placement.SavePath, td.FromSlash(d.ToSlash(rel)))
		lands := target
		if crossSeed != nil && src != representative {
			lands = crossSeed
			m.TargetInstanceID = crossSeed.ID
			m.Placement.Category, m.Placement.Tags = "", nil
		}
		if i > 0 || lands != target {
			if onTarget, err := s.has(ctx, lands.ID, m.Hash); err != nil {
				return nil, err
			} else if onTarget {
				return nil, refuse("Already on the target: %s has %s, which shares the files of this torrent.", lands.Name, m.Name)
			}
		}
		members = append(members, m)
	}
	return members, nil
}

// crossSeedInstance returns the Cross-seed instance of the target, nil when
// it has none.
func (s *Service) crossSeedInstance(ctx context.Context, targetID int) (*models.Instance, error) {
	if s.deps.Routes == nil || s.deps.CrossSeeds == nil {
		return nil, nil
	}
	route, err := s.deps.Routes.Get(ctx, targetID)
	if err != nil {
		return nil, fmt.Errorf("read the cross-seed route of instance %d: %w", targetID, err)
	}
	if route == nil {
		return nil, nil
	}
	return s.instance(ctx, route.CrossSeedInstanceID)
}

func sameFiles(d fsops.PathDialect, group []*source) bool {
	filesOf := func(src *source) []string {
		save := d.Clean(src.torrent.SavePath)
		out := make([]string, 0, len(src.files))
		for _, f := range src.files {
			out = append(out, d.Join(save, d.FromSlash(f.Name)))
		}
		slices.Sort(out)
		return out
	}
	first := filesOf(group[0])
	for _, src := range group[1:] {
		if !slices.Equal(first, filesOf(src)) {
			return false
		}
	}
	return true
}

// topFolder is the deepest folder that holds each of dirs.
func topFolder(d fsops.PathDialect, dirs []string) string {
	top := d.Clean(dirs[0])
	for _, dir := range dirs[1:] {
		dir = d.Clean(dir)
		for !within(d, top, dir) {
			parent := d.Dir(top)
			if parent == top {
				break
			}
			top = parent
		}
	}
	return top
}

// within reports whether p is dir or below it.
func within(d fsops.PathDialect, dir, p string) bool {
	rel, err := d.Rel(dir, p)
	if err != nil {
		return false
	}
	rel = d.ToSlash(rel)
	return rel != ".." && !strings.HasPrefix(rel, "../")
}
