// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import "context"

// SearchPriority returns the desired scheduler priority embedded in ctx via WithSearchPriority.
func SearchPriority(ctx context.Context) (RateLimitPriority, bool) {
	return getSearchPriorityFromContext(ctx)
}
