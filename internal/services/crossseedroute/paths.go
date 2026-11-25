// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseedroute

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"unicode/utf8"

	qbt "github.com/autobrr/go-qbittorrent"
	"golang.org/x/text/unicode/norm"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/pathcmp"
)

func ToCrossSeed(d fsops.PathDialect, rows []models.CrossSeedRouteRow, baseDir, p string) (string, bool) {
	key := fsops.CleanPath(d, strings.TrimSpace(baseDir))
	i := slices.IndexFunc(rows, func(row models.CrossSeedRouteRow) bool { return row.BaseDir == key })
	if i < 0 {
		return "", false
	}
	rel, err := d.Rel(key, fsops.CleanPath(d, p))
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+d.Separator()) {
		return "", false
	}
	mapped := rows[i].MappedPath
	switch {
	case mapped == "":
		return d.Join(key, rel), true
	case rel == ".":
		return mapped, true
	}
	return strings.TrimRight(mapped, `/\`) + "/" + d.ToSlash(rel), true
}

func FromCrossSeed(d fsops.PathDialect, rows []models.CrossSeedRouteRow, p string) (string, int) {
	raw := pathcmp.NormalizePath(p)
	reported := nfc(raw)
	if reported == "" {
		return "", -1
	}
	for i, row := range rows {
		target := row.MappedPath
		if target == "" {
			target = row.BaseDir
		}
		rest, ok := under(crossSeedPath(target), reported)
		if !ok {
			continue
		}
		if rest == "" {
			return row.BaseDir, i
		}
		if reported != raw {
			head := raw
			for range strings.Count(rest, "/") + 1 {
				head, _, _ = strings.CutLast(head, "/")
			}
			rest = raw[len(head)+1:]
		}
		return d.Join(row.BaseDir, d.FromSlash(rest)), i
	}
	return "", -1
}

func crossSeedPath(p string) string {
	return nfc(pathcmp.NormalizePath(p))
}

func nfc(p string) string {
	for i := range len(p) {
		if p[i] >= utf8.RuneSelf {
			if utf8.ValidString(p) && !norm.NFC.IsNormalString(p) {
				return norm.NFC.String(p)
			}
			break
		}
	}
	return p
}

func under(dir, p string) (string, bool) {
	equal := func(a, b string) bool { return a == b }
	if pathcmp.IsWindowsDriveAbs(dir) {
		equal = strings.EqualFold
	}
	if equal(p, dir) {
		return "", true
	}
	prefix := dir
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	if len(p) > len(prefix) && equal(p[:len(prefix)], prefix) {
		return p[len(prefix):], true
	}
	return "", false
}

type TorrentReader interface {
	GetAllTorrentRefs(ctx context.Context, instanceID int) ([]*qbt.Torrent, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
}

type RoutedTorrents struct {
	Torrents []qbt.Torrent
	Files    map[string]qbt.TorrentFiles

	crossSeedInstanceID int
}

func (s *Service) LoadRouted(ctx context.Context, read TorrentReader, sourceID int) (*RoutedTorrents, error) {
	routed, err := s.ListRouted(ctx, read, sourceID)
	if err != nil || routed == nil {
		return nil, err
	}
	if err := routed.LoadFiles(ctx, read); err != nil {
		return nil, err
	}
	return routed, nil
}

func (s *Service) ListRouted(ctx context.Context, read TorrentReader, sourceID int) (*RoutedTorrents, error) {
	route, err := s.Get(ctx, sourceID)
	if err != nil || route == nil {
		return nil, err
	}
	d, all, err := s.readCrossSeed(ctx, read, route)
	if err != nil {
		return nil, err
	}
	routed := &RoutedTorrents{crossSeedInstanceID: route.CrossSeedInstanceID}
	for _, shared := range all {
		savePath, row := FromCrossSeed(d, route.Rows, shared.SavePath)
		if row < 0 {
			continue
		}
		torrent := *shared
		torrent.SavePath = savePath
		torrent.ContentPath, _ = FromCrossSeed(d, route.Rows, torrent.ContentPath)
		routed.Torrents = append(routed.Torrents, torrent)
	}
	return routed, nil
}

func (r *RoutedTorrents) LoadFiles(ctx context.Context, read TorrentReader) error {
	hashes := make([]string, len(r.Torrents))
	for i := range r.Torrents {
		hashes[i] = r.Torrents[i].Hash
	}
	files, err := read.GetTorrentFilesBatch(ctx, r.crossSeedInstanceID, hashes)
	if err != nil {
		return fmt.Errorf("%w: read the file lists of the Cross-seed instance: %w", ErrUnavailable, err)
	}
	for _, hash := range hashes {
		if _, ok := files[hash]; !ok {
			return fmt.Errorf("%w: read the file list of torrent %s on the Cross-seed instance", ErrUnavailable, hash)
		}
	}
	r.Files = files
	return nil
}

func (s *Service) CountRouted(ctx context.Context, read TorrentReader, route *models.CrossSeedRoute) ([]int, error) {
	d, all, err := s.readCrossSeed(ctx, read, route)
	if err != nil {
		return nil, err
	}
	counts := make([]int, len(route.Rows))
	for _, torrent := range all {
		if _, row := FromCrossSeed(d, route.Rows, torrent.SavePath); row >= 0 {
			counts[row]++
		}
	}
	return counts, nil
}

func (s *Service) readCrossSeed(ctx context.Context, read TorrentReader, route *models.CrossSeedRoute) (_ fsops.PathDialect, _ []*qbt.Torrent, err error) {
	defer func() {
		if err != nil && ctx.Err() != nil && !errors.Is(err, ErrUnavailable) {
			err = fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
	}()
	if err := checkMappedPaths(route.Rows, false); err != nil {
		return nil, nil, err
	}
	if err := s.CheckAvailable(ctx, route.CrossSeedInstanceID, false); err != nil {
		return nil, nil, err
	}
	backend, _, err := s.backends.Resolve(ctx, route.SourceInstanceID)
	if err != nil {
		return nil, nil, err
	}
	all, err := read.GetAllTorrentRefs(ctx, route.CrossSeedInstanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read the torrents of the Cross-seed instance: %w", ErrUnavailable, err)
	}
	return backend.Paths(), all, nil
}
