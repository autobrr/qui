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

// ToCrossSeed maps p, a Source instance path under baseDir, to the path where
// the Cross-seed instance sees it. d is the Source instance's path dialect. It
// returns false when the route has no row for baseDir or p is not under it.
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

// FromCrossSeed maps p, a path that the Cross-seed instance reports, back to
// a Source instance path with the Source instance's dialect d. It also returns
// the index of the row that matched, or -1 when p is under no row.
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
			// NFC keeps each "/", so the last parts of raw hold rest as reported.
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

// crossSeedPath returns p from pathcmp.NormalizePath in NFC, the form of the
// base folder keys, so that a decomposed name matches its composed form.
func crossSeedPath(p string) string {
	return nfc(pathcmp.NormalizePath(p))
}

// nfc returns p in NFC. An ASCII path is already NFC, and the check of norm
// allocates.
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

// under returns the part of p after dir, both from crossSeedPath. The
// Cross-seed instance has no dialect, so only a drive-letter dir ignores case.
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

// TorrentReader reads an instance's torrents and their file lists, fresh or
// cached as the caller needs.
type TorrentReader interface {
	GetAllTorrents(ctx context.Context, instanceID int) ([]qbt.Torrent, error)
	GetTorrentFilesBatch(ctx context.Context, instanceID int, hashes []string) (map[string]qbt.TorrentFiles, error)
}

// RoutedTorrents are the Cross-seed instance's torrents under the rows of one
// route, in Source instance paths.
type RoutedTorrents struct {
	// Torrents have SavePath and ContentPath mapped back. ContentPath is empty
	// when it is under no row.
	Torrents []qbt.Torrent
	// Files holds the file lists by hash. The names stay relative to the save
	// path, as qBittorrent reports them. ListRouted leaves it nil.
	Files map[string]qbt.TorrentFiles

	crossSeedInstanceID int
}

// LoadRouted returns the torrents of sourceID's Cross-seed instance that are
// under a row of the route, or nil when sourceID has no route. When the
// Cross-seed instance is unavailable or a read fails, the error matches
// ErrUnavailable. When two rows map to the same or nested folders on the
// Cross-seed instance, the error is a *RuleError.
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

// ListRouted is LoadRouted without the file lists, for a caller that reads
// them only when the torrents changed.
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
	for _, torrent := range all {
		savePath, row := FromCrossSeed(d, route.Rows, torrent.SavePath)
		if row < 0 {
			continue
		}
		torrent.SavePath = savePath
		torrent.ContentPath, _ = FromCrossSeed(d, route.Rows, torrent.ContentPath)
		routed.Torrents = append(routed.Torrents, torrent)
	}
	return routed, nil
}

// LoadFiles reads the file lists of the torrents into Files. Errors are as
// for LoadRouted.
func (r *RoutedTorrents) LoadFiles(ctx context.Context, read TorrentReader) error {
	hashes := make([]string, len(r.Torrents))
	for i := range r.Torrents {
		hashes[i] = r.Torrents[i].Hash
	}
	files, err := read.GetTorrentFilesBatch(ctx, r.crossSeedInstanceID, hashes)
	if err != nil {
		return fmt.Errorf("%w: read the file lists of the Cross-seed instance: %w", ErrUnavailable, err)
	}
	// The reader leaves out a file list that it fails to read.
	for _, hash := range hashes {
		if _, ok := files[hash]; !ok {
			return fmt.Errorf("%w: read the file list of torrent %s on the Cross-seed instance", ErrUnavailable, hash)
		}
	}
	r.Files = files
	return nil
}

// CountRouted returns, for each row of route, the number of the Cross-seed
// instance's torrents whose save path is under it. Errors are as for
// LoadRouted.
func (s *Service) CountRouted(ctx context.Context, read TorrentReader, route *models.CrossSeedRoute) ([]int, error) {
	d, all, err := s.readCrossSeed(ctx, read, route)
	if err != nil {
		return nil, err
	}
	counts := make([]int, len(route.Rows))
	for i := range all {
		if _, row := FromCrossSeed(d, route.Rows, all[i].SavePath); row >= 0 {
			counts[row]++
		}
	}
	return counts, nil
}

// readCrossSeed reads the torrents of route's Cross-seed instance after the
// availability check, with the Source instance's path dialect.
func (s *Service) readCrossSeed(ctx context.Context, read TorrentReader, route *models.CrossSeedRoute) (_ fsops.PathDialect, _ []qbt.Torrent, err error) {
	defer func() {
		// A read that the deadline of ctx cut off means that the Cross-seed
		// instance did not answer in time.
		if err != nil && ctx.Err() != nil && !errors.Is(err, ErrUnavailable) {
			err = fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
	}()
	// A base folder added after the save can map to the folder of a saved
	// row, and a torrent under it must not count for the wrong row.
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
	all, err := read.GetAllTorrents(ctx, route.CrossSeedInstanceID)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: read the torrents of the Cross-seed instance: %w", ErrUnavailable, err)
	}
	return backend.Paths(), all, nil
}
