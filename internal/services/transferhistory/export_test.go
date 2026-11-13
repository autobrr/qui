// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

// WaitForFolds waits until each fold that the module started ends.
func WaitForFolds(m *Module) { m.folds.Wait() }
