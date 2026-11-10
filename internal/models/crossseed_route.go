// Copyright (c) 2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json/v2"
	"errors"
	"fmt"

	"github.com/autobrr/qui/internal/dbinterface"
)

// CrossSeedRoute names the Cross-seed instance of one Source instance (ADR 0035).
type CrossSeedRoute struct {
	SourceInstanceID    int                 `json:"sourceInstanceId"`
	CrossSeedInstanceID int                 `json:"crossSeedInstanceId"`
	Rows                []CrossSeedRouteRow `json:"rows"`
}

// CrossSeedRouteRow maps one base folder of the Source instance to the path
// where the Cross-seed instance sees it. BaseDir is the key from
// fsops.CleanPath with the Source instance's dialect. An empty MappedPath
// means the same path on both instances.
type CrossSeedRouteRow struct {
	BaseDir    string `json:"baseDir"`
	MappedPath string `json:"mappedPath"`
}

// CrossSeedRouteStore persists cross-seed routes.
type CrossSeedRouteStore struct {
	db dbinterface.Querier
}

func NewCrossSeedRouteStore(db dbinterface.Querier) *CrossSeedRouteStore {
	return &CrossSeedRouteStore{db: db}
}

// Get returns the route of a Source instance, or nil when it has none.
func (s *CrossSeedRouteStore) Get(ctx context.Context, sourceInstanceID int) (*CrossSeedRoute, error) {
	row := s.db.QueryRowContext(ctx, `SELECT source_instance_id, cross_seed_instance_id, rows_json
		FROM cross_seed_routes WHERE source_instance_id = ?`, sourceInstanceID)
	route, err := scanCrossSeedRoute(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return route, err
}

// List returns every route, ordered by Source instance.
func (s *CrossSeedRouteStore) List(ctx context.Context) ([]*CrossSeedRoute, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_instance_id, cross_seed_instance_id, rows_json
		FROM cross_seed_routes ORDER BY source_instance_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	routes := []*CrossSeedRoute{}
	for rows.Next() {
		route, err := scanCrossSeedRoute(rows)
		if err != nil {
			return nil, err
		}
		routes = append(routes, route)
	}
	return routes, rows.Err()
}

// ListSources returns the Source instances that route to a Cross-seed instance.
func (s *CrossSeedRouteStore) ListSources(ctx context.Context, crossSeedInstanceID int) ([]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT source_instance_id FROM cross_seed_routes
		WHERE cross_seed_instance_id = ? ORDER BY source_instance_id`, crossSeedInstanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int
	for rows.Next() {
		var id int
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

// Upsert saves the route of route.SourceInstanceID, replacing an older one.
func (s *CrossSeedRouteStore) Upsert(ctx context.Context, route *CrossSeedRoute) error {
	rowsJSON, err := json.Marshal(route.Rows)
	if err != nil {
		return err
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO cross_seed_routes (source_instance_id, cross_seed_instance_id, rows_json)
		VALUES (?, ?, ?)
		ON CONFLICT(source_instance_id) DO UPDATE SET
			cross_seed_instance_id = excluded.cross_seed_instance_id,
			rows_json = excluded.rows_json`,
		route.SourceInstanceID, route.CrossSeedInstanceID, string(rowsJSON))
	return err
}

// Delete clears the route of a Source instance. Clearing a missing route is not an error.
func (s *CrossSeedRouteStore) Delete(ctx context.Context, sourceInstanceID int) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cross_seed_routes WHERE source_instance_id = ?`, sourceInstanceID)
	return err
}

func scanCrossSeedRoute(scanner interface{ Scan(dest ...any) error }) (*CrossSeedRoute, error) {
	var (
		route    CrossSeedRoute
		rowsJSON string
	)
	if err := scanner.Scan(&route.SourceInstanceID, &route.CrossSeedInstanceID, &rowsJSON); err != nil {
		return nil, err
	}
	if err := json.Unmarshal([]byte(rowsJSON), &route.Rows); err != nil {
		return nil, fmt.Errorf("decode cross-seed route rows: %w", err)
	}
	if route.Rows == nil {
		route.Rows = []CrossSeedRouteRow{}
	}
	return &route, nil
}
