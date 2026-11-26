// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseedroute

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/linktree"
	"github.com/autobrr/qui/pkg/pathcmp"
)

var ErrUnavailable = errors.New("instance unavailable")

type RuleError struct {
	Message string
	Err     error
}

func (e *RuleError) Error() string { return e.Message }
func (e *RuleError) Unwrap() error { return e.Err }

func broken(format string, args ...any) error {
	return &RuleError{Message: fmt.Sprintf(format, args...)}
}

type instanceGetter interface {
	Get(ctx context.Context, id int) (*models.Instance, error)
}

type Service struct {
	*models.CrossSeedRouteStore
	instances instanceGetter
	clients   *qbittorrent.ClientPool
	backends  *fsops.Pool
	saveMu    sync.Mutex
}

func NewService(store *models.CrossSeedRouteStore, instances instanceGetter, clients *qbittorrent.ClientPool, backends *fsops.Pool) *Service {
	return &Service{CrossSeedRouteStore: store, instances: instances, clients: clients, backends: backends}
}

func (s *Service) CheckAvailable(ctx context.Context, instanceID int, forListing bool) error {
	instance, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return err
	}
	if !instance.IsActive {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, instance.Name, qbittorrent.ErrInstanceDisabled)
	}
	loginTimeout := 60 * time.Second
	if deadline, ok := ctx.Deadline(); ok {
		loginTimeout = time.Until(deadline)
	}
	client, err := s.clients.GetClientWithTimeout(ctx, instanceID, loginTimeout)
	if err != nil {
		return fmt.Errorf("%w: %s: %w", ErrUnavailable, instance.Name, err)
	}
	if !forListing {
		return nil
	}
	if err := client.EnsureCapabilities(ctx); err != nil {
		return fmt.Errorf("%w: %s: read the qBittorrent version: %w", ErrUnavailable, instance.Name, err)
	}
	if !client.Supports(qbittorrent.CapPathAutocomplete) {
		return fmt.Errorf("%w: %s runs a qBittorrent version older than 5.0", ErrUnavailable, instance.Name)
	}
	return nil
}

func (s *Service) Get(ctx context.Context, sourceInstanceID int) (*models.CrossSeedRoute, error) {
	route, err := s.CrossSeedRouteStore.Get(ctx, sourceInstanceID)
	if err != nil || route == nil {
		return route, err
	}
	source, err := s.instances.Get(ctx, sourceInstanceID)
	if err != nil {
		return nil, err
	}
	backend, _, err := s.backends.Resolve(ctx, sourceInstanceID)
	if err != nil {
		return nil, err
	}
	for _, key := range BaseDirKeys(backend.Paths(), source.HardlinkBaseDir) {
		if !slices.ContainsFunc(route.Rows, func(row models.CrossSeedRouteRow) bool { return row.BaseDir == key }) {
			route.Rows = append(route.Rows, models.CrossSeedRouteRow{BaseDir: key})
		}
	}
	return route, nil
}

type HashFinder interface {
	HasTorrentByAnyHash(ctx context.Context, instanceID int, hashes []string) (*qbt.Torrent, bool, error)
}

func (s *Service) FindLinked(ctx context.Context, finder HashFinder, instanceID int, hashes []string) (*qbt.Torrent, string, error) {
	route, err := s.CrossSeedRouteStore.Get(ctx, instanceID)
	if err != nil {
		return nil, "", fmt.Errorf("read cross-seed route: %w", err)
	}
	if route != nil {
		torrent, name, err := s.findOn(ctx, finder, route.CrossSeedInstanceID, hashes)
		if errors.Is(err, ErrUnavailable) {
			return nil, "", CrossSeedUnavailable(err)
		}
		if torrent != nil || err != nil {
			return torrent, "Cross-seed instance " + name, err
		}
	}

	sources, err := s.ListSources(ctx, instanceID)
	if err != nil {
		return nil, "", fmt.Errorf("read cross-seed routes: %w", err)
	}
	for _, sourceID := range sources {
		torrent, name, err := s.findOn(ctx, finder, sourceID, hashes)
		switch {
		case errors.Is(err, qbittorrent.ErrInstanceDisabled):
		case errors.Is(err, ErrUnavailable):
			return nil, "", fmt.Errorf("%w. Enable %s or clear its cross-seed route", err, name)
		case torrent != nil || err != nil:
			return torrent, "Source instance " + name, err
		}
	}
	return nil, "", nil
}

func (s *Service) findOn(ctx context.Context, finder HashFinder, instanceID int, hashes []string) (*qbt.Torrent, string, error) {
	instance, err := s.instances.Get(ctx, instanceID)
	if err != nil {
		return nil, "", err
	}
	if err := s.CheckAvailable(ctx, instanceID, false); err != nil {
		return nil, instance.Name, err
	}
	torrent, _, err := finder.HasTorrentByAnyHash(ctx, instanceID, hashes)
	if err != nil {
		return nil, instance.Name, fmt.Errorf("%w: %s: %w", ErrUnavailable, instance.Name, err)
	}
	return torrent, instance.Name, nil
}

