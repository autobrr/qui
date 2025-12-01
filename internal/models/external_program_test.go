// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"database/sql"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	_ "modernc.org/sqlite"
)

func setupExternalProgramStore(t *testing.T) (*ExternalProgramStore, func()) {
	t.Helper()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)

	db := newMockQuerier(sqlDB)

	ctx := t.Context()

	// Create table
	_, err = db.ExecContext(ctx, `
		CREATE TABLE external_programs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			name TEXT NOT NULL,
			path TEXT NOT NULL,
			args_template TEXT NOT NULL DEFAULT '',
			enabled INTEGER NOT NULL DEFAULT 1,
			use_terminal INTEGER NOT NULL DEFAULT 0,
			path_mappings TEXT NOT NULL DEFAULT '[]',
			created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
			updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	require.NoError(t, err)

	store := NewExternalProgramStore(db)
	cleanup := func() { _ = sqlDB.Close() }

	return store, cleanup
}

func TestNewExternalProgramStore(t *testing.T) {
	t.Parallel()

	sqlDB, err := sql.Open("sqlite", ":memory:")
	require.NoError(t, err)
	defer sqlDB.Close()

	db := newMockQuerier(sqlDB)
	store := NewExternalProgramStore(db)

	require.NotNil(t, store)
	assert.Equal(t, db, store.db)
}

func TestExternalProgramStore_Create(t *testing.T) {
	t.Parallel()

	t.Run("successful creation", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()
		create := &ExternalProgramCreate{
			Name:         "Test Program",
			Path:         "/usr/bin/test",
			ArgsTemplate: "--input {path}",
			Enabled:      true,
			UseTerminal:  false,
			PathMappings: nil,
		}

		program, err := store.Create(ctx, create)
		require.NoError(t, err)
		require.NotNil(t, program)
		assert.Equal(t, 1, program.ID)
		assert.Equal(t, "Test Program", program.Name)
		assert.Equal(t, "/usr/bin/test", program.Path)
		assert.Equal(t, "--input {path}", program.ArgsTemplate)
		assert.True(t, program.Enabled)
		assert.False(t, program.UseTerminal)
		assert.Empty(t, program.PathMappings)
		assert.False(t, program.CreatedAt.IsZero())
		assert.False(t, program.UpdatedAt.IsZero())
	})

	t.Run("with path mappings", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()
		create := &ExternalProgramCreate{
			Name:         "Remote Program",
			Path:         "/usr/bin/remote",
			ArgsTemplate: "{path}",
			Enabled:      true,
			UseTerminal:  true,
			PathMappings: []PathMapping{
				{From: "/remote/path", To: "/local/path"},
				{From: "/remote/other", To: "/local/other"},
			},
		}

		program, err := store.Create(ctx, create)
		require.NoError(t, err)
		require.NotNil(t, program)
		assert.Equal(t, "Remote Program", program.Name)
		assert.True(t, program.UseTerminal)
		assert.Len(t, program.PathMappings, 2)
		assert.Equal(t, "/remote/path", program.PathMappings[0].From)
		assert.Equal(t, "/local/path", program.PathMappings[0].To)
	})

	t.Run("disabled program", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()
		create := &ExternalProgramCreate{
			Name:    "Disabled Program",
			Path:    "/usr/bin/disabled",
			Enabled: false,
		}

		program, err := store.Create(ctx, create)
		require.NoError(t, err)
		require.NotNil(t, program)
		assert.False(t, program.Enabled)
	})
}

func TestExternalProgramStore_GetByID(t *testing.T) {
	t.Parallel()

	t.Run("existing program", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create program first
		create := &ExternalProgramCreate{
			Name:         "Test Program",
			Path:         "/usr/bin/test",
			ArgsTemplate: "--test",
			Enabled:      true,
			PathMappings: []PathMapping{
				{From: "/src", To: "/dst"},
			},
		}
		created, err := store.Create(ctx, create)
		require.NoError(t, err)

		// Get by ID
		program, err := store.GetByID(ctx, created.ID)
		require.NoError(t, err)
		require.NotNil(t, program)
		assert.Equal(t, created.ID, program.ID)
		assert.Equal(t, "Test Program", program.Name)
		assert.Len(t, program.PathMappings, 1)
	})

	t.Run("program not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		program, err := store.GetByID(ctx, 999)
		assert.ErrorIs(t, err, ErrExternalProgramNotFound)
		assert.Nil(t, program)
	})
}

func TestExternalProgramStore_List(t *testing.T) {
	t.Parallel()

	t.Run("returns all programs", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create multiple programs
		_, err := store.Create(ctx, &ExternalProgramCreate{Name: "Program B", Path: "/usr/bin/b", Enabled: true})
		require.NoError(t, err)
		_, err = store.Create(ctx, &ExternalProgramCreate{Name: "Program A", Path: "/usr/bin/a", Enabled: false})
		require.NoError(t, err)
		_, err = store.Create(ctx, &ExternalProgramCreate{Name: "Program C", Path: "/usr/bin/c", Enabled: true})
		require.NoError(t, err)

		// List all
		programs, err := store.List(ctx)
		require.NoError(t, err)
		assert.Len(t, programs, 3)

		// Should be ordered by name ASC
		assert.Equal(t, "Program A", programs[0].Name)
		assert.Equal(t, "Program B", programs[1].Name)
		assert.Equal(t, "Program C", programs[2].Name)
	})

	t.Run("empty list", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		programs, err := store.List(ctx)
		require.NoError(t, err)
		assert.Empty(t, programs)
	})
}

func TestExternalProgramStore_ListEnabled(t *testing.T) {
	t.Parallel()

	t.Run("returns only enabled programs", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create mixed enabled/disabled programs
		_, err := store.Create(ctx, &ExternalProgramCreate{Name: "Enabled 1", Path: "/usr/bin/e1", Enabled: true})
		require.NoError(t, err)
		_, err = store.Create(ctx, &ExternalProgramCreate{Name: "Disabled 1", Path: "/usr/bin/d1", Enabled: false})
		require.NoError(t, err)
		_, err = store.Create(ctx, &ExternalProgramCreate{Name: "Enabled 2", Path: "/usr/bin/e2", Enabled: true})
		require.NoError(t, err)

		// List enabled only
		programs, err := store.ListEnabled(ctx)
		require.NoError(t, err)
		assert.Len(t, programs, 2)

		// All should be enabled
		for _, p := range programs {
			assert.True(t, p.Enabled)
		}
	})

	t.Run("empty when all disabled", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create only disabled programs
		_, err := store.Create(ctx, &ExternalProgramCreate{Name: "Disabled", Path: "/usr/bin/d", Enabled: false})
		require.NoError(t, err)

		programs, err := store.ListEnabled(ctx)
		require.NoError(t, err)
		assert.Empty(t, programs)
	})
}

func TestExternalProgramStore_Update(t *testing.T) {
	t.Parallel()

	t.Run("successful update", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create program
		created, err := store.Create(ctx, &ExternalProgramCreate{
			Name:         "Original Name",
			Path:         "/usr/bin/original",
			ArgsTemplate: "--original",
			Enabled:      true,
			UseTerminal:  false,
		})
		require.NoError(t, err)

		// Update
		update := &ExternalProgramUpdate{
			Name:         "Updated Name",
			Path:         "/usr/bin/updated",
			ArgsTemplate: "--updated",
			Enabled:      false,
			UseTerminal:  true,
			PathMappings: []PathMapping{
				{From: "/new/from", To: "/new/to"},
			},
		}

		updated, err := store.Update(ctx, created.ID, update)
		require.NoError(t, err)
		require.NotNil(t, updated)
		assert.Equal(t, created.ID, updated.ID)
		assert.Equal(t, "Updated Name", updated.Name)
		assert.Equal(t, "/usr/bin/updated", updated.Path)
		assert.Equal(t, "--updated", updated.ArgsTemplate)
		assert.False(t, updated.Enabled)
		assert.True(t, updated.UseTerminal)
		assert.Len(t, updated.PathMappings, 1)
	})

	t.Run("program not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		update := &ExternalProgramUpdate{
			Name: "Nonexistent",
			Path: "/usr/bin/nonexistent",
		}

		updated, err := store.Update(ctx, 999, update)
		assert.ErrorIs(t, err, ErrExternalProgramNotFound)
		assert.Nil(t, updated)
	})
}

func TestExternalProgramStore_Delete(t *testing.T) {
	t.Parallel()

	t.Run("successful deletion", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create program
		created, err := store.Create(ctx, &ExternalProgramCreate{
			Name: "To Delete",
			Path: "/usr/bin/delete",
		})
		require.NoError(t, err)

		// Delete
		err = store.Delete(ctx, created.ID)
		require.NoError(t, err)

		// Verify deleted
		_, err = store.GetByID(ctx, created.ID)
		assert.ErrorIs(t, err, ErrExternalProgramNotFound)
	})

	t.Run("program not found", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		err := store.Delete(ctx, 999)
		assert.ErrorIs(t, err, ErrExternalProgramNotFound)
	})
}

func TestPathMapping(t *testing.T) {
	t.Parallel()

	t.Run("empty path mappings serialization", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create with nil path mappings
		created, err := store.Create(ctx, &ExternalProgramCreate{
			Name:         "No Mappings",
			Path:         "/usr/bin/test",
			PathMappings: nil,
		})
		require.NoError(t, err)
		assert.Empty(t, created.PathMappings)

		// Retrieve and verify
		retrieved, err := store.GetByID(ctx, created.ID)
		require.NoError(t, err)
		assert.Empty(t, retrieved.PathMappings)
	})

	t.Run("empty array path mappings", func(t *testing.T) {
		t.Parallel()

		store, cleanup := setupExternalProgramStore(t)
		defer cleanup()

		ctx := t.Context()

		// Create with empty slice
		created, err := store.Create(ctx, &ExternalProgramCreate{
			Name:         "Empty Array",
			Path:         "/usr/bin/test",
			PathMappings: []PathMapping{},
		})
		require.NoError(t, err)
		assert.Empty(t, created.PathMappings)
	})
}
