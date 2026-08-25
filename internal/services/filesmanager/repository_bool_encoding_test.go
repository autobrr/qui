// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package filesmanager

import (
	"database/sql"
	"testing"
)

func TestEncodeNullableBoolAsInt(t *testing.T) {
	t.Parallel()

	if got := encodeNullableBoolAsInt(nil); got != nil {
		t.Fatalf("expected nil, got %v", got)
	}

	trueValue := true
	if got := encodeNullableBoolAsInt(&trueValue); got != 1 {
		t.Fatalf("expected 1 for true, got %v", got)
	}

	falseValue := false
	if got := encodeNullableBoolAsInt(&falseValue); got != 0 {
		t.Fatalf("expected 0 for false, got %v", got)
	}
}

func TestDecodeNullableBoolFromInt(t *testing.T) {
	t.Parallel()

	if got := decodeNullableBoolFromInt(sql.NullInt64{}); got != nil {
		t.Fatalf("expected nil for invalid value, got %v", *got)
	}

	if got := decodeNullableBoolFromInt(sql.NullInt64{Int64: 1, Valid: true}); got == nil || !*got {
		t.Fatalf("expected true for Int64=1, got %v", got)
	}

	if got := decodeNullableBoolFromInt(sql.NullInt64{Int64: 0, Valid: true}); got == nil || *got {
		t.Fatalf("expected false for Int64=0, got %v", got)
	}
}
