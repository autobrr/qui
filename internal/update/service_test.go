// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"sync"
	"testing"

	"github.com/rs/zerolog"
)

// A configuration reload calls SetEnabled while the update loop runs
// CheckUpdates. The flag stays false, so no request leaves the process; the
// race detector is the gate.
func TestServiceSetEnabledDuringCheckUpdates(t *testing.T) {
	svc := NewService(zerolog.Nop(), false, "v1.0.0", "qui-test")

	var wg sync.WaitGroup
	wg.Go(func() { svc.SetEnabled(false) })
	wg.Go(func() { svc.CheckUpdates(t.Context()) })
	wg.Wait()
}
