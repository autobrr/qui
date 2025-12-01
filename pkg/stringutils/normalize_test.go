// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestDefaultTransform(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"lowercase", "HELLO", "hello"},
		{"trim spaces", "  hello  ", "hello"},
		{"both", "  HELLO WORLD  ", "hello world"},
		{"empty string", "", ""},
		{"already normalized", "hello", "hello"},
		{"mixed case", "HeLLo WoRLd", "hello world"},
		{"tabs and newlines", "\t Hello \n", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := defaultTransform(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewNormalizer(t *testing.T) {
	t.Parallel()

	transform := func(s string) string {
		return strings.ToUpper(s)
	}

	normalizer := NewNormalizer(time.Minute, transform)
	assert.NotNil(t, normalizer)
	assert.NotNil(t, normalizer.cache)
	assert.NotNil(t, normalizer.transform)
}

func TestNewDefaultNormalizer(t *testing.T) {
	t.Parallel()

	normalizer := NewDefaultNormalizer()
	assert.NotNil(t, normalizer)
	assert.NotNil(t, normalizer.cache)
	assert.NotNil(t, normalizer.transform)
}

func TestNormalizer_Normalize(t *testing.T) {
	t.Parallel()

	t.Run("default normalizer", func(t *testing.T) {
		t.Parallel()

		normalizer := NewDefaultNormalizer()

		// First call - not cached
		result := normalizer.Normalize("  HELLO  ")
		assert.Equal(t, "hello", result)

		// Second call - should use cache
		result = normalizer.Normalize("  HELLO  ")
		assert.Equal(t, "hello", result)
	})

	t.Run("custom transform", func(t *testing.T) {
		t.Parallel()

		transform := func(s string) string {
			return strings.ToUpper(strings.TrimSpace(s))
		}

		normalizer := NewNormalizer(time.Minute, transform)

		result := normalizer.Normalize("  hello  ")
		assert.Equal(t, "HELLO", result)

		// Cached result
		result = normalizer.Normalize("  hello  ")
		assert.Equal(t, "HELLO", result)
	})

	t.Run("different keys", func(t *testing.T) {
		t.Parallel()

		normalizer := NewDefaultNormalizer()

		result1 := normalizer.Normalize("HELLO")
		result2 := normalizer.Normalize("WORLD")

		assert.Equal(t, "hello", result1)
		assert.Equal(t, "world", result2)
	})

	t.Run("generic types", func(t *testing.T) {
		t.Parallel()

		transform := func(n int) string {
			switch n {
			case 1:
				return "one"
			case 2:
				return "two"
			default:
				return "other"
			}
		}

		normalizer := NewNormalizer[int, string](time.Minute, transform)

		assert.Equal(t, "one", normalizer.Normalize(1))
		assert.Equal(t, "two", normalizer.Normalize(2))
		assert.Equal(t, "other", normalizer.Normalize(99))
	})
}

func TestNormalizer_Clear(t *testing.T) {
	t.Parallel()

	callCount := 0
	transform := func(s string) string {
		callCount++
		return strings.ToLower(s)
	}

	normalizer := NewNormalizer(time.Minute, transform)

	// First call
	_ = normalizer.Normalize("HELLO")
	assert.Equal(t, 1, callCount)

	// Second call - should use cache, no new transform
	_ = normalizer.Normalize("HELLO")
	assert.Equal(t, 1, callCount)

	// Clear the cache entry
	normalizer.Clear("HELLO")

	// Third call - should transform again
	_ = normalizer.Normalize("HELLO")
	assert.Equal(t, 2, callCount)
}

func TestDefaultNormalizer_StaticInstance(t *testing.T) {
	t.Parallel()

	// The default normalizer should be usable globally
	assert.NotNil(t, DefaultNormalizer)

	result := DefaultNormalizer.Normalize("  TEST  ")
	assert.Equal(t, "test", result)
}
