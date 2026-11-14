// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package dbinterface

import (
	"testing"
	"time"
)

func TestNullTimeScan(t *testing.T) {
	t.Parallel()

	instant := time.Date(2026, 11, 9, 11, 41, 31, 947225000, time.UTC)
	tests := []struct {
		name      string
		src       any
		want      time.Time
		wantValid bool
		wantErr   bool
	}{
		{name: "nil", src: nil},
		{name: "time", src: instant, want: instant, wantValid: true},
		{name: "sqlite form", src: "2026-11-09 11:41:31.947225+00:00", want: instant, wantValid: true},
		{name: "database clock form", src: "2026-11-09 11:41:31", want: time.Date(2026, 11, 9, 11, 41, 31, 0, time.UTC), wantValid: true},
		{name: "unknown string", src: "2026-11-09 11:41:31.947225 +0000 UTC", wantErr: true},
		{name: "unknown source type", src: int64(1794224491), wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var got NullTime
			err := got.Scan(tt.src)
			if (err != nil) != tt.wantErr {
				t.Fatalf("Scan(%v) error = %v, wantErr %v", tt.src, err, tt.wantErr)
			}
			if got.Valid != tt.wantValid {
				t.Fatalf("Scan(%v) Valid = %v, want %v", tt.src, got.Valid, tt.wantValid)
			}
			if !got.Time.Equal(tt.want) {
				t.Fatalf("Scan(%v) Time = %s, want %s", tt.src, got.Time, tt.want)
			}
			if got.Valid && got.Time.Location() != time.UTC {
				t.Fatalf("Scan(%v) Location = %s, want UTC", tt.src, got.Time.Location())
			}
		})
	}
}
