// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package fsexec provides path-safe filesystem primitives for the qui-helper.
// All operations are resolved through os.Root handles that prevent path
// traversal outside configured allowed roots. This is the security backbone
// of the remote helper — even if SSH credentials are stolen, the helper
// enforces that operations stay within the directories the operator allowed.
package fsexec

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrPathNotAllowed is returned when a path is not under any allowed root.
var ErrPathNotAllowed = errors.New("path is not under any allowed root")

// ErrRootItself is returned when a destructive op targets the allowed root itself.
var ErrRootItself = errors.New("refusing to operate on the allowed root itself")

// ErrDeviceMismatch is returned when a resolved path is on a different device
// than the allowed root (e.g., the root was unmounted).
var ErrDeviceMismatch = errors.New("path is on a different device than the allowed root")

// SafeRoot holds a validated os.Root handle along with the device ID of the
// root directory. All filesystem operations should go through this handle.
type SafeRoot struct {
	root  *os.Root
	path  string // absolute path of the root
	devID uint64 // device ID recorded at open time
}

// OpenSafeRoot opens a directory as a SafeRoot, recording its device ID.
// The caller must call Close when done.
func OpenSafeRoot(rootPath string) (*SafeRoot, error) {
	abs, err := filepath.Abs(rootPath)
	if err != nil {
		return nil, fmt.Errorf("resolve root path: %w", err)
	}

	fi, err := os.Lstat(abs)
	if err != nil {
		return nil, fmt.Errorf("stat root: %w", err)
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", abs)
	}

	devID, err := deviceID(fi)
	if err != nil {
		return nil, fmt.Errorf("get device ID: %w", err)
	}

	root, err := os.OpenRoot(abs)
	if err != nil {
		return nil, fmt.Errorf("open root: %w", err)
	}

	return &SafeRoot{root: root, path: abs, devID: devID}, nil
}

// Close releases the underlying os.Root handle.
func (sr *SafeRoot) Close() error {
	return sr.root.Close()
}

// Root returns the underlying os.Root for direct operations.
func (sr *SafeRoot) Root() *os.Root { return sr.root }

// Path returns the absolute path of the root.
func (sr *SafeRoot) Path() string { return sr.path }

// Roots manages multiple SafeRoot handles, one per allowed root directory.
type Roots struct {
	roots []*SafeRoot
}

// OpenRoots opens SafeRoot handles for each allowed root path.
// Returns an error if any root cannot be opened.
func OpenRoots(paths []string) (*Roots, error) {
	if len(paths) == 0 {
		return nil, errors.New("no allowed roots provided")
	}

	roots := make([]*SafeRoot, 0, len(paths))
	for _, p := range paths {
		sr, err := OpenSafeRoot(p)
		if err != nil {
			// Close already-opened roots on failure.
			for _, opened := range roots {
				opened.Close()
			}
			return nil, fmt.Errorf("open root %s: %w", p, err)
		}
		roots = append(roots, sr)
	}
	return &Roots{roots: roots}, nil
}

// Close releases all SafeRoot handles.
func (r *Roots) Close() error {
	var firstErr error
	for _, sr := range r.roots {
		if err := sr.Close(); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// ResolveSafe validates an absolute path against the allowed roots and returns
// the matching SafeRoot along with the relative path within that root.
//
// Validation:
//   - Path must be absolute.
//   - Path must be filepath.Clean-ed (no redundant separators or dots).
//   - Path must not contain NUL bytes.
//   - Path must not contain ".." components.
//   - Path must be under one of the allowed roots.
//   - For destructive operations, path must be a strict descendant (not the root itself).
func (r *Roots) ResolveSafe(requestPath string, destructive bool) (*SafeRoot, string, error) {
	if err := validatePath(requestPath); err != nil {
		return nil, "", err
	}

	for _, sr := range r.roots {
		rel, ok := relUnder(requestPath, sr.path)
		if !ok {
			continue
		}

		if destructive && (rel == "." || rel == "") {
			return nil, "", fmt.Errorf("%w: %s", ErrRootItself, requestPath)
		}

		return sr, rel, nil
	}

	return nil, "", fmt.Errorf("%w: %s", ErrPathNotAllowed, requestPath)
}

// validatePath performs pre-resolution validation on a request path.
func validatePath(path string) error {
	if path == "" {
		return fmt.Errorf("%w: empty path", ErrPathNotAllowed)
	}

	// Reject NUL bytes.
	if strings.ContainsRune(path, 0) {
		return fmt.Errorf("%w: path contains NUL byte", ErrPathNotAllowed)
	}

	// Must be absolute.
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: relative path %q", ErrPathNotAllowed, path)
	}

	// Must be clean (no ".", "..", redundant separators).
	if path != filepath.Clean(path) {
		return fmt.Errorf("%w: path is not clean: %q", ErrPathNotAllowed, path)
	}

	// Reject ".." components explicitly (defense in depth — Clean removes them,
	// but we reject unclean paths above, so this catches paths that Clean would
	// resolve differently).
	for _, seg := range strings.Split(path, string(filepath.Separator)) {
		if seg == ".." {
			return fmt.Errorf("%w: path contains ..: %q", ErrPathNotAllowed, path)
		}
	}

	return nil
}

// relUnder returns the relative path of target under base, and true if target
// is under base. Returns (".", true) if target equals base.
func relUnder(target, base string) (string, bool) {
	if target == base {
		return ".", true
	}
	prefix := base + string(filepath.Separator)
	if strings.HasPrefix(target, prefix) {
		return target[len(prefix):], true
	}
	return "", false
}

// CheckDeviceID verifies that the file at the given relative path is on the
// same device as the SafeRoot. Protects against unmounted roots.
func (sr *SafeRoot) CheckDeviceID(rel string) error {
	fi, err := sr.root.Lstat(rel)
	if err != nil {
		return err
	}
	dev, err := deviceID(fi)
	if err != nil {
		return err
	}
	if dev != sr.devID {
		return fmt.Errorf("%w: root dev=%d, path dev=%d", ErrDeviceMismatch, sr.devID, dev)
	}
	return nil
}

// deviceID extracts the device ID from an os.FileInfo.
func deviceID(fi os.FileInfo) (uint64, error) {
	stat, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, errors.New("failed to get syscall.Stat_t from FileInfo")
	}
	//nolint:gosec // Stat_t.Dev is always non-negative
	return uint64(stat.Dev), nil
}
