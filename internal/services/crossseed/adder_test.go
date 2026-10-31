// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/database"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/crossseedroute"
	"github.com/autobrr/qui/internal/testutil/qbtstub"
	"github.com/autobrr/qui/internal/testutil/testdb"
)

const (
	adderV1     = "1111111111111111111111111111111111111111"
	adderV2     = "2222222222222222222222222222222222222222222222222222222222222222"
	adderBudget = int64(5 << 20)
)

// withAdder gives svc the real Adder over its own qBittorrent fake,
// blocklist store, and Settler, and an Aligner with short waits.
func withAdder(svc *Service) {
	svc.adder = NewAdder(svc.syncManager, svc.blocklistStore, svc.settler, svc.ResumeBudgetBytes, svc.RunExternalProgram)
	svc.aligner = testAligner(svc.syncManager)
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
			name:        "dir scan complete add starts at once",
			facts:       AddFacts{DirScan: true, StartRunning: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(false, true),
		},
		{
			name:        "dir scan complete add with start paused stays paused",
			facts:       AddFacts{DirScan: true, SkipAutoResume: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(true, true),
			wantMessage: " - auto-resume skipped per settings",
		},
		{
			name:        "dir scan pending files are checked on add and resume at 100%",
			facts:       AddFacts{DirScan: true, PendingFiles: true, StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(false, false),
			wantGate:    settle(GateComplete),
		},
		{
			name:        "dir scan alignment is added paused and resumes at 100%",
			facts:       AddFacts{DirScan: true, Align: true, StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateComplete),
		},
		{
			name:        "dir scan disc layout waits for 100%",
			facts:       AddFacts{DirScan: true, DiscMarker: "BDMV", StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, true),
			wantGate:    settle(GateComplete),
			wantMessage: " - disc layout detected (BDMV), full recheck required",
		},
		{
			name:        "dir scan complete link tree starts at once",
			facts:       AddFacts{DirScan: true, LinkTree: true, StartRunning: true},
			wantOutcome: AddAdded,
			wantOptions: addOptions(false, true),
		},
		{
			name:        "dir scan link tree with pending files resumes after its recheck",
			facts:       AddFacts{DirScan: true, LinkTree: true, PendingFiles: true, StartRunning: true, LinkedPaths: map[string]struct{}{"Movie/movie.mkv": {}}},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, false),
			wantGate:    settle(GateAfterRecheck),
		},
		{
			name:        "dir scan link tree of a disc layout with pending files resumes after its recheck",
			facts:       AddFacts{DirScan: true, LinkTree: true, PendingFiles: true, DiscMarker: "BDMV", StartRunning: true},
			wantOutcome: AddSettling,
			wantOptions: addOptions(true, false),
			wantGate:    settle(GateAfterRecheck),
			wantMessage: " - disc layout detected (BDMV), full recheck required",
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
			require.Equal(t, tt.wantOptions["paused"] == "true", result.Paused)
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
		existingOn   int
		findErr      error
		// link stores a route between instance 1 and instance 2.
		link *adderLink
		// noRoutes leaves the Adder without the route service.
		noRoutes    bool
		wantStatus  string
		wantMessage string
		// wantMessageRe matches a message that holds a transport error.
		wantMessageRe string
		wantExisting  *qbt.Torrent
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
		{name: "torrent on no linked instance may be added", link: &adderLink{}},
		{
			name: "hash on the Cross-seed instance", link: &adderLink{}, existing: existing, existingOn: 2,
			wantStatus: "exists", wantMessage: "Torrent already exists in Cross-seed instance seeds", wantExisting: existing,
		},
		{
			name: "hash on a Source instance", link: &adderLink{fromSource: true}, existing: existing, existingOn: 2,
			wantStatus: "exists", wantMessage: "Torrent already exists in Source instance lib", wantExisting: existing,
		},
		{name: "disabled Source instance is skipped", link: &adderLink{fromSource: true, state: "disabled"}, existing: existing, existingOn: 2},
		{
			name: "offline Source instance refuses and names it", link: &adderLink{fromSource: true, state: "offline"},
			wantStatus: "error", wantMessageRe: `^instance unavailable: lib: .+\. Enable lib or clear its cross-seed route$`,
		},
		{
			name: "offline Cross-seed instance refuses", link: &adderLink{state: "offline"},
			wantStatus: "error", wantMessageRe: `^Cross-seed instance unavailable: seeds: .+`,
		},
		{
			name: "disabled Cross-seed instance refuses", link: &adderLink{state: "disabled"},
			wantStatus: "error", wantMessage: "Cross-seed instance unavailable: seeds: qBittorrent instance is disabled",
		},
		{name: "blocklist of a Source instance does not apply", link: &adderLink{fromSource: true, blocked: true}},
		// A user with no saved route sees no change.
		{name: "no saved route checks no other instance", link: &adderLink{unsaved: true}, existing: existing, existingOn: 2},
		{name: "without the route service no linked instance is checked", link: &adderLink{}, noRoutes: true, existing: existing, existingOn: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			blocklist, db := adderBlocklist(t, tt.blocked)
			if tt.blocklistErr {
				blocklist = models.NewCrossSeedBlocklistStore(closedDB(t))
			}
			rig := newAdderRig(t, &adderFake{existing: tt.existing, existingOn: tt.existingOn, findErr: tt.findErr}, blocklist)
			if tt.link != nil {
				routes := adderRoutes(t, db, *tt.link)
				if !tt.noRoutes {
					rig.adder.SetCrossSeedRoutes(routes)
				}
			}
			check, refusal := rig.adder.Check(t.Context(), 1, tt.facts, adderV1, adderV2)

			if tt.wantStatus == "" {
				require.Nil(t, refusal)
				return
			}
			require.Equal(t, PreAddCheck{}, check)
			require.NotNil(t, refusal)
			require.Equal(t, AddRefused, refusal.Outcome)
			require.Equal(t, tt.wantStatus, refusal.Status)
			if tt.wantMessageRe != "" {
				require.Regexp(t, tt.wantMessageRe, refusal.Message)
			} else {
				require.Equal(t, tt.wantMessage, refusal.Message)
			}
			require.Equal(t, tt.wantExisting, refusal.Existing)
			require.Equal(t, tt.wantStatus == "error", refusal.Err != nil)
		})
	}
}

