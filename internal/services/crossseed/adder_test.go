// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	adderV1     = "1111111111111111111111111111111111111111"
	adderV2     = "2222222222222222222222222222222222222222222222222222222222222222"
	adderBudget = int64(5 << 20)
)

// withAdder gives svc the real Adder over its own qBittorrent fake,
// blocklist store, and Settler.
func withAdder(svc *Service) {
	svc.adder = NewAdder(svc.syncManager, svc.blocklistStore, svc.settler, svc.ResumeBudgetBytes, svc.RunExternalProgram)
}

// adderFake is the qBittorrent seam of the Adder.
type adderFake struct {
	addedIDs    []string
	instanceIDs []int
	existing    *qbt.Torrent
	// existingOn limits existing to one instance when it is set.
	existingOn int
	findErr    error
	addErr     error
	added      []map[string]string
	actions    []string
}

func (f *adderFake) AddTorrent(_ context.Context, instanceID int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	f.instanceIDs = append(f.instanceIDs, instanceID)
	f.added = append(f.added, options)
	return &qbt.TorrentAddResponse{AddedTorrentIds: f.addedIDs}, f.addErr
}

func (f *adderFake) HasTorrentByAnyHash(_ context.Context, instanceID int, _ []string) (*qbt.Torrent, bool, error) {
	if f.existingOn != 0 && instanceID != f.existingOn {
		return nil, false, f.findErr
	}
	return f.existing, f.existing != nil, f.findErr
}

func (f *adderFake) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	for _, hash := range hashes {
		f.actions = append(f.actions, action+":"+hash)
	}
	return nil
}

// linkCheck is the Pre-add check that the regular add hands to link mode.
func linkCheck(instanceID int, hashes ...string) PreAddCheck {
	return PreAddCheck{instanceID: instanceID, hashes: dedupeHashes(hashes...)}
}

// withLinkAdder gives svc the real Adder over the three-method qBittorrent fake
// and the blocklist store of svc. Call it after startSettleScript, which
// replaces the Settler.
func withLinkAdder(svc *Service) *adderFake {
	sync := &adderFake{}
	svc.adder = NewAdder(sync, svc.blocklistStore, svc.settler, svc.ResumeBudgetBytes, svc.RunExternalProgram)
	return sync
}

type adderRig struct {
	adder    *Adder
	sync     *adderFake
	settler  *Settler
	programs [][]string
}

func newAdderRig(t *testing.T, sync *adderFake, blocklist *models.CrossSeedBlocklistStore) *adderRig {
	t.Helper()
	rig := &adderRig{sync: sync, settler: NewSettler(&settleFake{}, nil)}
	svc := &Service{postInjectionHook: func(_ context.Context, _ int, hashes []string) {
		rig.programs = append(rig.programs, hashes)
	}}
	rig.adder = NewAdder(sync, blocklist, rig.settler, func(context.Context) int64 { return adderBudget }, svc.RunExternalProgram)
	return rig
}

// queued returns the torrent that the add handed to Settle, or nil.
func (r *adderRig) queued() *pendingResume {
	select {
	case req := <-r.settler.queue:
		return req
	default:
		return nil
	}
}

func addOptions(paused, skipChecking bool) map[string]string {
	options := map[string]string{
		"autoTMM":         "false",
		"savepath":        "/data",
		"useDownloadPath": "false",
		"paused":          "false",
		"stopped":         "false",
	}
	if paused {
		options["paused"] = "true"
		options["stopped"] = "true"
	}
	if skipChecking {
		options["skip_checking"] = "true"
	}
	return options
}

