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

// crossSeedUnavailableStatus is the result status of a routed add whose
// Cross-seed instance is unavailable or whose route breaks a route rule.
const crossSeedUnavailableStatus = "crossseed_instance_unavailable"

const crossSeedUnavailablePrefix = "Cross-seed instance unavailable: "

// crossSeedCannotSeeStatus is the result status of a routed add whose
// Cross-seed instance does not list the linked files.
const crossSeedCannotSeeStatus = "crossseed_instance_cannot_see_data"

// ponytail: the visibility check lists 3 more times over about 15 seconds. NFS
// can cache a folder listing for up to 60 seconds, so a slow mount can still
// fail a good add. Lower the NFS cache time, or wait longer here if users hit it.
const (
	visibleRetries    = 3
	visibleRetryDelay = 5 * time.Second
)

// failedRouteDirsKey holds, in a context, a *sync.Map of each failedRouteDir
// whose visibility check failed in one Trigger run. A later add under such a
// base folder lists once.
type failedRouteDirsKey struct{}

type failedRouteDir struct {
	sourceID int
	baseDir  string
}

// withRouteRun starts the memory of failed base folders for one Trigger run.
// A ctx that already holds one keeps it.
func withRouteRun(ctx context.Context) context.Context {
	if _, ok := ctx.Value(failedRouteDirsKey{}).(*sync.Map); ok {
		return ctx
	}
	return context.WithValue(ctx, failedRouteDirsKey{}, &sync.Map{})
}

// candidateRoute is the route of a routed add (ADR 0035). The add goes to
// crossSeed in link mode, with the tree on the Source instance's disk.
type candidateRoute struct {
	rows      []models.CrossSeedRouteRow
	crossSeed *models.Instance
	// source and backend come from one capability read, after the Pre-add check.
	source  *models.Instance
	backend fsops.Backend
}

// receiver returns the instance that gets the add of c.
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

// routeOf reads the route of instanceID, or nil when it has none. With a
// route, a non-nil error means that the route breaks a route rule or that the
// Cross-seed instance is unavailable.
func (s *Service) routeOf(ctx context.Context, instanceID int) (*candidateRoute, error) {
	if s.crossSeedRoutes == nil {
		return nil, nil
	}
	route, err := s.crossSeedRoutes.Get(ctx, instanceID)
	if err != nil || route == nil {
		return nil, err
	}
	crossSeed, err := s.instanceStore.Get(ctx, route.CrossSeedInstanceID)
	if err != nil {
		return nil, err
	}
	// The rule check also makes sure that the Cross-seed instance is available.
	return &candidateRoute{rows: route.Rows, crossSeed: crossSeed}, s.crossSeedRoutes.Check(ctx, route)
}

// sortSourceInstancesFirst puts the candidates of Source instances first, in a stable
// sort. A routed add then builds its own link tree on the Cross-seed instance
// before a match on that instance adds the same torrent there in its own mode.
func (s *Service) sortSourceInstancesFirst(ctx context.Context, candidates []CrossSeedCandidate) {
	if s.crossSeedRoutes == nil || len(candidates) < 2 {
		return
	}
	routes, err := s.crossSeedRoutes.List(ctx)
	if err != nil {
		// Each candidate reads its route again, and that read reports the failure.
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

// routeFailure returns the status and message of a failed route read or rule check.
func routeFailure(err error) (string, string) {
	ruleErr, ok := errors.AsType[*crossseedroute.RuleError](err)
	if !ok {
		return "error", "Failed to read the cross-seed route: " + err.Error()
	}
	if errors.Is(err, crossseedroute.ErrUnavailable) {
		// crossseedroute.CrossSeedUnavailable already names the cause.
		return crossSeedUnavailableStatus, ruleErr.Message
	}
	return crossSeedUnavailableStatus, crossSeedUnavailablePrefix + ruleErr.Message
}

// loadSource reads the Source instance's row and backend for route from one
// capability read. It returns a failed result when the row no longer allows a
// routed add. A routed add never falls back to a regular add.
func (s *Service) loadSource(ctx context.Context, candidate CrossSeedCandidate) *InstanceCrossSeedResult {
	backend, source, err := s.getBackendPool().Require(ctx, candidate.InstanceID, models.CapabilityWrite)
	if err != nil {
		result := candidate.newResult("error", fmt.Sprintf("Failed to read the Source instance: %v", err))
		return &result
	}
	var broken string
	switch {
	case models.FilesystemAccessMode(source) != models.FilesystemModeLocal:
		broken = source.Name + " needs Local Filesystem Access to route its cross-seeds"
	case !source.UseHardlinks && !source.UseReflinks:
		broken = source.Name + " must use hardlink or reflink mode to route its cross-seeds"
	}
	if broken != "" {
		result := candidate.newResult(crossSeedUnavailableStatus, crossSeedUnavailablePrefix+broken)
		return &result
	}
	candidate.route.source, candidate.route.backend = source, backend
	return nil
}

// checkVisible makes sure that the Cross-seed instance lists target, a linked
// file of the plan under baseDir on the Source instance. It returns a failed
// result when the Cross-seed instance cannot see the file or is unavailable.
func (s *Service) checkVisible(ctx context.Context, candidate CrossSeedCandidate, d fsops.PathDialect, baseDir, target string) *InstanceCrossSeedResult {
	route := candidate.route
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
	stopped := func() *InstanceCrossSeedResult {
		result := candidate.newResult("error", "Visibility check stopped: "+context.Cause(ctx).Error())
		return &result
	}
	for attempt := 0; ; attempt++ {
		content, err := s.syncManager.GetDirectoryContentCtx(ctx, route.crossSeed.ID, dir, qbt.DirectoryContentFiles, false)
		found, err := crossseedroute.ListingHas(content, err, dir, name)
		switch {
		case found:
			return nil
		case ctx.Err() != nil:
			return stopped()
		case err != nil && !errors.Is(err, crossseedroute.ErrCannotSeeFolder):
			cause := strings.TrimPrefix(err.Error(), crossseedroute.ErrUnavailable.Error()+": ")
			result := candidate.newResult(crossSeedUnavailableStatus, crossSeedUnavailablePrefix+route.crossSeed.Name+": "+cause)
			return &result
		case attempt < retries:
			select {
			case <-ctx.Done():
				return stopped()
			case <-time.After(cmp.Or(s.visibleRetryDelay, visibleRetryDelay)):
			}
			continue
		}
		failed.Store(key, struct{}{})
		result := candidate.newResult(crossSeedCannotSeeStatus, "Cross-seed instance cannot see the data at "+dir)
		return &result
	}
}
