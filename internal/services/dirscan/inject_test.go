// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/internal/services/jackett"
	"github.com/autobrr/qui/pkg/hardlinktree"
)

func testBackendPool(instance *models.Instance) *fsops.Pool {
	return fsops.NewPool(&fakeInstanceStore{instance: instance}, local.NewBackend())
}

type fakeInstanceStore struct {
	instance *models.Instance
}

func (s *fakeInstanceStore) Get(_ context.Context, _ int) (*models.Instance, error) {
	return s.instance, nil
}

type failingTorrentAdder struct {
	err    error
	called bool
}

func (a *failingTorrentAdder) AddTorrent(_ context.Context, _ int, _ []byte, _ map[string]string) (*qbt.TorrentAddResponse, error) {
	a.called = true
	return nil, a.err
}

func (a *failingTorrentAdder) RenameTorrentFile(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (a *failingTorrentAdder) RenameTorrentFolder(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (a *failingTorrentAdder) GetTorrentFilesBatch(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
	return nil, nil
}

// injectFixture is the on-disk source file, instance and partial-match request
// shared by the partial-link-tree tests.
type injectFixture struct {
	hardlinkBase string
	instance     *models.Instance
	req          *InjectRequest
}

// newInjectFixture builds a partial match that reaches AddTorrent. Without
// DownloadMissingFiles the partial match is rejected before the add, so the
// add-failure rollbacks would never be exercised; the rejection test flips the
// flag back off to take that earlier path.
func newInjectFixture(t *testing.T) injectFixture {
	t.Helper()
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
		DownloadMissingFiles: true,
	}

	return injectFixture{hardlinkBase: hardlinkBase, instance: instance, req: req}
}

func TestInjector_Inject_RollsBackLinkTreeOnAddFailure(t *testing.T) {
	fx := newInjectFixture(t)

	adder := &failingTorrentAdder{err: errors.New("add failed")}
	injector := NewInjector(nil, adder, (&settleFake{}).idle(), nil, &fakeInstanceStore{instance: fx.instance}, nil, testBackendPool(fx.instance))

	_, err := injector.Inject(context.Background(), fx.req)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !adder.called {
		t.Fatalf("expected AddTorrent to be called; the rollback assertion would be vacuous")
	}

	entries, readErr := os.ReadDir(fx.hardlinkBase)
	if readErr != nil {
		t.Fatalf("readdir hardlink base: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected hardlink base dir to be empty after rollback, got %d entries", len(entries))
	}
}

// cancellingTorrentAdder cancels the run's context from inside AddTorrent, so
// the rollback that follows sees a cancelled ctx. It records how many entries
// the link base held at that moment, which keeps the "tree is gone" assertion
// non-vacuous. Every other method is failingTorrentAdder's no-op.
type cancellingTorrentAdder struct {
	*failingTorrentAdder
	cancel       context.CancelFunc
	watchDir     string
	entriesAtAdd int
}

func (a *cancellingTorrentAdder) AddTorrent(_ context.Context, _ int, _ []byte, _ map[string]string) (*qbt.TorrentAddResponse, error) {
	entries, _ := os.ReadDir(a.watchDir)
	a.entriesAtAdd = len(entries)
	a.cancel()
	return nil, a.err
}

func TestInjector_Inject_RollsBackLinkTreeWhenAddFailsUnderCancelledContext(t *testing.T) {
	fx := newInjectFixture(t)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	addErr := errors.New("add failed")
	adder := &cancellingTorrentAdder{
		failingTorrentAdder: &failingTorrentAdder{err: addErr},
		cancel:              cancel,
		watchDir:            fx.hardlinkBase,
	}
	injector := NewInjector(nil, adder, (&settleFake{}).idle(), nil, &fakeInstanceStore{instance: fx.instance}, nil, testBackendPool(fx.instance))

	_, err := injector.Inject(ctx, fx.req)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if !errors.Is(err, addErr) {
		t.Fatalf("expected the add failure to surface, got %v", err)
	}
	// One entry: the tree root the injector created under the base dir.
	if adder.entriesAtAdd != 1 {
		t.Fatalf("expected the link tree to exist when AddTorrent ran, got %d entries; the rollback assertion would be vacuous", adder.entriesAtAdd)
	}

	entries, readErr := os.ReadDir(fx.hardlinkBase)
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

func TestBuildLinkTreeMatchedFiles_UsesTorrentFileSize(t *testing.T) {
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
					Size:    1000,
				},
			},
		},
	}

	linkable, existing, err := buildLinkTreeMatchedFiles(match)
	if err != nil {
		t.Fatalf("buildLinkTreeMatchedFiles: %v", err)
	}

	// The bug was that existing[0].Size used the searchee file size instead of the
	// torrent file size. Exact file-size matching means these should now be equal.
	_, err = hardlinktree.BuildPlan(fsops.HostPaths, linkable, existing, hardlinktree.LayoutOriginal, "Show", t.TempDir())
	if err != nil {
		t.Fatalf("BuildPlan should succeed with exact matched sizes, got: %v", err)
	}
}

