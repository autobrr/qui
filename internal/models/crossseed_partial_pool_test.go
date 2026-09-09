// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func newPartialPoolTestStore(t *testing.T) (*models.CrossSeedStore, *models.InstanceStore, int, int) {
	t.Helper()
	return newPartialPoolTestStoreWithDB(t, setupCrossSeedTestDB(t))
}

func newPartialPoolTestStoreWithDB(t *testing.T, db *database.DB) (*models.CrossSeedStore, *models.InstanceStore, int, int) {
	t.Helper()
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

func partialPoolRegistration(t *testing.T, instanceID, sourceInstanceID int, torrentKey, v1, v2, sourceKey string) models.CrossSeedPartialPoolRegistration {
	t.Helper()
	return models.CrossSeedPartialPoolRegistration{
		SourceInstanceID:  sourceInstanceID,
		SourceTorrentKey:  sourceKey,
		SourceAliases:     []string{sourceKey},
		MatchedInstanceID: sourceInstanceID,
		MatchedTorrentKey: sourceKey,
		MatchedAliases:    []string{sourceKey},
		Member: models.CrossSeedPartialPoolMember{
			InstanceID:   instanceID,
			TorrentKey:   torrentKey,
			InfoHashV1:   v1,
			InfoHashV2:   v2,
			Mode:         models.CrossSeedPartialPoolModeHardlink,
			RootPath:     filepath.Join(t.TempDir(), "pool"),
			Status:       models.CrossSeedPartialPoolMemberStatusVerifying,
			MissingBytes: 200,
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

	registration := partialPoolRegistration(t, secondID, firstID, "BBBB", "AAAA", "BBBB", "SOURCE")
	pool, member, err := store.RegisterPartialPoolMember(ctx, registration)
	require.NoError(t, err)
	require.NotNil(t, member)
	require.Equal(t, "source", pool.SourceTorrentKey)
	require.Len(t, member.Files, 2)

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

func TestCrossSeedPartialPoolReAdmissionReplacesStaleState(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	registration := partialPoolRegistration(t, secondID, firstID, "BBBB", "AAAA", "BBBB", "SOURCE")
	pool, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.Len(t, member.Files, 2)
	oldFileIDs := []int64{member.Files[0].ID, member.Files[1].ID}

	zero := int64(0)
	changed, err := store.TransitionPartialPoolMember(t.Context(), member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{MissingBytes: &zero})
	require.NoError(t, err)
	require.True(t, changed)
	now := time.Now().UTC()
	claimed, err := store.ClaimPartialPoolDownloader(t.Context(), member.ID, 321, member.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)
	retryAfter := now.Add(time.Hour)
	staleReason := "stale admission state"
	staleReviewPause := true
	staleResumeAttempts := int64(2)
	staleRecoveryAttempts := int64(1)
	changed, err = store.TransitionPartialPoolMember(t.Context(), member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{
		RetryAfter:         models.NullableTimeUpdate{Set: true, Value: &retryAfter},
		ReviewPausePending: &staleReviewPause,
		ResumeAttempts:     models.NullableInt64Update{Set: true, Value: &staleResumeAttempts},
		RecoveryAttempts:   models.NullableInt64Update{Set: true, Value: &staleRecoveryAttempts},
		LastError:          &staleReason,
	})
	require.NoError(t, err)
	require.True(t, changed)
	dependentRegistration := partialPoolRegistration(t, firstID, secondID, "CCCC", "CCCC", "", "BBBB")
	_, dependent, err := store.RegisterPartialPoolMember(t.Context(), dependentRegistration)
	require.NoError(t, err)
	require.Len(t, dependent.Files, 2)
	changed, err = store.TransitionPartialPoolFile(t.Context(), dependent.Files[1].ID, dependent.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &member.Files[1].ID},
	})
	require.NoError(t, err)
	require.True(t, changed)

	reAdmission := registration
	reAdmission.Member.TorrentKey = "AAAA"
	reAdmission.Member.InfoHashV1 = "AAAA"
	reAdmission.Member.InfoHashV2 = "BBBB"
	reAdmission.Member.Mode = models.CrossSeedPartialPoolModeReflink
	reAdmission.Member.RootPath = filepath.Join(t.TempDir(), "new-pool")
	reAdmission.Member.MissingBytes = 77
	reAdmission.Member.LastError = models.CrossSeedPartialPoolRecheckPending
	reAdmission.Files = []models.CrossSeedPartialPoolMemberFile{{
		FileIndex:         7,
		RelativePath:      "Synthetic.Release/new-extra.nfo",
		SizeBytes:         77,
		WantedAtAdmission: true,
	}}

	reloadedPool, reloaded, err := store.RegisterPartialPoolMember(t.Context(), reAdmission)
	require.NoError(t, err)
	require.Equal(t, pool.ID, reloadedPool.ID)
	require.Equal(t, member.ID, reloaded.ID)
	require.Len(t, reloadedPool.Members, 2)
	require.Equal(t, reAdmission.Member.RootPath, reloaded.RootPath)
	require.Equal(t, models.CrossSeedPartialPoolModeReflink, reloaded.Mode)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, reloaded.Status)
	require.Equal(t, int64(77), reloaded.MissingBytes)
	require.False(t, reloaded.StartedByPool)
	require.Nil(t, reloaded.LastDownloadedBytes)
	require.Nil(t, reloaded.LastProgressAt)
	require.Nil(t, reloaded.RetryAfter)
	require.False(t, reloaded.ReviewPausePending)
	require.Nil(t, reloaded.ResumeAttempts)
	require.Nil(t, reloaded.RecoveryAttempts)
	require.Equal(t, models.CrossSeedPartialPoolRecheckPending, reloaded.LastError)
	require.Len(t, reloaded.Files, 1)
	require.Equal(t, 7, reloaded.Files[0].FileIndex)
	require.Equal(t, "Synthetic.Release/new-extra.nfo", reloaded.Files[0].RelativePath)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, reloaded.Files[0].Status)
	require.NotContains(t, oldFileIDs, reloaded.Files[0].ID)
	require.NotEqual(t, member.CreatedAt, reloaded.CreatedAt)
	require.NoError(t, store.MarkPartialPoolMemberRemoved(t.Context(), member.PoolID, member.ID, member.CreatedAt, "stale removal"))
	reloadedPool, err = store.GetPartialPool(t.Context(), pool.ID)
	require.NoError(t, err)
	for _, candidate := range reloadedPool.Members {
		if candidate.ID == member.ID {
			reloaded = candidate
			break
		}
	}
	require.NotNil(t, reloaded)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, reloaded.Status)
	var reloadedDependent *models.CrossSeedPartialPoolMember
	for _, candidate := range reloadedPool.Members {
		if candidate.TorrentKey == "cccc" {
			reloadedDependent = candidate
			break
		}
	}
	require.NotNil(t, reloadedDependent)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedDependent.Status)
	require.True(t, reloadedDependent.ReviewPausePending)
	require.Equal(t, "propagation source was replaced during re-admission", reloadedDependent.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, reloadedDependent.Files[1].Status)
	require.Nil(t, reloadedDependent.Files[1].SourceFileID)
	require.Equal(t, "propagation source was replaced during re-admission", reloadedDependent.Files[1].LastError)
}

