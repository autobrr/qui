// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package update

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/creativeprojects/go-selfupdate"
	selfupdateapply "github.com/creativeprojects/go-selfupdate/update"
	"github.com/rs/zerolog/log"
)

const releaseChecksumsAsset = "checksums.txt"

var (
	ErrReleaseNotFound = errors.New("release not found")
	ErrNotNewer        = errors.New("release is not newer than the running version")
	// ErrSwap marks a failure to replace the binary on disk, such as a permission error.
	ErrSwap = errors.New("could not replace the binary")
)

// statBackup is a seam for the test of a backup that is missing after the swap.
var statBackup = os.Stat

type Config struct {
	Repository string
	Version    string
	// BinaryPath is the binary that Install replaces, resolved at startup. Run
	// resolves its own.
	BinaryPath string
}

// Result describes a successful Self-update.
type Result struct {
	// Version has no "v" prefix, the same format as GET /api/version.
	Version         string `json:"version"`
	RollbackCommand string `json:"rollbackCommand"`
	BackupError     string `json:"backupError"`
}

type Updater struct {
	config Config
	// source is nil in production, which makes go-selfupdate use GitHub.
	source      selfupdate.Source
	certificate []byte
}

func NewUpdater(config Config) *Updater {
	return &Updater{
		config:      config,
		certificate: releaseSigningCertificate,
	}
}

// Run installs the latest release. It serves the qui update CLI command.
func (u *Updater) Run(ctx context.Context) error {
	current, err := semver.NewVersion(u.config.Version)
	if err != nil {
		return fmt.Errorf("could not parse version: %w", err)
	}

	binary, err := resolveBinaryPath()
	if err != nil {
		return fmt.Errorf("could not locate executable path: %w", err)
	}

	backup := backupPath(binary, current)
	updater, archive, err := u.newSelfUpdater(savePath(backup))
	if err != nil {
		return err
	}

	latest, found, err := updater.DetectLatest(ctx, selfupdate.ParseSlug(u.config.Repository))
	if err != nil {
		return fmt.Errorf("error occurred while detecting version: %w", err)
	}
	if !found {
		return fmt.Errorf("latest version for %s/%s could not be found from github repository", u.config.Repository, u.config.Version)
	}

	if latest.LessOrEqual(u.config.Version) {
		fmt.Printf("Current binary is the latest version: %s\n", u.config.Version)
		return nil
	}

	result, err := swap(ctx, updater, archive, latest, binary, current)
	if err != nil {
		return fmt.Errorf("error occurred while updating binary: %w", err)
	}

	fmt.Printf("Successfully updated to version: %s\n", result.Version)
	if result.BackupError != "" {
		fmt.Printf("Warning: %s, a rollback needs a manual download\n", result.BackupError)
	} else {
		fmt.Printf("To roll back, stop qui and run: %s\n", result.RollbackCommand)
	}
	return nil
}

// Install replaces the binary with the release that has this exact tag. It
// never falls back to the latest release: the dialog showed this tag, and the
// user read the notes for it. On an error the binary does not change.
func (u *Updater) Install(ctx context.Context, tag string) (Result, error) {
	current, err := semver.NewVersion(u.config.Version)
	if err != nil {
		return Result{}, fmt.Errorf("could not parse version: %w", err)
	}

	updater, archive, err := u.newSelfUpdater(savePath(backupPath(u.config.BinaryPath, current)))
	if err != nil {
		return Result{}, err
	}

	release, found, err := updater.DetectVersion(ctx, selfupdate.ParseSlug(u.config.Repository), tag)
	if err != nil {
		return Result{}, fmt.Errorf("could not find release %s: %w", tag, err)
	}
	if !found {
		return Result{}, fmt.Errorf("%w: no release %s for %s/%s", ErrReleaseNotFound, tag, runtime.GOOS, runtime.GOARCH)
	}
	if release.LessOrEqual(current.String()) {
		return Result{}, fmt.Errorf("%w: %s is not newer than %s", ErrNotNewer, tag, current)
	}

	return swap(ctx, updater, archive, release, u.config.BinaryPath, current)
}

