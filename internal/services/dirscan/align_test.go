// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	qbsync "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
)

func TestBuildAlignmentPlan(t *testing.T) {
	tests := []struct {
		name          string
		torrentFiles  []TorrentFile
		searcheePath  string
		searcheeIsDir bool
		matched       []MatchedFilePair
		wantNeeded    bool
		wantStrip     bool
		wantSource    string
		wantTarget    string
		wantFolder    bool
		wantFiles     []fileRename
	}{
		{
			name:          "folder mismatch, identical filenames",
			torrentFiles:  []TorrentFile{{Path: "Linux.Distribution.Release.01/a.iso", Size: 1}},
			searcheePath:  "/data/isos/Release 01",
			searcheeIsDir: true,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 1},
				TorrentFile:  TorrentFile{Path: "Linux.Distribution.Release.01/a.iso", Size: 1},
			}},
			wantNeeded: true,
			wantSource: "Linux.Distribution.Release.01",
			wantTarget: "Release 01",
			wantFolder: true,
		},
		{
			name:          "folder and filename mismatch",
			torrentFiles:  []TorrentFile{{Path: "Some.Release/movie.2020.mkv", Size: 2}},
			searcheePath:  "/media/Release Folder",
			searcheeIsDir: true,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "Movie (2020).mkv", Size: 2},
				TorrentFile:  TorrentFile{Path: "Some.Release/movie.2020.mkv", Size: 2},
			}},
			wantNeeded: true,
			wantSource: "Some.Release",
			wantTarget: "Release Folder",
			wantFolder: true,
			wantFiles:  []fileRename{{oldPath: "Some.Release/movie.2020.mkv", newPath: "Some.Release/Movie (2020).mkv"}},
		},
		{
			name:          "nested subdir, folder mismatch only",
			torrentFiles:  []TorrentFile{{Path: "Root/Sub/a.mkv", Size: 3}},
			searcheePath:  "/x/Target",
			searcheeIsDir: true,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "Sub/a.mkv", Size: 3},
				TorrentFile:  TorrentFile{Path: "Root/Sub/a.mkv", Size: 3},
			}},
			wantNeeded: true,
			wantSource: "Root",
			wantTarget: "Target",
			wantFolder: true,
		},
		{
			name:          "identical folder and filenames, no alignment",
			torrentFiles:  []TorrentFile{{Path: "Release 01/a.iso", Size: 1}},
			searcheePath:  "/data/isos/Release 01",
			searcheeIsDir: true,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 1},
				TorrentFile:  TorrentFile{Path: "Release 01/a.iso", Size: 1},
			}},
			wantNeeded: false,
		},
		{
			name:          "rootless torrent, identical filename, no alignment",
			torrentFiles:  []TorrentFile{{Path: "a.iso", Size: 1}},
			searcheePath:  "/data/isos/whatever",
			searcheeIsDir: true,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 1},
				TorrentFile:  TorrentFile{Path: "a.iso", Size: 1},
			}},
			wantNeeded: false,
		},
		{
			name:          "rootless torrent, differing filename, file rename only",
			torrentFiles:  []TorrentFile{{Path: "ep01.mkv", Size: 5}},
			searcheePath:  "/data/tv/Season 01",
			searcheeIsDir: true,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "Show S01E01.mkv", Size: 5},
				TorrentFile:  TorrentFile{Path: "ep01.mkv", Size: 5},
			}},
			wantNeeded: true,
			wantSource: "",
			wantTarget: "",
			wantFolder: false,
			wantFiles:  []fileRename{{oldPath: "ep01.mkv", newPath: "Show S01E01.mkv"}},
		},
		{
			// No on-disk folder to rename the root to; the root is stripped at add time
			// (contentLayout=NoSubfolder) and the stripped name already matches disk.
			name:          "foldered torrent matched to single loose file, identical filename, strip only",
			torrentFiles:  []TorrentFile{{Path: "Some.Folder/Movie.mkv", Size: 7}},
			searcheePath:  "/downloads/Movie.mkv",
			searcheeIsDir: false,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "Movie.mkv", Size: 7},
				TorrentFile:  TorrentFile{Path: "Some.Folder/Movie.mkv", Size: 7},
			}},
			wantNeeded: false,
			wantStrip:  true,
		},
		{
			name:          "foldered torrent matched to single loose file, differing filename, strip and rename",
			torrentFiles:  []TorrentFile{{Path: "Some.Folder/movie.mkv", Size: 7}},
			searcheePath:  "/downloads/Movie (2020).mkv",
			searcheeIsDir: false,
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "Movie (2020).mkv", Size: 7},
				TorrentFile:  TorrentFile{Path: "Some.Folder/movie.mkv", Size: 7},
			}},
			wantNeeded: true,
			wantStrip:  true,
			wantSource: "Some.Folder",
			wantFiles:  []fileRename{{oldPath: "movie.mkv", newPath: "Movie (2020).mkv"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InjectRequest{
				ParsedTorrent: &ParsedTorrent{Files: tt.torrentFiles},
				Searchee:      &Searchee{Path: tt.searcheePath},
				MatchResult:   &MatchResult{MatchedFiles: tt.matched},
			}
			plan := buildAlignmentPlan(req, tt.searcheeIsDir)

			if plan.needed() != tt.wantNeeded {
				t.Fatalf("needed() = %v, want %v (plan=%+v)", plan.needed(), tt.wantNeeded, plan)
			}
			if plan.stripRoot != tt.wantStrip {
				t.Errorf("stripRoot = %v, want %v", plan.stripRoot, tt.wantStrip)
			}
			if !tt.wantNeeded {
				return
			}
			if plan.sourceRoot != tt.wantSource {
				t.Errorf("sourceRoot = %q, want %q", plan.sourceRoot, tt.wantSource)
			}
			if plan.targetRoot != tt.wantTarget {
				t.Errorf("targetRoot = %q, want %q", plan.targetRoot, tt.wantTarget)
			}
			if plan.renameFolder != tt.wantFolder {
				t.Errorf("renameFolder = %v, want %v", plan.renameFolder, tt.wantFolder)
			}
			if len(plan.fileRenames) != len(tt.wantFiles) {
				t.Fatalf("fileRenames = %+v, want %+v", plan.fileRenames, tt.wantFiles)
			}
			for i := range tt.wantFiles {
				if plan.fileRenames[i] != tt.wantFiles[i] {
					t.Errorf("fileRenames[%d] = %+v, want %+v", i, plan.fileRenames[i], tt.wantFiles[i])
				}
			}
		})
	}
}

