// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"
)

// regression: #4242
// A copy that stalls right after several writes in one second shows its
// speed while those bytes are in the speed window, and no speed once nothing
// moved over the whole window.
func TestTheSpeedOfACopyThatStallsAfterQuickWritesDropsToZero(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		m := &meter{now: time.Now}
		m.set(0, 1000)
		time.Sleep(100 * time.Millisecond)
		m.add(60)
		m.add(40)

		time.Sleep(speedWindow - 200*time.Millisecond)
		require.Positive(t, m.progress().Speed, "the bytes moved inside the window")

		time.Sleep(200 * time.Millisecond)
		p := m.progress()
		require.Equal(t, int64(100), p.Done)
		require.Zero(t, p.Speed, "nothing moved over the window")
	})
}
