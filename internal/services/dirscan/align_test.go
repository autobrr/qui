// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	qbt "github.com/autobrr/go-qbittorrent"

	"github.com/autobrr/qui/internal/models"
	qbsync "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
)

func TestBuildAlignmentPlan(t *testing.T) {
	tests := []struct {
		name         string
		torrentFiles []TorrentFile
		searcheePath string
		matched      []MatchedFilePair
		wantNeeded   bool
		wantSource   string
		wantTarget   string
		wantFolder   bool
		wantFiles    []fileRename
	}{
		{
			name:         "folder mismatch, identical filenames",
			torrentFiles: []TorrentFile{{Path: "Linux.Distribution.Release.01/a.iso", Size: 1}},
			searcheePath: "/data/isos/Release 01",
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
			name:         "folder and filename mismatch",
			torrentFiles: []TorrentFile{{Path: "Some.Release/movie.2020.mkv", Size: 2}},
			searcheePath: "/media/Release Folder",
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
			name:         "nested subdir, folder mismatch only",
			torrentFiles: []TorrentFile{{Path: "Root/Sub/a.mkv", Size: 3}},
			searcheePath: "/x/Target",
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
			name:         "identical folder and filenames, no alignment",
			torrentFiles: []TorrentFile{{Path: "Release 01/a.iso", Size: 1}},
			searcheePath: "/data/isos/Release 01",
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 1},
				TorrentFile:  TorrentFile{Path: "Release 01/a.iso", Size: 1},
			}},
			wantNeeded: false,
		},
		{
			name:         "rootless torrent, no alignment",
			torrentFiles: []TorrentFile{{Path: "a.iso", Size: 1}},
			searcheePath: "/data/isos/whatever",
			matched: []MatchedFilePair{{
				SearcheeFile: &ScannedFile{RelPath: "a.iso", Size: 1},
				TorrentFile:  TorrentFile{Path: "a.iso", Size: 1},
			}},
			wantNeeded: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := &InjectRequest{
				ParsedTorrent: &ParsedTorrent{Files: tt.torrentFiles},
				Searchee:      &Searchee{Path: tt.searcheePath},
				MatchResult:   &MatchResult{MatchedFiles: tt.matched},
			}
			plan := buildAlignmentPlan(req)

			if plan.needed() != tt.wantNeeded {
				t.Fatalf("needed() = %v, want %v (plan=%+v)", plan.needed(), tt.wantNeeded, plan)
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

// alignFakeManager is a stateful TorrentAdder that applies renames to an in-memory file list,
// so verification via GetTorrentFilesBatch reflects the renames the injector issued.
type alignFakeManager struct {
	mu            sync.Mutex
	hash          string
	files         qbt.TorrentFiles
	addOptions    map[string]string
	fileRenames   [][2]string
	folderRenames [][2]string
	bulkActions   []string
	resumeCount   int
	// renameErr, when set, makes every rename fail without touching the file list, simulating a
	// qBittorrent that does not support renaming (e.g. WebAPI < 2.7.0).
	renameErr error
}

func (m *alignFakeManager) AddTorrent(_ context.Context, _ int, _ []byte, options map[string]string) (*qbt.TorrentAddResponse, error) {
	m.addOptions = options
	return nil, nil
}

func (m *alignFakeManager) BulkAction(_ context.Context, _ int, _ []string, action string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bulkActions = append(m.bulkActions, action)
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

	instance := &models.Instance{ID: 1, Name: "test"} // regular mode (no hardlink/reflink)

	manager := &alignFakeManager{
		hash: "deadbeef01",
		files: qbt.TorrentFiles{
			{Name: "Linux.Distribution.Release.01/a.iso", Size: 4},
			{Name: "Linux.Distribution.Release.01/b.iso", Size: 5},
		},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

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

	// Added force-paused so qBittorrent does not act on the pre-rename paths, and not skip_checking.
	if manager.addOptions["paused"] != "true" || manager.addOptions["stopped"] != "true" {
		t.Errorf("expected paused+stopped add options, got %+v", manager.addOptions)
	}
	if _, ok := manager.addOptions["skip_checking"]; ok {
		t.Errorf("did not expect skip_checking when alignment is needed, got %+v", manager.addOptions)
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

func TestInjector_Inject_AlignmentFailure_ReportsFailureAndDoesNotResume(t *testing.T) {
	searcheeDir := filepath.Join(t.TempDir(), "Release 01")
	if err := os.MkdirAll(searcheeDir, 0o755); err != nil {
		t.Fatalf("mkdir searchee: %v", err)
	}

	instance := &models.Instance{ID: 1, Name: "test"}

	// Simulate a qBittorrent that cannot rename (e.g. WebAPI < 2.7.0): every rename errors and the
	// file list never changes, so alignment can never be confirmed.
	manager := &alignFakeManager{
		hash:      "deadbeef03",
		files:     qbt.TorrentFiles{{Name: "Linux.Distribution.Release.01/a.iso", Size: 4}},
		renameErr: errors.New("qBittorrent instance does not support folder renaming"),
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

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

	instance := &models.Instance{ID: 1, Name: "test"}

	manager := &alignFakeManager{
		hash:  "deadbeef02",
		files: qbt.TorrentFiles{{Name: "Release 01/a.iso", Size: 4}},
	}
	injector := NewInjector(nil, manager, nil, &fakeInstanceStore{instance: instance}, nil)

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
