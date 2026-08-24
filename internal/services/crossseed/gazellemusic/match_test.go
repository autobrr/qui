package gazellemusic

import "testing"

func TestFilesConflict_SizeOnlyNotEnough(t *testing.T) {
	local := map[string]int64{
		"01 - Alpha.flac": 100,
		"02 - Beta.flac":  200,
	}
	remote := map[string]int64{
		"a.flac": 100,
		"b.flac": 200,
	}
	if !filesConflict(local, remote) {
		t.Fatalf("expected conflict when names differ but sizes match")
	}
}

func TestFilesConflict_IgnoresRootFolderAndFormatting(t *testing.T) {
	local := map[string]int64{
		"Some Album/01 - Track_Name.FLAC":  100,
		"Some Album/02 - Other.Track.flac": 200,
	}
	remote := map[string]int64{
		"01-Track Name.flac":  100,
		"02 Other Track.flac": 200,
	}
	if filesConflict(local, remote) {
		t.Fatalf("expected no conflict when names match after normalization and root folder is ignored")
	}
}

func TestFilesConflict_PreservesSubdirectories(t *testing.T) {
	local := map[string]int64{
		"CD1/01 - Alpha.flac": 100,
		"CD2/02 - Beta.flac":  200,
	}
	remote := map[string]int64{
		"CD2/01 - Alpha.flac": 100,
		"CD1/02 - Beta.flac":  200,
	}
	if !filesConflict(local, remote) {
		t.Fatalf("expected conflict when subdirectories differ")
	}
}