func TestCrossSeedPartialPoolTransitionsRejectStaleAdmission(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	registration := partialPoolRegistration(t, secondID, firstID, "member", "member", "", "source")
	stalePool, stale, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	currentPool, current, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.Equal(t, stale.ID, current.ID)
	require.Equal(t, stale.Files[1].ID, current.Files[1].ID)
	require.NotEqual(t, stale.CreatedAt, current.CreatedAt)
	require.Equal(t, current.CreatedAt, current.Files[1].CreatedAt)

	changed, err := store.TransitionPartialPoolMember(t.Context(), stale.ID, stale.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = store.TransitionPartialPoolMember(t.Context(), current.ID, current.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	changed, err = store.TransitionPartialPoolFile(t.Context(), stale.Files[1].ID, stale.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.False(t, changed)
	changed, err = store.TransitionPartialPoolFile(t.Context(), current.Files[1].ID, current.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	now := time.Now().UTC()
	claimed, err := store.ClaimPartialPoolDownloader(t.Context(), stale.ID, 0, stale.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, claimed)
	claimed, err = store.ClaimPartialPoolDownloader(t.Context(), current.ID, 0, current.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed)

	statusChanged, err := store.SetPartialPoolStatusIfUnchanged(t.Context(), stalePool.ID, stalePool.UpdatedAt, models.CrossSeedPartialPoolStatusDormant)
	require.NoError(t, err)
	require.False(t, statusChanged)
	statusChanged, err = store.SetPartialPoolStatusIfUnchanged(t.Context(), currentPool.ID, currentPool.UpdatedAt, models.CrossSeedPartialPoolStatusDormant)
	require.NoError(t, err)
	require.True(t, statusChanged)
}

func TestCrossSeedPartialPoolReAdmissionPreservesLiveFileDependencies(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	registration := partialPoolRegistration(t, secondID, firstID, "BBBB", "AAAA", "BBBB", "SOURCE")
	registration.Files[1].ReplaceableAtAdd = true
	pool, source, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)

	dependentRegistration := partialPoolRegistration(t, firstID, secondID, "CCCC", "CCCC", "", "BBBB")
	_, dependent, err := store.RegisterPartialPoolMember(t.Context(), dependentRegistration)
	require.NoError(t, err)
	changed, err := store.TransitionPartialPoolFile(t.Context(), dependent.Files[1].ID, dependent.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &source.Files[1].ID},
	})
	require.NoError(t, err)
	require.True(t, changed)

	reloadedPool, reloadedSource, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.NoError(t, err)
	require.Equal(t, pool.ID, reloadedPool.ID)
	require.Equal(t, source.Files[1].ID, reloadedSource.Files[1].ID)
	require.True(t, reloadedSource.Files[1].ReplaceableAtAdd)

	var reloadedDependent *models.CrossSeedPartialPoolMember
	for _, member := range reloadedPool.Members {
		if member.ID == dependent.ID {
			reloadedDependent = member
			break
		}
	}
	require.NotNil(t, reloadedDependent)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusPropagating, reloadedDependent.Files[1].Status)
	require.NotNil(t, reloadedDependent.Files[1].SourceFileID)
	require.Equal(t, source.Files[1].ID, *reloadedDependent.Files[1].SourceFileID)
}

