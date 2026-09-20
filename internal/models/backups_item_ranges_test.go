// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models_test

import (
	"context"
	"fmt"
	"maps"
	"math/rand/v2"
	"reflect"
	"slices"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

func TestBackupItemRangesSQLite(t *testing.T) {
	t.Parallel()
	runBackupItemRangeTests(t, func(t *testing.T) *database.DB { return testdb.NewMigratedSQLite(t, "backup-item-ranges") })
}

func TestBackupItemRangesPostgresIntegration(t *testing.T) {
	t.Parallel()
	runBackupItemRangeTests(t, func(t *testing.T) *database.DB { return testdb.NewMigratedPostgres(t, "backup-item-ranges") })
}

func runBackupItemRangeTests(t *testing.T, open func(t *testing.T) *database.DB) {
	t.Run("matches one snapshot per run under random history", func(t *testing.T) {
		checkRandomBackupHistory(t, open(t))
	})
	t.Run("every item field is part of a snapshot", func(t *testing.T) {
		checkEveryItemFieldIsVersioned(t, open(t))
	})
	t.Run("unchanged snapshot writes no rows", func(t *testing.T) {
		checkUnchangedSnapshotWritesNothing(t, open(t))
	})
	t.Run("commits race with deleting the newest run", func(t *testing.T) {
		checkCommitRacesNewestRunDelete(t, open(t))
	})
	t.Run("item writers wait for each other on postgres", func(t *testing.T) {
		checkItemWritersSerialize(t, open(t))
	})
	t.Run("deleting a run whose items are committing leaves no uncovered rows", func(t *testing.T) {
		checkDeleteDuringCommit(t, open(t))
	})
	t.Run("concurrent commits take distinct snapshot sequences", func(t *testing.T) {
		checkConcurrentCommits(t, open(t))
	})
}

// waitForLockWaiters blocks until want writers are queued on the instance lock,
// so the Postgres tests order their writers without sleeping for a fixed time.
func waitForLockWaiters(t *testing.T, db *database.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		var waiting int
		require.NoError(t, db.QueryRowContext(t.Context(),
			"SELECT COUNT(*) FROM pg_locks WHERE locktype = 'advisory' AND NOT granted AND classid = CAST(? AS INTEGER)",
			models.BackupItemsLockClass).Scan(&waiting))
		if waiting >= want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %d writers on the instance lock, saw %d", want, waiting)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

type backupFixture struct {
	t     *testing.T
	ctx   context.Context
	db    *database.DB
	store *models.BackupStore
}

func newBackupFixture(t *testing.T, db *database.DB) *backupFixture {
	return &backupFixture{t: t, ctx: t.Context(), db: db, store: models.NewBackupStore(db)}
}

func (f *backupFixture) instance(name string) int {
	f.t.Helper()
	instances, err := models.NewInstanceStore(f.db, []byte("01234567890123456789012345678901"))
	require.NoError(f.t, err)
	inst, err := instances.Create(f.ctx, name, "http://localhost:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(f.t, err)
	return inst.ID
}

func (f *backupFixture) run(instanceID int) int64 {
	f.t.Helper()
	run := &models.BackupRun{
		InstanceID:  instanceID,
		Kind:        models.BackupRunKindHourly,
		Status:      models.BackupRunStatusSuccess,
		RequestedBy: "test",
		RequestedAt: time.Now().UTC(),
	}
	require.NoError(f.t, f.store.CreateRun(f.ctx, run))
	return run.ID
}

func (f *backupFixture) rowCount() int {
	f.t.Helper()
	var n int
	require.NoError(f.t, f.db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM instance_backup_items").Scan(&n))
	return n
}

// itemKey is a BackupItem's snapshot content, independent of storage ids.
func itemKey(item models.BackupItem) string {
	s := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return fmt.Sprintf("%q", *p)
	}
	return fmt.Sprintf("%s|%s|%s|%d|%s|%s|%s|%s|%s|%s", item.TorrentHash, item.Name, s(item.Category), item.SizeBytes,
		s(item.ArchiveRelPath), s(item.InfoHashV1), s(item.InfoHashV2), s(item.Tags), s(item.TorrentBlobPath), s(item.SavePath))
}

func sortedKeys(items []models.BackupItem) []string {
	keys := make([]string, len(items))
	for i, item := range items {
		keys[i] = itemKey(item)
	}
	sort.Strings(keys)
	return keys
}

func randomItem(rng *rand.Rand, n int) models.BackupItem {
	item := models.BackupItem{
		TorrentHash: fmt.Sprintf("%040x", n),
		Name:        fmt.Sprintf("Some.Title.%d.1080p-GRP", n),
		SizeBytes:   rng.Int64N(1 << 40),
	}
	optional := []**string{&item.Category, &item.ArchiveRelPath, &item.InfoHashV1, &item.InfoHashV2, &item.Tags, &item.TorrentBlobPath, &item.SavePath}
	for i, field := range optional {
		if rng.IntN(4) > 0 {
			*field = new(fmt.Sprintf("v%d-%d", i, rng.IntN(3)))
		}
	}
	return item
}

// mutateItem changes one field, often back to a value it held before, the way
// a tracker tag or category flips.
func mutateItem(rng *rand.Rand, item models.BackupItem) models.BackupItem {
	v := reflect.ValueOf(&item).Elem()
	fields := versionedItemFields()
	field := v.FieldByName(fields[rng.IntN(len(fields))])
	switch field.Kind() {
	case reflect.String:
		field.SetString(fmt.Sprintf("name-%d", rng.IntN(3)))
	case reflect.Int64:
		field.SetInt(int64(rng.IntN(3)))
	case reflect.Pointer:
		if rng.IntN(4) == 0 {
			field.Set(reflect.Zero(field.Type()))
		} else {
			field.Set(reflect.ValueOf(new(fmt.Sprintf("m-%d", rng.IntN(3)))))
		}
	default:
		panic("unhandled field kind " + field.Kind().String())
	}
	return item
}

// versionedItemFields lists the BackupItem fields that describe a torrent's
// state, i.e. everything except storage bookkeeping.
func versionedItemFields() []string {
	var names []string
	typ := reflect.TypeFor[models.BackupItem]()
	for field := range typ.Fields() {
		switch name := field.Name; name {
		case "ID", "RunID", "CreatedAt":
		case "TorrentHash":
			// Changing the hash is a different torrent, covered by add/remove.
		default:
			names = append(names, name)
		}
	}
	return names
}

type modelRun struct {
	id       int64
	instance int
	items    []models.BackupItem // nil when the run never committed items
}

func checkRandomBackupHistory(t *testing.T, db *database.DB) {
	f := newBackupFixture(t, db)
	rng := rand.New(rand.NewPCG(7, 11))
	instances := []int{f.instance("ranges-a"), f.instance("ranges-b")}

	libraries := map[int]map[string]models.BackupItem{}
	nextTorrent := 0
	for _, inst := range instances {
		libraries[inst] = map[string]models.BackupItem{}
		for range 12 {
			item := randomItem(rng, nextTorrent)
			nextTorrent++
			libraries[inst][item.TorrentHash] = item
		}
	}

	var retained []modelRun // commit order
	removeRun := func(i int) {
		require.NoError(t, f.store.CleanupRun(f.ctx, retained[i].id))
		retained = slices.Delete(retained, i, i+1)
	}

	for step := range 160 {
		inst := instances[rng.IntN(len(instances))]
		lib := libraries[inst]
		switch op := rng.IntN(20); {
		case op < 11:
			for range rng.IntN(4) {
				hashes := make([]string, 0, len(lib))
				for h := range lib {
					hashes = append(hashes, h)
				}
				sort.Strings(hashes)
				switch rng.IntN(3) {
				case 0:
					item := randomItem(rng, nextTorrent)
					nextTorrent++
					lib[item.TorrentHash] = item
				case 1:
					if len(hashes) > 1 {
						delete(lib, hashes[rng.IntN(len(hashes))])
					}
				default:
					h := hashes[rng.IntN(len(hashes))]
					lib[h] = mutateItem(rng, lib[h])
				}
			}
			items := make([]models.BackupItem, 0, len(lib)+1)
			for _, item := range lib {
				items = append(items, item)
			}
			if rng.IntN(10) == 0 {
				// Imported manifests can list a torrent twice.
				items = append(items, items[rng.IntN(len(items))])
			}
			id := f.run(inst)
			require.NoError(t, f.store.InsertItems(f.ctx, id, items), "step %d", step)
			retained = append(retained, modelRun{id: id, instance: inst, items: items})
		case op < 12:
			retained = append(retained, modelRun{id: f.run(inst), instance: inst})
		case op < 15 && len(retained) > 0:
			removeRun(rng.IntN(len(retained)))
		case op < 17:
			for i, r := range slices.Backward(retained) {
				if r.instance == inst {
					removeRun(i)
					break
				}
			}
		default:
			var ofInstance []int
			for i, r := range retained {
				if r.instance == inst {
					ofInstance = append(ofInstance, i)
				}
			}
			if len(ofInstance) > 4 {
				removeRun(ofInstance[0])
			}
		}

		verifyBackupHistory(t, f, rng, retained, step)
	}
}

func verifyBackupHistory(t *testing.T, f *backupFixture, rng *rand.Rand, retained []modelRun, step int) {
	t.Helper()

	blobs := map[string]struct{}{}
	allIDs := make([]int64, 0, len(retained))
	total := 0
	for _, r := range retained {
		allIDs = append(allIDs, r.id)
		total += len(r.items)

		got, err := f.store.ListItems(f.ctx, r.id)
		require.NoError(t, err)
		gotItems := make([]models.BackupItem, len(got))
		for i, item := range got {
			require.Equal(t, r.id, item.RunID)
			gotItems[i] = *item
		}
		require.Equal(t, sortedKeys(r.items), sortedKeys(gotItems), "step %d run %d", step, r.id)

		if len(r.items) > 0 {
			want := r.items[rng.IntN(len(r.items))]
			item, err := f.store.GetItemByHash(f.ctx, r.id, want.TorrentHash)
			require.NoError(t, err)
			require.Equal(t, itemKey(want), itemKey(*item), "step %d run %d", step, r.id)
		}
		for _, item := range r.items {
			if item.TorrentBlobPath != nil {
				blobs[*item.TorrentBlobPath] = struct{}{}
			}
		}
	}

	forRuns, err := f.store.ListItemsForRuns(f.ctx, allIDs)
	require.NoError(t, err)
	require.Len(t, forRuns, total, "step %d", step)

	// Blob cleanup trusts that any stored row is still referenced by a retained run.
	paths, err := f.store.ListTorrentBlobPaths(f.ctx)
	require.NoError(t, err)
	require.ElementsMatch(t, slices.Collect(maps.Keys(blobs)), paths, "step %d", step)

	// The cached blob for a hash is the one from the newest run holding it.
	for _, r := range retained {
		for _, item := range r.items {
			want := ""
			for _, later := range retained {
				if later.instance != r.instance {
					continue
				}
				for _, other := range later.items {
					if other.TorrentHash == item.TorrentHash && other.TorrentBlobPath != nil {
						want = *other.TorrentBlobPath
					}
				}
			}
			got, err := f.store.FindCachedTorrentBlob(f.ctx, r.instance, item.TorrentHash)
			require.NoError(t, err)
			if want == "" {
				require.Nil(t, got, "step %d hash %s", step, item.TorrentHash)
			} else {
				require.NotNil(t, got, "step %d hash %s", step, item.TorrentHash)
				require.Equal(t, want, *got, "step %d hash %s", step, item.TorrentHash)
			}
		}
	}
}

func checkEveryItemFieldIsVersioned(t *testing.T, db *database.DB) {
	for i, field := range versionedItemFields() {
		t.Run(field, func(t *testing.T) {
			f := newBackupFixture(t, db)
			inst := f.instance("field-" + field)
			base := models.BackupItem{
				TorrentHash: fmt.Sprintf("%040x", i), Name: "Name", SizeBytes: 1,
				Category: new("c"), ArchiveRelPath: new("a"), InfoHashV1: new("v1"), InfoHashV2: new("v2"),
				Tags: new("t"), TorrentBlobPath: new("b"), SavePath: new("/s"),
			}
			changed := base
			v := reflect.ValueOf(&changed).Elem().FieldByName(field)
			switch v.Kind() {
			case reflect.String:
				v.SetString("Other")
			case reflect.Int64:
				v.SetInt(2)
			case reflect.Pointer:
				v.Set(reflect.ValueOf(new("other")))
			default:
				t.Fatalf("unhandled kind %s for %s", v.Kind(), field)
			}

			first, second := f.run(inst), f.run(inst)
			require.NoError(t, f.store.InsertItems(f.ctx, first, []models.BackupItem{base}))
			require.NoError(t, f.store.InsertItems(f.ctx, second, []models.BackupItem{changed}))

			for runID, want := range map[int64]models.BackupItem{first: base, second: changed} {
				got, err := f.store.ListItems(f.ctx, runID)
				require.NoError(t, err)
				require.Len(t, got, 1)
				require.Equal(t, itemKey(want), itemKey(*got[0]))
			}
		})
	}
}

func checkUnchangedSnapshotWritesNothing(t *testing.T, db *database.DB) {
	f := newBackupFixture(t, db)
	inst := f.instance("unchanged")
	rng := rand.New(rand.NewPCG(1, 2))
	items := make([]models.BackupItem, 50)
	for i := range items {
		items[i] = randomItem(rng, i)
	}

	require.NoError(t, f.store.InsertItems(f.ctx, f.run(inst), items))
	before := f.rowCount()
	for range 3 {
		require.NoError(t, f.store.InsertItems(f.ctx, f.run(inst), items))
	}
	require.Equal(t, before, f.rowCount())

	items[0].Tags = new("changed")
	require.NoError(t, f.store.InsertItems(f.ctx, f.run(inst), items))
	require.Equal(t, before+1, f.rowCount())
}

// checkCommitRacesNewestRunDelete deletes the newest run while the next
// snapshot commits. The commit reuses rows opened by the run being deleted, so
// cleanup must not drop them underneath it.
func checkCommitRacesNewestRunDelete(t *testing.T, db *database.DB) {
	f := newBackupFixture(t, db)
	inst := f.instance("race")
	rng := rand.New(rand.NewPCG(3, 4))
	items := make([]models.BackupItem, 200)
	for i := range items {
		items[i] = randomItem(rng, i)
	}

	for round := range 15 {
		previous := f.run(inst)
		require.NoError(t, f.store.InsertItems(f.ctx, previous, items))
		next := f.run(inst)

		var wg sync.WaitGroup
		var commitErr, deleteErr error
		wg.Go(func() { commitErr = f.store.InsertItems(f.ctx, next, items) })
		wg.Go(func() { deleteErr = f.store.CleanupRun(f.ctx, previous) })
		wg.Wait()
		require.NoError(t, commitErr, "round %d", round)
		require.NoError(t, deleteErr, "round %d", round)

		got, err := f.store.ListItems(f.ctx, next)
		require.NoError(t, err)
		gotItems := make([]models.BackupItem, len(got))
		for i, item := range got {
			gotItems[i] = *item
		}
		require.Equal(t, sortedKeys(items), sortedKeys(gotItems), "round %d", round)
		require.NoError(t, f.store.CleanupRun(f.ctx, next))
	}

	var remaining int
	require.NoError(t, db.QueryRowContext(f.ctx, "SELECT COUNT(*) FROM instance_backup_items").Scan(&remaining))
	require.Zero(t, remaining)
}

// checkItemWritersSerialize holds the per-instance lock the way an in-flight
// commit does and requires both item writers to wait for it. The race it
// prevents (cleanup deleting open rows a commit is reusing) is too narrow for
// the timing test above to hit reliably.
func checkItemWritersSerialize(t *testing.T, db *database.DB) {
	if db.Dialect() != string(database.DialectPostgres) {
		t.Skip("SQLite serializes all writes on one connection")
	}
	f := newBackupFixture(t, db)
	inst := f.instance("serialize")
	items := []models.BackupItem{randomItem(rand.New(rand.NewPCG(5, 6)), 1)}
	older := f.run(inst)
	require.NoError(t, f.store.InsertItems(f.ctx, older, items))
	newer := f.run(inst)

	for name, write := range map[string]func() error{
		"InsertItems": func() error { return f.store.InsertItems(f.ctx, newer, items) },
		"CleanupRuns": func() error { return f.store.CleanupRun(f.ctx, older) },
	} {
		holder, err := db.BeginTx(f.ctx, nil)
		require.NoError(t, err)
		_, err = holder.ExecContext(f.ctx, "SELECT pg_advisory_xact_lock(CAST(? AS INTEGER), CAST(? AS INTEGER))", models.BackupItemsLockClass, inst)
		require.NoError(t, err)

		done := make(chan error, 1)
		go func() { done <- write() }()
		waitForLockWaiters(t, db, 1)
		select {
		case err := <-done:
			_ = holder.Rollback()
			t.Fatalf("%s finished while another writer held the instance lock (err=%v)", name, err)
		default:
		}
		require.NoError(t, holder.Rollback())
		require.NoError(t, <-done, name)
	}
}

// checkDeleteDuringCommit deletes a run while its items wait to commit behind
// the instance lock. Cleanup must read the run's snapshot after that commit,
// or the committed rows outlive their run.
func checkDeleteDuringCommit(t *testing.T, db *database.DB) {
	if db.Dialect() != string(database.DialectPostgres) {
		t.Skip("SQLite serializes all writes on one connection")
	}
	f := newBackupFixture(t, db)
	inst := f.instance("delete-during-commit")
	rng := rand.New(rand.NewPCG(8, 9))
	kept := make([]models.BackupItem, 20)
	for i := range kept {
		kept[i] = randomItem(rng, i)
	}
	older := f.run(inst)
	require.NoError(t, f.store.InsertItems(f.ctx, older, kept))

	changed := slices.Clone(kept)
	for i := range 5 {
		changed[i].Tags = new(fmt.Sprintf("changed-%d", i))
	}
	committing := f.run(inst)

	holder, err := db.BeginTx(f.ctx, nil)
	require.NoError(t, err)
	_, err = holder.ExecContext(f.ctx, "SELECT pg_advisory_xact_lock(CAST(? AS INTEGER), CAST(? AS INTEGER))", models.BackupItemsLockClass, inst)
	require.NoError(t, err)

	insertDone := make(chan error, 1)
	go func() { insertDone <- f.store.InsertItems(f.ctx, committing, changed) }()
	waitForLockWaiters(t, db, 1) // InsertItems queues on the lock first
	cleanupDone := make(chan error, 1)
	go func() { cleanupDone <- f.store.CleanupRun(f.ctx, committing) }()
	waitForLockWaiters(t, db, 2) // cleanup queues behind it
	require.NoError(t, holder.Rollback())
	require.NoError(t, <-insertDone)
	require.NoError(t, <-cleanupDone)

	var uncovered int
	require.NoError(t, db.QueryRowContext(f.ctx, `
		SELECT COUNT(*) FROM instance_backup_items i
		WHERE NOT EXISTS (
			SELECT 1 FROM instance_backup_runs r
			WHERE r.instance_id = i.instance_id AND r.items_seq >= i.from_seq
			  AND (i.to_seq IS NULL OR r.items_seq < i.to_seq))
	`).Scan(&uncovered))
	require.Zero(t, uncovered, "item rows left with no run covering them")

	got, err := f.store.ListItems(f.ctx, older)
	require.NoError(t, err)
	gotItems := make([]models.BackupItem, len(got))
	for i, item := range got {
		gotItems[i] = *item
	}
	require.Equal(t, sortedKeys(kept), sortedKeys(gotItems))
}

// checkConcurrentCommits starts two snapshots for one instance at once. Both
// must commit, with their own sequence number and their own item set.
func checkConcurrentCommits(t *testing.T, db *database.DB) {
	f := newBackupFixture(t, db)
	inst := f.instance("concurrent-commits")
	rng := rand.New(rand.NewPCG(11, 12))
	first := make([]models.BackupItem, 30)
	for i := range first {
		first[i] = randomItem(rng, i)
	}
	second := slices.Clone(first)
	second[0].Tags = new("second-run")

	runA, runB := f.run(inst), f.run(inst)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	wg.Go(func() { errs <- f.store.InsertItems(f.ctx, runA, first) })
	wg.Go(func() { errs <- f.store.InsertItems(f.ctx, runB, second) })
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}

	var seqA, seqB int64
	require.NoError(t, db.QueryRowContext(f.ctx, "SELECT items_seq FROM instance_backup_runs WHERE id = ?", runA).Scan(&seqA))
	require.NoError(t, db.QueryRowContext(f.ctx, "SELECT items_seq FROM instance_backup_runs WHERE id = ?", runB).Scan(&seqB))
	require.NotEqual(t, seqA, seqB, "concurrent commits reused a snapshot sequence")

	for runID, want := range map[int64][]models.BackupItem{runA: first, runB: second} {
		got, err := f.store.ListItems(f.ctx, runID)
		require.NoError(t, err)
		gotItems := make([]models.BackupItem, len(got))
		for i, item := range got {
			gotItems[i] = *item
		}
		require.Equal(t, sortedKeys(want), sortedKeys(gotItems), "run %d", runID)
	}
}
