// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer_test

import (
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/transfer"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	source = 1
	target = 2
)

func engines(t *testing.T, run func(t *testing.T, db *database.DB)) {
	t.Run("sqlite", func(t *testing.T) {
		t.Parallel()
		run(t, testdb.NewMigratedSQLite(t, "transfer-records"))
	})
	t.Run("postgres", func(t *testing.T) {
		run(t, testdb.NewMigratedPostgres(t, "transfer-records"))
	})
}

func open(t *testing.T, db *database.DB) *transfer.Store {
	t.Helper()
	store, err := transfer.NewStore(t.Context(), db)
	require.NoError(t, err)
	return store
}

func group(hashes ...string) *transfer.Record {
	record := &transfer.Record{
		Kind:             transfer.KindTransfer,
		State:            "copying",
		StartedBy:        transfer.StartedByUser,
		SourceInstanceID: source,
		TargetInstanceID: target,
	}
	for _, hash := range hashes {
		record.Members = append(record.Members, transfer.Member{
			Hash:             hash,
			Name:             "Name " + hash,
			SourceSavePath:   "/src/" + hash,
			TargetInstanceID: target,
			Placement:        transfer.Placement{SavePath: "/dst/" + hash},
		})
	}
	return record
}

func TestStartClaimsTheGroupInOneWritePostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		require.NoError(t, store.Create(t.Context(), group("aaaa", "bbbb")))

		require.True(t, store.IsClaimed(source, "aaaa"))
		require.True(t, store.IsClaimed(source, "bbbb"))
		require.False(t, store.IsClaimed(target, "aaaa"), "a claim holds the torrent on its own instance only")

		err := store.Create(t.Context(), group("cccc", "bbbb"))
		require.ErrorIs(t, err, transfer.ErrClaimed)
		require.False(t, store.IsClaimed(source, "cccc"), "a refused claim claims no torrent of its group")

		reopened := open(t, db)
		require.True(t, reopened.IsClaimed(source, "aaaa"))
		require.True(t, reopened.IsClaimed(source, "bbbb"))
		require.False(t, reopened.IsClaimed(source, "cccc"))
		folders, err := reopened.RunningFolders(t.Context(), source)
		require.NoError(t, err)
		require.ElementsMatch(t, []string{"/src/aaaa", "/src/bbbb"}, folders, "the refused Transfer is not stored")
	})
}

func TestOneOfManyClaimsOfATorrentWinsPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)

		const callers = 8
		start := make(chan struct{})
		errs := make([]error, callers)
		var wg sync.WaitGroup
		for i := range callers {
			wg.Go(func() {
				<-start
				errs[i] = store.Create(t.Context(), group("aaaa"))
			})
		}
		close(start)
		wg.Wait()

		won := 0
		for _, err := range errs {
			if err == nil {
				won++
				continue
			}
			require.ErrorIs(t, err, transfer.ErrClaimed)
		}
		require.Equal(t, 1, won)
	})
}

func TestFinishReleasesTheClaimsAndTheFoldersPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		record := group("aaaa")
		require.NoError(t, store.Create(t.Context(), record))

		folders, err := store.RunningFolders(t.Context(), target)
		require.NoError(t, err)
		require.Equal(t, []string{"/dst/aaaa"}, folders)

		require.NoError(t, store.Finish(t.Context(), record.ID, "done"))

		require.False(t, store.IsClaimed(source, "aaaa"))
		require.False(t, open(t, db).IsClaimed(source, "aaaa"))
		for _, instanceID := range []int{source, target} {
			folders, err := store.RunningFolders(t.Context(), instanceID)
			require.NoError(t, err)
			require.Empty(t, folders)
		}
		require.NoError(t, store.Create(t.Context(), group("aaaa")), "a finished Transfer no longer holds its torrents")
	})
}

func TestRecordKeepsEveryFieldPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		ruleID := 7
		retryAt := time.Date(2026, 11, 16, 20, 0, 0, 0, time.UTC)
		at := time.Date(2026, 11, 16, 19, 0, 0, 0, time.UTC)
		record := &transfer.Record{
			Kind:             transfer.KindExport,
			Way:              "sftp",
			State:            "waiting",
			StartedBy:        transfer.StartedByRule,
			RuleID:           &ruleID,
			SourceInstanceID: source,
			TargetInstanceID: target,
			SourceEndpoint:   fsops.Endpoint{URL: "http://seedbox.test:8080", Mode: models.FilesystemModeRemote, Walked: "qui@seedbox.test:22#pin.key"},
			TargetEndpoint:   fsops.Endpoint{URL: "http://home.test:8080", Mode: models.FilesystemModeLocal},
			Options:          transfer.Options{After: transfer.AfterKeepFiles, LeaveStopped: true, Paused: true, SkipChecking: new(false)},
			Steps:            []transfer.Step{{Name: "copy", At: at, Detail: "file 1 of 2"}},
			Log:              []transfer.LogLine{{At: at, Message: "Connected over SFTP"}},
			Members: []transfer.Member{{
				Hash:             "aaaa",
				Name:             "Name aaaa",
				SourceSavePath:   "/src",
				TargetInstanceID: 3,
				Placement:        transfer.Placement{SavePath: "/dst", Category: "tv", Tags: []string{"moved"}, ContentLayout: "Original", SavePathOfTarget: true, AutoTMM: true},
				ProvenPieces:     []byte{0b1010_0000},
			}},
			FlushState:    transfer.FlushConfirmed,
			ReservedBytes: 5 << 30,
			Size:          8 << 30,
			RetryAt:       retryAt,
			FailureClass:  transfer.FailureTemporary,
			FailureReason: "the source went offline",
		}
		require.NoError(t, store.Create(t.Context(), record))
		require.NotZero(t, record.ID)

		got, err := store.Get(t.Context(), record.ID)
		require.NoError(t, err)
		require.False(t, got.CreatedAt.IsZero())
		require.True(t, got.FinishedAt.IsZero())
		got.CreatedAt = record.CreatedAt
		require.Equal(t, record, got)

		require.NoError(t, store.Finish(t.Context(), record.ID, "failed"))
		got, err = store.Get(t.Context(), record.ID)
		require.NoError(t, err)
		require.Equal(t, transfer.State("failed"), got.State)
		require.False(t, got.FinishedAt.IsZero())
	})
}

func TestTheRegistryKnowsAHelperFromItsRowPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		helper := transfer.Helper{InstanceID: target, Hash: "ffff", Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: 4}
		require.False(t, store.IsHelper(target, "ffff"))

		require.NoError(t, store.RegisterHelper(t.Context(), helper))
		require.NoError(t, store.RegisterHelper(t.Context(), helper), "recovery registers the helper that it adds again")

		require.True(t, store.IsHelper(target, "ffff"))
		require.Equal(t, "share", store.HelperKind(target, "ffff"))
		require.False(t, store.IsHelper(source, "ffff"), "the row names one instance")

		reopened := open(t, db)
		require.True(t, reopened.IsHelper(target, "ffff"))
		helpers, err := reopened.HelpersOf(t.Context(), transfer.OwnerShare, 4)
		require.NoError(t, err)
		require.Len(t, helpers, 1)
		require.False(t, helpers[0].CreatedAt.IsZero())
		helpers[0].CreatedAt = time.Time{}
		require.Equal(t, helper, helpers[0])

		other := helper
		other.OwnerID = 5
		require.ErrorIs(t, store.RegisterHelper(t.Context(), other), transfer.ErrHelperOwned)
	})
}

func TestHoldNamesWhyQuiKeepsATorrent(t *testing.T) {
	t.Parallel()

	store := open(t, testdb.NewMigratedSQLite(t, "transfer-records"))
	require.NoError(t, store.Create(t.Context(), group("aaaa")))
	require.NoError(t, store.RegisterHelper(t.Context(), transfer.Helper{InstanceID: target, Hash: "ffff", Random: []byte("salt"), Owner: transfer.OwnerTransfer, OwnerID: 1}))

	require.ErrorIs(t, store.Hold(source, "aaaa"), transfer.ErrClaimed)
	require.ErrorIs(t, store.Hold(target, "ffff"), transfer.ErrHelper)
	require.NoError(t, store.Hold(source, "bbbb"))
	require.ErrorIs(t, store.Hold(source, "AAAA"), transfer.ErrClaimed, "a hash in upper case names the same torrent")
}

func TestTheChecksOfAnInstanceNameWhatQuiKeepsThere(t *testing.T) {
	t.Parallel()

	store := open(t, testdb.NewMigratedSQLite(t, "transfer-records"))
	require.Nil(t, store.HelperKinds(target))
	require.Nil(t, store.ClaimCheck(source))

	require.NoError(t, store.Create(t.Context(), group("aaaa")))
	require.NoError(t, store.RegisterHelper(t.Context(), transfer.Helper{InstanceID: target, Hash: "ffff", Random: []byte("salt"), Owner: transfer.OwnerShare, OwnerID: 1}))
	require.Nil(t, store.HelperKinds(source), "a claim is no helper torrent")
	require.Equal(t, map[string]string{"ffff": "share"}, store.HelperKinds(target))

	isClaimed := store.ClaimCheck(source)
	require.True(t, isClaimed("AAAA"))
	require.False(t, isClaimed("ffff"))
	require.Nil(t, store.ClaimCheck(target), "the claim holds the torrent on the source only, and a helper torrent is no claim")
}

func TestSaveKeepsTheProgressPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		record := group("aaaa", "bbbb")
		require.NoError(t, store.Create(t.Context(), record))
		at := time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)

		record.State = transfer.StateFailed
		record.Steps = append(record.Steps, transfer.Step{Name: "copy", At: at})
		record.Log = append(record.Log, transfer.LogLine{At: at, Message: "copied"})
		record.FlushState = transfer.FlushConfirmed
		record.ReservedBytes = 64
		record.FailureClass, record.FailureReason = transfer.FailurePermanent, "conflict"
		require.NoError(t, store.Save(t.Context(), record))
		require.NoError(t, store.SaveProven(t.Context(), record.ID, 1, []byte{0b1010_0000}))

		got, err := open(t, db).Get(t.Context(), record.ID)
		require.NoError(t, err)
		require.Equal(t, transfer.StateFailed, got.State)
		require.Equal(t, []transfer.Step{{Name: "copy", At: at}}, normalizeSteps(got.Steps))
		require.Len(t, got.Log, 1)
		require.Equal(t, "copied", got.Log[0].Message)
		require.Equal(t, transfer.FlushConfirmed, got.FlushState)
		require.Equal(t, int64(64), got.ReservedBytes)
		require.Equal(t, transfer.FailurePermanent, got.FailureClass)
		require.Equal(t, "conflict", got.FailureReason)
		require.Empty(t, got.Members[0].ProvenPieces)
		require.Equal(t, []byte{0b1010_0000}, got.Members[1].ProvenPieces)
		require.True(t, got.FinishedAt.IsZero(), "a save does not finish the Transfer")
	})
}

func normalizeSteps(steps []transfer.Step) []transfer.Step {
	for i := range steps {
		steps[i].At = steps[i].At.UTC()
	}
	return steps
}

func TestTheCleanupKeepsAPermanentFailureThatWaitsForTheUserPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		end := func(record *transfer.Record, class transfer.FailureClass) int64 {
			t.Helper()
			require.NoError(t, store.Create(t.Context(), record))
			record.State, record.FailureClass = transfer.StateFailed, class
			require.NoError(t, store.Save(t.Context(), record))
			require.NoError(t, store.Finish(t.Context(), record.ID, transfer.StateFailed))
			return record.ID
		}
		exists := func(id int64) bool {
			t.Helper()
			_, err := store.Get(t.Context(), id)
			return err == nil
		}
		cleanup := func() int {
			t.Helper()
			n, err := store.RemoveFinishedBefore(t.Context(), time.Now().Add(time.Hour), func(int, string, time.Time) bool { return true })
			require.NoError(t, err)
			return n
		}

		permanent := end(group("aaaa", "bbbb"), transfer.FailurePermanent)
		temporary := end(group("cccc"), transfer.FailureTemporary)
		require.Equal(t, 1, cleanup())
		require.False(t, exists(temporary))
		require.True(t, exists(permanent))
		waits, err := store.WaitsForUser(t.Context(), source, "aaaa")
		require.NoError(t, err)
		require.True(t, waits, "the rule stays off")

		retry := group("aaaa", "bbbb")
		require.NoError(t, store.Create(t.Context(), retry))
		require.Equal(t, 1, cleanup())
		require.False(t, exists(permanent), "a later Transfer of each torrent exists")
		require.True(t, exists(retry.ID))
	})
}

func TestTheLastPermanentFailureWaitsForTheUserPostgresIntegration(t *testing.T) {
	t.Parallel()

	engines(t, func(t *testing.T, db *database.DB) {
		store := open(t, db)
		waits := func(hash string) bool {
			t.Helper()
			got, err := store.WaitsForUser(t.Context(), source, hash)
			require.NoError(t, err)
			return got
		}
		end := func(record *transfer.Record, class transfer.FailureClass) {
			t.Helper()
			require.NoError(t, store.Create(t.Context(), record))
			record.State, record.FailureClass = transfer.StateFailed, class
			require.NoError(t, store.Save(t.Context(), record))
			require.NoError(t, store.Finish(t.Context(), record.ID, transfer.StateFailed))
		}

		require.False(t, waits("aaaa"), "a torrent with no Transfer")
		end(group("aaaa", "bbbb"), transfer.FailurePermanent)
		require.True(t, waits("aaaa"))
		require.True(t, waits("BBBB"), "each member, by any case of its hash")
		end(group("aaaa"), transfer.FailureTemporary)
		require.False(t, waits("aaaa"), "a later Transfer ended the wait")
		require.True(t, waits("bbbb"))
	})
}
