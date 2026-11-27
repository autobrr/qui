// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package infohash

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

const otherV1 = "fedcba9876543210fedcba9876543210fedcba98"

func TestUniqueKeepsTheFirstOfEachHashInOrderAndDropsWhatDoesNotParse(t *testing.T) {
	got := Unique([]string{otherV1, "", strings.ToUpper(v1Lower), "all", " " + otherV1 + " ", v1Lower, v2Lower})
	assert.Equal(t, []Hash{mustParse(t, otherV1), mustParse(t, v1Lower), mustParse(t, v2Lower)}, got)
}

func TestUniqueStringsGivesTheCanonicalTextOfEachHashOnce(t *testing.T) {
	got := UniqueStrings([]string{" " + strings.ToUpper(otherV1), "not a hash", otherV1, v1Lower})
	assert.Equal(t, []string{otherV1, v1Lower}, got)
}

func TestSetAddsEachHashOnceAndKeepsTheOrderOfTheFirstAdd(t *testing.T) {
	var s Set
	assert.True(t, s.Add(mustParse(t, otherV1)))
	assert.True(t, s.Add(mustParse(t, strings.ToUpper(v2Lower))))
	assert.False(t, s.Add(mustParse(t, " "+otherV1)))
	assert.False(t, s.Add(Hash{}))

	assert.True(t, s.Has(mustParse(t, v2Lower)))
	assert.False(t, s.Has(mustParse(t, v1Lower)))
	assert.False(t, s.Has(Hash{}))
	assert.Equal(t, 2, s.Len())
	assert.Equal(t, []Hash{mustParse(t, otherV1), mustParse(t, v2Lower)}, s.Hashes())
	assert.Equal(t, []string{otherV1, v2Lower}, s.Strings())
}

func TestAnAppendToHashesLeavesTheSetAlone(t *testing.T) {
	s := NewSet(mustParse(t, v1Lower), mustParse(t, v2Lower))
	s.Add(mustParse(t, otherV1))
	head := s.Hashes()[:1]
	_ = append(head, Hash{})
	assert.Equal(t, []Hash{mustParse(t, v1Lower), mustParse(t, v2Lower), mustParse(t, otherV1)}, s.Hashes())
}

func FuzzUniqueHoldsEachParsedValueOnce(f *testing.F) {
	f.Add(v1Lower + "|" + strings.ToUpper(v1Lower) + "|all|" + v2Lower)
	f.Add(" |" + otherV1)
	f.Fuzz(func(t *testing.T, joined string) {
		values := strings.Split(joined, "|")
		got := Unique(values)
		s := NewSet(got...)
		assert.Equal(t, got, s.Hashes())
		assert.Equal(t, got, Unique(s.Strings()))
		assert.Equal(t, s.Strings(), UniqueStrings(values))
		for _, value := range values {
			if hash, err := Parse(value); err == nil {
				assert.True(t, s.Has(hash), value)
			}
		}
	})
}
