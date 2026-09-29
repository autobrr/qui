// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package tray

// Menu holds what the Tray shows and what its items do.
type Menu struct {
	Version   string
	URL       string
	ConfigDir string
	// Binary is the resolved qui-tray.exe path that Start with Windows registers.
	Binary  string
	Restart func()
	Quit    func()
}
