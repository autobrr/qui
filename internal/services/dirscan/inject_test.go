// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	qbsync "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

type fakeInstanceStore struct {
	instance *models.Instance
}

func (s *fakeInstanceStore) Get(_ context.Context, _ int) (*models.Instance, error) {
	return s.instance, nil
}

type failingTorrentAdder struct {
	err error
}

func (a *failingTorrentAdder) AddTorrent(_ context.Context, _ int, _ []byte, _ map[string]string) error {
	return a.err
}

func (a *failingTorrentAdder) BulkAction(_ context.Context, _ int, _ []string, _ string) error {
	return nil
}

func (a *failingTorrentAdder) ResumeWhenComplete(_ int, _ []string, _ qbsync.ResumeWhenCompleteOptions) {
}

func TestInjector_Inject_RollsBackLinkTreeOnAddFailure(t *testing.T) {
	tmp := t.TempDir()

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	hardlinkBase := filepath.Join(tmp, "links")

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          hardlinkBase,
		FallbackToRegularMode:    false,
	}

	injector := NewInjector(nil, &failingTorrentAdder{err: errors.New("add failed")}, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
				{Path: "Example.Release/extras.nfo", Size: 1, Offset: 4},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: sourceDir,
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
	}

	_, err := injector.Inject(context.Background(), req)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}

	entries, readErr := os.ReadDir(hardlinkBase)
	if readErr != nil {
		t.Fatalf("readdir hardlink base: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected hardlink base dir to be empty after rollback, got %d entries", len(entries))
	}
}

func TestHumanizeLinkPlanError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "no matching file",
			err:  &hardlinktree.LinkPlanError{Kind: hardlinktree.LinkPlanErrorNoMatchingFile, File: "Example.Release/file.mkv"},
			want: "couldn't prepare linked files for this release: no matching local source file was found for a required release file (Example.Release/file.mkv). The local file may be missing, renamed, or a different size",
		},
		{
			name: "no available file",
			err:  &hardlinktree.LinkPlanError{Kind: hardlinktree.LinkPlanErrorNoAvailableFile, File: "Example.Release/file.mkv"},
			want: "couldn't prepare linked files for this release: no usable local source file remained for (Example.Release/file.mkv)",
		},
		{
			name: "could not match",
			err:  &hardlinktree.LinkPlanError{Kind: hardlinktree.LinkPlanErrorCouldNotMatch, File: "Example.Release/file.mkv"},
			want: "couldn't prepare linked files for this release: couldn't map a required release file to a local source file (Example.Release/file.mkv)",
		},
		{
			name: "generic fallback",
			err:  errors.New("boom"),
			want: "couldn't prepare linked files for this release",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := humanizeLinkPlanError(tc.err)
			if got == nil {
				t.Fatal("expected error, got nil")
			}
			if got.Error() != tc.want {
				t.Fatalf("expected error %q, got %q", tc.want, got.Error())
			}
		})
	}
}

func TestBuildLinkTreeMatchedFiles_SizeToleranceMismatch(t *testing.T) {
	// Simulate flexible-mode match where sizes differ within tolerance:
	// torrent file is 1000 bytes, searchee file is 1040 bytes (4% diff).
	match := &MatchResult{
		MatchedFiles: []MatchedFilePair{
			{
				TorrentFile: TorrentFile{
					Path: "[SubsPlease] Show - 07 (1080p) [ABCD1234].mkv",
					Size: 1000,
				},
				SearcheeFile: &ScannedFile{
					Path:    "/media/tv/Show (2024)/Season 01/Show (2024) - S01E07 - Title [CR][WEBDL-1080p]-Group.mkv",
					RelPath: "Show (2024) - S01E07 - Title [CR][WEBDL-1080p]-Group.mkv",
					Size:    1040,
				},
			},
		},
	}

	linkable, existing, err := buildLinkTreeMatchedFiles(match)
	if err != nil {
		t.Fatalf("buildLinkTreeMatchedFiles: %v", err)
	}

	// The bug: existing[0].Size was set to SearcheeFile.Size (1040),
	// but linkable[0].Size is TorrentFile.Size (1000).
	// BuildPlan indexes by existing size and looks up by candidate size,
	// so mismatched sizes cause "no matching file" error.
	_, err = hardlinktree.BuildPlan(linkable, existing, hardlinktree.LayoutOriginal, "Show", t.TempDir())
	if err != nil {
		t.Fatalf("BuildPlan should succeed with tolerance-matched sizes, got: %v", err)
	}
}

