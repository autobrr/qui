//go:build linux

// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package reflinktree

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func TestCloneFile_RetriesEAGAIN(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.txt")
	dstPath := filepath.Join(tmpDir, "dst.txt")

	if err := os.WriteFile(srcPath, []byte("reflink test"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	originalClone := ioctlFileClone
	originalCloneRange := ioctlFileCloneRange
	originalSleep := sleepForRetry
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		ioctlFileCloneRange = originalCloneRange
		sleepForRetry = originalSleep
	})

	attempts := 0
	ioctlFileClone = func(_, _ int) error {
		attempts++
		if attempts < 3 {
			return unix.EAGAIN
		}
		return nil
	}
	ioctlFileCloneRange = func(_ int, _ *unix.FileCloneRange) error {
		t.Fatalf("unexpected FICLONERANGE fallback")
		return nil
	}
	var delays []time.Duration
	sleepForRetry = func(delay time.Duration) {
		delays = append(delays, delay)
	}

	if err := cloneFile(srcPath, dstPath); err != nil {
		t.Fatalf("expected reflink clone to succeed after retries: %v", err)
	}

	if attempts != 3 {
		t.Fatalf("expected 3 clone attempts, got %d", attempts)
	}
	if len(delays) != 2 {
		t.Fatalf("expected 2 backoff delays, got %d", len(delays))
	}
	if delays[0] != 25*time.Millisecond || delays[1] != 50*time.Millisecond {
		t.Fatalf("unexpected backoff delays: %v", delays)
	}

	if _, err := os.Stat(dstPath); err != nil {
		t.Fatalf("expected destination file to exist: %v", err)
	}
}

func TestCloneFile_EAGAINRetriesExhausted(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.txt")
	dstPath := filepath.Join(tmpDir, "dst.txt")

	if err := os.WriteFile(srcPath, []byte("reflink test"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	originalClone := ioctlFileClone
	originalCloneRange := ioctlFileCloneRange
	originalSleep := sleepForRetry
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		ioctlFileCloneRange = originalCloneRange
		sleepForRetry = originalSleep
	})

	attempts := 0
	sleepCalls := 0
	ioctlFileClone = func(_, _ int) error {
		attempts++
		return unix.EAGAIN
	}
	ioctlFileCloneRange = func(_ int, _ *unix.FileCloneRange) error {
		t.Fatalf("unexpected FICLONERANGE fallback")
		return nil
	}
	sleepForRetry = func(time.Duration) {
		sleepCalls++
	}

	err := cloneFile(srcPath, dstPath)
	if err == nil {
		t.Fatal("expected clone to fail after exhausting retries")
	}
	if !errors.Is(err, unix.EAGAIN) {
		t.Fatalf("expected wrapped EAGAIN error, got: %v", err)
	}
	if attempts != reflinkCloneRetryAttempts {
		t.Fatalf("expected %d clone attempts, got %d", reflinkCloneRetryAttempts, attempts)
	}
	if sleepCalls != reflinkCloneRetryAttempts-1 {
		t.Fatalf("expected %d sleep calls, got %d", reflinkCloneRetryAttempts-1, sleepCalls)
	}
}

func TestCloneFile_FallbacksToCloneRange(t *testing.T) {
	tmpDir := t.TempDir()
	srcPath := filepath.Join(tmpDir, "src.txt")
	dstPath := filepath.Join(tmpDir, "dst.txt")

	if err := os.WriteFile(srcPath, []byte("reflink test"), 0o600); err != nil {
		t.Fatalf("failed to write source file: %v", err)
	}

	originalClone := ioctlFileClone
	originalCloneRange := ioctlFileCloneRange
	originalSleep := sleepForRetry
	t.Cleanup(func() {
		ioctlFileClone = originalClone
		ioctlFileCloneRange = originalCloneRange
		sleepForRetry = originalSleep
	})

	attempts := 0
	cloneRangeCalls := 0
	ioctlFileClone = func(_, _ int) error {
		attempts++
		return unix.EOPNOTSUPP
	}
	ioctlFileCloneRange = func(_ int, _ *unix.FileCloneRange) error {
		cloneRangeCalls++
		return nil
	}
	sleepForRetry = func(time.Duration) {
		t.Fatal("unexpected retry sleep")
	}

	if err := cloneFile(srcPath, dstPath); err != nil {
		t.Fatalf("expected clone-range fallback to succeed: %v", err)
	}
	if attempts != 1 {
		t.Fatalf("expected 1 clone attempt, got %d", attempts)
	}
	if cloneRangeCalls != 1 {
		t.Fatalf("expected 1 clone-range call, got %d", cloneRangeCalls)
	}
}