func TestCrossSeedPartialPoolReAdmissionQuarantinesHardlinkDependencyChain(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	sourceRegistration := partialPoolRegistration(t, secondID, firstID, "BBBB", "AAAA", "BBBB", "SOURCE")
	pool, source, err := store.RegisterPartialPoolMember(t.Context(), sourceRegistration)
	require.NoError(t, err)

	middleRegistration := partialPoolRegistration(t, firstID, secondID, "CCCC", "CCCC", "", "BBBB")
	_, middle, err := store.RegisterPartialPoolMember(t.Context(), middleRegistration)
	require.NoError(t, err)
	changed, err := store.TransitionPartialPoolFile(t.Context(), middle.Files[1].ID, middle.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusAvailable, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &source.Files[1].ID},
	})
	require.NoError(t, err)
	require.True(t, changed)

	leafRegistration := partialPoolRegistration(t, secondID, firstID, "DDDD", "DDDD", "", "CCCC")
	_, leaf, err := store.RegisterPartialPoolMember(t.Context(), leafRegistration)
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolFile(t.Context(), leaf.Files[1].ID, leaf.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &middle.Files[1].ID},
	})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolFile(t.Context(), leaf.Files[1].ID, leaf.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusPropagating}, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolFile(t.Context(), leaf.Files[1].ID, leaf.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusVerifying}, models.CrossSeedPartialPoolFileStatusVerified, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	reflinkRegistration := partialPoolRegistration(t, secondID, firstID, "EEEE", "EEEE", "", "CCCC")
	reflinkRegistration.Member.Mode = models.CrossSeedPartialPoolModeReflink
	_, reflinkDependent, err := store.RegisterPartialPoolMember(t.Context(), reflinkRegistration)
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolFile(t.Context(), reflinkDependent.Files[1].ID, reflinkDependent.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &middle.Files[1].ID},
	})
	require.NoError(t, err)
	require.True(t, changed)

	reAdmission := sourceRegistration
	reAdmission.Files = append([]models.CrossSeedPartialPoolMemberFile(nil), sourceRegistration.Files...)
	reAdmission.Files[1].RelativePath = "Synthetic.Release/replaced-extra.nfo"
	reloadedPool, _, err := store.RegisterPartialPoolMember(t.Context(), reAdmission)
	require.NoError(t, err)
	require.Equal(t, pool.ID, reloadedPool.ID)

	const reason = "propagation source was replaced during re-admission"
	for _, torrentKey := range []string{"cccc", "dddd"} {
		var dependent *models.CrossSeedPartialPoolMember
		for _, member := range reloadedPool.Members {
			if member.TorrentKey == torrentKey {
				dependent = member
				break
			}
		}
		require.NotNil(t, dependent)
		require.Equal(t, models.CrossSeedPartialPoolMemberStatusManual, dependent.Status)
		require.True(t, dependent.ReviewPausePending)
		require.Equal(t, reason, dependent.LastError)
		require.Equal(t, models.CrossSeedPartialPoolFileStatusManual, dependent.Files[1].Status)
		require.Nil(t, dependent.Files[1].SourceFileID)
		require.Equal(t, reason, dependent.Files[1].LastError)
	}

	reloadedReflink := func() *models.CrossSeedPartialPoolMember {
		for _, member := range reloadedPool.Members {
			if member.TorrentKey == "eeee" {
				return member
			}
		}
		return nil
	}()
	require.NotNil(t, reloadedReflink)
	require.NotEqual(t, models.CrossSeedPartialPoolMemberStatusManual, reloadedReflink.Status)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, reloadedReflink.Files[1].Status)
	require.Nil(t, reloadedReflink.Files[1].SourceFileID)
	require.Empty(t, reloadedReflink.Files[1].LastError)
}

