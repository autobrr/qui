// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package automations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/autobrr/qui/internal/models"
)

func TestAutomationSummaryMessageDoesNotDuplicateTopFailures(t *testing.T) {
	t.Parallel()

	summary := newAutomationSummary()
	summary.failed = 1
	summary.failedByAction[models.ActivityActionDeleteFailed] = 1

	msg := summary.message()
	require.Equal(t, 1, strings.Count(msg, "Top failures:"))
}
