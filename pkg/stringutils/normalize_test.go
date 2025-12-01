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

func TestNewInternNormalizer(t *testing.T) {
	t.Parallel()

	transform := func(s string) string {
		return Intern(strings.ToUpper(s))
	}

	normalizer := NewInternNormalizer(time.Minute, transform)
	assert.NotNil(t, normalizer)
	assert.NotNil(t, normalizer.cache)
	assert.NotNil(t, normalizer.transform)

	result := normalizer.Normalize("hello")
	assert.Equal(t, "HELLO", result)
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

		// First call
		result := normalizer.Normalize("  HELLO  ")
		assert.Equal(t, "hello", result)

		// Second call - same result (interned)
		result = normalizer.Normalize("  HELLO  ")
		assert.Equal(t, "hello", result)
	})

	t.Run("custom transform", func(t *testing.T) {
		t.Parallel()

		transform := func(s string) string {
			return Intern(strings.ToUpper(strings.TrimSpace(s)))
		}

		normalizer := NewInternNormalizer(time.Minute, transform)

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
				return Intern("one")
			case 2:
				return Intern("two")
			default:
				return Intern("other")
			}
		}

		normalizer := NewInternNormalizer(time.Minute, transform)

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
		return Intern(strings.ToLower(s))
	}

	normalizer := NewInternNormalizer(time.Minute, transform)

	// First call
	result1 := normalizer.Normalize("HELLO")
	assert.Equal(t, "hello", result1)
	assert.Equal(t, 1, callCount)

	// Second call - should use cache, no new transform
	result2 := normalizer.Normalize("HELLO")
	assert.Equal(t, "hello", result2)
	assert.Equal(t, 1, callCount)

	// Clear the cache entry
	normalizer.Clear("HELLO")

	// Third call - should transform again
	result3 := normalizer.Normalize("HELLO")
	assert.Equal(t, "hello", result3)
	assert.Equal(t, 2, callCount)
}

func TestDefaultNormalizer_StaticInstance(t *testing.T) {
	t.Parallel()

	// The default normalizer should be usable globally
	assert.NotNil(t, DefaultNormalizer)

	result := DefaultNormalizer.Normalize("  TEST  ")
	assert.Equal(t, "test", result)
}

func BenchmarkNormalizer(b *testing.B) {
	normalizer := NewDefaultNormalizer()
	inputs := []string{"HELLO", "  WORLD  ", "TeSt", "already lowercase"}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, input := range inputs {
			_ = normalizer.Normalize(input)
		}
	}
}

func BenchmarkNormalizerParallel(b *testing.B) {
	normalizer := NewDefaultNormalizer()
	inputs := []string{"HELLO", "  WORLD  ", "TeSt", "already lowercase"}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			_ = normalizer.Normalize(inputs[i%len(inputs)])
			i++
		}
	})
}
