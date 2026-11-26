// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package local

import (
	"os"

	"github.com/autobrr/qui/internal/fsops"
)

func statFields(fi os.FileInfo, path string, follow bool) *fsops.LstatInfo {
	return &fsops.LstatInfo{
		Path:      path,
		Size:      fi.Size(),
		ModTime:   fi.ModTime(),
		IsDir:     fi.IsDir(),
		IsSymlink: fi.Mode()&os.ModeSymlink != 0,
		Mode:      localFileMode(path, fi.Mode(), follow),
	}
}

func hasIdentity(info *fsops.LstatInfo) bool {
	return info.Mode.IsRegular() || info.IsDir
}
