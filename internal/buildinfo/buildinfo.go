// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package buildinfo

import (
	"fmt"
	"runtime"

	"github.com/Masterminds/semver/v3"
)

var (
	Version   = "0.0.0-dev"
	Commit    = ""
	Date      = ""
	UserAgent = ""
	// Tray is "true" in qui-tray.exe, the Windows GUI-subsystem build that serves
	// with the Tray. The linker sets it, not the file name, so a renamed exe still works.
	Tray = ""
)

func init() {
	UserAgent = fmt.Sprintf("qui/%s (%s %s)", Version, runtime.GOOS, runtime.GOARCH)
}

// IsRelease rejects dev builds ("0.0.0-dev") and git describe builds
// ("v1.30.0-30-gde99118d"): both parse as semver but carry a prerelease part.
func IsRelease(version string) bool {
	v, err := semver.NewVersion(version)
	return err == nil && v.Prerelease() == "" && v.Metadata() == ""
}
