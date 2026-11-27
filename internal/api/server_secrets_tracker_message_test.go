// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package api

import (
	"net/url"
	"strings"
)

func (rig *secretsRig) trackerMessage(tracker string) string {
	announce, err := url.Parse(tracker)
	if err != nil {
		panic(err)
	}
	bareKey, _, _ := strings.Cut(strings.Trim(announce.Path, "/"), "/")
	mirror := "https://mirror.example.invalid/t?key=" + rig.plant("tracker message key", shownWithTorrentsWrite)
	return "Unregistered torrent " + bareKey + " see " + mirror
}