type recordingTorrentManager struct {
	addOptions map[string]string

	bulkCalls []struct {
		instanceID int
		hashes     []string
		action     string
	}
	resumeCalls []struct {
		instanceID int
		hashes     []string
		opts       qbsync.ResumeWhenCompleteOptions
	}
}

func (m *recordingTorrentManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) error {
	m.addOptions = options
	return nil
}

func (m *recordingTorrentManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	m.bulkCalls = append(m.bulkCalls, struct {
		instanceID int
		hashes     []string
		action     string
	}{instanceID: instanceID, hashes: hashes, action: action})
	return nil
}

func (m *recordingTorrentManager) ResumeWhenComplete(instanceID int, hashes []string, opts qbsync.ResumeWhenCompleteOptions) {
	m.resumeCalls = append(m.resumeCalls, struct {
		instanceID int
		hashes     []string
		opts       qbsync.ResumeWhenCompleteOptions
	}{instanceID: instanceID, hashes: hashes, opts: opts})
}

func TestInjector_Inject_PausedPartial_TriggersRecheckAndResumeWhenComplete(t *testing.T) {
	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	manager := &recordingTorrentManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
				{Path: "Example.Release/extras.nfo", Size: 1, Offset: 4},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: "/tmp",
			Files: []*ScannedFile{{
				Path:    "/tmp/file.mkv",
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: "/tmp/file.mkv", RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
		StartPaused:  true,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	if len(manager.bulkCalls) != 1 {
		t.Fatalf("expected 1 bulk call, got %d", len(manager.bulkCalls))
	}
	if manager.bulkCalls[0].action != "recheck" {
		t.Fatalf("expected action recheck, got %q", manager.bulkCalls[0].action)
	}
	if len(manager.bulkCalls[0].hashes) != 1 || manager.bulkCalls[0].hashes[0] != "deadbeef" {
		t.Fatalf("expected hash deadbeef, got %+v", manager.bulkCalls[0].hashes)
	}

	if len(manager.resumeCalls) != 1 {
		t.Fatalf("expected 1 resume call, got %d", len(manager.resumeCalls))
	}
	if len(manager.resumeCalls[0].hashes) != 1 || manager.resumeCalls[0].hashes[0] != "deadbeef" {
		t.Fatalf("expected hash deadbeef, got %+v", manager.resumeCalls[0].hashes)
	}
	if manager.resumeCalls[0].opts.Timeout != 60*time.Minute {
		t.Fatalf("expected timeout 60m, got %v", manager.resumeCalls[0].opts.Timeout)
	}
}

func TestInjector_Inject_HardlinkMode_SelectsConcreteBaseDirFromCommaSeparatedList(t *testing.T) {
	tmp := t.TempDir()

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	firstBase := filepath.Join(tmp, "links-a")
	secondBase := filepath.Join(tmp, "links-b")

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          firstBase + ", " + secondBase,
		FallbackToRegularMode:    false,
	}

	manager := &recordingTorrentManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: sourceDir,
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			IsMatch: true,
		},
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.Mode != injectModeHardlink {
		t.Fatalf("expected hardlink mode, got %q", res.Mode)
	}
	if strings.Contains(res.SavePath, ",") {
		t.Fatalf("save path should use one base dir, got %q", res.SavePath)
	}
	if !strings.HasPrefix(res.SavePath, firstBase+string(os.PathSeparator)) {
		t.Fatalf("expected save path under first matching base dir %q, got %q", firstBase, res.SavePath)
	}
	if got := manager.addOptions["savepath"]; got != res.SavePath {
		t.Fatalf("expected add savepath %q, got %q", res.SavePath, got)
	}
}

