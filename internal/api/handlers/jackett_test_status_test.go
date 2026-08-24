// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package handlers

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestWaitForIndexerTestResultUsesCallbackError(t *testing.T) {
	ctx := context.Background()
	ch := make(chan error, 1)
	ch <- errors.New("connection refused")

	err := waitForIndexerTestResult(ctx, nil, ch)
	require.EqualError(t, err, "connection refused")
}

func TestWaitForIndexerTestResultPrefersScheduleError(t *testing.T) {
	ctx := context.Background()
	ch := make(chan error, 1)
	ch <- errors.New("callback")

	err := waitForIndexerTestResult(ctx, errors.New("empty query"), ch)
	require.EqualError(t, err, "empty query")
}

func TestWaitForIndexerTestResultSuccess(t *testing.T) {
	ctx := context.Background()
	ch := make(chan error, 1)
	ch <- nil

	require.NoError(t, waitForIndexerTestResult(ctx, nil, ch))
}

func TestWaitForIndexerTestResultTimeout(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	err := waitForIndexerTestResult(ctx, nil, make(chan error))
	require.ErrorIs(t, err, context.DeadlineExceeded)
}
