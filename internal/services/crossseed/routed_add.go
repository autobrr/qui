// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"fmt"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/crossseedroute"
)

// crossSeedUnavailableStatus is the result status of a routed add whose
// Cross-seed instance is unavailable or whose route breaks a route rule.
const crossSeedUnavailableStatus = "crossseed_instance_unavailable"

const crossSeedUnavailablePrefix = "Cross-seed instance unavailable: "

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
