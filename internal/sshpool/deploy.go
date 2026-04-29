// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"fmt"
	"strings"

	"golang.org/x/crypto/ssh"
)

// DetectArch probes the remote host's architecture via SSH.
// Returns (os, arch) matching Go's GOOS/GOARCH naming.
func DetectArch(client *ssh.Client) (goos, goarch string, err error) {
	session, err := client.NewSession()
	if err != nil {
		return "", "", fmt.Errorf("open session: %w", err)
	}
	defer session.Close()

	output, err := session.CombinedOutput("uname -m && uname -s")
	if err != nil {
		return "", "", fmt.Errorf("uname: %w", err)
	}

	lines := strings.Split(strings.TrimSpace(string(output)), "\n")
	if len(lines) < 2 {
		return "", "", fmt.Errorf("unexpected uname output: %q", string(output))
	}

	machine := strings.TrimSpace(lines[0]) // e.g., "x86_64", "aarch64"
	system := strings.TrimSpace(lines[1])  // e.g., "Linux", "Darwin"

	switch strings.ToLower(system) {
	case "linux":
		goos = "linux"
	case "darwin":
		goos = "darwin"
	default:
		return "", "", fmt.Errorf("unsupported OS: %s", system)
	}

	switch machine {
	case "x86_64", "amd64":
		goarch = "amd64"
	case "aarch64", "arm64":
		goarch = "arm64"
	default:
		return "", "", fmt.Errorf("unsupported architecture: %s", machine)
	}

	return goos, goarch, nil
}

// DeployHelper pushes a helper binary to the remote host via SCP.
// The binary is written atomically (tmp + rename) to the configured path.
//
// This is a scaffold — the actual download-from-GitHub + SHA256 verification
// is implemented when the release pipeline is ready.
func DeployHelper(client *ssh.Client, binary []byte, remotePath string) error {
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("open session: %w", err)
	}
	defer session.Close()

	// Ensure parent directory exists.
	dir := remotePath[:strings.LastIndex(remotePath, "/")]
	if _, err := runCommand(client, "mkdir -p "+dir); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}

	// Write binary to temp file.
	tmpPath := remotePath + ".new"

	stdin, err := session.StdinPipe()
	if err != nil {
		return fmt.Errorf("stdin pipe: %w", err)
	}

	if err := session.Start(fmt.Sprintf("cat > %s && chmod 700 %s", tmpPath, tmpPath)); err != nil {
		return fmt.Errorf("start scp: %w", err)
	}

	if _, err := stdin.Write(binary); err != nil {
		return fmt.Errorf("write binary: %w", err)
	}
	stdin.Close()

	if err := session.Wait(); err != nil {
		return fmt.Errorf("scp wait: %w", err)
	}

	// Atomic rename.
	if _, err := runCommand(client, fmt.Sprintf("mv %s %s", tmpPath, remotePath)); err != nil {
		return fmt.Errorf("rename: %w", err)
	}

	return nil
}

func runCommand(client *ssh.Client, cmd string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()
	out, err := session.CombinedOutput(cmd)
	return string(out), err
}
