// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/autobrr/qui/internal/dbinterface"
)

var ErrExternalProgramNotFound = errors.New("external program not found")

// PathMapping represents a path mapping from remote to local
type PathMapping struct {
	From string `json:"from"` // Remote path prefix
	To   string `json:"to"`   // Local path prefix
}

// ExternalProgram represents a configured external program that can be executed from the torrent context menu
type ExternalProgram struct {
	ID           int           `json:"id"`
	Name         string        `json:"name"`
	Path         string        `json:"path"`
	ArgsTemplate string        `json:"args_template"`
	Enabled      bool          `json:"enabled"`
	UseTerminal  bool          `json:"use_terminal"`
	PathMappings []PathMapping `json:"path_mappings"`
	CreatedAt    time.Time     `json:"created_at"`
	UpdatedAt    time.Time     `json:"updated_at"`
}

// ExternalProgramCreate represents the data needed to create a new external program
type ExternalProgramCreate struct {
	Name         string        `json:"name"`
	Path         string        `json:"path"`
	ArgsTemplate string        `json:"args_template"`
	Enabled      bool          `json:"enabled"`
	UseTerminal  bool          `json:"use_terminal"`
	PathMappings []PathMapping `json:"path_mappings"`
}

// ExternalProgramUpdate represents the data needed to update an external program
type ExternalProgramUpdate struct {
	Name         string        `json:"name"`
	Path         string        `json:"path"`
	ArgsTemplate string        `json:"args_template"`
	Enabled      bool          `json:"enabled"`
	UseTerminal  bool          `json:"use_terminal"`
	PathMappings []PathMapping `json:"path_mappings"`
}

// ExternalProgramExecute represents a request to execute an external program with torrent data
type ExternalProgramExecute struct {
	ProgramID  int      `json:"program_id"`
	InstanceID int      `json:"instance_id"`
	Hashes     []string `json:"hashes"`
}

type ExternalProgramStore struct {
	db dbinterface.Querier
}

func NewExternalProgramStore(db dbinterface.Querier) *ExternalProgramStore {
	return &ExternalProgramStore{db: db}
}

