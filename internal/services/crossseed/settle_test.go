// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"
	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

// settlePoll is what qBittorrent shows on one poll of the settle worker.
type settlePoll struct {
	// torrent is nil while qBittorrent does not list the torrent.
	torrent *qbt.Torrent
	// before changes the fake just before the worker reads this poll.
	before func(*settleFake)
}

// settleFake is qBittorrent for the settle module. Each worker poll takes the
// next entry of polls; after the last one the torrent is gone.
type settleFake struct {
	mu              sync.Mutex
	polls           []settlePoll
	next            int
	current         *qbt.Torrent
	files           map[string]qbt.TorrentFiles
	filesErr        error
	pieces          map[string][]qbt.PieceState
	piecesErr       error
	recheckFailures int
	resumeFailures  int
	actions         []string
	fileReads       int
	runs            []*models.SeasonPackRun
}

func (f *settleFake) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	// The worker reads once by hash and again unfiltered only when a hash is missing.
	if len(filter.Hashes) > 0 {
		f.current = nil
		if f.next < len(f.polls) {
			poll := f.polls[f.next]
			f.next++
			if poll.before != nil {
				poll.before(f)
			}
			f.current = poll.torrent
		}
	}
	if f.current == nil {
		return nil, nil
	}
	return []qbt.Torrent{*f.current}, nil
}

func (f *settleFake) BulkAction(_ context.Context, _ int, hashes []string, action string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, hash := range hashes {
		f.actions = append(f.actions, action+":"+hash)
	}
	switch {
	case action == "recheck" && f.recheckFailures > 0:
		f.recheckFailures--
		return errors.New("recheck failed")
	case action == "resume" && f.resumeFailures > 0:
		f.resumeFailures--
		return errors.New("resume failed")
	}
	return nil
}

func (f *settleFake) GetTorrentFilesBatch(_ context.Context, _ int, hashes []string) (map[string]qbt.TorrentFiles, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.fileReads++
	if f.filesErr != nil {
		return nil, f.filesErr
	}
	out := make(map[string]qbt.TorrentFiles)
	for _, hash := range hashes {
		if files, ok := f.files[normalizeHash(hash)]; ok {
			out[normalizeHash(hash)] = files
		}
	}
	return out, nil
}

func (f *settleFake) GetTorrentPieceStates(_ context.Context, _ int, hash string) ([]qbt.PieceState, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.piecesErr != nil {
		return nil, f.piecesErr
	}
	return f.pieces[normalizeHash(hash)], nil
}

func (f *settleFake) Create(_ context.Context, run *models.SeasonPackRun) (*models.SeasonPackRun, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.runs = append(f.runs, run)
	return run, nil
}

func torrentAt(state qbt.TorrentState, progress float64, amountLeft int64) *qbt.Torrent {
	torrent := &qbt.Torrent{Hash: "hash1", State: state, Progress: progress, AmountLeft: amountLeft}
	if progress > 0 {
		torrent.Completed = 1
	}
	return torrent
}

func polls(torrents ...*qbt.Torrent) []settlePoll {
	out := make([]settlePoll, len(torrents))
	for i, torrent := range torrents {
		out[i].torrent = torrent
	}
	return out
}

func repeatPoll(torrent *qbt.Torrent, n int) []settlePoll {
	out := make([]settlePoll, n)
	for i := range out {
		out[i].torrent = torrent
	}
	return out
}