func TestCrossSeedPartialPoolInheritsOriginalSource(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()

	firstPool, firstMember, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, firstID, firstID, "member-one", "member-one", "", "original-source"))
	require.NoError(t, err)

	registration := partialPoolRegistration(t, secondID, secondID, "member-two", "member-two", "", "new-source")
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
	pool, member, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, secondID, firstID, "member", "member-v1", "member-v2", "source"))
	require.NoError(t, err)
	now := time.Now().UTC().Truncate(time.Microsecond)
	claimed, err := store.ClaimPartialPoolDownloader(ctx, member.ID, 321, member.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, claimed, "a verification-owned member blocks the pool claim")

	zero := int64(0)
	changed, err := store.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{MissingBytes: &zero})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusBlocked, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.False(t, changed)

	claimed, err = store.ClaimPartialPoolDownloader(ctx, member.ID, 321, member.CreatedAt, now, member.CreatedAt.Add(-time.Second))
	require.NoError(t, err)
	require.False(t, claimed, "a recent admission holds the whole pool")

	_, peer, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, firstID, secondID, "peer", "peer", "", "member"))
	require.NoError(t, err)
	claimed, err = store.ClaimPartialPoolDownloader(ctx, member.ID, 321, member.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, claimed, "one verification-owned peer holds every downloader")
	deferred := models.CrossSeedPartialPoolRecheckPending
	changed, err = store.TransitionPartialPoolMember(ctx, peer.ID, peer.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusVerifying, models.PartialPoolMemberMutation{LastError: &deferred})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolFile(ctx, peer.Files[1].ID, peer.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolFile(ctx, peer.Files[1].ID, peer.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusPropagating}, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)
	claimed, err = store.ClaimPartialPoolDownloader(ctx, member.ID, 321, member.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, claimed, "a deferred member with propagated files still holds the downloader")
	changed, err = store.TransitionPartialPoolFile(ctx, peer.Files[1].ID, peer.Files[1].CreatedAt, []string{models.CrossSeedPartialPoolFileStatusVerifying}, models.CrossSeedPartialPoolFileStatusMissing, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	claimed, err = store.ClaimPartialPoolDownloader(ctx, member.ID, 321, member.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.True(t, claimed, "an unselected initial verifier does not hold the chosen downloader")

	reloaded, err := store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	var claimedMember *models.CrossSeedPartialPoolMember
	for _, candidate := range reloaded.Members {
		if candidate.ID == member.ID {
			claimedMember = candidate
			break
		}
	}
	require.NotNil(t, claimedMember)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusAcquiring, claimedMember.Status)
	require.True(t, claimedMember.StartedByPool)
	require.Equal(t, int64(321), *claimedMember.LastDownloadedBytes)
	require.WithinDuration(t, now, *claimedMember.LastProgressAt, time.Second)

	_, other, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, firstID, secondID, "other", "other", "", "member"))
	require.NoError(t, err)
	changed, err = store.TransitionPartialPoolMember(ctx, other.ID, other.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.True(t, changed)
	claimed, err = store.ClaimPartialPoolDownloader(ctx, other.ID, 0, other.CreatedAt, now, now.Add(time.Hour))
	require.NoError(t, err)
	require.False(t, claimed, "one acquiring member excludes another claim in the same pool")

	requestedTrue := true
	reason := "manual"
	changed, err = store.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusAcquiring}, models.CrossSeedPartialPoolMemberStatusManual, models.PartialPoolMemberMutation{
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

	_, err = store.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusManual}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.Error(t, err)
}