func TestInjector_Inject_PausedPerfect_DoesNotTriggerRecheck(t *testing.T) {
	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	manager := &recordingTorrentManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: "/tmp",
			Files: []*ScannedFile{{
				Path:    "/tmp/file.mkv",
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: "/tmp/file.mkv", RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
		StartPaused:  true,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	if len(manager.bulkCalls) != 0 || len(manager.resumeCalls) != 0 {
		t.Fatalf("expected no recheck/resume calls, got bulk=%d resume=%d", len(manager.bulkCalls), len(manager.resumeCalls))
	}
	if manager.addOptions["skip_checking"] != "true" {
		t.Fatalf("expected skip_checking=true, got %q", manager.addOptions["skip_checking"])
	}
}

// fakeTorrentChecker simulates state transitions. It returns states from the
// list in order, staying on the last state once exhausted. This lets tests
// model the recheck flow: checking -> paused/downloading.
type fakeTorrentChecker struct {
	mu     sync.Mutex
	hash   string
	states []qbt.TorrentState
	idx    int
}

func (c *fakeTorrentChecker) HasTorrentByAnyHash(_ context.Context, _ int, _ []string) (*qbt.Torrent, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.states) == 0 {
		return nil, false, nil
	}
	state := c.states[c.idx]
	if c.idx < len(c.states)-1 {
		c.idx++
	}
	return &qbt.Torrent{Hash: c.hash, State: state}, true, nil
}

// safeRecordingManager is a thread-safe version of recordingTorrentManager for tests
// involving async goroutines (resumeAfterRecheck).
type safeRecordingManager struct {
	mu         sync.Mutex
	addOptions map[string]string
	bulkCalls  []struct {
		instanceID int
		hashes     []string
		action     string
	}
}

func (m *safeRecordingManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addOptions = options
	return nil
}

func (m *safeRecordingManager) BulkAction(_ context.Context, instanceID int, hashes []string, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bulkCalls = append(m.bulkCalls, struct {
		instanceID int
		hashes     []string
		action     string
	}{instanceID: instanceID, hashes: hashes, action: action})
	return nil
}

func (m *safeRecordingManager) ResumeWhenComplete(_ int, _ []string, _ qbsync.ResumeWhenCompleteOptions) {
}

func (m *safeRecordingManager) getBulkCalls() []struct {
	instanceID int
	hashes     []string
	action     string
} {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make([]struct {
		instanceID int
		hashes     []string
		action     string
	}, len(m.bulkCalls))
	copy(cp, m.bulkCalls)
	return cp
}

func (m *safeRecordingManager) getAddOptions() map[string]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := make(map[string]string, len(m.addOptions))
	maps.Copy(cp, m.addOptions)
	return cp
}

