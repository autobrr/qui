// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package sharedextents reports whether two known files currently share
// allocated storage.
package sharedextents

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
)

// ErrUnsupported indicates that shared-extent detection is unavailable on the
// current platform or filesystem.
var ErrUnsupported = errors.New("shared extent detection unsupported")

const (
	retrievalPointersHeaderSize = 16
	retrievalPointerExtentSize  = 16
)

type clusterRange struct {
	start int64
	end   int64
}

type retrievalPointerQuery func(startVCN int64) (data []byte, more bool, err error)

func collectAllocationRanges(query retrievalPointerQuery) ([]clusterRange, error) {
	var ranges []clusterRange
	var startVCN int64

	for {
		data, more, err := query(startVCN)
		if err != nil {
			return nil, err
		}
		if len(data) == 0 {
			if more {
				return nil, errors.New("retrieval pointers continuation returned no data")
			}
			return mergeClusterRanges(ranges), nil
		}

		chunk, nextVCN, err := parseRetrievalPointers(data)
		if err != nil {
			return nil, err
		}
		ranges = append(ranges, chunk...)

		if !more {
			return mergeClusterRanges(ranges), nil
		}
		if nextVCN <= startVCN {
			return nil, fmt.Errorf("retrieval pointers continuation did not progress: %d to %d", startVCN, nextVCN)
		}
		startVCN = nextVCN
	}
}

func parseRetrievalPointers(data []byte) ([]clusterRange, int64, error) {
	if len(data) < retrievalPointersHeaderSize {
		return nil, 0, fmt.Errorf("retrieval pointers output too short: %d", len(data))
	}

	extentCount := binary.LittleEndian.Uint32(data[:4])
	maxExtents := (len(data) - retrievalPointersHeaderSize) / retrievalPointerExtentSize
	if uint64(extentCount) > uint64(maxExtents) {
		return nil, 0, fmt.Errorf("retrieval pointers extent count %d exceeds returned buffer", extentCount)
	}

	currentVCN := int64(binary.LittleEndian.Uint64(data[8:16]))
	if currentVCN < 0 {
		return nil, 0, fmt.Errorf("retrieval pointers returned negative starting VCN: %d", currentVCN)
	}

	ranges := make([]clusterRange, 0, extentCount)
	for i := range extentCount {
		offset := retrievalPointersHeaderSize + int(i)*retrievalPointerExtentSize
		nextVCN := int64(binary.LittleEndian.Uint64(data[offset : offset+8]))
		lcn := int64(binary.LittleEndian.Uint64(data[offset+8 : offset+16]))

		if nextVCN <= currentVCN {
			return nil, 0, fmt.Errorf("retrieval pointers VCN did not progress: %d to %d", currentVCN, nextVCN)
		}

		length := nextVCN - currentVCN
		if lcn >= 0 {
			if lcn > math.MaxInt64-length {
				return nil, 0, fmt.Errorf("retrieval pointers LCN range overflows: %d + %d", lcn, length)
			}
			ranges = append(ranges, clusterRange{start: lcn, end: lcn + length})
		}
		currentVCN = nextVCN
	}

	return ranges, currentVCN, nil
}

func mergeClusterRanges(ranges []clusterRange) []clusterRange {
	if len(ranges) < 2 {
		return ranges
	}

	slices.SortFunc(ranges, func(a, b clusterRange) int {
		switch {
		case a.start < b.start:
			return -1
		case a.start > b.start:
			return 1
		case a.end < b.end:
			return -1
		case a.end > b.end:
			return 1
		default:
			return 0
		}
	})

	merged := ranges[:1]
	for _, current := range ranges[1:] {
		last := &merged[len(merged)-1]
		if current.start <= last.end {
			last.end = max(last.end, current.end)
			continue
		}
		merged = append(merged, current)
	}
	return merged
}

func clusterRangesIntersect(left, right []clusterRange) bool {
	left = mergeClusterRanges(left)
	right = mergeClusterRanges(right)

	for i, j := 0, 0; i < len(left) && j < len(right); {
		a := left[i]
		b := right[j]
		if max(a.start, b.start) < min(a.end, b.end) {
			return true
		}
		if a.end <= b.end {
			i++
		} else {
			j++
		}
	}
	return false
}
