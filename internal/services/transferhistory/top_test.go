// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

// The limit comes from the request, so a limit far above the number of groups
// must not size what top allocates.
// regression: #4113
func TestALimitAboveTheGroupsAllocatesForTheGroupsOnly(t *testing.T) {
	rows := []TransferRow{{Group: "a", Uploaded: 2}, {Group: "b", Uploaded: 1}}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	kept := top(rows, 1<<24)
	runtime.ReadMemStats(&after)

	require.Equal(t, []string{"a", "b"}, []string{kept[0].Group, kept[1].Group})
	require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(1<<20))
}
