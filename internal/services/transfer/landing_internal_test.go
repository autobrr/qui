// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transfer

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/fsops"
)

func TestTransfersIntoOneFolderLandInTheOrderTheyStarted(t *testing.T) {
	s := &Service{landings: map[int64]landing{}, left: make(chan struct{})}
	show := func(sourceID int) landing {
		return landing{sourceID: sourceID, targetID: 9, paths: fsops.SlashPaths, tops: map[string]struct{}{"/data/Show.S01": {}}}
	}
	const second, first, otherSource = 1, 2, 3
	s.landings[otherSource] = landing{order: 1, sourceID: 2, targetID: 9}
	s.landings[first] = landing{order: 2, sourceID: 1, targetID: 9}
	s.landings[second] = landing{order: 3, sourceID: 1, targetID: 9}

	inTheWay, left := s.land(second, show(1))
	require.NotNil(t, left, "the second Transfer lands while the first one still reads its paths")
	require.Zero(t, inTheWay, "no Transfer is known to write at the path")

	_, left = s.land(first, show(1))
	require.Nil(t, left, "the first Transfer waits for the second one, or for a Transfer of another source that still reads its paths")

	inTheWay, _ = s.land(otherSource, show(2))
	require.EqualValues(t, first, inTheWay, "a Transfer of another source lands at the path that the first one holds")
	_, left = s.land(second, show(1))
	require.NotNil(t, left)

	s.leave(first)
	<-left
	_, left = s.land(otherSource, show(2))
	require.Nil(t, left, "the Transfer that started first still waits after the path is free")
	inTheWay, _ = s.land(second, show(1))
	require.EqualValues(t, otherSource, inTheWay)
}