func TestCrossSeedPartialPoolHardlinkRollbackTransitionIsAtomic(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()
	_, member, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, secondID, firstID, "member", "member-v1", "member-v2", "source"))
	require.NoError(t, err)
	file := member.Files[1]
	sourceFileID := member.Files[0].ID

	changed, err := store.TransitionPartialPoolFile(ctx, file.ID, file.CreatedAt, []string{models.CrossSeedPartialPoolFileStatusMissing}, models.CrossSeedPartialPoolFileStatusPropagating, models.PartialPoolFileMutation{
		SourceFileID: models.NullableInt64Update{Set: true, Value: &sourceFileID},
	})
	require.NoError(t, err)
	require.True(t, changed)
	changed, err = store.TransitionPartialPoolFile(ctx, file.ID, file.CreatedAt, []string{models.CrossSeedPartialPoolFileStatusPropagating}, models.CrossSeedPartialPoolFileStatusVerifying, models.PartialPoolFileMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	changed, err = store.TransitionPartialPoolHardlinkRollback(ctx, member.ID, file.ID, member.CreatedAt, file.CreatedAt, models.CrossSeedPartialPoolMemberStatusRechecking)
	require.NoError(t, err)
	require.False(t, changed)
	_, member, err = store.ResolvePartialPoolMember(ctx, secondID, "member")
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
	require.Empty(t, member.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusVerifying, member.Files[1].Status, "the file update must roll back when the member CAS fails")
	require.NotNil(t, member.Files[1].SourceFileID)

	changed, err = store.TransitionPartialPoolHardlinkRollback(ctx, member.ID, member.Files[1].ID, member.CreatedAt, member.Files[1].CreatedAt, member.Status)
	require.NoError(t, err)
	require.True(t, changed)
	_, member, err = store.ResolvePartialPoolMember(ctx, secondID, "member")
	require.NoError(t, err)
	require.Equal(t, models.CrossSeedPartialPoolMemberStatusVerifying, member.Status)
	require.Equal(t, models.CrossSeedPartialPoolRecheckPending, member.LastError)
	require.Equal(t, models.CrossSeedPartialPoolFileStatusMissing, member.Files[1].Status)
	require.Nil(t, member.Files[1].SourceFileID)
}

