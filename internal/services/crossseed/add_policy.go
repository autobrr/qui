// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package crossseed

import (
	"strings"

	qbt "github.com/autobrr/go-qbittorrent"
)

var discLayoutMarkers = []string{"BDMV", "VIDEO_TS"}

type AddPolicy struct {
	DiscLayout bool

	DiscMarker string
}

func PolicyForSourceFiles(sourceFiles qbt.TorrentFiles) AddPolicy {
	isDisc, marker := isDiscLayoutTorrent(sourceFiles)
	if isDisc {
		return AddPolicy{
			DiscLayout: true,
			DiscMarker: marker,
		}
	}
	return AddPolicy{}
}

func isDiscLayoutTorrent(files qbt.TorrentFiles) (isDisc bool, marker string) {
	for _, f := range files {
		path := strings.ReplaceAll(f.Name, "\\", "/")

		segments := strings.Split(path, "/")
		if len(segments) < 2 {
			continue
		}
		dirSegments := segments[:len(segments)-1]

		for _, seg := range dirSegments {
			segUpper := strings.ToUpper(seg)
			for _, m := range discLayoutMarkers {
				if segUpper == m {
					return true, m
				}
			}
		}
	}
	return false, ""
}

func KeepAtSavePath(options map[string]string) {
	if options["savepath"] != "" {
		options["useDownloadPath"] = "false"
	}
}
