// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func newPartialPoolTestStore(t *testing.T) (*models.CrossSeedStore, *models.InstanceStore, int, int) {
	t.Helper()
	db := setupCrossSeedTestDB(t)
	key := []byte("01234567890123456789012345678901")
	instanceStore, err := models.NewInstanceStore(db, key)
	require.NoError(t, err)
	local := true
	first, err := instanceStore.Create(t.Context(), "first", "http://127.0.0.1:8081", "user", "pass", nil, nil, false, &local)
	require.NoError(t, err)
	second, err := instanceStore.Create(t.Context(), "second", "http://127.0.0.1:8082", "user", "pass", nil, nil, false, &local)
	require.NoError(t, err)
	store, err := models.NewCrossSeedStore(db, key)
	require.NoError(t, err)
	return store, instanceStore, first.ID, second.ID
}

func partialPoolRegistration(instanceID, sourceInstanceID int, torrentKey, v1, v2, sourceKey string) models.CrossSeedPartialPoolRegistration {
	return models.CrossSeedPartialPoolRegistration{
		SourceInstanceID:  sourceInstanceID,
		SourceTorrentKey:  sourceKey,
		SourceAliases:     []string{sourceKey},
		MatchedInstanceID: sourceInstanceID,
		MatchedTorrentKey: sourceKey,
		MatchedAliases:    []string{sourceKey},
		Member: models.CrossSeedPartialPoolMember{
			InstanceID:      instanceID,
			TorrentKey:      torrentKey,
			InfoHashV1:      v1,
			InfoHashV2:      v2,
			Mode:            models.CrossSeedPartialPoolModeHardlink,
			RootPath:        `C:\cross-seeds\pool`,
			ReportedSeeders: 12,
			Status:          models.CrossSeedPartialPoolMemberStatusVerifying,
			MissingBytes:    200,
		},
		Files: []models.CrossSeedPartialPoolMemberFile{
			{FileIndex: 0, RelativePath: "Synthetic.Release/file.mkv", SizeBytes: 1000, PiecesRoot: "abcd", WantedAtAdmission: true, MaterializedAtAdd: true},
			{FileIndex: 1, RelativePath: "Synthetic.Release/extra.nfo", SizeBytes: 200, WantedAtAdmission: true},
		},
	}
}

func TestCrossSeedPartialPoolRegistrationIsAliasIdempotent(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()

	registration := partialPoolRegistration(secondID, firstID, "BBBB", "AAAA", "BBBB", "SOURCE")
	pool, member, err := store.RegisterPartialPoolMember(ctx, registration)
	require.NoError(t, err)
	require.NotNil(t, member)
	require.Equal(t, "source", pool.SourceTorrentKey)
	require.Len(t, member.Files, 2)
	require.Equal(t, 12, member.ReportedSeeders)

	duplicate := registration
	duplicate.Member.TorrentKey = "AAAA"
	duplicate.Member.InfoHashV1 = "AAAA"
	duplicate.Member.InfoHashV2 = "BBBB"
	duplicatePool, duplicateMember, err := store.RegisterPartialPoolMember(ctx, duplicate)
	require.NoError(t, err)
	require.Equal(t, pool.ID, duplicatePool.ID)
	require.Equal(t, member.ID, duplicateMember.ID)
	require.Len(t, duplicatePool.Members, 1)
}

func TestCrossSeedPartialPoolInheritsOriginalSource(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()

	firstPool, firstMember, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(firstID, firstID, "member-one", "member-one", "", "original-source"))
	require.NoError(t, err)

	registration := partialPoolRegistration(secondID, secondID, "member-two", "member-two", "", "new-source")
	registration.SourceInstanceID = firstID
	registration.SourceTorrentKey = "member-one"
	registration.SourceAliases = []string{"MEMBER-ONE"}
	secondPool, _, err := store.RegisterPartialPoolMember(ctx, registration)
	require.NoError(t, err)
	require.Equal(t, firstPool.ID, secondPool.ID)
	require.Equal(t, "original-source", secondPool.SourceTorrentKey)
	require.Len(t, secondPool.Members, 2)

	resolvedPool, resolvedMember, err := store.ResolvePartialPoolMember(ctx, firstID, "MEMBER-ONE")
	require.NoError(t, err)
	require.Equal(t, firstPool.ID, resolvedPool.ID)
	require.Equal(t, firstMember.ID, resolvedMember.ID)
}