// swap downloads, validates and installs the release, keeping the old binary at
// its backup path. It checks that exact path afterwards and never guesses
// another. Then it replaces the sibling Windows binary from the same archive, so
// qui.exe and qui-tray.exe never run different versions against one database.
func swap(ctx context.Context, updater *selfupdate.Updater, archive *archiveSource, release *selfupdate.Release, binary string, current *semver.Version) (Result, error) {
	backup := backupPath(binary, current)
	if err := updater.UpdateTo(ctx, release, binary); err != nil {
		_, pathErr := errors.AsType[*os.PathError](err)
		_, linkErr := errors.AsType[*os.LinkError](err)
		// A failed rollback wraps its cause out of reach of errors.AsType.
		if pathErr || linkErr || selfupdateapply.RollbackError(err) != nil {
			return Result{}, fmt.Errorf("%w: %w", ErrSwap, err)
		}
		return Result{}, err
	}

	result := Result{Version: release.Version()}
	if _, err := statBackup(backup); err != nil {
		// A *PathError names the checked path and the real cause.
		result.BackupError = err.Error()
		// The older backups are now the only way back, so keep them.
	} else {
		result.RollbackCommand = "mv " + shellQuote(backup) + " " + shellQuote(binary)
		if runtime.GOOS == "windows" {
			// cmd /c: in PowerShell, the Windows 11 default, move is Move-Item and rejects /Y.
			result.RollbackCommand = fmt.Sprintf(`cmd /c move /Y "%s" "%s"`, backup, binary)
		}
		removeOlderBackups(binary, backup)
	}

	sibling := siblingPath(binary)
	if _, err := os.Stat(sibling); err != nil {
		return result, nil
	}
	siblingBackup := backupPath(sibling, current)
	asset, err := selfupdate.DecompressCommand(bytes.NewReader(archive.archive), release.AssetName, filepath.Base(sibling), runtime.GOOS, runtime.GOARCH)
	if err == nil {
		err = selfupdateapply.Apply(asset, selfupdateapply.Options{TargetPath: sibling, OldSavePath: savePath(siblingBackup)})
	}
	if err != nil {
		return Result{}, fmt.Errorf("%w: replaced %s, but not %s: %w", ErrSwap, binary, sibling, err)
	}
	removeOlderBackups(sibling, siblingBackup)
	return result, nil
}

// savePath is where a swap keeps the file it replaces. A backup of the running
// version exists already when an earlier swap did not end in a Restart, such
// as a failed sibling swap. That backup holds the real old binary, so the swap
// keeps it and deletes the replaced file.
func savePath(backup string) string {
	if _, err := os.Stat(backup); err == nil {
		return ""
	}
	return backup
}

// siblingPath is the other Windows binary: qui-tray.exe next to qui.exe, and
// the reverse. Only the Windows release ships both.
func siblingPath(binary string) string {
	sibling := "qui-tray"
	if binaryName(binary) == "qui-tray" {
		sibling = "qui"
	}
	return filepath.Join(filepath.Dir(binary), sibling+filepath.Ext(binary))
}

// shellQuote keeps $, backticks, and quotes in a path literal when the user
// pastes the command into a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// backupPath sits next to the binary because the swap is a rename. The name
// starts with the binary name, so qui and qui-tray keep one backup each.
func backupPath(binary string, current *semver.Version) string {
	return filepath.Join(filepath.Dir(binary), binaryName(binary)+"-v"+current.String()+backupSuffix())
}

func binaryName(binary string) string {
	return strings.TrimSuffix(filepath.Base(binary), filepath.Ext(binary))
}

func backupSuffix() string {
	if runtime.GOOS == "windows" {
		return ".bak.exe"
	}
	return ".bak"
}

// removeOlderBackups keeps only the latest backup of binary, for seedbox disk quotas.
func removeOlderBackups(binary, keep string) {
	dir := filepath.Dir(binary)
	entries, err := os.ReadDir(dir)
	if err != nil {
		log.Warn().Err(err).Str("dir", dir).Msg("could not list older backups")
		return
	}
	for _, entry := range entries {
		name := entry.Name()
		if matched, _ := filepath.Match(binaryName(binary)+"-v*"+backupSuffix(), name); !matched || name == filepath.Base(keep) || entry.IsDir() {
			continue
		}
		if err := os.Remove(filepath.Join(dir, name)); err != nil {
			log.Warn().Err(err).Str("path", filepath.Join(dir, name)).Msg("could not remove older backup")
		}
	}
}
