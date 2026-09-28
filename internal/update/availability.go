// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"os"
	"path/filepath"

	"github.com/Masterminds/semver/v3"
	"github.com/rs/zerolog"
)

// Inputs are the facts that decide Availability. qui measures them once at startup.
type Inputs struct {
	AppContainer      bool
	DisableSelfUpdate bool
	ReleaseVersion    bool
	DirWritable       bool
	// BinaryPath is the resolved binary that a Restart execs. Decide ignores it.
	BinaryPath string
}

// Availability reports which of Self-update and Restart qui offers.
type Availability struct {
	SelfUpdate bool
	Restart    bool
}

func Decide(in Inputs) Availability {
	return Availability{
		// A Self-update ends in a Restart, so it needs one.
		SelfUpdate: restartSupported && !in.AppContainer && !in.DisableSelfUpdate && in.ReleaseVersion && in.DirWritable,
		Restart:    restartSupported && !in.AppContainer,
	}
}

// Measure reads the Availability inputs from the host and logs each one at DEBUG level.
func Measure(log zerolog.Logger, disableSelfUpdate bool, version string) Inputs {
	in := Inputs{
		AppContainer: appContainer(func(path string) bool {
			_, err := os.Stat(path)
			return err == nil
		}, os.Getpid(), os.Getenv("KUBERNETES_SERVICE_HOST") != ""),
		DisableSelfUpdate: disableSelfUpdate,
		ReleaseVersion:    isReleaseVersion(version),
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

// appContainer reports whether qui runs as the application of a container.
// Unlike config.detectContainer it ignores /dev/.lxc-boot-id: a system container
// such as Proxmox LXC runs qui under its own init, so a Restart works there.
// The kubelet sets KUBERNETES_SERVICE_HOST in every pod, which covers a pod on
// containerd where qui is not PID 1 (shareProcessNamespace, an init wrapper).
func appContainer(exists func(path string) bool, pid int, kubernetes bool) bool {
	return exists("/.dockerenv") || exists("/run/.containerenv") || pid == 1 || kubernetes
}

// isReleaseVersion rejects dev builds ("0.0.0-dev") and git describe builds
// ("v1.30.0-30-gde99118d"): both parse as semver but carry a prerelease part.
func isReleaseVersion(version string) bool {
	v, err := semver.NewVersion(version)
	return err == nil && v.Prerelease() == "" && v.Metadata() == ""
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