func TestInjector_PartialLinkTree_DownloadMissingEnabled_NotPaused(t *testing.T) {
	tmp := t.TempDir()

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	hardlinkBase := filepath.Join(tmp, "links")

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          hardlinkBase,
	}

	checker := &fakeTorrentChecker{
		hash: "deadbeef",
		// Return non-checking state: simulates recheck completing quickly.
		states: []qbt.TorrentState{qbt.TorrentStatePausedDl},
	}

	manager := &safeRecordingManager{}
	injector := NewInjector(nil, manager, checker, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
				{Path: "Example.Release/extras.nfo", Size: 1, Offset: 4},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: sourceDir,
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult:         &jackett.SearchResult{Indexer: "Test"},
		StartPaused:          false,
		DownloadMissingFiles: true,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.Mode != injectModeHardlink {
		t.Fatalf("expected hardlink mode, got %q", res.Mode)
	}

	// Torrent must be forced paused for the recheck, even though StartPaused=false.
	opts := manager.getAddOptions()
	if opts["paused"] != "true" || opts["stopped"] != "true" {
		t.Fatalf("expected forced paused/stopped for partial link tree, got paused=%q stopped=%q",
			opts["paused"], opts["stopped"])
	}

	// Wait for the async resume goroutine: 3s grace + first poll.
	deadline := time.After(15 * time.Second)
	for {
		calls := manager.getBulkCalls()
		if len(calls) >= 2 {
			if calls[0].action != "recheck" {
				t.Fatalf("first bulk call should be recheck, got %q", calls[0].action)
			}
			if calls[1].action != "resume" {
				t.Fatalf("second bulk call should be resume, got %q", calls[1].action)
			}
			break
		}
		select {
		case <-deadline:
			t.Fatalf("timed out waiting for resume bulk call, got %d calls", len(calls))
		default:
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func TestInjector_PartialLinkTree_DownloadMissingEnabled_Paused(t *testing.T) {
	tmp := t.TempDir()

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	hardlinkBase := filepath.Join(tmp, "links")

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          hardlinkBase,
	}

	manager := &safeRecordingManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
				{Path: "Example.Release/extras.nfo", Size: 1, Offset: 4},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: sourceDir,
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult:         &jackett.SearchResult{Indexer: "Test"},
		StartPaused:          true,
		DownloadMissingFiles: true,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	calls := manager.getBulkCalls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 bulk call (recheck only, no resume), got %d", len(calls))
	}
	if calls[0].action != "recheck" {
		t.Fatalf("expected recheck, got %q", calls[0].action)
	}
}

func TestInjector_PartialLinkTree_DownloadMissingDisabled(t *testing.T) {
	tmp := t.TempDir()

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	hardlinkBase := filepath.Join(tmp, "links")

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          hardlinkBase,
	}

	manager := &safeRecordingManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
				{Path: "Example.Release/extras.nfo", Size: 1, Offset: 4},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: sourceDir,
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult:         &jackett.SearchResult{Indexer: "Test"},
		DownloadMissingFiles: false,
	}

	_, err := injector.Inject(context.Background(), req)
	if err == nil {
		t.Fatal("expected error for partial link tree with DownloadMissingFiles=false")
	}
	if !strings.Contains(err.Error(), "missing files") {
		t.Fatalf("expected 'missing files' in error, got: %v", err)
	}

	// No torrent should have been added.
	calls := manager.getBulkCalls()
	if len(calls) != 0 {
		t.Fatalf("expected no bulk calls, got %d", len(calls))
	}

	// Link tree should have been cleaned up.
	entries, readErr := os.ReadDir(hardlinkBase)
	if readErr != nil {
		t.Fatalf("readdir: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected hardlink base to be empty after rejection rollback, got %d entries", len(entries))
	}
}

func TestInjector_PerfectMatch_UnaffectedByDownloadMissing(t *testing.T) {
	tmp := t.TempDir()

	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	hardlinkBase := filepath.Join(tmp, "links")

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          hardlinkBase,
	}

	manager := &safeRecordingManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: sourceDir,
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult:         &jackett.SearchResult{Indexer: "Test"},
		DownloadMissingFiles: false, // Should not matter for perfect match.
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if res.Mode != injectModeHardlink {
		t.Fatalf("expected hardlink mode, got %q", res.Mode)
	}

	opts := manager.getAddOptions()
	if opts["skip_checking"] != "true" {
		t.Fatalf("expected skip_checking=true for perfect match, got %q", opts["skip_checking"])
	}

	calls := manager.getBulkCalls()
	if len(calls) != 0 {
		t.Fatalf("expected no bulk calls for perfect match, got %d", len(calls))
	}
}

func TestInjector_Inject_RunningPartial_DoesNotTriggerRecheck(t *testing.T) {
	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	manager := &recordingTorrentManager{}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Example.Release",
			InfoHash: "deadbeef",
			Files: []TorrentFile{
				{Path: "Example.Release/file.mkv", Size: 4, Offset: 0},
				{Path: "Example.Release/extras.nfo", Size: 1, Offset: 4},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Example.Release",
			Path: "/tmp",
			Files: []*ScannedFile{{
				Path:    "/tmp/file.mkv",
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: "/tmp/file.mkv", RelPath: "file.mkv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Example.Release/file.mkv", Size: 4},
			}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
		StartPaused:  false,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}

	if len(manager.bulkCalls) != 0 || len(manager.resumeCalls) != 0 {
		t.Fatalf("expected no recheck/resume calls, got bulk=%d resume=%d", len(manager.bulkCalls), len(manager.resumeCalls))
	}
}