// alignFakeManager is a stateful TorrentManager that applies renames to an in-memory file list,
// so verification via GetTorrentFilesBatch reflects the renames the injector issued.
type alignFakeManager struct {
	mu            sync.Mutex
	hash          string
	files         qbt.TorrentFiles
	addOptions    map[string]string
	fileRenames   [][2]string
	folderRenames [][2]string
	// renameLog records file and folder renames in a single call-ordered list ("file"/"folder")
	// so tests can assert files are renamed before the root folder.
	renameLog   []string
	bulkActions []string
	resumeCount int
	// renameErr, when set, makes every rename fail without touching the file list, simulating a
	// qBittorrent that does not support renaming (e.g. WebAPI < 2.7.0).
	renameErr error
	// recheckErr, when set, makes the "recheck" BulkAction fail, simulating an instance that
	// briefly could not schedule the post-alignment recheck.
	recheckErr error
	// filesErr, when set, makes GetTorrentFilesBatch fail, simulating an instance whose file list
	// cannot be read so a rename can never be confirmed.
	filesErr error
	// visibleAfterCalls, when > 0, hides the torrent (empty file list, renames error) until
	// GetTorrentFilesBatch has been called that many times, simulating a slow instance where a
	// just-added torrent takes a while to become visible/renameable.
	visibleAfterCalls int
	filesCalls        int
}

