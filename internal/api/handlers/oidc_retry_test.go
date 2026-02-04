// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscoverOIDCProviderRetriesUntilSuccess(t *testing.T) {

	originalProvider := oidcNewProvider
	originalSleep := oidcSleep
	defer func() {
		oidcNewProvider = originalProvider
		oidcSleep = originalSleep
	}()

	attempts := 0
	var issuersTried []string

	oidcNewProvider = func(_ context.Context, issuer string) (*oidc.Provider, error) {
		issuersTried = append(issuersTried, issuer)
		attempts++
		if attempts < 3 {
			return nil, fmt.Errorf("attempt %d", attempts)
		}
		return &oidc.Provider{}, nil
	}

	oidcSleep = func(time.Duration) {}

	issuer := "https://issuer.example.com"
	provider, usedIssuer, err := discoverOIDCProvider(context.Background(), issuer)
	require.NoError(t, err)
	require.NotNil(t, provider)
	assert.Equal(t, issuer, usedIssuer)
	assert.Equalf(t, 3, attempts, "issuers tried: %v", issuersTried)
}

func TestDiscoverOIDCProviderFailsAfterMaxAttempts(t *testing.T) {

	originalProvider := oidcNewProvider
	originalSleep := oidcSleep
	defer func() {
		oidcNewProvider = originalProvider
		oidcSleep = originalSleep
	}()

	calls := 0
	var issuersTried []string

	oidcNewProvider = func(_ context.Context, issuer string) (*oidc.Provider, error) {
		calls++
		issuersTried = append(issuersTried, issuer)
		return nil, fmt.Errorf("attempt %d", calls)
	}

	oidcSleep = func(time.Duration) {}

	issuer := "https://issuer.example.com"
	provider, usedIssuer, err := discoverOIDCProvider(context.Background(), issuer)
	require.Error(t, err)
	assert.Nil(t, provider)
	assert.Equal(t, "", usedIssuer)

	expectedCalls := oidcInitMaxAttempts * 2
	assert.Equalf(t, expectedCalls, calls, "issuers tried: %v", issuersTried)

	expectedErrSnippet := fmt.Sprintf("attempted %d times", oidcInitMaxAttempts)
	assert.Contains(t, err.Error(), expectedErrSnippet)
}

func TestDiscoverOIDCProviderTrimsTrailingSlash(t *testing.T) {

	originalProvider := oidcNewProvider
	originalSleep := oidcSleep
	defer func() {
		oidcNewProvider = originalProvider
		oidcSleep = originalSleep
	}()

	issuer := "https://issuer.example.com/"
	trimmed := strings.TrimRight(issuer, "/")

	var issuersTried []string

	oidcNewProvider = func(_ context.Context, candidate string) (*oidc.Provider, error) {
		issuersTried = append(issuersTried, candidate)
		if candidate == trimmed {
			return &oidc.Provider{}, nil
		}
		return nil, fmt.Errorf("issuer %s failed", candidate)
	}

	oidcSleep = func(time.Duration) {}

	provider, usedIssuer, err := discoverOIDCProvider(context.Background(), issuer)
	require.NoErrorf(t, err, "issuers tried: %v", issuersTried)
	require.NotNil(t, provider)
	assert.Equal(t, trimmed, usedIssuer)
	assert.GreaterOrEqual(t, len(issuersTried), 2)
	assert.Equal(t, issuer, issuersTried[0])
	assert.Equal(t, trimmed, issuersTried[1])
}
