// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package buildinfo

import (
	"encoding/json"
	"fmt"
	"runtime"
)

var (
	Version   = "0.0.0-dev"
	Commit    = ""
	Date      = ""
	UserAgent = ""
)

func init() {
	UserAgent = fmt.Sprintf("qui/%s (%s %s)", Version, runtime.GOOS, runtime.GOARCH)
}

type buildInfo struct {
	Version string `json:"version"`
	Commit  string `json:"commit"`
	Date    string `json:"date"`
}

func String() string {
	return fmt.Sprintf("Version: %v\nCommit: %v\nBuild date: %s\n", Version, Commit, Date)
}

func Print() {
	fmt.Printf("Version: %v\nCommit: %v\nBuild date: %s\n", Version, Commit, Date)
}

func JSON() ([]byte, error) {
	i := buildInfo{
		Version: Version,
		Commit:  Commit,
		Date:    Date,
	}

	// json marshal buildinfo
	data, err := json.Marshal(i)
	if err != nil {
		return nil, err
	}

	return data, nil
}
