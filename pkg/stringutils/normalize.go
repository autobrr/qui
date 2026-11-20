// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package stringutils

import (
	"strings"
	"time"

	"github.com/autobrr/go-cache/ttlcache"
)

const defaultNormalizerTTL = 5 * time.Minute

type TransformFunc[K, V any] func(K) V

type Normalizer[K comparable, V any] struct {
	cache     *ttlcache.Cache[K, V]
	transform TransformFunc[K, V]
}

func NewNormalizer[K comparable, V any](ttl time.Duration, transform TransformFunc[K, V]) *Normalizer[K, V] {
	cache := ttlcache.New[K, V](
		ttlcache.SetDefaultTTL(ttl))
	return &Normalizer[K, V]{
		cache:     cache,
		transform: transform,
	}
}

func NewDefaultNormalizer() *Normalizer[string, string] {
	return NewNormalizer(defaultNormalizerTTL, defaultTransform)
}

func defaultTransform(s string) string {
	return strings.ToLower(strings.TrimSpace(s))
}

func (n *Normalizer[K, V]) Normalize(key K) V {
	if cached, ok := n.cache.Get(key); ok {
		return cached
	}

	transformed := n.transform(key)
	n.cache.Set(key, transformed, ttlcache.DefaultTTL)
	return transformed
}

func (n *Normalizer[K, V]) Clear(key K) {
	n.cache.Delete(key)
}

var DefaultNormalizer = NewDefaultNormalizer()