func (s *Service) Check(ctx context.Context, route *models.CrossSeedRoute) error {
	_, err := s.check(ctx, route)
	return err
}

func (s *Service) Save(ctx context.Context, route *models.CrossSeedRoute) (*models.CrossSeedRoute, error) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	rows, err := s.check(ctx, route)
	if err != nil {
		return nil, err
	}
	saved := &models.CrossSeedRoute{SourceInstanceID: route.SourceInstanceID, CrossSeedInstanceID: route.CrossSeedInstanceID, Rows: rows}
	if err := s.Upsert(ctx, saved); err != nil {
		return nil, err
	}
	return saved, nil
}

func (s *Service) check(ctx context.Context, route *models.CrossSeedRoute) ([]models.CrossSeedRouteRow, error) {
	source, err := s.instances.Get(ctx, route.SourceInstanceID)
	if err != nil {
		return nil, err
	}
	crossSeed, err := s.instances.Get(ctx, route.CrossSeedInstanceID)
	if err != nil {
		return nil, err
	}

	if !source.UseHardlinks && !source.UseReflinks || len(linktree.ParseBaseDirs(source.HardlinkBaseDir)) == 0 {
		return nil, broken("%s must use hardlink or reflink mode with at least one base folder", source.Name)
	}
	switch {
	case models.FilesystemAccessMode(source) == models.FilesystemModeRemote:
		return nil, broken("%s is a remote instance and cannot route its cross-seeds", source.Name)
	case !models.FilesystemCapabilitiesOf(source).Write || models.FilesystemAccessMode(source) != models.FilesystemModeLocal:
		return nil, broken("%s needs Local Filesystem Access to route its cross-seeds", source.Name)
	}
	if crossSeed.ID == source.ID {
		return nil, broken("%s cannot be its own Cross-seed instance", source.Name)
	}
	if own, err := s.CrossSeedRouteStore.Get(ctx, crossSeed.ID); err != nil {
		return nil, err
	} else if own != nil {
		return nil, broken("%s routes its own cross-seeds, so it cannot be a Cross-seed instance", crossSeed.Name)
	}
	sources, err := s.ListSources(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	if len(sources) > 0 {
		return nil, broken("%s is the Cross-seed instance of another instance, so it cannot route its cross-seeds", source.Name)
	}
	if models.FilesystemAccessMode(crossSeed) == models.FilesystemModeRemote {
		return nil, broken("%s is a remote instance and cannot be a Cross-seed instance", crossSeed.Name)
	}
	if err := s.CheckAvailable(ctx, crossSeed.ID, true); err != nil {
		if !errors.Is(err, ErrUnavailable) {
			return nil, err
		}
		return nil, CrossSeedUnavailable(err)
	}

	backend, _, err := s.backends.Resolve(ctx, source.ID)
	if err != nil {
		return nil, err
	}
	rows, err := cleanRows(backend.Paths(), source, route.Rows)
	if err != nil {
		return nil, err
	}
	return rows, checkMappedPaths(rows, models.FilesystemAccessMode(crossSeed) == models.FilesystemModeLocal)
}

func CrossSeedUnavailable(err error) error {
	return &RuleError{Message: "Cross-seed instance unavailable: " + strings.TrimPrefix(err.Error(), ErrUnavailable.Error()+": "), Err: err}
}

func cleanRows(d fsops.PathDialect, source *models.Instance, in []models.CrossSeedRouteRow) ([]models.CrossSeedRouteRow, error) {
	keys := BaseDirKeys(d, source.HardlinkBaseDir)
	cleaned := make([]models.CrossSeedRouteRow, len(in))
	for i, row := range in {
		key := fsops.CleanPath(d, strings.TrimSpace(row.BaseDir))
		if !slices.Contains(keys, key) {
			return nil, broken("%s is not a base folder of %s", key, source.Name)
		}
		cleaned[i] = models.CrossSeedRouteRow{BaseDir: key, MappedPath: strings.TrimSpace(row.MappedPath)}
	}
	return rowsFor(cleaned, keys), nil
}

func checkMappedPaths(rows []models.CrossSeedRouteRow, crossSeedLocal bool) error {
	targets := make([]string, len(rows))
	for i, row := range rows {
		target := row.BaseDir
		if row.MappedPath != "" {
			if !pathcmp.IsAbsolute(row.MappedPath) {
				return broken("mapped path %s for %s is not an absolute path", row.MappedPath, row.BaseDir)
			}
			target = row.MappedPath
		}
		targets[i] = crossSeedKey(target)
	}
	for i := range targets {
		for j := i + 1; j < len(targets); j++ {
			if nested(targets[i], targets[j]) || nested(targets[j], targets[i]) {
				return broken("%s and %s map to the same or nested folders on the Cross-seed instance", rows[i].BaseDir, rows[j].BaseDir)
			}
		}
	}
	if crossSeedLocal {
		if row, moved := movedRow(rows); moved {
			return broken("the Cross-seed instance has Local Filesystem Access, so %s must keep the same path", row.BaseDir)
		}
	}
	return nil
}

func movedRow(rows []models.CrossSeedRouteRow) (models.CrossSeedRouteRow, bool) {
	for _, row := range rows {
		if row.MappedPath != "" && crossSeedKey(row.MappedPath) != crossSeedKey(row.BaseDir) {
			return row, true
		}
	}
	return models.CrossSeedRouteRow{}, false
}

func (s *Service) CheckInstanceChange(ctx context.Context, before, after *models.Instance) error {
	routes, err := s.List(ctx)
	if err != nil {
		return err
	}
	for _, route := range routes {
		var rules func(*models.Instance) []string
		switch after.ID {
		case route.SourceInstanceID:
			crossSeed, err := s.instances.Get(ctx, route.CrossSeedInstanceID)
			if err != nil {
				return err
			}
			rules = func(source *models.Instance) []string { return sourceChangeBreaks(route, crossSeed.Name, source) }
		case route.CrossSeedInstanceID:
			source, err := s.instances.Get(ctx, route.SourceInstanceID)
			if err != nil {
				return err
			}
			rules = func(crossSeed *models.Instance) []string { return crossSeedChangeBreaks(route, source.Name, crossSeed) }
		default:
			continue
		}
		was := rules(before)
		for _, msg := range rules(after) {
			if !slices.Contains(was, msg) {
				return &RuleError{Message: msg}
			}
		}
	}
	return nil
}

func sourceChangeBreaks(route *models.CrossSeedRoute, crossSeedName string, source *models.Instance) []string {
	keys := BaseDirKeys(fsops.HostPaths, source.HardlinkBaseDir)
	var breaks []string
	if !source.UseHardlinks && !source.UseReflinks || len(keys) == 0 {
		breaks = append(breaks, fmt.Sprintf("This instance routes its cross-seeds to %s. Clear the route before you turn off hardlink and reflink mode or remove all base folders.", crossSeedName))
	}
	if models.FilesystemAccessMode(source) != models.FilesystemModeLocal {
		breaks = append(breaks, fmt.Sprintf("This instance routes its cross-seeds to %s. Clear the route before you turn off Local Filesystem Access.", crossSeedName))
	}
	if err := checkMappedPaths(rowsFor(route.Rows, keys), false); err != nil {
		breaks = append(breaks, fmt.Sprintf("This instance routes its cross-seeds to %s, and %s. Clear the route before you add this base folder.", crossSeedName, err.Error()))
	}
	return breaks
}

func crossSeedChangeBreaks(route *models.CrossSeedRoute, sourceName string, crossSeed *models.Instance) []string {
	switch models.FilesystemAccessMode(crossSeed) {
	case models.FilesystemModeRemote:
		return []string{fmt.Sprintf("%s routes its cross-seeds to this instance, so this instance cannot be a remote instance. Clear the route of %s first.", sourceName, sourceName)}
	case models.FilesystemModeLocal:
		if row, moved := movedRow(route.Rows); moved {
			return []string{fmt.Sprintf("%s routes its cross-seeds to this instance and maps %s to %s. Clear the route of %s before you turn on Local Filesystem Access.", sourceName, row.BaseDir, row.MappedPath, sourceName)}
		}
	case models.FilesystemModeNone:
	}
	return nil
}

func rowsFor(stored []models.CrossSeedRouteRow, keys []string) []models.CrossSeedRouteRow {
	rows := make([]models.CrossSeedRouteRow, len(keys))
	for i, key := range keys {
		rows[i] = models.CrossSeedRouteRow{BaseDir: key}
		for _, row := range stored {
			if row.BaseDir == key {
				rows[i].MappedPath = row.MappedPath
			}
		}
	}
	return rows
}

func (s *Service) Delete(ctx context.Context, sourceInstanceID int) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	return s.CrossSeedRouteStore.Delete(ctx, sourceInstanceID)
}