func TestCrossSeedPartialPoolConcurrentClaimsChooseOnePostgres(t *testing.T) {
	db := testdb.NewMigratedPostgres(t, "partial-pool-concurrent-claims")
	store, _, firstID, secondID := newPartialPoolTestStoreWithDB(t, db)
	ctx := t.Context()

	members := make([]*models.CrossSeedPartialPoolMember, 0, 4)
	for i, key := range []string{"candidate-alpha", "candidate-beta", "candidate-gamma", "candidate-delta"} {
		_, member, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, secondID, firstID, key, key, "", "source"))
		require.NoError(t, err)
		changed, err := store.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
		require.NoError(t, err)
		require.Truef(t, changed, "transition member %d", i)
		members = append(members, member)
	}

	type claimResult struct {
		claimed bool
		err     error
	}
	start := make(chan struct{})
	results := make(chan claimResult, len(members))
	var wg sync.WaitGroup
	now := time.Now().UTC()
	for _, member := range members {
		wg.Add(1)
		go func(member *models.CrossSeedPartialPoolMember) {
			defer wg.Done()
			<-start
			claimed, err := store.ClaimPartialPoolDownloader(ctx, member.ID, 0, member.CreatedAt, now, now.Add(time.Hour))
			results <- claimResult{claimed: claimed, err: err}
		}(member)
	}
	close(start)
	wg.Wait()
	close(results)

	claimedCount := 0
	for result := range results {
		require.NoError(t, result.err)
		if result.claimed {
			claimedCount++
		}
	}
	require.Equal(t, 1, claimedCount, "all concurrent indexer members share one downloader owner")
}

func TestCrossSeedPartialPoolConcurrentRegistrationBlocksClaimPostgres(t *testing.T) {
	db := testdb.NewMigratedPostgres(t, "partial-pool-registration-claim")
	store, _, firstID, secondID := newPartialPoolTestStoreWithDB(t, db)
	ctx := t.Context()

	pool, member, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, secondID, firstID, "candidate-alpha", "candidate-alpha", "", "source"))
	require.NoError(t, err)
	changed, err := store.TransitionPartialPoolMember(ctx, member.ID, member.CreatedAt, []string{models.CrossSeedPartialPoolMemberStatusVerifying}, models.CrossSeedPartialPoolMemberStatusWaiting, models.PartialPoolMemberMutation{})
	require.NoError(t, err)
	require.True(t, changed)

	_, err = db.ExecContext(ctx, `
		CREATE FUNCTION delay_partial_pool_member_insert() RETURNS trigger
		LANGUAGE plpgsql AS $$
		BEGIN
			PERFORM pg_sleep(1);
			RETURN NEW;
		END;
		$$
	`)
	require.NoError(t, err)
	_, err = db.ExecContext(ctx, `
		CREATE TRIGGER delay_partial_pool_member_insert
		BEFORE INSERT ON cross_seed_partial_pool_members
		FOR EACH ROW EXECUTE FUNCTION delay_partial_pool_member_insert()
	`)
	require.NoError(t, err)

	peerRegistration := partialPoolRegistration(t, secondID, firstID, "candidate-beta", "candidate-beta", "", "source")
	registrationDone := make(chan error, 1)
	go func() {
		_, _, registerErr := store.RegisterPartialPoolMember(ctx, peerRegistration)
		registrationDone <- registerErr
	}()
	require.Eventually(t, func() bool {
		var sleeping bool
		queryErr := db.QueryRowContext(ctx, `
			SELECT EXISTS (
				SELECT 1 FROM pg_stat_activity
				WHERE pid <> pg_backend_pid()
				  AND wait_event = 'PgSleep'
				  AND query LIKE '%cross_seed_partial_pool_members%'
			)
		`).Scan(&sleeping)
		return queryErr == nil && sleeping
	}, 3*time.Second, 20*time.Millisecond, "registration did not reach the delayed insert")

	claimed, claimErr := store.ClaimPartialPoolDownloader(ctx, member.ID, 0, member.CreatedAt, time.Now().UTC(), time.Now().UTC().Add(time.Hour))
	require.NoError(t, claimErr)
	require.False(t, claimed, "a claim must re-read every member after the in-flight admission commits")
	require.NoError(t, <-registrationDone)

	reloaded, err := store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.Members, 2)
	for _, candidate := range reloaded.Members {
		require.NotEqual(t, models.CrossSeedPartialPoolMemberStatusAcquiring, candidate.Status)
	}
}