func (m *alignFakeManager) visibleLocked() bool {
	return m.filesCalls >= m.visibleAfterCalls
}

func (m *alignFakeManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addOptions = options
	return nil, nil
}

func (m *alignFakeManager) BulkAction(_ context.Context, _ int, _ []string, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bulkActions = append(m.bulkActions, action)
	if action == "recheck" && m.recheckErr != nil {
		return m.recheckErr
	}
	return nil
}

func (m *alignFakeManager) ResumeWhenComplete(_ int, _ []string, _ qbsync.ResumeWhenCompleteOptions) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.resumeCount++
}

func (m *alignFakeManager) RenameTorrentFile(_ context.Context, _ int, _, oldPath, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.fileRenames = append(m.fileRenames, [2]string{oldPath, newPath})
	m.renameLog = append(m.renameLog, "file")
	if !m.visibleLocked() {
		return errors.New("torrent not found")
	}
	if m.renameErr != nil {
		return m.renameErr
	}
	for i := range m.files {
		if m.files[i].Name == oldPath {
			m.files[i].Name = newPath
		}
	}
	return nil
}

func (m *alignFakeManager) RenameTorrentFolder(_ context.Context, _ int, _, oldPath, newPath string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.folderRenames = append(m.folderRenames, [2]string{oldPath, newPath})
	m.renameLog = append(m.renameLog, "folder")
	if !m.visibleLocked() {
		return errors.New("torrent not found")
	}
	if m.renameErr != nil {
		return m.renameErr
	}
	for i := range m.files {
		name := m.files[i].Name
		switch {
		case name == oldPath:
			m.files[i].Name = newPath
		case strings.HasPrefix(name, oldPath+"/"):
			m.files[i].Name = newPath + "/" + strings.TrimPrefix(name, oldPath+"/")
		}
	}
	return nil
}

func (m *alignFakeManager) GetTorrentFilesBatch(_ context.Context, _ int, _ []string) (map[string]qbt.TorrentFiles, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.filesErr != nil {
		return nil, m.filesErr
	}
	m.filesCalls++
	if !m.visibleLocked() {
		return map[string]qbt.TorrentFiles{}, nil
	}
	cloned := make(qbt.TorrentFiles, len(m.files))
	copy(cloned, m.files)
	return map[string]qbt.TorrentFiles{strings.ToLower(m.hash): cloned}, nil
}

func (m *alignFakeManager) currentNames() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := make([]string, len(m.files))
	for i, f := range m.files {
		names[i] = f.Name
	}
	return names
}

