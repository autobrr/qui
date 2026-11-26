package hardlinktree

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCreate_SharedDestRollbackKeepsSiblingFiles(t *testing.T) {
	tmp := t.TempDir()

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

	plan1 := &TreePlan{RootDir: destRoot, Files: files}
	plan2 := &TreePlan{RootDir: destRoot, Files: files}

	if _, err := Create(plan1); err != nil {
		t.Fatalf("attempt 1 Create: %v", err)
	}

	created2, err := Create(plan2)
	if err != nil {
		t.Fatalf("attempt 2 Create: %v", err)
	}
	if err := created2.Rollback(); err != nil {
		t.Fatalf("Rollback: %v", err)
	}

	for _, fp := range plan1.Files {
		if _, err := os.Stat(fp.TargetPath); err != nil {
			t.Errorf("attempt 1's file gone after sibling rollback: %v", err)
		}
	}
}
