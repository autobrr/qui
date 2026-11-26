// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog/log"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
)

const crossSeedUnavailableStatus = "crossseed_instance_unavailable"

const crossSeedUnavailablePrefix = "Cross-seed instance unavailable: "

const crossSeedCannotSeeStatus = "crossseed_instance_cannot_see_data"

const (
	visibleRetries    = 3
	visibleRetryDelay = 5 * time.Second
)

type failedRouteDirsKey struct{}

type failedRouteDir struct {
	sourceID int
	baseDir  string
}

func withRouteRun(ctx context.Context) context.Context {
	if _, ok := ctx.Value(failedRouteDirsKey{}).(*sync.Map); ok {
		return ctx
	}
	return context.WithValue(ctx, failedRouteDirsKey{}, &sync.Map{})
}

type candidateRoute struct {
	rows      []models.CrossSeedRouteRow
	crossSeed *models.Instance
	source    *models.Instance
	backend   fsops.Backend
}

func (r *candidateRoute) receiverOf(instanceID int) int {
	if r == nil {
		return instanceID
	}
	return r.crossSeed.ID
}

func (c CrossSeedCandidate) receiver() (int, string) {
	if c.route != nil {
		return c.route.crossSeed.ID, c.route.crossSeed.Name
	}
	return c.InstanceID, c.InstanceName
}

func (c CrossSeedCandidate) newResult(status, message string) InstanceCrossSeedResult {
	result := InstanceCrossSeedResult{InstanceID: c.InstanceID, InstanceName: c.InstanceName, Status: status, Message: message}
	if c.route != nil {
		result.CrossSeedInstanceID, result.CrossSeedInstanceName = c.receiver()
	}
	return result
}

func (s *Service) routeOf(ctx context.Context, instanceID int) (*candidateRoute, error) {
	route, saved, err := s.savedRouteOf(ctx, instanceID)
	if err != nil || route == nil {
		return nil, err
	}
	return route, s.crossSeedRoutes.Check(ctx, saved)
}

func (s *Service) savedRouteOf(ctx context.Context, instanceID int) (*candidateRoute, *models.CrossSeedRoute, error) {
	if s.crossSeedRoutes == nil {
		return nil, nil, nil
	}
	saved, err := s.crossSeedRoutes.Get(ctx, instanceID)
	if err != nil || saved == nil {
		return nil, nil, err
	}
	crossSeed, err := s.instanceStore.Get(ctx, saved.CrossSeedInstanceID)
	if err != nil {
		return nil, nil, err
	}
	return &candidateRoute{rows: saved.Rows, crossSeed: crossSeed}, saved, nil
}

func (s *Service) sortSourceInstancesFirst(ctx context.Context, candidates []CrossSeedCandidate) {
	if s.crossSeedRoutes == nil || len(candidates) < 2 {
		return
	}
	routes, err := s.crossSeedRoutes.List(ctx)
	if err != nil {
		log.Debug().Err(err).Msg("[CROSSSEED] Failed to read cross-seed routes; candidates keep their order")
		return
	}
	routed := make(map[int]bool, len(routes))
	for _, route := range routes {
		routed[route.SourceInstanceID] = true
	}
	rank := func(c CrossSeedCandidate) int {
		if routed[c.InstanceID] {
			return 0
		}
		return 1
	}
	slices.SortStableFunc(candidates, func(a, b CrossSeedCandidate) int { return cmp.Compare(rank(a), rank(b)) })
}

func routeFailure(err error) (string, string) {
	ruleErr, ok := errors.AsType[*crossseedroute.RuleError](err)
	if !ok {
		return "error", "Failed to read the cross-seed route: " + err.Error()
	}
	if errors.Is(err, crossseedroute.ErrUnavailable) {
		return crossSeedUnavailableStatus, ruleErr.Message
	}
	return crossSeedUnavailableStatus, crossSeedUnavailablePrefix + ruleErr.Message
}

func (s *Service) loadSource(ctx context.Context, route *candidateRoute, sourceID int) (string, string) {
	backend, source, err := s.getBackendPool().Require(ctx, sourceID, models.CapabilityWrite)
	if err != nil {
		return "error", fmt.Sprintf("Failed to read the Source instance: %v", err)
	}
	switch {
	case models.FilesystemAccessMode(source) != models.FilesystemModeLocal:
		return crossSeedUnavailableStatus, crossSeedUnavailablePrefix + source.Name + " needs Local Filesystem Access to route its cross-seeds"
	case !source.UseHardlinks && !source.UseReflinks:
		return crossSeedUnavailableStatus, crossSeedUnavailablePrefix + source.Name + " must use hardlink or reflink mode to route its cross-seeds"
	}
	route.source, route.backend = source, backend
	return "", ""
}

func (s *Service) checkVisible(ctx context.Context, route *candidateRoute, d fsops.PathDialect, baseDir, target string) (string, string) {
	dir, _ := crossseedroute.ToCrossSeed(d, route.rows, baseDir, d.Dir(target))
	name := d.Base(target)
	key := failedRouteDir{route.source.ID, fsops.CleanPath(d, baseDir)}
	failed, ok := ctx.Value(failedRouteDirsKey{}).(*sync.Map)
	if !ok {
		failed = &sync.Map{}
	}
	retries := visibleRetries
	if _, ok := failed.Load(key); ok {
		retries = 0
	}
	stopped := func() (string, string) {
		return "error", "Visibility check stopped: " + context.Cause(ctx).Error()
	}
	for attempt := 0; ; attempt++ {
		content, err := s.syncManager.GetDirectoryContentCtx(ctx, route.crossSeed.ID, dir, qbt.DirectoryContentFiles, false)
		found, err := crossseedroute.ListingHas(content, err, dir, name)
		switch {
		case found:
			return "", ""
		case ctx.Err() != nil:
			return stopped()
		case err != nil && !errors.Is(err, crossseedroute.ErrCannotSeeFolder):
			cause := strings.TrimPrefix(err.Error(), crossseedroute.ErrUnavailable.Error()+": ")
			return crossSeedUnavailableStatus, crossSeedUnavailablePrefix + route.crossSeed.Name + ": " + cause
		case attempt < retries:
			select {
			case <-ctx.Done():
				return stopped()
			case <-time.After(cmp.Or(s.visibleRetryDelay, visibleRetryDelay)):
			}
			continue
		}
		failed.Store(key, struct{}{})
		return crossSeedCannotSeeStatus, "Cross-seed instance cannot see the data at " + dir
	}
}