func TestInjector_Inject_AlignsFolderToDiskAndRechecks(t *testing.T) {
	searcheeDir := filepath.Join(t.TempDir(), "Release 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true} // regular mode (no hardlink/reflink)

	manager := &alignFakeManager{
		hash: "deadbeef01",
		files: qbt.TorrentFiles{
			{Name: "Linux.Distribution.Release.01/a.iso", Size: 4},
			{Name: "Linux.Distribution.Release.01/b.iso", Size: 5},
		},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:     "Linux.Distribution.Release.01",
			InfoHash: "deadbeef01",
			Files: []TorrentFile{
				{Path: "Linux.Distribution.Release.01/a.iso", Size: 4},
				{Path: "Linux.Distribution.Release.01/b.iso", Size: 5},
			},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name: "Release 01",
			Path: searcheeDir,
			Files: []*ScannedFile{
				{Path: filepath.Join(searcheeDir, "a.iso"), RelPath: "a.iso", Size: 4},
				{Path: filepath.Join(searcheeDir, "b.iso"), RelPath: "b.iso", Size: 5},
			},
		},
		MatchResult: &MatchResult{
			MatchedFiles: []MatchedFilePair{
				{SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 4}, TorrentFile: TorrentFile{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}},
				{SearcheeFile: &ScannedFile{RelPath: "b.iso", Size: 5}, TorrentFile: TorrentFile{Path: "Linux.Distribution.Release.01/b.iso", Size: 5}},
			},
			IsMatch:        true,
			IsPerfectMatch: true,
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

	// Added force-paused AND skip_checking so qBittorrent does not verify (and thereby block renames
	// on) the pre-rename paths before we align them; the manual recheck after alignment verifies.
	if manager.addOptions["paused"] != "true" || manager.addOptions["stopped"] != "true" {
		t.Errorf("expected paused+stopped add options, got %+v", manager.addOptions)
	}
	if manager.addOptions["skip_checking"] != "true" {
		t.Errorf("expected skip_checking when alignment is needed, got %+v", manager.addOptions)
	}

	// The top-level folder was renamed to the on-disk directory name; filenames already matched.
	if len(manager.folderRenames) != 1 || manager.folderRenames[0] != [2]string{"Linux.Distribution.Release.01", "Release 01"} {
		t.Fatalf("expected folder rename to on-disk name, got %+v", manager.folderRenames)
	}
	if len(manager.fileRenames) != 0 {
		t.Errorf("expected no file renames (filenames already match), got %+v", manager.fileRenames)
	}

	for _, name := range manager.currentNames() {
		if !strings.HasPrefix(name, "Release 01/") {
			t.Errorf("torrent file %q not aligned under on-disk folder", name)
		}
	}

	// After aligning, qBittorrent must recheck the data, then resume (StartPaused=false).
	if len(manager.bulkActions) != 1 || manager.bulkActions[0] != "recheck" {
		t.Fatalf("expected a single recheck, got %+v", manager.bulkActions)
	}
	if manager.resumeCount != 1 {
		t.Errorf("expected ResumeWhenComplete to be queued once, got %d", manager.resumeCount)
	}
}

func TestInjector_Inject_AlignsRootlessFileNameToDisk(t *testing.T) {
	// A rootless torrent dropped into a directory searchee whose file has a different on-disk name.
	searcheeDir := filepath.Join(t.TempDir(), "Season 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	manager := &alignFakeManager{
		hash:  "deadbeef04",
		files: qbt.TorrentFiles{{Name: "ep01.mkv", Size: 6}},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "ep01",
			InfoHash:    "deadbeef04",
			Files:       []TorrentFile{{Path: "ep01.mkv", Size: 6}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Season 01",
			Path:  searcheeDir,
			Files: []*ScannedFile{{Path: filepath.Join(searcheeDir, "Show S01E01.mkv"), RelPath: "Show S01E01.mkv", Size: 6}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "Show S01E01.mkv", Size: 6}, TorrentFile: TorrentFile{Path: "ep01.mkv", Size: 6}}},
			IsMatch:        true,
			IsPerfectMatch: true,
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

	if len(manager.folderRenames) != 0 {
		t.Errorf("expected no folder rename for a rootless torrent, got %+v", manager.folderRenames)
	}
	if len(manager.fileRenames) != 1 || manager.fileRenames[0] != [2]string{"ep01.mkv", "Show S01E01.mkv"} {
		t.Fatalf("expected file rename to on-disk name, got %+v", manager.fileRenames)
	}
	if names := manager.currentNames(); len(names) != 1 || names[0] != "Show S01E01.mkv" {
		t.Errorf("torrent file not aligned to on-disk name, got %+v", names)
	}
	if len(manager.bulkActions) != 1 || manager.bulkActions[0] != "recheck" {
		t.Fatalf("expected a single recheck, got %+v", manager.bulkActions)
	}
	if manager.resumeCount != 1 {
		t.Errorf("expected ResumeWhenComplete once, got %d", manager.resumeCount)
	}
}

func TestInjector_Inject_AlignmentFailure_ReportsFailureAndDoesNotResume(t *testing.T) {
	searcheeDir := filepath.Join(t.TempDir(), "Release 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	// Simulate a qBittorrent that cannot rename (e.g. WebAPI < 2.7.0): every rename errors and the
	// file list never changes, so alignment can never be confirmed.
	manager := &alignFakeManager{
		hash:      "deadbeef03",
		files:     qbt.TorrentFiles{{Name: "Linux.Distribution.Release.01/a.iso", Size: 4}},
		renameErr: errors.New("qBittorrent instance does not support folder renaming"),
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Linux.Distribution.Release.01",
			InfoHash:    "deadbeef03",
			Files:       []TorrentFile{{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Release 01",
			Path:  searcheeDir,
			Files: []*ScannedFile{{Path: filepath.Join(searcheeDir, "a.iso"), RelPath: "a.iso", Size: 4}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 4}, TorrentFile: TorrentFile{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
		StartPaused:  false,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject returned error: %v", err)
	}
	// The torrent was added, but alignment failed, so the injection is reported as a failure.
	if res.Success {
		t.Fatalf("expected Success=false when alignment fails, got %+v", res)
	}
	if res.ErrorMessage == "" {
		t.Errorf("expected an error message describing the alignment failure")
	}
	// It must be left paused for inspection: no recheck and no resume watcher.
	if len(manager.bulkActions) != 0 {
		t.Errorf("expected no recheck when alignment fails, got %+v", manager.bulkActions)
	}
	if manager.resumeCount != 0 {
		t.Errorf("expected no resume watcher when alignment fails, got %d", manager.resumeCount)
	}
}

func TestInjector_Inject_NoAlignmentWhenNamesMatch(t *testing.T) {
	searcheeDir := filepath.Join(t.TempDir(), "Release 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	manager := &alignFakeManager{
		hash:  "deadbeef02",
		files: qbt.TorrentFiles{{Name: "Release 01/a.iso", Size: 4}},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Release 01",
			InfoHash:    "deadbeef02",
			Files:       []TorrentFile{{Path: "Release 01/a.iso", Size: 4}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Release 01",
			Path:  searcheeDir,
			Files: []*ScannedFile{{Path: filepath.Join(searcheeDir, "a.iso"), RelPath: "a.iso", Size: 4}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 4}, TorrentFile: TorrentFile{Path: "Release 01/a.iso", Size: 4}}},
			IsMatch:        true,
			IsPerfectMatch: true,
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

	if len(manager.folderRenames) != 0 || len(manager.fileRenames) != 0 {
		t.Errorf("expected no renames when names match, got folders=%+v files=%+v", manager.folderRenames, manager.fileRenames)
	}
	// Perfect match with matching names keeps the fast path: skip_checking, no forced pause, no recheck.
	if manager.addOptions["skip_checking"] != "true" {
		t.Errorf("expected skip_checking on the fast path, got %+v", manager.addOptions)
	}
	if len(manager.bulkActions) != 0 {
		t.Errorf("expected no recheck on the fast path, got %+v", manager.bulkActions)
	}
}

func TestInjector_Inject_AlignmentRecheckFailure_ReportsFailureAndDoesNotResume(t *testing.T) {
	searcheeDir := filepath.Join(t.TempDir(), "Release 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	// Renames succeed, but the post-alignment recheck can't be scheduled. The torrent was added
	// force-paused for alignment, so a swallowed recheck error would strand it paused while the
	// run reports success. It must be reported as a failure instead.
	manager := &alignFakeManager{
		hash:       "deadbeef05",
		files:      qbt.TorrentFiles{{Name: "Linux.Distribution.Release.01/a.iso", Size: 4}},
		recheckErr: errors.New("instance temporarily unreachable"),
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Linux.Distribution.Release.01",
			InfoHash:    "deadbeef05",
			Files:       []TorrentFile{{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Release 01",
			Path:  searcheeDir,
			Files: []*ScannedFile{{Path: filepath.Join(searcheeDir, "a.iso"), RelPath: "a.iso", Size: 4}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 4}, TorrentFile: TorrentFile{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
		StartPaused:  false,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject returned error: %v", err)
	}
	if res.Success {
		t.Fatalf("expected Success=false when the post-alignment recheck fails, got %+v", res)
	}
	if res.ErrorMessage == "" {
		t.Errorf("expected an error message describing the alignment failure")
	}
	// The renames landed (alignment itself worked)...
	if len(manager.folderRenames) != 1 {
		t.Errorf("expected the folder rename to have been applied, got %+v", manager.folderRenames)
	}
	// ...the recheck was attempted and failed, so no resume watcher must be queued.
	if manager.resumeCount != 0 {
		t.Errorf("expected no resume watcher when the recheck fails, got %d", manager.resumeCount)
	}
}

func TestInjector_Inject_AlignsFilesThenFolderToDisk(t *testing.T) {
	// Both the top-level folder and the file name differ from the on-disk layout, so alignment must
	// rename the file (within the still-nonexistent source root) BEFORE renaming the root folder,
	// then recheck and resume.
	searcheeDir := filepath.Join(t.TempDir(), "Release Folder")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	manager := &alignFakeManager{
		hash:  "deadbeef07",
		files: qbt.TorrentFiles{{Name: "Some.Release/movie.2020.mkv", Size: 8}},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Some.Release",
			InfoHash:    "deadbeef07",
			Files:       []TorrentFile{{Path: "Some.Release/movie.2020.mkv", Size: 8}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Release Folder",
			Path:  searcheeDir,
			Files: []*ScannedFile{{Path: filepath.Join(searcheeDir, "Movie (2020).mkv"), RelPath: "Movie (2020).mkv", Size: 8}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "Movie (2020).mkv", Size: 8}, TorrentFile: TorrentFile{Path: "Some.Release/movie.2020.mkv", Size: 8}}},
			IsMatch:        true,
			IsPerfectMatch: true,
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

	// The file is renamed within the source root first, then the root folder is renamed onto the
	// on-disk directory name.
	if len(manager.fileRenames) != 1 || manager.fileRenames[0] != [2]string{"Some.Release/movie.2020.mkv", "Some.Release/Movie (2020).mkv"} {
		t.Fatalf("expected one file rename inside the source root, got %+v", manager.fileRenames)
	}
	if len(manager.folderRenames) != 1 || manager.folderRenames[0] != [2]string{"Some.Release", "Release Folder"} {
		t.Fatalf("expected the folder rename to the on-disk name, got %+v", manager.folderRenames)
	}
	// Files must be renamed before the folder, otherwise the folder rename would land the file on a
	// path that already exists on disk.
	if want := []string{"file", "folder"}; !slices.Equal(manager.renameLog, want) {
		t.Fatalf("expected renames in order %v, got %v", want, manager.renameLog)
	}
	// Every torrent path now lands under the on-disk folder with the on-disk file name.
	if names := manager.currentNames(); len(names) != 1 || names[0] != "Release Folder/Movie (2020).mkv" {
		t.Errorf("torrent file not aligned to on-disk layout, got %+v", names)
	}
	if len(manager.bulkActions) != 1 || manager.bulkActions[0] != "recheck" {
		t.Fatalf("expected a single recheck, got %+v", manager.bulkActions)
	}
	if manager.resumeCount != 1 {
		t.Errorf("expected ResumeWhenComplete once, got %d", manager.resumeCount)
	}
}

func TestInjector_Inject_StripsRootForLooseFileAndRenames(t *testing.T) {
	// A foldered torrent matched to a loose on-disk file: added with contentLayout=NoSubfolder so
	// qBittorrent strips the root, then the (root-stripped) file is renamed to the on-disk name.
	looseFile := filepath.Join(t.TempDir(), "Movie (2020).mkv")
	if err := os.WriteFile(looseFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write loose file: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	manager := &alignFakeManager{
		hash: "deadbeef09",
		// qBittorrent stores the root-stripped path after a NoSubfolder add.
		files: qbt.TorrentFiles{{Name: "movie.mkv", Size: 7}},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Some.Folder",
			InfoHash:    "deadbeef09",
			Files:       []TorrentFile{{Path: "Some.Folder/movie.mkv", Size: 7}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Movie (2020)",
			Path:  looseFile,
			Files: []*ScannedFile{{Path: looseFile, RelPath: "Movie (2020).mkv", Size: 7}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "Movie (2020).mkv", Size: 7}, TorrentFile: TorrentFile{Path: "Some.Folder/movie.mkv", Size: 7}}},
			IsMatch:        true,
			IsPerfectMatch: true,
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

	if manager.addOptions["contentLayout"] != "NoSubfolder" {
		t.Errorf("expected contentLayout=NoSubfolder, got %+v", manager.addOptions)
	}
	if manager.addOptions["paused"] != "true" || manager.addOptions["stopped"] != "true" {
		t.Errorf("expected paused+stopped add options, got %+v", manager.addOptions)
	}
	if len(manager.folderRenames) != 0 {
		t.Errorf("expected no folder rename when the root is stripped, got %+v", manager.folderRenames)
	}
	if len(manager.fileRenames) != 1 || manager.fileRenames[0] != [2]string{"movie.mkv", "Movie (2020).mkv"} {
		t.Fatalf("expected root-stripped file rename to on-disk name, got %+v", manager.fileRenames)
	}
	if names := manager.currentNames(); len(names) != 1 || names[0] != "Movie (2020).mkv" {
		t.Errorf("torrent file not aligned to on-disk name, got %+v", names)
	}
	if len(manager.bulkActions) != 1 || manager.bulkActions[0] != "recheck" {
		t.Fatalf("expected a single recheck, got %+v", manager.bulkActions)
	}
	if manager.resumeCount != 1 {
		t.Errorf("expected ResumeWhenComplete once, got %d", manager.resumeCount)
	}
}

func TestInjector_Inject_StripsRootForLooseFile_NoRenameNeeded(t *testing.T) {
	// Same shape but the file name already matches disk: the NoSubfolder add alone aligns the
	// torrent, so it keeps the fast path (no forced pause, no renames, no recheck).
	looseFile := filepath.Join(t.TempDir(), "Movie.mkv")
	if err := os.WriteFile(looseFile, []byte("x"), 0o600); err != nil {
		t.Fatalf("write loose file: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	manager := &alignFakeManager{
		hash:  "deadbeef10",
		files: qbt.TorrentFiles{{Name: "Movie.mkv", Size: 7}},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Some.Folder",
			InfoHash:    "deadbeef10",
			Files:       []TorrentFile{{Path: "Some.Folder/Movie.mkv", Size: 7}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Movie",
			Path:  looseFile,
			Files: []*ScannedFile{{Path: looseFile, RelPath: "Movie.mkv", Size: 7}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "Movie.mkv", Size: 7}, TorrentFile: TorrentFile{Path: "Some.Folder/Movie.mkv", Size: 7}}},
			IsMatch:        true,
			IsPerfectMatch: true,
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

	if manager.addOptions["contentLayout"] != "NoSubfolder" {
		t.Errorf("expected contentLayout=NoSubfolder, got %+v", manager.addOptions)
	}
	if manager.addOptions["paused"] == "true" {
		t.Errorf("expected no forced pause when the strip alone aligns, got %+v", manager.addOptions)
	}
	if len(manager.fileRenames) != 0 || len(manager.folderRenames) != 0 {
		t.Errorf("expected no renames, got files=%+v folders=%+v", manager.fileRenames, manager.folderRenames)
	}
	if len(manager.bulkActions) != 0 {
		t.Errorf("expected no recheck on the fast path, got %+v", manager.bulkActions)
	}
}

func TestInjector_waitForTorrentFiles_NeverVisibleFails(t *testing.T) {
	manager := &alignFakeManager{
		hash:              "deadbeef11",
		files:             qbt.TorrentFiles{{Name: "a.iso", Size: 4}},
		visibleAfterCalls: 1 << 30, // never becomes visible
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: &models.Instance{ID: 1}}, nil, testBackendPool(&models.Instance{ID: 1}))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if injector.waitForTorrentFiles(ctx, 1, "deadbeef11") {
		t.Fatal("expected waitForTorrentFiles to fail for a torrent that never appears")
	}
}

func TestInjector_Inject_WaitsForTorrentVisibilityBeforeRenaming(t *testing.T) {
	// The just-added torrent is not visible in qBittorrent for the first few polls (slow
	// instance); renames error until it is. Alignment must wait for visibility instead of burning
	// its rename attempts on a torrent that is not registered yet.
	searcheeDir := filepath.Join(t.TempDir(), "Release 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test", HasLocalFilesystemAccess: true}

	manager := &alignFakeManager{
		hash:              "deadbeef12",
		files:             qbt.TorrentFiles{{Name: "Linux.Distribution.Release.01/a.iso", Size: 4}},
		visibleAfterCalls: 5, // more polls than renameTorrentPath alone would attempt
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil, testBackendPool(instance))

	req := &InjectRequest{
		InstanceID:   1,
		TorrentBytes: []byte("x"),
		ParsedTorrent: &ParsedTorrent{
			Name:        "Linux.Distribution.Release.01",
			InfoHash:    "deadbeef12",
			Files:       []TorrentFile{{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}},
			PieceLength: 16384,
		},
		Searchee: &Searchee{
			Name:  "Release 01",
			Path:  searcheeDir,
			Files: []*ScannedFile{{Path: filepath.Join(searcheeDir, "a.iso"), RelPath: "a.iso", Size: 4}},
		},
		MatchResult: &MatchResult{
			MatchedFiles:   []MatchedFilePair{{SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 4}, TorrentFile: TorrentFile{Path: "Linux.Distribution.Release.01/a.iso", Size: 4}}},
			IsMatch:        true,
			IsPerfectMatch: true,
		},
		SearchResult: &jackett.SearchResult{Indexer: "Test"},
		StartPaused:  false,
	}

	res, err := injector.Inject(context.Background(), req)
	if err != nil {
		t.Fatalf("inject: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success once the torrent becomes visible, got %+v", res)
	}
	if len(manager.folderRenames) != 1 || manager.folderRenames[0] != [2]string{"Linux.Distribution.Release.01", "Release 01"} {
		t.Fatalf("expected folder rename after the torrent became visible, got %+v", manager.folderRenames)
	}
	if len(manager.bulkActions) != 1 || manager.bulkActions[0] != "recheck" {
		t.Fatalf("expected a single recheck, got %+v", manager.bulkActions)
	}
}

func TestInjector_renameTorrentPath_UnconfirmedRenameFails(t *testing.T) {
	// The rename API call "succeeds" but the file list can never be read, so the rename cannot be
	// confirmed. An unconfirmed rename must return false (torrent left paused), never a best-effort
	// success. A short context keeps the retry/verify budget from stalling the test.
	manager := &alignFakeManager{
		hash:     "deadbeef08",
		files:    qbt.TorrentFiles{{Name: "Some.Release/a.mkv", Size: 4}},
		filesErr: errors.New("files temporarily unavailable"),
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: &models.Instance{ID: 1}}, nil, testBackendPool(&models.Instance{ID: 1}))

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	if injector.renameTorrentPath(ctx, 1, "deadbeef08", "Some.Release", "Release Folder", true) {
		t.Fatal("expected renameTorrentPath to fail when the rename cannot be verified")
	}
}