func TestCrossSeedPartialPoolClaimsAndTransitionsPersist(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()
	pool, member, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(secondID, firstID, "member", "member-v1", "member-v2", "source"))
	require.NoError(t, err)

	zero := int64(0)
	changed, err := store.TransitionPartialPoolMember(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{MissingBytes: &zero})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolMember(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusBlocked, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.False(t, changed)

	now := time.Now().UTC().Truncate(time.Microsecond)
	claimed, err := store.ClaimPartialPoolDownloader(ctx, member.ID, 321, now)
	require.NoError(t, err)
	require.True(t, claimed)

	reloaded, err := store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, reloaded.Members[0].Status)
	require.True(t, reloaded.Members[0].StartedByPool)
	require.Equal(t, int64(321), *reloaded.Members[0].LastDownloadedBytes)
	require.WithinDuration(t, now, *reloaded.Members[0].LastProgressAt, time.Second)

	_, other, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(firstID, secondID, "other", "other", "", "member"))
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolMember(ctx, other.ID, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.True(t, changed)
	claimed, err = store.ClaimPartialPoolDownloader(ctx, other.ID, 0, now)
	require.NoError(t, err)
	require.False(t, claimed, "one acquiring member excludes another claim in the same pool")

	requestedTrue := true
	reason := "manual"
	changed, err = store.TransitionPartialPoolMember(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{
		StartedByPool: &requestedTrue,
		LastError:     &reason,
	})
	require.NoError(t, err)
	require.True(t, changed)
	reloaded, err = store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	var terminalMember *models.CrossSeedPartialPoolMember
	for _, candidate := range reloaded.Members {
		if candidate.ID == member.ID {
			terminalMember = candidate
			break
		}
	}
	require.NotNil(t, terminalMember)
	require.False(t, terminalMember.StartedByPool, "terminal state always clears the downloader claim")

	_, err = store.TransitionPartialPoolMember(ctx, member.ID, []string{models.CrossSeedPartialPoolMemberStatusManual}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.Error(t, err)
}

func TestCrossSeedPartialPoolRemovalPreservesOtherMembers(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()
	pool, firstMember, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(firstID, firstID, "one", "one", "", "source"))
	require.NoError(t, err)
	secondRegistration := partialPoolRegistration(secondID, firstID, "two", "two", "", "one")
	secondRegistration.SourceAliases = []string{"one"}
	_, secondMember, err := store.RegisterPartialPoolMember(ctx, secondRegistration)
	require.NoError(t, err)

	require.NoError(t, store.MarkPartialPoolMemberRemoved(ctx, firstMember.ID, "missing"))
	reloaded, err := store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.Members, 1)
	require.Equal(t, secondMember.ID, reloaded.Members[0].ID)

	require.NoError(t, store.MarkPartialPoolMemberRemoved(ctx, secondMember.ID, "missing"))
	_, err = store.GetPartialPool(ctx, pool.ID)
	require.Error(t, err)
}

func TestCrossSeedPartialPoolRegistrationRollsBackAtomically(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	registration := partialPoolRegistration(secondID, firstID, "member", "member", "", "source")
	registration.Files[1].FileIndex = registration.Files[0].FileIndex

	_, _, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.Error(t, err)
	pools, err := store.ListPartialPoolsForReconciliation(t.Context())
	require.NoError(t, err)
	require.Empty(t, pools)
}

func TestCrossSeedPartialPoolInstanceCascadePreservesOtherMembers(t *testing.T) {
	store, instanceStore, firstID, secondID := newPartialPoolTestStore(t)
	ctx := t.Context()
	pool, _, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(firstID, firstID, "one", "one", "", "source"))
	require.NoError(t, err)
	registration := partialPoolRegistration(secondID, firstID, "two", "two", "", "one")
	registration.SourceAliases = []string{"one"}
	_, secondMember, err := store.RegisterPartialPoolMember(ctx, registration)
	require.NoError(t, err)

	require.NoError(t, instanceStore.Delete(ctx, firstID))
	reloaded, err := store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.Members, 1)
	require.Equal(t, secondMember.ID, reloaded.Members[0].ID)

	require.NoError(t, instanceStore.Delete(ctx, secondID))
	require.NoError(t, store.PruneEmptyPartialPools(ctx))
	_, err = store.GetPartialPool(ctx, pool.ID)
	require.Error(t, err)
}
