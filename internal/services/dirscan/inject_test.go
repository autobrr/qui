// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dirscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/autobrr/qui/internal/models"
	qbsync "github.com/autobrr/qui/internal/qbittorrent"
	"github.com/autobrr/qui/internal/services/jackett"
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
	if err := os.WriteFile(sourceFile, []byte("data"), 0o644); err != nil {
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
