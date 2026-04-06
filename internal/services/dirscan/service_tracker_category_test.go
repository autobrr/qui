// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
)

type fakeDirscanCategoryGetter struct {
	categories map[string]qbt.Category
	err        error
}

func (g *fakeDirscanCategoryGetter) GetCategories(context.Context, int) (map[string]qbt.Category, error) {
	if g.err != nil {
		return nil, g.err
	}
	return g.categories, nil
}

func TestResolveDirscanTrackerCategory_FallsBackOnLookupError(t *testing.T) {
	t.Parallel()

	dbPath := filepath.Join(t.TempDir(), "indexer_categories.db")
	db, err := database.New(dbPath)
	require.NoError(t, err)

	store := models.NewCrossSeedIndexerCategoryStore(db)
	require.NoError(t, db.Close())

	logger := zerolog.New(io.Discard)
	instance := &models.Instance{
		ID:                7,
		UseHardlinks:      true,
		HardlinkDirPreset: "by-tracker",
	}

	category, savePath, fatal := resolveDirscanTrackerCategory(
		context.Background(),
		7,
		"Aither",
		"aither.cc",
		instance,
		store,
		&fakeDirscanCategoryGetter{err: errors.New("should not be called")},
		&logger,
	)

	require.False(t, fatal)
	require.Empty(t, category)
	require.Empty(t, savePath)
}
