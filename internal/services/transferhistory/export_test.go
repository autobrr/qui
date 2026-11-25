// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package transferhistory

func WaitForFolds(m *Module) { m.folds.Wait() }

func SetPartSize(m *Module, torrents int) { m.partSize = torrents }
