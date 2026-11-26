// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sharedextents

import (
	"encoding/binary"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
)

type rawExtent struct {
	nextVCN int64
	lcn     int64
}

func TestClusterRangesIntersect(t *testing.T) {
	tests := []struct {
		name  string
		left  []clusterRange
		right []clusterRange
		want  bool
	}{
		{
			name:  "identical LCN run",
			left:  []clusterRange{{start: 100, end: 108}},
			right: []clusterRange{{start: 100, end: 108}},
			want:  true,
		},
		{
			name:  "partial interval overlap",
			left:  []clusterRange{{start: 100, end: 108}},
			right: []clusterRange{{start: 107, end: 110}},
			want:  true,
		},
		{
			name:  "adjacent ranges do not overlap",
			left:  []clusterRange{{start: 100, end: 108}},
			right: []clusterRange{{start: 108, end: 110}},
			want:  false,
		},
		{
			name:  "fragmented unsorted maps",
			left:  []clusterRange{{start: 300, end: 305}, {start: 100, end: 105}},
			right: []clusterRange{{start: 500, end: 505}, {start: 302, end: 304}},
			want:  true,
		},
		{
			name:  "no overlap",
			left:  []clusterRange{{start: 100, end: 108}},
			right: []clusterRange{{start: 200, end: 208}},
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, clusterRangesIntersect(tt.left, tt.right))
		})
	}
}

func TestParseRetrievalPointers(t *testing.T) {
	t.Run("same LCN independent of VCN", func(t *testing.T) {
		left, _, err := parseRetrievalPointers(retrievalPointersBuffer(0, rawExtent{nextVCN: 4, lcn: 1000}))
		require.NoError(t, err)
		right, _, err := parseRetrievalPointers(retrievalPointersBuffer(20, rawExtent{nextVCN: 24, lcn: 1000}))
		require.NoError(t, err)
		require.True(t, clusterRangesIntersect(left, right))
	})

	t.Run("sparse entries ignored", func(t *testing.T) {
		ranges, nextVCN, err := parseRetrievalPointers(retrievalPointersBuffer(
			0,
			rawExtent{nextVCN: 2, lcn: -1},
			rawExtent{nextVCN: 5, lcn: 100},
		))
		require.NoError(t, err)
		require.Equal(t, int64(5), nextVCN)
		require.Equal(t, []clusterRange{{start: 100, end: 103}}, ranges)
	})

	t.Run("truncated header", func(t *testing.T) {
		_, _, err := parseRetrievalPointers(make([]byte, retrievalPointersHeaderSize-1))
		require.ErrorContains(t, err, "too short")
	})

	t.Run("extent count exceeds output", func(t *testing.T) {
		data := retrievalPointersBuffer(0, rawExtent{nextVCN: 1, lcn: 10})
		binary.LittleEndian.PutUint32(data[:4], 2)
		_, _, err := parseRetrievalPointers(data)
		require.ErrorContains(t, err, "exceeds returned buffer")
	})

	t.Run("non-progressing VCN", func(t *testing.T) {
		_, _, err := parseRetrievalPointers(retrievalPointersBuffer(5, rawExtent{nextVCN: 5, lcn: 10}))
		require.ErrorContains(t, err, "did not progress")
	})

	t.Run("arithmetic overflow", func(t *testing.T) {
		_, _, err := parseRetrievalPointers(retrievalPointersBuffer(
			0,
			rawExtent{nextVCN: 2, lcn: math.MaxInt64 - 1},
		))
		require.ErrorContains(t, err, "overflows")
	})
}

func TestCollectAllocationRanges(t *testing.T) {
	t.Run("multi-buffer continuation", func(t *testing.T) {
		var starts []int64
		ranges, err := collectAllocationRanges(func(startVCN int64) ([]byte, bool, error) {
			starts = append(starts, startVCN)
			if startVCN == 0 {
				return retrievalPointersBuffer(0, rawExtent{nextVCN: 2, lcn: 100}), true, nil
			}
			return retrievalPointersBuffer(2, rawExtent{nextVCN: 5, lcn: 200}), false, nil
		})
		require.NoError(t, err)
		require.Equal(t, []int64{0, 2}, starts)
		require.Equal(t, []clusterRange{{start: 100, end: 102}, {start: 200, end: 203}}, ranges)
	})

	t.Run("non-progressing continuation", func(t *testing.T) {
		_, err := collectAllocationRanges(func(int64) ([]byte, bool, error) {
			return retrievalPointersBuffer(0), true, nil
		})
		require.ErrorContains(t, err, "did not progress")
	})

	t.Run("empty file", func(t *testing.T) {
		ranges, err := collectAllocationRanges(func(int64) ([]byte, bool, error) {
			return nil, false, nil
		})
		require.NoError(t, err)
		require.Empty(t, ranges)
	})
}

func retrievalPointersBuffer(startingVCN int64, extents ...rawExtent) []byte {
	data := make([]byte, retrievalPointersHeaderSize+len(extents)*retrievalPointerExtentSize)
	binary.LittleEndian.PutUint32(data[:4], uint32(len(extents)))
	binary.LittleEndian.PutUint64(data[8:16], uint64(startingVCN))
	for i, extent := range extents {
		offset := retrievalPointersHeaderSize + i*retrievalPointerExtentSize
		binary.LittleEndian.PutUint64(data[offset:offset+8], uint64(extent.nextVCN))
		binary.LittleEndian.PutUint64(data[offset+8:offset+16], uint64(extent.lcn))
	}
	return data
}
