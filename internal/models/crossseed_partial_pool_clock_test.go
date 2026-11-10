// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestPartialPoolAdmissionTimeRepeatedTick(t *testing.T) {
	for _, now := range []time.Time{
		time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		time.Date(2026, 1, 1, 0, 0, 0, 999999000, time.UTC),
	} {
		t.Run(now.Format(time.RFC3339Nano), func(t *testing.T) {
			previous := now
			for range 3 {
				next := nextPartialPoolAdmissionTime(now, previous)
				require.True(t, next.After(previous), "a repeated tick must not reuse an older admission, including across a second boundary")
				require.Equal(t, next, next.Truncate(time.Microsecond))
				previous = next
			}
			require.Equal(t, now, nextPartialPoolAdmissionTime(now, now.Add(time.Second)), "large clock rollback must preserve scheduling age")
			require.Equal(t, now, nextPartialPoolAdmissionTime(now, now.Add(-time.Second)))
		})
	}
}