// adderBlocklist returns a blocklist store on the test database that blocks
// hash on instance 1, or no entry when hash is empty, and the database.
func adderBlocklist(t *testing.T, hash string) (*models.CrossSeedBlocklistStore, *database.DB) {
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
	return store, db
}

// adderLink is a route between instance 1 and instance 2.
type adderLink struct {
	// fromSource makes instance 2 a Source instance that routes to instance 1.
	// Otherwise instance 1 routes to instance 2.
	fromSource bool
	// state of instance 2: online when empty, "disabled", or "offline".
	state string
	// blocked puts adderV1 on the blocklist of instance 2.
	blocked bool
	// unsaved leaves the route table empty.
	unsaved bool
}

// adderRoutes adds instance 2 to db, "lib" as a Source instance or "seeds" as
// a Cross-seed instance, on a local fake qBittorrent. It stores the route of
// link, unless link.unsaved, and returns the route service.
func adderRoutes(t *testing.T, db *database.DB, link adderLink) *crossseedroute.Service {
	t.Helper()
	ctx := t.Context()

	instances, err := models.NewInstanceStore(db, []byte("01234567890123456789012345678901"))
	require.NoError(t, err)
	host := qbtstub.New(t, t.TempDir()).URL
	if link.state == "offline" {
		down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "unavailable", http.StatusServiceUnavailable)
		}))
		t.Cleanup(down.Close)
		host = down.URL
	}
	route := &models.CrossSeedRoute{SourceInstanceID: 1, CrossSeedInstanceID: 2}
	name := "seeds"
	if link.fromSource {
		route.SourceInstanceID, route.CrossSeedInstanceID = 2, 1
		name = "lib"
	}
	linked, err := instances.Create(ctx, name, host, "user", "pass", nil, nil, false, nil)
	require.NoError(t, err)
	require.Equal(t, 2, linked.ID)
	if link.state == "disabled" {
		_, err = instances.SetActiveState(ctx, linked.ID, false)
		require.NoError(t, err)
	}
	if link.blocked {
		_, err = models.NewCrossSeedBlocklistStore(db).Upsert(ctx, &models.CrossSeedBlocklistEntry{InstanceID: linked.ID, InfoHash: adderV1})
		require.NoError(t, err)
	}
	store := models.NewCrossSeedRouteStore(db)
	if !link.unsaved {
		require.NoError(t, store.Upsert(ctx, route))
	}

	clients, err := qbittorrent.NewClientPool(instances, models.NewInstanceErrorStore(db), 60*time.Second)
	require.NoError(t, err)
	t.Cleanup(func() { _ = clients.Close() })
	return crossseedroute.NewService(store, instances, clients, nil)
}

func closedDB(t *testing.T) *database.DB {
	t.Helper()
	db := testdb.NewMigratedSQLite(t, "adder-blocklist-closed")
	require.NoError(t, db.Close())
	return db
}
