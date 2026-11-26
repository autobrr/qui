// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"os"
	"path/filepath"

	"github.com/rs/zerolog"

	"github.com/autobrr/qui/internal/buildinfo"
)

type Inputs struct {
	AppContainer      bool
	DisableSelfUpdate bool
	ReleaseVersion    bool
	DirWritable       bool
	BinaryPath        string
}

type Availability struct {
	SelfUpdate                  bool
	SelfUpdateUnavailableReason string
	Restart                     bool
}

func Decide(in Inputs) Availability {
	var reason string
	switch {
	case in.AppContainer:
		reason = "container"
	case in.DisableSelfUpdate:
		reason = "disabled"
	case !in.ReleaseVersion:
		reason = "development"
	case !in.DirWritable:
		reason = "directory"
	}
	return Availability{
		SelfUpdate:                  reason == "",
		SelfUpdateUnavailableReason: reason,
		Restart:                     !in.AppContainer,
	}
}

func Measure(log zerolog.Logger, disableSelfUpdate bool, version string) Inputs {
	in := Inputs{
		AppContainer: appContainer(func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}, os.Getpid(), os.Getenv("KUBERNETES_SERVICE_HOST") != ""),
		DisableSelfUpdate: disableSelfUpdate,
		ReleaseVersion:    buildinfo.IsRelease(version),
	}

	binaryPath, err := resolveBinaryPath()
	if err == nil {
		in.BinaryPath = binaryPath
		err = probeWritable(filepath.Dir(binaryPath))
		in.DirWritable = err == nil
	}

	log.Debug().
		Err(err).
		Bool("appContainer", in.AppContainer).
		Bool("disableSelfUpdate", in.DisableSelfUpdate).
		Bool("releaseVersion", in.ReleaseVersion).
		Str("version", version).
		Bool("dirWritable", in.DirWritable).
		Str("binaryPath", binaryPath).
		Msg("availability inputs")

	return in
}

func appContainer(exists func(path string) bool, pid int, kubernetes bool) bool {
	return exists("/.dockerenv") || exists("/run/.containerenv") || pid == 1 || kubernetes
}

func resolveBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(exe)
}

func probeWritable(dir string) error {
	f, err := os.CreateTemp(dir, ".qui-write-test-*")
	if err != nil {
		return err
	}
	f.Close()
	return os.Remove(f.Name())
}
