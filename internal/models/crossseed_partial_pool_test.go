// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestCrossSeedPartialPoolMemberStore_RoundTripAndPrune(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedPartialPoolMemberStore(db)
	ctx := context.Background()

	member := &models.CrossSeedPartialPoolMember{
		SourceInstanceID:            1,
		SourceHash:                  "sourcehash",
		TargetInstanceID:            2,
		TargetHash:                  "targethash",
		TargetHashV2:                "targethashv2",
		TargetAddedOn:               12345,
		TargetName:                  "Test Torrent",
		Mode:                        models.CrossSeedPartialMemberModeHardlink,
		ManagedRoot:                 t.TempDir(),
		SourcePieceLength:           1024,
		MaxMissingBytesAfterRecheck: 512,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "data/file.mkv", Size: 1234, Key: "file"},
			{Name: "data/file.nfo", Size: 56, Key: "file"},
		},
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	}

	stored, err := store.Upsert(ctx, member)
	require.NoError(t, err)
	require.NotNil(t, stored)
	assert.Equal(t, "SOURCEHASH", stored.SourceHash)
	assert.Equal(t, "TARGETHASH", stored.TargetHash)
	assert.Equal(t, "TARGETHASHV2", stored.TargetHashV2)
	assert.EqualValues(t, 12345, stored.TargetAddedOn)

	loaded, err := store.GetByAnyHash(ctx, 2, "targethashv2")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, stored.ID, loaded.ID)
	assert.Len(t, loaded.SourceFiles, 2)
	assert.Equal(t, member.SourceFiles[0].Key, loaded.SourceFiles[0].Key)
	assert.EqualValues(t, 512, loaded.MaxMissingBytesAfterRecheck)
	assert.EqualValues(t, 12345, loaded.TargetAddedOn)

	active, err := store.ListActive(ctx, time.Now().UTC())
	require.NoError(t, err)
	require.Len(t, active, 1)
	assert.Equal(t, stored.ID, active[0].ID)

	rows, err := store.DeleteExpired(ctx, time.Now().UTC().Add(2*time.Hour))
	require.NoError(t, err)
	assert.EqualValues(t, 1, rows)

	active, err = store.ListActive(ctx, time.Now().UTC())
	require.NoError(t, err)
	assert.Empty(t, active)
}

func TestCrossSeedPartialPoolMemberStore_DeleteByAnyHash(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedPartialPoolMemberStore(db)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &models.CrossSeedPartialPoolMember{
		SourceInstanceID:            1,
		SourceHash:                  "sourcehash",
		TargetInstanceID:            2,
		TargetHash:                  "targethash",
		TargetHashV2:                "targethashv2",
		TargetName:                  "Test Torrent",
		Mode:                        models.CrossSeedPartialMemberModeReflink,
		ManagedRoot:                 t.TempDir(),
		SourcePieceLength:           1024,
		MaxMissingBytesAfterRecheck: 1024,
		SourceFiles: []models.CrossSeedPartialFile{
			{Name: "data/file.mkv", Size: 1234},
		},
		ExpiresAt: time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteByAnyHash(ctx, 2, "targethashv2"))

	loaded, err := store.GetByAnyHash(ctx, 2, "targethash", "targethashv2")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestCrossSeedPartialPoolMemberStore_GetByAnyHash_WithMoreThanTwoHashes(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedPartialPoolMemberStore(db)
	ctx := context.Background()

	stored, err := store.Upsert(ctx, &models.CrossSeedPartialPoolMember{
		SourceInstanceID:            1,
		SourceHash:                  "sourcehash",
		TargetInstanceID:            2,
		TargetHash:                  "targethash",
		TargetHashV2:                "targethashv2",
		TargetName:                  "Test Torrent",
		Mode:                        models.CrossSeedPartialMemberModeReflink,
		ManagedRoot:                 t.TempDir(),
		SourcePieceLength:           1024,
		MaxMissingBytesAfterRecheck: 1024,
		SourceFiles:                 []models.CrossSeedPartialFile{{Name: "data/file.mkv", Size: 1234}},
		ExpiresAt:                   time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	loaded, err := store.GetByAnyHash(ctx, 2, "does-not-exist", "also-missing", "targethashv2")
	require.NoError(t, err)
	require.NotNil(t, loaded)
	assert.Equal(t, stored.ID, loaded.ID)
}

func TestCrossSeedPartialPoolMemberStore_GetByAnyHash_IgnoresExpiredMembers(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedPartialPoolMemberStore(db)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &models.CrossSeedPartialPoolMember{
		SourceInstanceID:            1,
		SourceHash:                  "sourcehash",
		TargetInstanceID:            2,
		TargetHash:                  "targethash",
		TargetHashV2:                "targethashv2",
		TargetName:                  "Expired Torrent",
		Mode:                        models.CrossSeedPartialMemberModeHardlink,
		ManagedRoot:                 t.TempDir(),
		SourcePieceLength:           1024,
		MaxMissingBytesAfterRecheck: 1024,
		SourceFiles:                 []models.CrossSeedPartialFile{{Name: "data/file.mkv", Size: 1234}},
		ExpiresAt:                   time.Now().UTC().Add(-time.Hour),
	})
	require.NoError(t, err)

	loaded, err := store.GetByAnyHash(ctx, 2, "targethash", "targethashv2")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}

func TestCrossSeedPartialPoolMemberStore_DeleteByAnyHash_WithMoreThanTwoHashes(t *testing.T) {
	db := setupCrossSeedTestDB(t)
	store := models.NewCrossSeedPartialPoolMemberStore(db)
	ctx := context.Background()

	_, err := store.Upsert(ctx, &models.CrossSeedPartialPoolMember{
		SourceInstanceID:            1,
		SourceHash:                  "sourcehash",
		TargetInstanceID:            2,
		TargetHash:                  "targethash",
		TargetHashV2:                "targethashv2",
		TargetName:                  "Test Torrent",
		Mode:                        models.CrossSeedPartialMemberModeHardlink,
		ManagedRoot:                 t.TempDir(),
		SourcePieceLength:           1024,
		MaxMissingBytesAfterRecheck: 1024,
		SourceFiles:                 []models.CrossSeedPartialFile{{Name: "data/file.mkv", Size: 1234}},
		ExpiresAt:                   time.Now().UTC().Add(time.Hour),
	})
	require.NoError(t, err)

	require.NoError(t, store.DeleteByAnyHash(ctx, 2, "does-not-exist", "also-missing", "targethash"))

	loaded, err := store.GetByAnyHash(ctx, 2, "targethash", "targethashv2")
	require.NoError(t, err)
	assert.Nil(t, loaded)
}