type recordingTorrentManager struct {
	addOptions map[string]string
}

func (m *recordingTorrentManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addOptions = options
	return nil, nil
}

func (m *recordingTorrentManager) RenameTorrentFile(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (m *recordingTorrentManager) RenameTorrentFolder(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (m *recordingTorrentManager) GetTorrentFilesBatch(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
	return nil, nil
}

func TestInjector_Inject_PausedPartial_RechecksWithoutResume(t *testing.T) {
	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	manager := &recordingTorrentManager{}
	settle := &settleFake{}
	injector := NewInjector(nil, manager, settle.idle(), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

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

	// StartPaused leaves nothing to watch, so the module only sends the recheck.
	if got := settle.hashedCalls(); !slices.Equal(got, []string{"recheck:deadbeef"}) {
		t.Fatalf("settle calls = %v, want recheck:deadbeef", got)
	}
}

// crossDeviceBackend fails HardlinkTree with EXDEV under the listed base dirs,
// the way link() fails on a host whose disks share one device ID.
type crossDeviceBackend struct {
	*local.Backend
	crossDevice []string
}

func (b crossDeviceBackend) HardlinkTree(ctx context.Context, plan *hardlinktree.TreePlan) (*fsops.TreeCreateResult, error) {
	for _, dir := range b.crossDevice {
		if plan.RootDir == dir || strings.HasPrefix(plan.RootDir, dir+string(filepath.Separator)) {
			return nil, &os.LinkError{Op: "link", Old: "src", New: plan.RootDir, Err: syscall.EXDEV}
		}
	}
	return b.Backend.HardlinkTree(ctx, plan)
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

	tests := []struct {
		name        string
		crossDevice []string
		wantBase    string
	}{
		{name: "first matching base dir", wantBase: firstBase},
		{name: "cross-device first base dir tries the next", crossDevice: []string{firstBase}, wantBase: secondBase},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance := &models.Instance{
				ID:                       1,
				Name:                     "test",
				HasLocalFilesystemAccess: true,
				UseHardlinks:             true,
				HardlinkBaseDir:          firstBase + ", " + secondBase,
				FallbackToRegularMode:    false,
			}

			manager := &recordingTorrentManager{}
			pool := fsops.NewPool(&fakeInstanceStore{instance: instance}, crossDeviceBackend{local.NewBackend(), tt.crossDevice})
			injector := NewInjector(nil, manager, (&settleFake{}).idle(), nil, &fakeInstanceStore{instance: instance}, nil, pool)

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

			res, err := injector.Inject(t.Context(), req)
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
			// The torrent's root folder is free, so it goes into the base dir
			// with no isolation folder.
			if res.SavePath != tt.wantBase {
				t.Fatalf("expected save path %q, got %q", tt.wantBase, res.SavePath)
			}
			if got := manager.addOptions["savepath"]; got != res.SavePath {
				t.Fatalf("expected add savepath %q, got %q", res.SavePath, got)
			}
		})
	}
}

func TestInjector_Inject_PausedPerfect_DoesNotTriggerRecheck(t *testing.T) {
	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	manager := &recordingTorrentManager{}
	settle := &settleFake{}
	injector := NewInjector(nil, manager, settle.idle(), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

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
			Path: "/downloads/Example.Release",
			Files: []*ScannedFile{{
				Path:    "/downloads/Example.Release/file.mkv",
				RelPath: "file.mkv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: "/downloads/Example.Release/file.mkv", RelPath: "file.mkv", Size: 4},
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

	if got := settle.calls(); len(got) != 0 {
		t.Fatalf("expected no recheck/resume calls, got %v", got)
	}
	if manager.addOptions["skip_checking"] != "true" {
		t.Fatalf("expected skip_checking=true, got %q", manager.addOptions["skip_checking"])
	}
}

func TestInjector_Inject_DiscLayoutPerfect_StaysPausedUntilComplete(t *testing.T) {
	instance, req := discLayoutRequest(t)
	manager := &recordingTorrentManager{}

	synctest.Test(t, func(t *testing.T) {
		// The recheck ends 1 MiB short: a disc layout resumes only when nothing is left.
		settle := &settleFake{polls: []*qbt.Torrent{
			{Hash: "deadbeef", State: qbt.TorrentStateCheckingUp, Progress: 0.5},
			{Hash: "deadbeef", State: qbt.TorrentStatePausedDl, Progress: 0.99, Completed: 4, AmountLeft: 1 << 20},
			{Hash: "deadbeef", State: qbt.TorrentStatePausedUp, Progress: 1},
			{Hash: "deadbeef", State: qbt.TorrentStatePausedUp, Progress: 1},
		}}
		injector := NewInjector(nil, manager, settle.start(t), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

		res, err := injector.Inject(t.Context(), req)
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if !res.Success {
			t.Fatalf("expected success, got %+v", res)
		}
		if manager.addOptions["paused"] != "true" || manager.addOptions["stopped"] != "true" {
			t.Fatalf("expected disc layout to force paused/stopped, got paused=%q stopped=%q",
				manager.addOptions["paused"], manager.addOptions["stopped"])
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if got := settle.hashedCalls(); !slices.Equal(got, []string{"recheck:deadbeef"}) {
			t.Fatalf("settle calls = %v, want only the recheck of deadbeef", got)
		}
	})
}

// discLayoutRequest is a perfect match of a disc layout, which the add policy force-pauses.
func discLayoutRequest(t *testing.T) (*models.Instance, *InjectRequest) {
	t.Helper()
	sourceDir := t.TempDir()
	sourceFile := filepath.Join(sourceDir, "Movie", "BDMV", "index.bdmv")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("bdmv"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Movie",
			InfoHash:    "deadbeef",
			Files:       []TorrentFile{{Path: "Movie/BDMV/index.bdmv", Size: 4, Offset: 0}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Movie",
			// A real searchee's Path is the release directory itself, with file RelPaths relative to
			// it (scanner.scanSearcheeDir), so the on-disk folder name matches the torrent root and
			// no content-path alignment is needed here.
			Path: filepath.Join(sourceDir, "Movie"),
			Files: []*ScannedFile{{
				Path:    sourceFile,
				RelPath: "BDMV/index.bdmv",
				Size:    4,
			}},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{Path: sourceFile, RelPath: "BDMV/index.bdmv", Size: 4},
				TorrentFile:  TorrentFile{Path: "Movie/BDMV/index.bdmv", Size: 4},
			}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
	}

	return instance, req
}

// safeRecordingManager records the add options under a lock.
type safeRecordingManager struct {
	mu         sync.Mutex
	addOptions map[string]string
}

func (m *safeRecordingManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.addOptions = options
	return nil, nil
}

func (m *safeRecordingManager) RenameTorrentFile(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (m *safeRecordingManager) RenameTorrentFolder(_ context.Context, _ int, _, _, _ string) error {
	return nil
}

func (m *safeRecordingManager) GetTorrentFilesBatch(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
	return nil, nil
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

	manager := &safeRecordingManager{}
	synctest.Test(t, func(t *testing.T) {
		// A fast recheck finishes between polls; completed bytes prove it ran.
		partial := &qbt.Torrent{Hash: "deadbeef", State: qbt.TorrentStatePausedDl, Progress: 0.8, Completed: 4, AmountLeft: 1}
		settle := &settleFake{
			polls: []*qbt.Torrent{
				partial, partial,
				{Hash: "deadbeef", State: qbt.TorrentStateDownloading, Progress: 0.8, AmountLeft: 1},
				{Hash: "deadbeef", State: qbt.TorrentStateDownloading, Progress: 0.8, AmountLeft: 1},
			},
			files: qbt.TorrentFiles{
				{Name: "Example.Release/file.mkv", Size: 4, PieceRange: []int{0, 0}, Progress: 1},
				{Name: "Example.Release/extras.nfo", Size: 1, PieceRange: []int{0, 0}},
			},
			pieces: []qbt.PieceState{qbt.PieceStateNotDownloadYet},
		}
		injector := NewInjector(nil, manager, settle.start(t), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

		res, err := injector.Inject(t.Context(), req)
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

		time.Sleep(time.Minute)
		synctest.Wait()
		if got := settle.calls(); !slices.Equal(got, []string{"recheck", "resume"}) {
			t.Fatalf("settle calls = %v, want recheck then resume", got)
		}
	})
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
	settle := &settleFake{}
	injector := NewInjector(nil, manager, settle.idle(), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

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

	if got := settle.calls(); !slices.Equal(got, []string{"recheck"}) {
		t.Fatalf("expected the recheck only, no resume, got %v", got)
	}
}

func TestInjector_PartialLinkTree_DownloadMissingDisabled(t *testing.T) {
	fx := newInjectFixture(t)
	fx.req.DownloadMissingFiles = false

	manager := &safeRecordingManager{}
	settle := &settleFake{}
	injector := NewInjector(nil, manager, settle.idle(), nil, &fakeInstanceStore{instance: fx.instance}, nil, testBackendPool(fx.instance))

	_, err := injector.Inject(context.Background(), fx.req)
	if err == nil {
		t.Fatal("expected error for partial link tree with DownloadMissingFiles=false")
	}
	if !strings.Contains(err.Error(), "missing files") {
		t.Fatalf("expected 'missing files' in error, got: %v", err)
	}

	// No torrent should have been added.
	calls := settle.calls()
	if len(calls) != 0 {
		t.Fatalf("expected no bulk calls, got %d", len(calls))
	}

	// Link tree should have been cleaned up.
	entries, readErr := os.ReadDir(fx.hardlinkBase)
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
	settle := &settleFake{}
	injector := NewInjector(nil, manager, settle.idle(), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

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

	calls := settle.calls()
	if len(calls) != 0 {
		t.Fatalf("expected no bulk calls for perfect match, got %d", len(calls))
	}
}

func TestInjector_Inject_RunningPartial_RechecksAndResumesWhenComplete(t *testing.T) {
	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
	}

	manager := &recordingTorrentManager{}

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

	synctest.Test(t, func(t *testing.T) {
		settle := &settleFake{polls: []*qbt.Torrent{
			{Hash: "deadbeef", State: qbt.TorrentStateCheckingDl, Progress: 0.5, AmountLeft: 1},
			{Hash: "deadbeef", State: qbt.TorrentStateDownloading, Progress: 0.9, AmountLeft: 1},
			{Hash: "deadbeef", State: qbt.TorrentStateUploading, Progress: 1},
			{Hash: "deadbeef", State: qbt.TorrentStateUploading, Progress: 1},
			{Hash: "deadbeef", State: qbt.TorrentStateUploading, Progress: 1},
		}}
		injector := NewInjector(nil, manager, settle.start(t), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		res, err := injector.Inject(ctx, req)
		if err != nil {
			t.Fatalf("inject: %v", err)
		}
		if !res.Success {
			t.Fatalf("expected success, got %+v", res)
		}
		time.Sleep(time.Minute)
		synctest.Wait()

		if settle.recheckCtxErr != nil {
			t.Fatalf("expected background recheck context, got ctx err %v", settle.recheckCtxErr)
		}
		// The torrent downloads its missing file and resumes once nothing is left.
		if got := settle.hashedCalls(); !slices.Equal(got, []string{"recheck:deadbeef", "resume:deadbeef"}) {
			t.Fatalf("settle calls = %v, want recheck then resume of deadbeef", got)
		}
	})
}

// partialLinkTreeRequest returns a partial match of file.mkv, with extras.nfo
// left to download.
func partialLinkTreeRequest(t *testing.T) (*models.Instance, *InjectRequest) {
	t.Helper()
	tmp := t.TempDir()
	sourceDir := filepath.Join(tmp, "source")
	if err := os.MkdirAll(sourceDir, 0o755); err != nil {
		t.Fatalf("mkdir source: %v", err)
	}
	sourceFile := filepath.Join(sourceDir, "file.mkv")
	if err := os.WriteFile(sourceFile, []byte("data"), 0o600); err != nil {
		t.Fatalf("write source file: %v", err)
	}

	instance := &models.Instance{
		ID:                       1,
		Name:                     "test",
		HasLocalFilesystemAccess: true,
		UseHardlinks:             true,
		HardlinkBaseDir:          filepath.Join(tmp, "links"),
	}
	scanned := &ScannedFile{Path: sourceFile, RelPath: "file.mkv", Size: 4}
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
		Searchee: &Searchee{Name: "Example.Release", Path: sourceDir, Files: []*ScannedFile{scanned}},
		MatchResult: &MatchResult{
			MatchedFiles:          []MatchedFilePair{{SearcheeFile: scanned, TorrentFile: TorrentFile{Path: "Example.Release/file.mkv", Size: 4}}},
			UnmatchedTorrentFiles: []TorrentFile{{Path: "Example.Release/extras.nfo", Size: 1}},
			IsMatch:               true,
			IsPartialMatch:        true,
		},
		SearchResult:         &jackett.SearchResult{Indexer: "Test"},
		DownloadMissingFiles: true,
	}
	return instance, req
}

// TestInjector_PartialHardlinkResumeLinkedFileCheck covers the ADR 0004 check
// before a partial hardlink add resumes after its recheck.
// partialSettle lists a partial link tree add under its v2 hash, as qBittorrent
// lists a hybrid torrent, with the parsed v1 hash as a variant.
func partialSettle(states ...qbt.TorrentState) []*qbt.Torrent {
	polls := make([]*qbt.Torrent, len(states))
	for i, state := range states {
		polls[i] = &qbt.Torrent{
			Hash: "cafef00d", InfohashV1: "deadbeef", InfohashV2: "cafef00d",
			State: state, Progress: 0.8, Completed: 4, AmountLeft: 1,
		}
	}
	return polls
}

// TestInjector_PartialHardlinkResumeLinkedFileCheck covers the ADR 0004 check
// before a partial hardlink add resumes after its recheck.
func TestInjector_PartialHardlinkResumeLinkedFileCheck(t *testing.T) {
	const (
		missing = qbt.PieceStateNotDownloadYet
		have    = qbt.PieceStateAlreadyDownloaded
	)
	tests := []struct {
		name          string
		files         qbt.TorrentFiles
		pieces        []qbt.PieceState
		pieceFailures int
		wantResume    bool
		wantReads     int
	}{
		{
			name: "linked file fails its own piece",
			files: qbt.TorrentFiles{
				{Name: "Example.Release/file.mkv", Size: 4, PieceRange: []int{0, 1}, Progress: 0.5},
				{Name: "Example.Release/extras.nfo", Size: 1, PieceRange: []int{2, 2}},
			},
			pieces: []qbt.PieceState{missing, have, missing},
			// One read, then the torrent stays paused: a failed read would retry.
			wantReads: 1,
		},
		{
			name: "only failed piece is shared with the unmatched file",
			files: qbt.TorrentFiles{
				{Name: "Example.Release/file.mkv", Size: 4, PieceRange: []int{0, 1}, Progress: 0.5},
				{Name: "Example.Release/extras.nfo", Size: 1, PieceRange: []int{1, 1}},
			},
			pieces:     []qbt.PieceState{have, missing},
			wantResume: true,
			// The check runs on each poll that can resume: the stable poll, the
			// resume, and the retry while qBittorrent still shows it paused.
			wantReads: 3,
		},
		{
			name: "piece state read fails once then succeeds",
			files: qbt.TorrentFiles{
				{Name: "Example.Release/file.mkv", Size: 4, PieceRange: []int{0, 0}, Progress: 1},
				{Name: "Example.Release/extras.nfo", Size: 1, PieceRange: []int{1, 1}},
			},
			pieces:        []qbt.PieceState{have, missing},
			pieceFailures: 1,
			wantResume:    true,
			wantReads:     3,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			instance, req := partialLinkTreeRequest(t)
			synctest.Test(t, func(t *testing.T) {
				settle := &settleFake{
					polls: partialSettle(
						qbt.TorrentStatePausedDl, qbt.TorrentStatePausedDl, qbt.TorrentStatePausedDl,
						qbt.TorrentStateDownloading, qbt.TorrentStateDownloading,
					),
					qbitHash:      "cafef00d",
					files:         tt.files,
					pieces:        tt.pieces,
					pieceFailures: tt.pieceFailures,
				}
				injector := NewInjector(nil, &safeRecordingManager{}, settle.start(t), nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

				res, err := injector.Inject(t.Context(), req)
				if err != nil || !res.Success || res.Mode != injectModeHardlink {
					t.Fatalf("inject: res=%+v err=%v", res, err)
				}
				time.Sleep(time.Minute)
				synctest.Wait()

				resumed := slices.Contains(settle.calls(), "resume")
				if resumed != tt.wantResume {
					t.Fatalf("resumed = %v (calls %v), want %v", resumed, settle.calls(), tt.wantResume)
				}
				if reads := settle.reads(); reads != tt.wantReads {
					t.Fatalf("piece state reads = %d, want %d", reads, tt.wantReads)
				}
			})
		})
	}
}

func TestInjector_PartialReflinkResumeSkipsLinkedFileCheck(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		_, req := partialLinkTreeRequest(t)
		// Every piece state read fails, so only a skipped check resumes.
		settle := &settleFake{
			polls:         partialSettle(qbt.TorrentStatePausedDl, qbt.TorrentStatePausedDl, qbt.TorrentStateDownloading, qbt.TorrentStateDownloading),
			pieceFailures: 1 << 30,
		}
		injector := NewInjector(nil, &safeRecordingManager{}, settle.start(t), nil, nil, nil, nil)

		if err := injector.triggerRecheckForPartialLinkTree(req, injectModeReflink); err != nil {
			t.Fatalf("trigger recheck: %v", err)
		}
		time.Sleep(time.Minute)
		synctest.Wait()

		if !slices.Contains(settle.calls(), "resume") {
			t.Fatal("expected the reflink add to resume")
		}
		if reads := settle.reads(); reads != 0 {
			t.Fatalf("piece state reads = %d, want 0", reads)
		}
	})
}

// Link-tree injection is host code until #2919, so it refuses a remote
// instance even with Write (#2942), a link mode and a base dir configured.
func TestValidateLinkTreeInstanceRefusesRemoteInstance(t *testing.T) {
	remote := &models.Instance{
		UseHardlinks: true, HardlinkBaseDir: "/links",
		SSHHost: "box.example.invalid", SSHKeyEncrypted: "enc-key", SSHHostKeyEncrypted: "enc-hostkey",
	}
	local := *remote
	local.HasLocalFilesystemAccess = true

	if err := validateLinkTreeInstance(&local); err != nil {
		t.Fatalf("local instance: %v", err)
	}
	if err := validateLinkTreeInstance(remote); err == nil {
		t.Fatal("remote instance: got no error, want a refusal")
	}
}