func TestCrossSeedPartialPoolRemovalPreservesOtherMembers(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	ctx := context.Background()
	pool, firstMember, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, firstID, firstID, "one", "one", "", "source"))
	require.NoError(t, err)
	secondRegistration := partialPoolRegistration(t, secondID, firstID, "two", "two", "", "one")
	secondRegistration.SourceAliases = []string{"one"}
	_, secondMember, err := store.RegisterPartialPoolMember(ctx, secondRegistration)
	require.NoError(t, err)

	require.NoError(t, store.MarkPartialPoolMemberRemoved(ctx, firstMember.PoolID, firstMember.ID, firstMember.CreatedAt, "missing"))
	reloaded, err := store.GetPartialPool(ctx, pool.ID)
	require.NoError(t, err)
	require.Len(t, reloaded.Members, 1)
	require.Equal(t, secondMember.ID, reloaded.Members[0].ID)

	readdedRegistration := partialPoolRegistration(t, firstID, secondID, "one", "one", "", "two")
	readdedRegistration.SourceAliases = []string{"two"}
	readdedPool, readdedMember, err := store.RegisterPartialPoolMember(ctx, readdedRegistration)
	require.NoError(t, err)
	require.Equal(t, pool.ID, readdedPool.ID)
	require.NotEqual(t, firstMember.ID, readdedMember.ID)
	require.Len(t, readdedPool.Members, 2)

	require.NoError(t, store.MarkPartialPoolMemberRemoved(ctx, secondMember.PoolID, secondMember.ID, secondMember.CreatedAt, "missing"))
	require.NoError(t, store.MarkPartialPoolMemberRemoved(ctx, readdedMember.PoolID, readdedMember.ID, readdedMember.CreatedAt, "missing"))
	_, err = store.GetPartialPool(ctx, pool.ID)
	require.Error(t, err)
}

func TestCrossSeedPartialPoolRegistrationRollsBackAtomically(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	registration := partialPoolRegistration(t, secondID, firstID, "member", "member", "", "source")
	registration.Files[1].FileIndex = registration.Files[0].FileIndex

	_, _, err := store.RegisterPartialPoolMember(t.Context(), registration)
	require.Error(t, err)
	pools, err := store.ListPartialPoolsForReconciliation(t.Context())
	require.NoError(t, err)
	require.Empty(t, pools)
}

