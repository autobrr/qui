package hardlinktree

import (
	"os"
	"path/filepath"
	"testing"
)

// Regression test for discussion #2282: two concurrent same-release plans share
// target paths (common-root multi-file torrent, by-tracker preset); the second
// attempt's add fails and its rollback must not delete the first attempt's links.
func TestCreate_SharedDestRollbackKeepsSiblingFiles(t *testing.T) {
	tmp := t.TempDir()

	// Source library files (same inodes for both attempts, as when dirscan
	// matches two local copies backed by the same content files).
	srcDir := filepath.Join(tmp, "library", "Pack.S01")
	if err := os.MkdirAll(srcDir, 0o755); err != nil {
		t.Fatal(err)
	}
	files := make([]FilePlan, 0, 2)
	destRoot := filepath.Join(tmp, "linkDir", "Tracker")
	for _, name := range []string{"e01.mkv", "e02.mkv"} {
		src := filepath.Join(srcDir, name)
		if err := os.WriteFile(src, []byte("data-"+name), 0o600); err != nil {
			t.Fatal(err)
		}
		files = append(files, FilePlan{
			SourcePath: src,
			TargetPath: filepath.Join(destRoot, "Pack.S01", name),
		})
	}

	// Both attempts compute the identical plan (common root folder, so no
	// per-hash isolation folder on by-tracker preset).
	plan1 := &TreePlan{RootDir: destRoot, Files: files}
	plan2 := &TreePlan{RootDir: destRoot, Files: files}

	// Attempt 1: creates the tree, torrent adds successfully.
	if _, err := Create(plan1); err != nil {
		t.Fatalf("attempt 1 Create: %v", err)
	}

	// Attempt 2: "creates" the tree (idempotent same-inode skips)...
	created2, err := Create(plan2)
	if err != nil {
		t.Fatalf("attempt 2 Create: %v", err)
	}
	// ...then qBittorrent rejects the duplicate add, and the service rolls
	// back attempt 2's handle.
	if err := created2.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	// Attempt 1's already-added torrent must still have its files.
	for _, fp := range plan1.Files {
		if _, err := os.Stat(fp.TargetPath); err != nil {
			t.Errorf("attempt 1's file gone after sibling rollback: %v", err)
		}
	}
}
