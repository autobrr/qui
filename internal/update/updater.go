// Copyright (c) 2025, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"context"
	"fmt"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
)

type Config struct {
	Repository string
	Version    string
}

type Updater struct {
	config Config
}

func NewUpdater(config Config) *Updater {
	return &Updater{
		config: config,
	}
}

// Run downloads and installs an updated binary when a newer release is available.
// It returns true when an update was applied.
func (u *Updater) Run(ctx context.Context) (bool, error) {
	_, err := semver.NewVersion(u.config.Version)
	if err != nil {
		return false, fmt.Errorf("could not parse version: %w", err)
	}

	latest, found, err := selfupdate.DetectLatest(ctx, selfupdate.ParseSlug(u.config.Repository))
	if err != nil {
		return false, fmt.Errorf("error occurred while detecting version: %w", err)
	}
	if !found {
		return false, fmt.Errorf("latest version for %s/%s could not be found from github repository", u.config.Repository, u.config.Version)
	}

	if latest.LessOrEqual(u.config.Version) {
		fmt.Printf("Current binary is the latest version: %s\n", u.config.Version)
		return false, nil
	}

	exe, err := selfupdate.ExecutablePath()
	if err != nil {
		return false, fmt.Errorf("could not locate executable path: %w", err)
	}

	if err := selfupdate.UpdateTo(ctx, latest.AssetURL, latest.AssetName, exe); err != nil {
		return false, fmt.Errorf("error occurred while updating binary: %w", err)
	}

	fmt.Printf("Successfully updated to version: %s\n", latest.Version())
	return true, nil
}
