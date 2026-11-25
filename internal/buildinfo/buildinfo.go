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
	Tray      = ""
)

func init() {
	UserAgent = fmt.Sprintf("qui/%s (%s %s)", Version, runtime.GOOS, runtime.GOARCH)
}

func IsRelease(version string) bool {
	v, err := semver.NewVersion(version)
	return err == nil && v.Prerelease() == "" && v.Metadata() == ""
}