func TestAdderAdd(t *testing.T) {
	t.Parallel()

	settle := func(gate SettleGate) *SettleGate { return &gate }
	packRun := &models.SeasonPackRun{TorrentName: "Show.S01", Phase: "resume"}
	tests := []struct {
		name          string
		facts         AddFacts
		afterAddFails bool
		wantStatus    string
		wantOutcome   AddOutcome
		wantOptions   map[string]string
		wantGate      *SettleGate
		wantActions   []string
		wantMessage   string
	}{
		{name: "skip recheck refuses title rescue", facts: AddFacts{SkipRecheck: true, Verify: VerifyTitleRescue}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses relaxed match", facts: AddFacts{SkipRecheck: true, Verify: VerifyRelaxed}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses manual match", facts: AddFacts{SkipRecheck: true, Verify: VerifyManual}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses alignment with size changes", facts: AddFacts{SkipRecheck: true, Align: true}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses pending files", facts: AddFacts{SkipRecheck: true, PendingFiles: true}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses disc layout", facts: AddFacts{SkipRecheck: true, DiscMarker: "BDMV"}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses link fallback of partial match", facts: AddFacts{SkipRecheck: true, LinkFallback: true}, wantStatus: "skipped_recheck"},
		{name: "skip recheck refuses link fallback recheck", facts: AddFacts{SkipRecheck: true, LinkFallback: true, LinkFallbackRecheck: true}, wantStatus: "skipped_recheck"},
		{
			name:        "skip recheck adds byte-complete link fallback at once",
			facts:       AddFacts{SkipRecheck: true, ExactMatch: true, LinkFallback: true, LinkFallbackRecheck: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(true, true),
			wantActions: []string{"resume:" + adderV1},
		},
		{
			name:        "skip recheck adds rename-only paused and resumes after the renames",
			facts:       AddFacts{SkipRecheck: true, StartRunning: true, Align: true, RenameOnly: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(true, true),
			wantActions: []string{"resume:" + adderV1},
		},
		{
			name:        "title rescue with auto-resume off is monitored",
			facts:       AddFacts{Verify: VerifyTitleRescue, SkipAutoResume: true, StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateMonitor),
			wantMessage: " - auto-resume skipped per settings",
		},
		{
			name:        "relaxed match waits for a verified recheck",
			facts:       AddFacts{Verify: VerifyRelaxed, StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateVerify),
		},
		{
			name:        "disc layout waits for 100%",
			facts:       AddFacts{DiscMarker: "BDMV", StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateComplete),
			wantMessage: " - disc layout detected (BDMV), full recheck required",
		},
		{
			name:        "link fallback waits for 100%",
			facts:       AddFacts{LinkFallback: true, StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateComplete),
			wantMessage: " - link-mode filesystem fallback, full recheck required",
		},
		{
			name:        "alignment rechecks within the download budget",
			facts:       AddFacts{Align: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateBudget),
		},
		{
			name:        "pending files recheck within the download budget",
			facts:       AddFacts{PendingFiles: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, false),
			wantGate:    settle(GateBudget),
		},
		{
			name:        "complete add starts at once",
			wantOutcome: AddAdded,
			wantOptions: addOptions(true, true),
			wantActions: []string{"resume:" + adderV1},
		},
		{
			name:        "complete add stays paused with auto-resume off",
			facts:       AddFacts{SkipAutoResume: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(true, true),
			wantMessage: " - auto-resume skipped per settings",
		},
		{
			name:        "complete add with start paused off is added running",
			facts:       AddFacts{StartRunning: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(false, true),
		},
		{name: "skip recheck refuses link tree with pending files", facts: AddFacts{SkipRecheck: true, LinkTree: true, PendingFiles: true}, wantStatus: "skipped_recheck"},
		{
			name:        "complete link tree starts at once",
			facts:       AddFacts{LinkTree: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(false, true),
		},
		{
			name:        "complete link tree stays paused with auto-resume off",
			facts:       AddFacts{LinkTree: true, SkipAutoResume: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(true, true),
			wantMessage: " - auto-resume skipped per settings",
		},
		{
			name:        "hardlink tree with pending files rechecks within the download budget",
			facts:       AddFacts{LinkTree: true, PendingFiles: true, LinkedPaths: map[string]struct{}{"Movie/movie.mkv": {}}},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateBudget),
		},
		{
			name:        "reflink tree with pending files recovers missing files",
			facts:       AddFacts{LinkTree: true, PendingFiles: true, RecoverMissingFiles: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateBudget),
		},
		{
			name:        "link tree of a relaxed match waits for a verified recheck",
			facts:       AddFacts{LinkTree: true, Verify: VerifyRelaxed},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateVerify),
		},
		{
			name:        "link tree verification wins over the download budget",
			facts:       AddFacts{LinkTree: true, PendingFiles: true, Verify: VerifyRelaxed},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateVerify),
		},
		{
			name:        "link tree of a disc layout waits for 100%",
			facts:       AddFacts{LinkTree: true, DiscMarker: "VIDEO_TS"},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateComplete),
			wantMessage: " - disc layout detected (VIDEO_TS), full recheck required",
		},
		{
			name:        "pool member waits paused for its pool",
			facts:       AddFacts{LinkTree: true, PendingFiles: true, PoolMember: true},
			wantOutcome: AddPooled,
			wantOptions: addOptions(true, true),
		},
		{
			name:          "failed pool registration leaves the member paused",
			facts:         AddFacts{LinkTree: true, PendingFiles: true, PoolMember: true},
			afterAddFails: true,
			wantOutcome:   AddLeftPaused,
			wantOptions:   addOptions(true, true),
		},
		{name: "skip recheck refuses season pack with pending files", facts: AddFacts{SkipRecheck: true, LinkTree: true, PendingFiles: true, SeasonPackLinked: 0.5}, wantStatus: "skipped_recheck"},
		{
			name:        "complete season pack starts at once",
			facts:       AddFacts{LinkTree: true, SeasonPackLinked: 1},
			wantOutcome: AddAdded,
			wantOptions: addOptions(false, true),
		},
		{
			name: "season pack with pending files waits for its linked fraction",
			facts: AddFacts{
				LinkTree: true, PendingFiles: true, SeasonPackLinked: 0.5,
				LinkedPaths: map[string]struct{}{"Show.S01/e01.mkv": {}}, SeasonPackRun: packRun,
			},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateThreshold),
		},
		{
			name:        "complete manual assembly is checked on add and waits for its linked fraction",
			facts:       AddFacts{LinkTree: true, ManualAssembly: true, SeasonPackLinked: 1},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, false),
			wantGate:    settle(GateThreshold),
		},
		{
			name:        "skip recheck adds manual assembly with pending files",
			facts:       AddFacts{SkipRecheck: true, LinkTree: true, PendingFiles: true, ManualAssembly: true, SeasonPackLinked: 0.5},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, false),
			wantGate:    settle(GateThreshold),
		},
		{
			name:          "failed alignment pauses a running add",
			facts:         AddFacts{StartRunning: true, Align: true},
			afterAddFails: true,
			wantOutcome:   AddLeftPaused,
			wantOptions:   addOptions(false, true),
			wantActions:   []string{"pause:" + adderV1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			rig := newAdderRig(t, &adderFake{}, nil)
			check, refusal := rig.adder.Check(t.Context(), 1, AddFacts{}, adderV1, adderV2)
			require.Nil(t, refusal)

			result := rig.adder.Add(t.Context(), AddRequest{
				Check:     check,
				Torrent:   []byte("torrent"),
				Facts:     tt.facts,
				Placement: AddPlacement{SavePath: "/data"},
				AfterAdd: func(context.Context, []string) (string, bool) {
					return "", !tt.afterAddFails
				},
			})

			require.Equal(t, tt.wantStatus, result.Status)
			require.Equal(t, tt.wantOutcome, result.Outcome)
			queued := rig.queued()
			if tt.wantStatus != "" {
				require.Equal(t, skippedRecheckMessage, result.Message)
				require.Empty(t, rig.sync.added)
				require.Nil(t, queued)
				require.Empty(t, rig.programs)
				return
			}

			require.Equal(t, tt.wantMessage, result.Message)
			require.Equal(t, []map[string]string{tt.wantOptions}, rig.sync.added)
			require.Equal(t, tt.wantActions, rig.sync.actions)
			if tt.wantGate == nil {
				require.Nil(t, queued)
			} else {
				require.NotNil(t, queued)
				require.Equal(t, *tt.wantGate, queued.gate)
				require.Equal(t, adderV1, queued.hash)
				require.Equal(t, tt.facts.LinkedPaths, queued.linkedPaths)
				require.Equal(t, tt.facts.RecoverMissingFiles, queued.recoverMissingFilesWithResume)
				require.Equal(t, tt.facts.SeasonPackRun, queued.blockedRun)
				if *tt.wantGate == GateBudget {
					require.Equal(t, adderBudget, *queued.budgetBytes)
				}
				require.InDelta(t, tt.facts.SeasonPackLinked*seasonPackResumeSlack, queued.threshold, 1e-9)
			}
			if tt.wantOutcome == AddLeftPaused {
				require.Empty(t, rig.programs)
			} else {
				require.Equal(t, [][]string{{adderV1, adderV2}}, rig.programs)
			}
		})
	}
}

