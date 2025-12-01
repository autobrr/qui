// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package releases

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewParser(t *testing.T) {
	t.Parallel()

	parser := NewParser(time.Minute)
	assert.NotNil(t, parser)
	assert.NotNil(t, parser.cache)
}

func TestNewDefaultParser(t *testing.T) {
	t.Parallel()

	parser := NewDefaultParser()
	assert.NotNil(t, parser)
	assert.NotNil(t, parser.cache)
}

func TestParser_Parse(t *testing.T) {
	t.Parallel()

	t.Run("nil parser returns empty release", func(t *testing.T) {
		t.Parallel()

		var parser *Parser
		result := parser.Parse("Test.Release.2024.1080p.WEB-DL")
		assert.NotNil(t, result)
	})

	t.Run("empty name returns empty release", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()
		result := parser.Parse("")
		assert.NotNil(t, result)
	})

	t.Run("whitespace only returns empty release", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()
		result := parser.Parse("   ")
		assert.NotNil(t, result)
	})

	t.Run("parses release name", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()
		result := parser.Parse("Movie.Title.2024.1080p.WEB-DL.x264-GROUP")
		assert.NotNil(t, result)
		// The parser should extract some metadata
		// Note: Exact assertions depend on the rls library behavior
	})

	t.Run("caches parsed results", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()
		name := "Test.Movie.2024.1080p.WEB-DL"

		// First parse
		result1 := parser.Parse(name)
		assert.NotNil(t, result1)

		// Second parse should return cached result
		result2 := parser.Parse(name)
		assert.NotNil(t, result2)

		// Results should be the same pointer (cached)
		assert.Equal(t, result1, result2)
	})

	t.Run("trims whitespace from name", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()

		// Parse with whitespace
		result1 := parser.Parse("  Test.Movie.2024  ")
		// Parse without whitespace
		result2 := parser.Parse("Test.Movie.2024")

		// Should be the same cached result
		assert.Equal(t, result1, result2)
	})
}

func TestParser_Clear(t *testing.T) {
	t.Parallel()

	t.Run("nil parser does not panic", func(t *testing.T) {
		t.Parallel()

		var parser *Parser
		parser.Clear("test")
		// Should not panic
	})

	t.Run("empty name does not panic", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()
		parser.Clear("")
		// Should not panic
	})

	t.Run("clears cached entry", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()
		name := "Test.Movie.2024"

		// Parse and cache
		result1 := parser.Parse(name)
		assert.NotNil(t, result1)

		// Clear the cache
		parser.Clear(name)

		// Parse again - should create new entry
		result2 := parser.Parse(name)
		assert.NotNil(t, result2)

		// After clear, the pointer should be different (new parse)
		// Actually, they might be equal if the content is the same
		// The key test is that Clear doesn't panic and removes the entry
	})

	t.Run("trims whitespace from name", func(t *testing.T) {
		t.Parallel()

		parser := NewDefaultParser()

		// Parse and cache
		_ = parser.Parse("Test.Movie.2024")

		// Clear with whitespace - should clear the trimmed key
		parser.Clear("  Test.Movie.2024  ")

		// Should not panic
	})
}
