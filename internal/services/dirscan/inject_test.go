package dirscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/autobrr/qui/internal/models"
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