func TestAdderAddFailureDoesNotRetry(t *testing.T) {
	t.Parallel()

	rig := newAdderRig(t, &adderFake{addErr: errors.New("boom")}, nil)
	check, refusal := rig.adder.Check(t.Context(), 1, AddFacts{}, adderV1)
	require.Nil(t, refusal)

	result := rig.adder.Add(t.Context(), AddRequest{Check: check, Placement: AddPlacement{SavePath: "/data"}})

	require.Equal(t, AddRefused, result.Outcome)
	require.Equal(t, "error", result.Status)
	require.Equal(t, "Failed to add torrent: boom", result.Message)
	require.EqualError(t, result.Err, "boom")
	require.Len(t, rig.sync.added, 1)
	require.Empty(t, rig.programs)
}

func TestAdderCheck(t *testing.T) {
	t.Parallel()

	existing := &qbt.Torrent{Hash: adderV1, Name: "Existing"}
	tests := []struct {
		name         string
		facts        AddFacts
		blocked      string
		blocklistErr bool
		existing     *qbt.Torrent
		findErr      error
		wantStatus   string
		wantMessage  string
		wantExisting *qbt.Torrent
	}{
		{name: "clear torrent may be added"},
		{name: "blocked v1 hash", blocked: adderV1, wantStatus: "blocked", wantMessage: "Blocked by cross-seed blocklist"},
		{name: "blocked v2 hash", blocked: adderV2, wantStatus: "blocked", wantMessage: "Blocked by cross-seed blocklist"},
		{name: "existing torrent", existing: existing, wantStatus: "exists", wantMessage: "Torrent already exists in this instance", wantExisting: existing},
		{name: "blocked existing torrent", blocked: adderV1, existing: existing, wantStatus: "exists", wantMessage: "Torrent already exists in this instance", wantExisting: existing},
		{name: "manual assembly skips the blocklist", facts: AddFacts{ManualAssembly: true}, blocked: adderV1},
		{name: "manual assembly refuses an existing torrent", facts: AddFacts{ManualAssembly: true}, existing: existing, wantStatus: "exists", wantMessage: "Torrent already exists in this instance", wantExisting: existing},
		{name: "blocklist read fails", blocklistErr: true, wantStatus: "error", wantMessage: "Failed to check cross-seed blocklist: sql: database is closed"},
		{name: "existing torrent read fails", findErr: errors.New("boom"), wantStatus: "error", wantMessage: "Failed to check existing torrents: boom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocklist := adderBlocklist(t, tt.blocked)
			if tt.blocklistErr {
				blocklist = models.NewCrossSeedBlocklistStore(closedDB(t))
			}
			rig := newAdderRig(t, &adderFake{existing: tt.existing, findErr: tt.findErr}, blocklist)
			check, refusal := rig.adder.Check(t.Context(), 1, tt.facts, adderV1, adderV2)

			if tt.wantStatus == "" {
				require.Nil(t, refusal)
				return
			}
			require.Equal(t, PreAddCheck{}, check)
			require.NotNil(t, refusal)
			require.Equal(t, AddRefused, refusal.Outcome)
			require.Equal(t, tt.wantStatus, refusal.Status)
			require.Equal(t, tt.wantMessage, refusal.Message)
			require.Equal(t, tt.wantExisting, refusal.Existing)
			require.Equal(t, tt.wantStatus == "error", refusal.Err != nil)
		})
	}
}

// adderBlocklist returns a blocklist store on the test database that blocks
// hash on instance 1, or no entry when hash is empty.
func adderBlocklist(t *testing.T, hash string) *models.CrossSeedBlocklistStore {
	t.Helper()

	db := testdb.NewMigratedSQLite(t, "adder-blocklist")
	instanceStore, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	instance, err := instanceStore.Create(t.Context(), "Test", "http://127.0.0.1:8080", "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)

	store := models.NewCrossSeedBlocklistStore(db)
	if hash != "" {
		_, err = store.Upsert(t.Context(), &models.CrossSeedBlocklistEntry{InstanceID: instance.ID, InfoHash: hash})
		require.NoError(t, err)
	}
	return store
}

func closedDB(t *testing.T) *database.DB {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "adder-blocklist-closed")
	require.NoError(t, db.Close())
	return db
}