func (s *Service) DropRemovedBaseDirs(ctx context.Context, sourceInstanceID int) error {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	route, err := s.CrossSeedRouteStore.Get(ctx, sourceInstanceID)
	if err != nil || route == nil {
		return err
	}
	source, err := s.instances.Get(ctx, sourceInstanceID)
	if err != nil {
		return err
	}
	keys := BaseDirKeys(fsops.HostPaths, source.HardlinkBaseDir)
	kept := slices.DeleteFunc(slices.Clone(route.Rows), func(row models.CrossSeedRouteRow) bool { return !slices.Contains(keys, row.BaseDir) })
	if len(kept) == len(route.Rows) {
		return nil
	}
	route.Rows = kept
	return s.Upsert(ctx, route)
}

func BaseDirKeys(d fsops.PathDialect, configured string) []string {
	var keys []string
	for _, dir := range linktree.ParseBaseDirs(configured) {
		if key := fsops.CleanPath(d, dir); !slices.Contains(keys, key) {
			keys = append(keys, key)
		}
	}
	return keys
}

func crossSeedKey(p string) string {
	p = crossSeedPath(p)
	if pathcmp.IsWindowsDriveAbs(p) {
		return strings.ToLower(p)
	}
	return p
}

func nested(parent, child string) bool {
	return parent == child || strings.HasPrefix(child, strings.TrimSuffix(parent, "/")+"/")
}
