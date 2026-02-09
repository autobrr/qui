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

func TestFilesConflict_IgnoresFolderAndFormatting(t *testing.T) {
	local := map[string]int64{
		"CD1/01 - Track_Name.FLAC":  100,
		"CD1/02 - Other.Track.flac": 200,
	}
	remote := map[string]int64{
		"01-Track Name.flac":  100,
		"02 Other Track.flac": 200,
	}
	if filesConflict(local, remote) {
		t.Fatalf("expected no conflict when base names match after normalization")
	}
}

// Intentionally allow folder differences; cross-seeding can still work if the files match.