func TestCrossSeedPartialPoolActiveReconciliationListing(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	activePool, _, err := store.RegisterPartialPoolMember(t.Context(), partialPoolRegistration(t, firstID, firstID, "active-member", "active-member", "", "active-source"))
	require.NoError(t, err)
	dormantPool, _, err := store.RegisterPartialPoolMember(t.Context(), partialPoolRegistration(t, secondID, secondID, "dormant-member", "dormant-member", "", "dormant-source"))
	require.NoError(t, err)
	require.NotEqual(t, activePool.ID, dormantPool.ID)
	require.NoError(t, store.SetPartialPoolStatus(t.Context(), dormantPool.ID, models.CrossSeedPartialPoolStatusDormant))

	pools, err := store.ListActivePartialPoolsForReconciliation(t.Context())
	require.NoError(t, err)
	require.Len(t, pools, 1)
	require.Equal(t, activePool.ID, pools[0].ID)

	pools, err = store.ListPartialPoolsForReconciliation(t.Context())
	require.NoError(t, err)
	require.Len(t, pools, 2, "startup recovery and settled audits retain dormant pools")
}

func TestCrossSeedPartialPoolRecheckObservationListing(t *testing.T) {
	store, _, firstID, secondID := newPartialPoolTestStore(t)
	register := func(torrentKey, sourceKey, status, lastError string) *models.CrossSeedPartialPoolMember {
		t.Helper()
		registration := partialPoolRegistration(t, secondID, firstID, torrentKey, torrentKey, "", sourceKey)
		registration.Member.Status = status
		registration.Member.LastError = lastError
		_, member, err := store.RegisterPartialPoolMember(t.Context(), registration)
		require.NoError(t, err)
		return member
	}

	verifying := register("verifying", "verifying-source", models.CrossSeedPartialPoolMemberStatusVerifying, "partial pool recheck requested")
	rechecking := register("rechecking", "rechecking-source", models.CrossSeedPartialPoolMemberStatusRechecking, "partial pool recheck requested")
	register("complete", "complete-source", models.CrossSeedPartialPoolMemberStatusComplete, "partial pool recheck requested")
	register("manual", "manual-source", models.CrossSeedPartialPoolMemberStatusManual, "partial pool recheck requested")
	register("pending", "pending-source", models.CrossSeedPartialPoolMemberStatusVerifying, models.CrossSeedPartialPoolRecheckPending)

	members, err := store.ListPartialPoolMembersAwaitingRecheckObservation(t.Context())
	require.NoError(t, err)
	require.Len(t, members, 2)
	require.Equal(t, []int64{verifying.ID, rechecking.ID}, []int64{members[0].ID, members[1].ID})
	for _, member := range members {
		require.Empty(t, member.Files)
	}
}

func TestCrossSeedPartialPoolRegistrationReportsInvalidFileField(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*models.CrossSeedPartialPoolMemberFile)
		message string
	}{
		{
			name:    "negative index",
			mutate:  func(file *models.CrossSeedPartialPoolMemberFile) { file.FileIndex = -1 },
			message: "partial pool file index must be non-negative: -1",
		},
		{
			name:    "empty path",
			mutate:  func(file *models.CrossSeedPartialPoolMemberFile) { file.RelativePath = "" },
			message: "partial pool file relative path is required",
		},
		{
			name:    "negative size",
			mutate:  func(file *models.CrossSeedPartialPoolMemberFile) { file.SizeBytes = -1 },
			message: "partial pool file size must be non-negative: -1",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, _, firstID, secondID := newPartialPoolTestStore(t)
			registration := partialPoolRegistration(t, secondID, firstID, "member", "member", "", "source")
			test.mutate(&registration.Files[0])

			_, _, err := store.RegisterPartialPoolMember(t.Context(), registration)
			require.EqualError(t, err, test.message)
		})
	}
}

func TestCrossSeedPartialPoolInstanceCascadePreservesOtherMembers(t *testing.T) {
	store, instanceStore, firstID, secondID := newPartialPoolTestStore(t)
	ctx := t.Context()
	pool, _, err := store.RegisterPartialPoolMember(ctx, partialPoolRegistration(t, firstID, firstID, "one", "one", "", "source"))
	require.NoError(t, err)
	registration := partialPoolRegistration(t, secondID, firstID, "two", "two", "", "one")
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
