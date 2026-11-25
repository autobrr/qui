// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package tray

type Menu struct {
	Version   string
	URL       string
	ConfigDir string
	Binary    string
	Restart   func()
	Quit      func()
}