func (s *ExternalProgramStore) List(ctx context.Context) ([]*ExternalProgram, error) {
	query := `
		SELECT id, name, path, args_template, enabled, use_terminal, path_mappings, created_at, updated_at
		FROM external_programs
		ORDER BY name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query external programs: %w", err)
	}
	defer rows.Close()

	var programs []*ExternalProgram
	for rows.Next() {
		program := &ExternalProgram{}
		var enabled, useTerminal int
		var pathMappingsJSON string
		if err := rows.Scan(
			&program.ID,
			&program.Name,
			&program.Path,
			&program.ArgsTemplate,
			&enabled,
			&useTerminal,
			&pathMappingsJSON,
			&program.CreatedAt,
			&program.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan external program: %w", err)
		}
		program.Enabled = enabled == 1
		program.UseTerminal = useTerminal == 1

		// Unmarshal path mappings
		if pathMappingsJSON != "" && pathMappingsJSON != "[]" {
			if err := json.Unmarshal([]byte(pathMappingsJSON), &program.PathMappings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal path mappings: %w", err)
			}
		}
		programs = append(programs, program)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating external programs: %w", err)
	}

	return programs, nil
}

func (s *ExternalProgramStore) ListEnabled(ctx context.Context) ([]*ExternalProgram, error) {
	query := `
		SELECT id, name, path, args_template, enabled, use_terminal, path_mappings, created_at, updated_at
		FROM external_programs
		WHERE enabled = 1
		ORDER BY name ASC
	`

	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("failed to query enabled external programs: %w", err)
	}
	defer rows.Close()

	var programs []*ExternalProgram
	for rows.Next() {
		program := &ExternalProgram{}
		var enabled, useTerminal int
		var pathMappingsJSON string
		if err := rows.Scan(
			&program.ID,
			&program.Name,
			&program.Path,
			&program.ArgsTemplate,
			&enabled,
			&useTerminal,
			&pathMappingsJSON,
			&program.CreatedAt,
			&program.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("failed to scan external program: %w", err)
		}
		program.Enabled = enabled == 1
		program.UseTerminal = useTerminal == 1

		// Unmarshal path mappings
		if pathMappingsJSON != "" && pathMappingsJSON != "[]" {
			if err := json.Unmarshal([]byte(pathMappingsJSON), &program.PathMappings); err != nil {
				return nil, fmt.Errorf("failed to unmarshal path mappings: %w", err)
			}
		}
		programs = append(programs, program)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating enabled external programs: %w", err)
	}

	return programs, nil
}

func (s *ExternalProgramStore) GetByID(ctx context.Context, id int) (*ExternalProgram, error) {
	query := `
		SELECT id, name, path, args_template, enabled, use_terminal, path_mappings, created_at, updated_at
		FROM external_programs
		WHERE id = ?
	`

	program := &ExternalProgram{}
	var enabled, useTerminal int
	var pathMappingsJSON string
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&program.ID,
		&program.Name,
		&program.Path,
		&program.ArgsTemplate,
		&enabled,
		&useTerminal,
		&pathMappingsJSON,
		&program.CreatedAt,
		&program.UpdatedAt,
	)

	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrExternalProgramNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("failed to get external program: %w", err)
	}

	program.Enabled = enabled == 1
	program.UseTerminal = useTerminal == 1

	// Unmarshal path mappings
	if pathMappingsJSON != "" && pathMappingsJSON != "[]" {
		if err := json.Unmarshal([]byte(pathMappingsJSON), &program.PathMappings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal path mappings: %w", err)
		}
	}

	return program, nil
}

func (s *ExternalProgramStore) Create(ctx context.Context, create *ExternalProgramCreate) (*ExternalProgram, error) {
	// Marshal path mappings to JSON
	pathMappingsJSON, err := json.Marshal(create.PathMappings)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal path mappings: %w", err)
	}

	query := `
		INSERT INTO external_programs (name, path, args_template, enabled, use_terminal, path_mappings, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, CURRENT_TIMESTAMP)
		RETURNING id, name, path, args_template, enabled, use_terminal, path_mappings, created_at, updated_at
	`

	enabledInt := 0
	if create.Enabled {
		enabledInt = 1
	}
	useTerminalInt := 0
	if create.UseTerminal {
		useTerminalInt = 1
	}

	program := &ExternalProgram{}
	var enabled, useTerminal int
	var pathMappingsJSONStr string
	err = s.db.QueryRowContext(ctx, query, create.Name, create.Path, create.ArgsTemplate, enabledInt, useTerminalInt, string(pathMappingsJSON)).Scan(
		&program.ID,
		&program.Name,
		&program.Path,
		&program.ArgsTemplate,
		&enabled,
		&useTerminal,
		&pathMappingsJSONStr,
		&program.CreatedAt,
		&program.UpdatedAt,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create external program: %w", err)
	}

	program.Enabled = enabled == 1
	program.UseTerminal = useTerminal == 1

	// Unmarshal path mappings
	if pathMappingsJSONStr != "" && pathMappingsJSONStr != "[]" {
		if err := json.Unmarshal([]byte(pathMappingsJSONStr), &program.PathMappings); err != nil {
			return nil, fmt.Errorf("failed to unmarshal path mappings: %w", err)
		}
	}

	return program, nil
}

func (s *ExternalProgramStore) Update(ctx context.Context, id int, update *ExternalProgramUpdate) (*ExternalProgram, error) {
	// Marshal path mappings to JSON
	pathMappingsJSON, err := json.Marshal(update.PathMappings)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal path mappings: %w", err)
	}

	query := `
		UPDATE external_programs
		SET name = ?, path = ?, args_template = ?, enabled = ?, use_terminal = ?, path_mappings = ?, updated_at = CURRENT_TIMESTAMP
		WHERE id = ?
	`

	enabledInt := 0
	if update.Enabled {
		enabledInt = 1
	}
	useTerminalInt := 0
	if update.UseTerminal {
		useTerminalInt = 1
	}

	result, err := s.db.ExecContext(ctx, query, update.Name, update.Path, update.ArgsTemplate, enabledInt, useTerminalInt, string(pathMappingsJSON), id)
	if err != nil {
		return nil, fmt.Errorf("failed to update external program: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return nil, fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return nil, ErrExternalProgramNotFound
	}

	return s.GetByID(ctx, id)
}

func (s *ExternalProgramStore) Delete(ctx context.Context, id int) error {
	query := `DELETE FROM external_programs WHERE id = ?`

	result, err := s.db.ExecContext(ctx, query, id)
	if err != nil {
		return fmt.Errorf("failed to delete external program: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return ErrExternalProgramNotFound
	}

	return nil
}
