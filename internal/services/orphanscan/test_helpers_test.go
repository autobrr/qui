// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package orphanscan

import (
	"github.com/autobrr/qui/internal/fsops"
	"github.com/autobrr/qui/internal/fsops/local"
)

func newTestBackend() fsops.Backend {
	return local.NewBackend()
}
