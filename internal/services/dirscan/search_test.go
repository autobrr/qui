package dirscan

import "testing"

func TestCalculateSizeRange_ZeroTolerance_IsExact(t *testing.T) {
	minSize, maxSize := CalculateSizeRange(1234, 0)
	if minSize != 1234 || maxSize != 1234 {
		t.Fatalf("expected exact range (1234,1234), got (%d,%d)", minSize, maxSize)
	}
}

func TestCalculateSizeRange_NonPositiveSize_IsZero(t *testing.T) {
	minSize, maxSize := CalculateSizeRange(0, 5)
	if minSize != 0 || maxSize != 0 {
		t.Fatalf("expected (0,0), got (%d,%d)", minSize, maxSize)
	}
}