func joinPolls(parts ...[]settlePoll) []settlePoll {
	var out []settlePoll
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

// TestSettle drives the settle module through Settle and a fake qBittorrent.
// Each row is an add, the torrent states the worker sees on each poll, and the
// calls qBittorrent must receive.
func TestSettle(t *testing.T) {
	const (
		have    = qbt.PieceStateAlreadyDownloaded
		missing = qbt.PieceStateNotDownloadYet
	)
	var (
		checkingUp   = torrentAt(qbt.TorrentStateCheckingUp, 0.5, 0)
		checkingDl   = torrentAt(qbt.TorrentStateCheckingDl, 0.5, 2<<30)
		resumeData   = torrentAt(qbt.TorrentStateCheckingResumeData, 1, 0)
		pausedFull   = torrentAt(qbt.TorrentStatePausedUp, 1, 0)
		stoppedFull  = torrentAt(qbt.TorrentStateStoppedUp, 1, 0)
		uploading    = torrentAt(qbt.TorrentStateUploading, 1, 0)
		queuedUp     = torrentAt(qbt.TorrentStateQueuedUp, 1, 0)
		missingFiles = torrentAt(qbt.TorrentStateMissingFiles, 0.0101, 2<<30)
		// probe resumes any entry still queued after a sawn piece check.
		probe = repeatPoll(torrentAt(qbt.TorrentStatePausedDl, 1, 0), 2)
	)

	// The mkv reports slightly under 1: qbit file progress is piece-based, so the
	// piece spanning the mkv/sample boundary fails hashing when the sample is
	// absent. Its few missing MiB must count against the budget, not veto forgiveness.
	sampleOnlyMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 0.999, Priority: 1, Size: 4 << 30},
		{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
		{Name: "Show.S01E01.nfo", Progress: 0, Priority: 1, Size: 4 << 10},
	}
	episodeMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 0.98, Priority: 1, Size: 4 << 30},
		{Name: "Show.S01E01.nfo", Progress: 1, Priority: 1, Size: 4 << 10},
	}
	unwantedRelevantMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 4 << 30},
		{Name: "Show.S01E02.mkv", Progress: 0, Priority: 0, Size: 4 << 30},
		{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
	}
	// A real title containing an ignore keyword must stay relevant: substring
	// matching would classify this episode as a "trailer" sidecar and resume.
	keywordTitleMissing := qbt.TorrentFiles{
		{Name: "Trailer.Park.Boys.S01E01.mkv", Progress: 0.9, Priority: 1, Size: 1500 << 20},
	}
	// A directory that merely ends in an ignore keyword is not a sidecar dir:
	// unanchored "sample/" matching would forgive this missing video.
	keywordSuffixDirMissing := qbt.TorrentFiles{
		{Name: "Movie.Resample/video.mkv", Progress: 0.5, Priority: 1, Size: 300 << 20},
	}
	// A keyword at the end of the stem is a qualifier under any separator, not
	// just "-": these samples must be forgivable. Each sample alone exceeds the
	// 50 MiB budget so a mutant dropping either separator fails this row.
	dottedSampleMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 4 << 30},
		{Name: "Movie.2024.Sample.mkv", Progress: 0, Priority: 1, Size: 60 << 20},
		{Name: "Movie_sample.mkv", Progress: 0, Priority: 1, Size: 60 << 20},
	}
	// "Resample" merely ends in "sample": separator-less suffix matching would
	// forgive this real video.
	keywordSuffixStemMissing := qbt.TorrentFiles{
		{Name: "Movie.Resample.mkv", Progress: 0.5, Priority: 1, Size: 300 << 20},
	}
	videoAndSampleMissing := qbt.TorrentFiles{
		{Name: "Show.S01E01.mkv", Progress: 0.2, Priority: 1, Size: 4 << 30},
		{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
	}

	// E01 and E02 are linked, E03 is pending. Piece 15 sits inside E02 alone.
	insideFiles := qbt.TorrentFiles{
		{Name: "Show.S01/Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 1 << 30, PieceRange: []int{0, 9}},
		{Name: "Show.S01/Show.S01E02.mkv", Progress: 0.9, Priority: 1, Size: 1 << 30, PieceRange: []int{10, 19}},
		{Name: "Show.S01/Show.S01E03.mkv", Progress: 0, Priority: 1, Size: 1 << 30, PieceRange: []int{20, 29}},
	}
	insidePieces := make([]qbt.PieceState, 30)
	for i := range insidePieces {
		insidePieces[i] = have
	}
	insidePieces[15] = missing
	for i := 20; i < 30; i++ {
		insidePieces[i] = missing
	}
	// E02 shares piece 20 with pending E03; that piece is the only failure.
	straddleFiles := qbt.TorrentFiles{
		{Name: "Show.S01/Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 1 << 30, PieceRange: []int{0, 9}},
		{Name: "Show.S01/Show.S01E02.mkv", Progress: 0.95, Priority: 1, Size: 1 << 30, PieceRange: []int{10, 20}},
		{Name: "Show.S01/Show.S01E03.mkv", Progress: 0, Priority: 1, Size: 1 << 30, PieceRange: []int{20, 29}},
	}
	straddlePieces := make([]qbt.PieceState, 30)
	for i := range straddlePieces {
		straddlePieces[i] = have
	}
	for i := 20; i < 30; i++ {
		straddlePieces[i] = missing
	}
	// An empty file reports [start, start-1]; it occupies no piece and must not
	// excuse a neighbour. E01 fails its last piece, an empty pending file follows
	// at [10,9], and another sits at offset zero as [0,-1].
	emptyFiles := qbt.TorrentFiles{
		{Name: "Show.S01/zero.txt", Progress: 1, Priority: 1, Size: 0, PieceRange: []int{0, -1}},
		{Name: "Show.S01/Show.S01E01.mkv", Progress: 0.9, Priority: 1, Size: 1 << 30, PieceRange: []int{0, 9}},
		{Name: "Show.S01/empty.txt", Progress: 1, Priority: 1, Size: 0, PieceRange: []int{10, 9}},
		{Name: "Show.S01/Show.S01E02.mkv", Progress: 0, Priority: 1, Size: 1 << 30, PieceRange: []int{10, 19}},
	}
	emptyPieces := make([]qbt.PieceState, 20)
	for i := range emptyPieces {
		emptyPieces[i] = have
	}
	for i := 9; i < 20; i++ {
		emptyPieces[i] = missing
	}
	// Keyed by path: qBittorrent drops pad files from its list and renumbers indexes.
	linked := map[string]struct{}{"Show.S01/Show.S01E01.mkv": {}, "Show.S01/Show.S01E02.mkv": {}}
	linkedPartial := torrentAt(qbt.TorrentStatePausedDl, 0.66, 30<<20)
	// qBittorrent lists a hybrid torrent by its v2 hash; the add knows the v1 hash first.
	hybridPartial := &qbt.Torrent{
		Hash: "v2hash", InfohashV1: "v1hash", InfohashV2: "v2hash",
		State: qbt.TorrentStatePausedDl, Progress: 0.66, AmountLeft: 30 << 20, Completed: 1,
	}
	hybridChecking := &qbt.Torrent{
		Hash: "v2hash", InfohashV1: "v1hash", InfohashV2: "v2hash",
		State: qbt.TorrentStateCheckingDl, Progress: 0.5, AmountLeft: 2 << 30,
	}

	budget := func(bytes int64) AddedTorrent { return AddedTorrent{Gate: GateBudget, Budget: bytes} }
	threshold := func(progress float64) AddedTorrent { return AddedTorrent{Gate: GateThreshold, Threshold: progress} }
	withFiles := func(files qbt.TorrentFiles) func(*settleFake) {
		return func(f *settleFake) { f.files = map[string]qbt.TorrentFiles{"hash1": files} }
	}
	budgetDecision := func(amountLeft int64, progress float64) []settlePoll {
		return polls(checkingDl, torrentAt(qbt.TorrentStatePausedDl, progress, amountLeft))
	}

	tests := []struct {
		name        string
		added       AddedTorrent
		fake        *settleFake
		wantOutcome SettleOutcome
		wantActions []string
		wantReads   int
		// wantBlocked is the linked file named in the season pack history row.
		wantBlocked string
	}{
		// Budget gate.
		{
			name:        "missing bytes within budget resume without file fetch",
			added:       budget(50 << 20),
			fake:        &settleFake{polls: budgetDecision(30<<20, 0.9)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:        "missing bytes at exact budget resume",
			added:       budget(50 << 20),
			fake:        &settleFake{polls: budgetDecision(50<<20, 0.9)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:  "over budget with relevant file missing stays paused",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(80<<20, 0.9), probe),
				files: map[string]qbt.TorrentFiles{"hash1": episodeMissing},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
		},
		{
			name:  "over budget but only irrelevant files missing resumes",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: budgetDecision(70<<20, 0.9),
				files: map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   1,
		},
		{
			name:  "irrelevant files above forgiveness cap stay paused",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(300<<20, 0.9), probe),
				files: map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
			},
			wantActions: []string{"recheck:hash1"},
		},
		{
			name:  "unwanted relevant file does not block forgiveness",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: budgetDecision(70<<20, 0.9),
				files: map[string]qbt.TorrentFiles{"hash1": unwantedRelevantMissing},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   1,
		},
		{
			name:  "title containing ignore keyword stays relevant",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(150<<20, 0.9), probe),
				files: map[string]qbt.TorrentFiles{"hash1": keywordTitleMissing},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
		},
		{
			name:  "dot and underscore separated sample names are forgiven",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: budgetDecision(120<<20, 0.9),
				files: map[string]qbt.TorrentFiles{"hash1": dottedSampleMissing},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   1,
		},
		{
			name:  "stem merely ending in ignore keyword stays relevant",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(150<<20, 0.9), probe),
				files: map[string]qbt.TorrentFiles{"hash1": keywordSuffixStemMissing},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
		},
		{
			name:  "directory ending in ignore keyword stays relevant",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(150<<20, 0.9), probe),
				files: map[string]qbt.TorrentFiles{"hash1": keywordSuffixDirMissing},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
		},
		{
			name:  "file fetch error keeps the entry and retries",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(70<<20, 0.98), []settlePoll{{
					torrent: torrentAt(qbt.TorrentStatePausedDl, 0.98, 70<<20),
					before:  func(f *settleFake) { f.filesErr = nil },
				}}),
				files:    map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
				filesErr: errors.New("qbit timeout"),
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   2,
		},
		{
			name:        "budget zero requires complete torrent",
			added:       budget(0),
			fake:        &settleFake{polls: budgetDecision(0, 1)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			// Ordinary zero-budget entries trust zero missing bytes even when
			// piece progress reads under 1. The verify gate uses full progress.
			name:        "budget zero trusts zero missing bytes over progress",
			added:       budget(0),
			fake:        &settleFake{polls: budgetDecision(0, 0.99)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:  "budget zero disables forgiveness",
			added: budget(0),
			fake: &settleFake{
				polls: joinPolls(budgetDecision(4<<10, 0.9), probe),
				files: map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
			},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// Before the recheck settles the video still reports incomplete, so
			// forgiveness says no; that verdict must not stick once a later poll
			// shows only the sample missing.
			name:  "forgiveness retries after a negative verdict",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: []settlePoll{
					{torrent: torrentAt(qbt.TorrentStatePausedDl, 0, 70<<20)},
					{torrent: checkingDl},
					{
						torrent: torrentAt(qbt.TorrentStatePausedDl, 0.98, 70<<20),
						before: withFiles(qbt.TorrentFiles{
							{Name: "Show.S01E01.mkv", Progress: 1, Priority: 1, Size: 4 << 30},
							{Name: "Sample/sample.mkv", Progress: 0, Priority: 1, Size: 70 << 20},
						}),
					},
				},
				files: map[string]qbt.TorrentFiles{"hash1": videoAndSampleMissing},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   2,
		},
		{
			// A recheck can start and finish between polls. A grant earned before
			// a checking state was seen must not be cached.
			name:  "forgiveness is not cached before a checking state",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls([]settlePoll{
					{torrent: torrentAt(qbt.TorrentStatePausedDl, 0.98, 70<<20)},
					{
						torrent: torrentAt(qbt.TorrentStatePausedDl, 0.96, 160<<20),
						before: withFiles(qbt.TorrentFiles{
							{Name: "Show.S01E01.mkv", Progress: 0.96, Priority: 1, Size: 4 << 30},
						}),
					},
				}, probe),
				files: map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   2,
		},
		{
			// A failed resume keeps a cached grant; a later checking state must
			// drop it so the post-check shortfall decides.
			name:  "checking clears a cached forgiveness verdict",
			added: budget(50 << 20),
			fake: &settleFake{
				polls: joinPolls([]settlePoll{
					{torrent: checkingDl},
					{torrent: torrentAt(qbt.TorrentStatePausedDl, 0.98, 70<<20)},
					{torrent: checkingDl},
					{
						torrent: torrentAt(qbt.TorrentStatePausedDl, 0.9, 103<<20),
						before: withFiles(qbt.TorrentFiles{
							{Name: "Show.S01E01.mkv", Progress: 0.9, Priority: 1, Size: 1 << 30},
						}),
					},
				}, probe),
				files:          map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
				resumeFailures: 1,
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   2,
		},

		// Missing-files recovery.
		{
			name:        "reflink missingFiles is resumed once",
			added:       AddedTorrent{Gate: GateBudget, Budget: 50 << 20, RecoverMissingFiles: true},
			fake:        &settleFake{polls: polls(missingFiles, missingFiles)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:        "missingFiles resume retries a transient failure",
			added:       AddedTorrent{Gate: GateBudget, Budget: 50 << 20, RecoverMissingFiles: true},
			fake:        &settleFake{polls: polls(missingFiles, missingFiles), resumeFailures: 1},
			wantActions: []string{"recheck:hash1", "resume:hash1", "resume:hash1"},
		},
		{
			name:  "missingFiles resume stops after repeated failures",
			added: AddedTorrent{Gate: GateBudget, Budget: 50 << 20, RecoverMissingFiles: true},
			fake: &settleFake{
				polls:          joinPolls(repeatPoll(missingFiles, maxMissingFilesResumeAttempts), probe),
				resumeFailures: maxMissingFilesResumeAttempts,
			},
			wantActions: []string{"recheck:hash1", "resume:hash1", "resume:hash1", "resume:hash1"},
		},
		{
			// The threshold gate never takes the recovery flag: a hardlink
			// season pack in missingFiles stays paused for review.
			name:        "threshold gate leaves missingFiles for manual review",
			added:       AddedTorrent{Gate: GateThreshold, Threshold: 0.95, RecoverMissingFiles: true},
			fake:        &settleFake{polls: joinPolls(polls(missingFiles), probe)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// A recovery nudge can start an over-budget torrent downloading.
			name:  "over-budget recovery download is paused",
			added: AddedTorrent{Gate: GateBudget, Budget: 50 << 20, RecoverMissingFiles: true},
			fake: &settleFake{
				polls: polls(
					torrentAt(qbt.TorrentStateMissingFiles, 0.05, 2<<30),
					torrentAt(qbt.TorrentStateDownloading, 0.05, 2<<30),
				),
				files: map[string]qbt.TorrentFiles{"hash1": {{Name: "Show.S01E01.mkv", Progress: 0.05, Priority: 1, Size: 2 << 30}}},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1", "pause:hash1"},
		},
		{
			// Fetching the file list for forgiveness can stall while the torrent
			// downloads, so the worker pauses first and decides from the paused state.
			name:  "recovery download is paused before the forgiveness verdict",
			added: AddedTorrent{Gate: GateBudget, Budget: 50 << 20, RecoverMissingFiles: true},
			fake: &settleFake{
				polls: polls(
					missingFiles,
					checkingDl,
					torrentAt(qbt.TorrentStateDownloading, 0.98, 70<<20),
					torrentAt(qbt.TorrentStatePausedDl, 0.98, 70<<20),
				),
				files: map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1", "pause:hash1", "resume:hash1"},
			wantReads:   1,
		},
		{
			// A grant earned from the paused state keeps a recovery download running.
			name:  "granted recovery download is not paused",
			added: AddedTorrent{Gate: GateBudget, Budget: 50 << 20, RecoverMissingFiles: true},
			fake: &settleFake{
				polls: []settlePoll{
					{torrent: missingFiles},
					{torrent: checkingDl},
					{
						torrent: torrentAt(qbt.TorrentStatePausedDl, 0.98, 70<<20),
						before:  func(f *settleFake) { f.resumeFailures = 1 },
					},
					{torrent: torrentAt(qbt.TorrentStateDownloading, 0.98, 70<<20)},
				},
				files: map[string]qbt.TorrentFiles{"hash1": sampleOnlyMissing},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1", "resume:hash1", "resume:hash1"},
			wantReads:   1,
		},

		// Threshold gate and resume confirmation.
		{
			name:        "below threshold while downloading keeps watching",
			added:       threshold(0.95),
			fake:        &settleFake{polls: polls(torrentAt(qbt.TorrentStateDownloading, 0.5, 2<<30), pausedFull, pausedFull)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:        "confirms running after resume",
			added:       threshold(1),
			fake:        &settleFake{polls: polls(checkingUp, pausedFull, uploading, uploading, pausedFull)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:        "retries a torrent that stops again after resume",
			added:       threshold(1),
			fake:        &settleFake{polls: polls(checkingUp, stoppedFull, stoppedFull, queuedUp, queuedUp, stoppedFull)},
			wantActions: []string{"recheck:hash1", "resume:hash1", "resume:hash1"},
		},
		{
			name:  "stops when confirmation drops below threshold",
			added: threshold(1),
			fake: &settleFake{polls: joinPolls(
				polls(checkingUp, pausedFull, torrentAt(qbt.TorrentStatePausedUp, 0.95, 0)),
				repeatPoll(pausedFull, 2),
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:  "resume attempts stop after the limit",
			added: threshold(1),
			fake: &settleFake{polls: joinPolls(
				polls(checkingUp),
				repeatPoll(stoppedFull, maxRecheckResumeAttempts+2),
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1", "resume:hash1", "resume:hash1"},
		},

		// Verify gate.
		{
			name:        "verify resumes after an observed full check",
			added:       AddedTorrent{Gate: GateVerify},
			fake:        &settleFake{polls: polls(checkingUp, pausedFull, uploading, uploading)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			// An optimistic 100% before any hash check must not resume.
			name:        "verify ignores a pre-check 100%",
			added:       AddedTorrent{Gate: GateVerify},
			fake:        &settleFake{polls: polls(pausedFull, pausedFull)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// Resume-data validation alone does not prove a piece check ran.
			name:        "verify ignores resume-data validation before the fast-complete delay",
			added:       AddedTorrent{Gate: GateVerify},
			fake:        &settleFake{polls: polls(resumeData, pausedFull, pausedFull)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			name:        "verify leaves a check below 100% paused",
			added:       AddedTorrent{Gate: GateVerify},
			fake:        &settleFake{polls: joinPolls(polls(checkingUp, torrentAt(qbt.TorrentStatePausedDl, 0.99, 0)), probe)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// A 100%-overlap recheck can finish between polls, so checking is never
			// seen. After the delay a settled 100% resumes, confirms, and drops (#2554).
			name:  "verify resumes a fast recheck after the delay",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{polls: joinPolls(
				repeatPoll(pausedFull, int(recheckFastCompleteMinElapsed/recheckPollInterval)+1),
				polls(uploading, uploading, pausedFull),
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			// Regression: the only checking state seen was the forced-recheck startup.
			name:  "verify fast resume after only resume-data validation",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{polls: joinPolls(
				repeatPoll(resumeData, int(recheckFastCompleteMinElapsed/recheckPollInterval)),
				polls(pausedFull, pausedFull, uploading, uploading, pausedFull),
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:  "verify below 100% after the delay stays paused",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{polls: repeatPoll(torrentAt(qbt.TorrentStatePausedDl, 0.99, 0),
				int(recheckFastCompleteMinElapsed/recheckPollInterval)+3)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// A resume-data check after a piece check is a qBittorrent restart:
			// the earlier check no longer proves the recheck ran.
			name:  "verify interrupted check stays paused after the delay",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{polls: joinPolls(
				polls(checkingUp, resumeData),
				repeatPoll(pausedFull, int(recheckFastCompleteMinElapsed/recheckPollInterval)+2),
			)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			name:  "verify resumes after a new piece check follows a restart",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{polls: polls(
				checkingUp, resumeData, pausedFull, pausedFull, checkingUp, pausedFull,
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:  "verify restart drops a pending resume confirmation",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{polls: polls(
				checkingUp, pausedFull, checkingUp, resumeData, pausedFull, pausedFull, checkingUp, pausedFull,
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1", "resume:hash1"},
		},

		// Monitor gate and skipped auto-resume.
		{
			name:        "monitor never resumes a full check",
			added:       AddedTorrent{Gate: GateMonitor, SkipAutoResume: true},
			fake:        &settleFake{polls: joinPolls(polls(checkingUp, torrentAt(qbt.TorrentStatePausedDl, 1, 0)), probe)},
			wantOutcome: SettleOutcome{Skipped: true},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// qBittorrent can report zero missing bytes at zero progress before
			// the recheck starts; the monitor must also require full progress.
			name:        "monitor waits for full progress",
			added:       AddedTorrent{Gate: GateMonitor, SkipAutoResume: true},
			fake:        &settleFake{polls: polls(torrentAt(qbt.TorrentStatePausedDl, 0, 0), pausedFull)},
			wantOutcome: SettleOutcome{Skipped: true},
			wantActions: []string{"recheck:hash1"},
		},
		{
			name:        "skipped auto-resume rechecks and leaves the torrent paused",
			added:       AddedTorrent{Gate: GateBudget, Budget: 50 << 20, SkipAutoResume: true},
			fake:        &settleFake{polls: polls(stoppedFull, stoppedFull, stoppedFull)},
			wantOutcome: SettleOutcome{Skipped: true},
			wantActions: []string{"recheck:hash1"},
		},

		// A recheck call that fails after the add.
		{
			// skip_checking reports 100% before any recheck, so the first poll
			// must send the recheck, not resume.
			name:        "failed recheck is sent again and then resumes",
			added:       budget(50 << 20),
			fake:        &settleFake{polls: polls(stoppedFull, checkingUp, stoppedFull), recheckFailures: 1},
			wantOutcome: SettleOutcome{RecheckFailed: true},
			wantActions: []string{"recheck:hash1", "recheck:hash1", "resume:hash1"},
		},
		{
			name:        "failed recheck with skipped auto-resume is sent again but not resumed",
			added:       AddedTorrent{Gate: GateBudget, Budget: 50 << 20, SkipAutoResume: true},
			fake:        &settleFake{polls: joinPolls(polls(stoppedFull, checkingUp, stoppedFull), probe), recheckFailures: 1},
			wantOutcome: SettleOutcome{Skipped: true, RecheckFailed: true},
			wantActions: []string{"recheck:hash1", "recheck:hash1"},
		},
		{
			name:        "failed recheck waits while qBittorrent hides the torrent",
			added:       AddedTorrent{Gate: GateComplete},
			fake:        &settleFake{polls: polls(nil, nil, nil, stoppedFull), recheckFailures: 1},
			wantOutcome: SettleOutcome{RecheckFailed: true},
			wantActions: []string{"recheck:hash1", "recheck:hash1"},
		},
		{
			// Resume-data validation comes first; a piece check then proves the
			// failed call landed, so the recheck is not sent again.
			name:        "failed recheck waits out resume-data validation and is not restarted",
			added:       AddedTorrent{Gate: GateComplete},
			fake:        &settleFake{polls: polls(resumeData, checkingUp, stoppedFull), recheckFailures: 1},
			wantOutcome: SettleOutcome{RecheckFailed: true},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			// A piece check seen while the recheck is pending proves the failed
			// call landed. A qBittorrent restart after it must still block the fast resume.
			name:  "failed recheck then restart does not resume",
			added: AddedTorrent{Gate: GateVerify},
			fake: &settleFake{
				polls: joinPolls(
					polls(torrentAt(qbt.TorrentStateCheckingUp, 0.3, 0), resumeData),
					repeatPoll(stoppedFull, int(recheckFastCompleteMinElapsed/recheckPollInterval)+2),
				),
				recheckFailures: 1,
			},
			wantOutcome: SettleOutcome{RecheckFailed: true},
			wantActions: []string{"recheck:hash1"},
		},

		// Dir Scan gates.
		{
			name:        "after-recheck resumes below 100% once the recheck ran",
			added:       AddedTorrent{Gate: GateAfterRecheck},
			fake:        &settleFake{polls: polls(checkingDl, linkedPartial)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			// A fast recheck can finish between polls; completed bytes prove it ran.
			name:        "after-recheck resumes on completed bytes without a seen check",
			added:       AddedTorrent{Gate: GateAfterRecheck},
			fake:        &settleFake{polls: polls(linkedPartial, linkedPartial)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:        "after-recheck waits while nothing is completed",
			added:       AddedTorrent{Gate: GateAfterRecheck},
			fake:        &settleFake{polls: repeatPoll(torrentAt(qbt.TorrentStatePausedDl, 0, 2<<30), 3)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			// Every path waits up to the same absolute timeout, then leaves the torrent paused.
			name:  "after-recheck gives up at the settle timeout",
			added: AddedTorrent{Gate: GateAfterRecheck},
			fake: &settleFake{polls: joinPolls(
				repeatPoll(torrentAt(qbt.TorrentStatePausedDl, 0, 2<<30), int(time.Hour/recheckPollInterval)),
				polls(linkedPartial, linkedPartial),
			)},
			wantActions: []string{"recheck:hash1"},
		},
		{
			name:  "complete waits until nothing is left",
			added: AddedTorrent{Gate: GateComplete},
			fake: &settleFake{polls: polls(
				checkingDl,
				torrentAt(qbt.TorrentStateDownloading, 0.9, 10<<20),
				torrentAt(qbt.TorrentStateUploading, 1, 0),
				uploading, uploading,
			)},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:        "complete leaves a paused partial paused",
			added:       AddedTorrent{Gate: GateComplete},
			fake:        &settleFake{polls: joinPolls(polls(checkingDl, torrentAt(qbt.TorrentStatePausedDl, 0.9, 10<<20)), probe)},
			wantActions: []string{"recheck:hash1"},
		},

		// ADR 0004 linked-file check. A linked file that rechecks below 100%
		// blocks the resume unless every failed piece straddles a pending file.
		{
			name:  "threshold mode mismatched linked file stays paused",
			added: AddedTorrent{Gate: GateThreshold, Threshold: 0.6, LinkedPaths: linked},
			fake: &settleFake{
				polls:  joinPolls(polls(checkingDl, linkedPartial), probe),
				files:  map[string]qbt.TorrentFiles{"hash1": insideFiles},
				pieces: map[string][]qbt.PieceState{"hash1": insidePieces},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
			wantBlocked: "Show.S01/Show.S01E02.mkv",
		},
		{
			name:  "budget mode mismatched linked file stays paused",
			added: AddedTorrent{Gate: GateBudget, Budget: 50 << 20, LinkedPaths: linked},
			fake: &settleFake{
				polls:  joinPolls(polls(checkingDl, linkedPartial), probe),
				files:  map[string]qbt.TorrentFiles{"hash1": insideFiles},
				pieces: map[string][]qbt.PieceState{"hash1": insidePieces},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
			wantBlocked: "Show.S01/Show.S01E02.mkv",
		},
		{
			name:  "after-recheck mismatched linked file stays paused",
			added: AddedTorrent{Gate: GateAfterRecheck, LinkedPaths: linked},
			fake: &settleFake{
				polls:  joinPolls(polls(checkingDl, linkedPartial), probe),
				files:  map[string]qbt.TorrentFiles{"hash1": insideFiles},
				pieces: map[string][]qbt.PieceState{"hash1": insidePieces},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
			wantBlocked: "Show.S01/Show.S01E02.mkv",
		},
		{
			name:  "reflink entry keeps the current rules",
			added: threshold(0.6),
			fake: &settleFake{
				polls:  polls(checkingDl, linkedPartial),
				files:  map[string]qbt.TorrentFiles{"hash1": insideFiles},
				pieces: map[string][]qbt.PieceState{"hash1": insidePieces},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
		},
		{
			name:  "failed piece straddling a pending file still resumes",
			added: AddedTorrent{Gate: GateThreshold, Threshold: 0.6, LinkedPaths: linked},
			fake: &settleFake{
				polls:  polls(checkingDl, linkedPartial),
				files:  map[string]qbt.TorrentFiles{"hash1": straddleFiles},
				pieces: map[string][]qbt.PieceState{"hash1": straddlePieces},
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   1,
		},
		{
			name:  "empty pending files excuse no piece",
			added: AddedTorrent{Gate: GateThreshold, Threshold: 0.6, LinkedPaths: map[string]struct{}{"Show.S01/Show.S01E01.mkv": {}}},
			fake: &settleFake{
				polls:  joinPolls(polls(checkingDl, linkedPartial), probe),
				files:  map[string]qbt.TorrentFiles{"hash1": emptyFiles},
				pieces: map[string][]qbt.PieceState{"hash1": emptyPieces},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   1,
			wantBlocked: "Show.S01/Show.S01E01.mkv",
		},
		{
			// An empty read would make every boundary pack look mismatched.
			name:  "no piece states retries instead of blocking a boundary pack",
			added: AddedTorrent{Gate: GateThreshold, Threshold: 0.6, LinkedPaths: linked},
			fake: &settleFake{
				polls: polls(checkingDl, linkedPartial, linkedPartial),
				files: map[string]qbt.TorrentFiles{"hash1": straddleFiles},
			},
			wantActions: []string{"recheck:hash1"},
			wantReads:   2,
		},
		{
			name:  "piece state fetch error retries instead of dropping",
			added: AddedTorrent{Gate: GateThreshold, Threshold: 0.6, LinkedPaths: linked},
			fake: &settleFake{
				polls: []settlePoll{
					{torrent: checkingDl},
					{torrent: linkedPartial},
					{torrent: linkedPartial, before: func(f *settleFake) { f.piecesErr = nil }},
				},
				files:     map[string]qbt.TorrentFiles{"hash1": straddleFiles},
				pieces:    map[string][]qbt.PieceState{"hash1": straddlePieces},
				piecesErr: errors.New("boom"),
			},
			wantActions: []string{"recheck:hash1", "resume:hash1"},
			wantReads:   2,
		},
		{
			name: "hybrid torrent reads files and pieces by the hash qBittorrent lists",
			added: AddedTorrent{
				Hashes: []string{"v1hash", "v2hash"}, Gate: GateThreshold, Threshold: 0.6, LinkedPaths: linked,
			},
			fake: &settleFake{
				polls:  polls(hybridChecking, hybridPartial),
				files:  map[string]qbt.TorrentFiles{"v2hash": straddleFiles},
				pieces: map[string][]qbt.PieceState{"v2hash": straddlePieces},
			},
			wantActions: []string{"recheck:v1hash", "recheck:v2hash", "resume:v2hash"},
			wantReads:   1,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				fake := tt.fake
				added := tt.added
				added.InstanceID = 1
				if added.Hashes == nil {
					added.Hashes = []string{"hash1"}
				}
				if added.LinkedPaths != nil {
					added.SeasonPackRun = &models.SeasonPackRun{TorrentName: "Show.S01", Phase: "resume", LinkMode: "hardlink"}
				}
				settler := NewSettler(fake, fake)
				settler.Start(t.Context())

				require.Equal(t, tt.wantOutcome, settler.Settle(t.Context(), added))
				time.Sleep(recheckPollInterval * time.Duration(len(fake.polls)+2))
				synctest.Wait()

				fake.mu.Lock()
				defer fake.mu.Unlock()
				require.Equal(t, tt.wantActions, fake.actions)
				require.Equal(t, tt.wantReads, fake.fileReads, "file list reads")
				if tt.wantBlocked == "" {
					require.Empty(t, fake.runs)
					return
				}
				require.Len(t, fake.runs, 1)
				require.Equal(t, "failed", fake.runs[0].Status)
				require.Equal(t, "linked_file_mismatch", fake.runs[0].Reason)
				require.Equal(t, "Linked file "+tt.wantBlocked+" does not match the torrent, left paused to protect the source", fake.runs[0].Message)
			})
		})
	}
}

// TestSettleQueueFull covers the status text when the worker cannot take a torrent.
func TestSettleQueueFull(t *testing.T) {
	tests := []struct {
		name        string
		added       AddedTorrent
		failRecheck bool
		want        string
	}{
		{
			name:  "auto-resume",
			added: AddedTorrent{Gate: GateBudget},
			want:  " - auto-resume queue full, manual resume required",
		},
		{
			name:        "auto-resume after a failed recheck",
			added:       AddedTorrent{Gate: GateBudget},
			failRecheck: true,
			want:        " - recheck failed and auto-resume queue full, manual intervention required",
		},
		{
			name:  "title rescue monitor",
			added: AddedTorrent{Gate: GateMonitor, SkipAutoResume: true},
			want:  " - verification monitor queue full, manual review required - auto-resume skipped per settings",
		},
		{
			name:        "skipped auto-resume after a failed recheck",
			added:       AddedTorrent{Gate: GateBudget, SkipAutoResume: true},
			failRecheck: true,
			want:        " - recheck failed and monitor queue full, manual intervention required - auto-resume skipped per settings",
		},
		{
			// Nothing to watch, so a full queue does not matter.
			name:  "skipped auto-resume",
			added: AddedTorrent{Gate: GateBudget, SkipAutoResume: true},
			want:  " - auto-resume skipped per settings",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			fake := &settleFake{}
			// No worker runs, so the queue fills up.
			settler := NewSettler(fake, nil)
			for range cap(settler.queue) {
				settler.Settle(t.Context(), AddedTorrent{InstanceID: 1, Hashes: []string{"other"}})
			}
			if tt.failRecheck {
				fake.recheckFailures = 1
			}
			added := tt.added
			added.InstanceID, added.Hashes = 1, []string{"hash1"}

			require.Equal(t, tt.want, settler.Settle(t.Context(), added).StatusSuffix())
		})
	}
}

// settleScript shows the settle module a scripted torrent over a caller's own
// sync fake, and records what the module sends to qBittorrent. Only the
// recheck reaches the caller's fake, so a fake that fails it still fails it.
type settleScript struct {
	qbittorrentSync
	mu sync.Mutex
	// The first recheckFailures recheck calls fail before they reach the caller's fake.
	recheckFailures int
	polls           []*qbt.Torrent
	next            int
	current         *qbt.Torrent
	actions         []string
}

// startSettleScript gives service a running settle module over its own sync
// fake. Each worker poll shows the next torrent; after the last one the
// torrent is gone. Call it inside a synctest bubble.
func startSettleScript(t *testing.T, service *Service, torrents ...*qbt.Torrent) *settleScript {
	t.Helper()
	script := &settleScript{qbittorrentSync: service.syncManager, polls: torrents}
	service.settler = NewSettler(script, service.seasonPackRunStore)
	service.settler.Start(t.Context())
	return script
}

func (s *settleScript) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(filter.Hashes) > 0 {
		s.current = nil
		if s.next < len(s.polls) {
			s.current = s.polls[s.next]
			s.next++
		}
	}
	if s.current == nil {
		return nil, nil
	}
	torrent := *s.current
	if torrent.Hash == "" && len(filter.Hashes) > 0 {
		// A scripted torrent with no hash answers to the hash the worker watches.
		torrent.Hash = filter.Hashes[0]
	}
	return []qbt.Torrent{torrent}, nil
}

func (s *settleScript) BulkAction(ctx context.Context, instanceID int, hashes []string, action string) error {
	s.mu.Lock()
	for _, hash := range hashes {
		s.actions = append(s.actions, action+":"+normalizeHash(hash))
	}
	failed := action == "recheck" && s.recheckFailures > 0
	if failed {
		s.recheckFailures--
	}
	s.mu.Unlock()
	if failed {
		return errors.New("recheck failed")
	}
	if action != "recheck" {
		// The caller's fake has no lock, and the test reads it while the worker runs.
		return nil
	}
	return s.qbittorrentSync.BulkAction(ctx, instanceID, hashes, action)
}

func (s *settleScript) GetTorrentPieceStates(context.Context, int, string) ([]qbt.PieceState, error) {
	return nil, errors.New("no piece states")
}

// wait lets the worker take every scripted poll, and returns the calls the
// module sent and how many polls it took.
func (s *settleScript) wait() ([]string, int) {
	time.Sleep(recheckPollInterval * time.Duration(len(s.polls)+2))
	synctest.Wait()
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.actions, s.next
}

// scripted is a torrent state for the settle script, listed under hash.
func scripted(hash string, state qbt.TorrentState, progress float64, amountLeft int64) *qbt.Torrent {
	torrent := torrentAt(state, progress, amountLeft)
	torrent.Hash = hash
	return torrent
}

// verifyScript tells the verify gate from a byte gate: two optimistic 100%
// polls, then an observed check. The verify gate resumes once, after the
// check; a byte gate resumes on the second poll and again after the check.
func verifyScript(hash string) []*qbt.Torrent {
	full := scripted(hash, qbt.TorrentStatePausedUp, 1, 0)
	return []*qbt.Torrent{full, full, scripted(hash, qbt.TorrentStateCheckingUp, 0.5, 0), full}
}

// completeScript tells the complete gate from the budget gate: a recheck that
// ends 1 MiB short. The budget gate resumes it, the complete gate does not.
func completeScript(hash string) []*qbt.Torrent {
	return []*qbt.Torrent{
		scripted(hash, qbt.TorrentStateCheckingDl, 0.5, 0),
		scripted(hash, qbt.TorrentStatePausedDl, 0.99, 1<<20),
	}
}

// actionNames drops the hashes from recorded actions.
func actionNames(actions []string) []string {
	names := make([]string, len(actions))
	for i, action := range actions {
		names[i], _, _ = strings.Cut(action, ":")
	}
	return names
}

// idleSettler takes torrents for a test that does not watch the settle;
// no worker runs.
func idleSettler(sync qbittorrentSync) *Settler {
	return NewSettler(&settleScript{qbittorrentSync: sync}, nil)
}

// lookupFake lists the same torrents for each filtered read. The unfiltered
// read fails with allErr.
type lookupFake struct {
	settleFake
	listed []qbt.Torrent
	allErr error
}

func (f *lookupFake) GetTorrents(_ context.Context, _ int, filter qbt.TorrentFilterOptions) ([]qbt.Torrent, error) {
	if len(filter.Hashes) == 0 && f.allErr != nil {
		return nil, f.allErr
	}
	return f.listed, nil
}

func TestPollInstanceTwoHashesOfOneTorrent(t *testing.T) {
	const v1 = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	const v2 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// The recheck ended short of the complete gate, so the step drops the entry.
	torrent := qbt.Torrent{Hash: v1, InfohashV1: v1, InfohashV2: v2, State: qbt.TorrentStatePausedDl, Progress: 0.5, AmountLeft: 100}
	settler := NewSettler(&lookupFake{listed: []qbt.Torrent{torrent}}, nil)
	pending := make(map[string]*pendingResume)
	for _, hash := range []string{v1, v2} {
		pending[recheckResumeKey(1, hash)] = &pendingResume{instanceID: 1, hash: hash, budgetBytes: new(int64), sawChecking: true}
	}

	// The v2 entry is rekeyed onto v1 and dropped before the v1 hash comes up.
	settler.pollInstance(t.Context(), pending, 1, []string{v2, v1})
	require.Empty(t, pending)
}

func TestPollInstanceKeepsEntryWhenFullLookupFails(t *testing.T) {
	const hash = "cccccccccccccccccccccccccccccccccccccccc"
	settler := NewSettler(&lookupFake{allErr: errors.New("timeout")}, nil)
	pending := map[string]*pendingResume{
		recheckResumeKey(1, hash): {instanceID: 1, hash: hash, budgetBytes: new(int64)},
	}

	settler.pollInstance(t.Context(), pending, 1, []string{hash})
	require.Contains(t, pending, recheckResumeKey(1, hash))
}
