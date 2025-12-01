// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"time"

	"github.com/autobrr/autobrr/pkg/ttlcache"
)

const defaultNormalizerTTL = 5 * time.Minute

// TransformFunc is a function that transforms K to V.
type TransformFunc[K, V any] func(K) V

// Normalizer caches transformed results so we do not repeatedly transform the same inputs.
// Transform functions should use interning (e.g., stringutils.Intern) to deduplicate
// the resulting strings in memory.
type Normalizer[K comparable, V any] struct {
	cache     *ttlcache.Cache[K, V]
	transform TransformFunc[K, V]
}

// NewNormalizer returns a normalizer with the provided TTL and transform function.
// The transform function is only called once per unique key (until TTL expires),
// so it should intern the result to avoid duplicate allocations across keys.
func NewNormalizer[K comparable, V any](ttl time.Duration, transform TransformFunc[K, V]) *Normalizer[K, V] {
	cache := ttlcache.New(ttlcache.Options[K, V]{}.
		SetDefaultTTL(ttl))
	return &Normalizer[K, V]{
		cache:     cache,
		transform: transform,
	}
}

// NewInternNormalizer returns a normalizer with the provided TTL and transform function.
// Alias for NewNormalizer for clarity when using with interning transforms.
func NewInternNormalizer[K comparable, V any](ttl time.Duration, transform TransformFunc[K, V]) *Normalizer[K, V] {
	return NewNormalizer(ttl, transform)
}

// NewDefaultNormalizer returns a normalizer using the default TTL and default transform
// (ToLower + TrimSpace + Intern).
func NewDefaultNormalizer() *Normalizer[string, string] {
	return NewNormalizer(defaultNormalizerTTL, defaultTransform)
}

// defaultTransform normalizes and interns the string.
// The result is cached by Normalizer, and interning ensures identical
// normalized strings share memory across different cache entries.
func defaultTransform(s string) string {
	return InternNormalized(s)
}

// Normalize returns the transformed value, using the cache to avoid repeated transforms.
func (n *Normalizer[K, V]) Normalize(key K) V {
	if cached, ok := n.cache.Get(key); ok {
		return cached
	}

	transformed := n.transform(key)
	n.cache.Set(key, transformed, ttlcache.DefaultTTL)
	return transformed
}

// Clear removes a cached entry.
func (n *Normalizer[K, V]) Clear(key K) {
	n.cache.Delete(key)
}

// DefaultNormalizer is a statically allocated default normalizer for strings.
var DefaultNormalizer = NewDefaultNormalizer()
