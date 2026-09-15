// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package sshpool

import (
	"bytes"
	"context"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// gnuProbeCommand asks both tools the remote file operations will lean on for
// their version. It is a fixed string with nothing interpolated into it.
const gnuProbeCommand = "LC_ALL=C find --version && LC_ALL=C stat --version"

// outputLimit caps what a probe keeps from a remote command. The server is the
// thing being probed, so its output is not trusted to be short.
const outputLimit = 8 << 10

// probe reports what the server lets us do. It is best effort and never fails
// the call: a sub-probe that errors leaves its flags false, because Test
// reports the outcome of the dial, not of the probe.
func probe(ctx context.Context, client *ssh.Client) *Capabilities {
	capabilities := &Capabilities{}

	if sftpClient, err := sftp.NewClient(client); err == nil {
		capabilities.SFTP = true
		_, capabilities.Statvfs = sftpClient.HasExtension("statvfs@openssh.com")
		_, capabilities.Hardlink = sftpClient.HasExtension("hardlink@openssh.com")
		_, capabilities.Limits = sftpClient.HasExtension("limits@openssh.com")
		_ = sftpClient.Close()
	}

	// A key restricted to sftp still opens a session channel: it runs the
	// command, prints a refusal and exits non-zero. So "commands run at all"
	// needs a command of its own before the userland question can be asked.
	_, err := run(client, "true")
	capabilities.Exec = err == nil
	if !capabilities.Exec || ctx.Err() != nil {
		return capabilities
	}

	// GNU tools print "<tool> (GNU <package>) <version>" as their first line;
	// the BSD and busybox ones print neither banner. Both are required: the
	// flags the remote backend passes belong to both packages.
	out, err := run(client, gnuProbeCommand)
	capabilities.GNUUserland = err == nil &&
		strings.Contains(out, "GNU findutils") &&
		strings.Contains(out, "GNU coreutils")

	return capabilities
}

func run(client *ssh.Client, command string) (string, error) {
	session, err := client.NewSession()
	if err != nil {
		return "", err
	}
	defer session.Close()

	// x/crypto copies each stream in its own goroutine, so the two streams need
	// a buffer each. Run waits for both copies before it returns, which is what
	// orders these reads.
	var stdout, stderr cappedBuffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Run(command); err != nil {
		return "", err
	}

	return stdout.buf.String() + stderr.buf.String(), nil
}

// cappedBuffer keeps the first outputLimit bytes and reports every write as
// accepted, so a chatty server is truncated rather than killed mid-command.
type cappedBuffer struct {
	buf bytes.Buffer
}

func (w *cappedBuffer) Write(p []byte) (int, error) {
	if room := outputLimit - w.buf.Len(); room > 0 {
		w.buf.Write(p[:min(room, len(p))])
	}
	return len(p), nil
}
