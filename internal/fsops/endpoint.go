// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strconv"

	"github.com/autobrr/qui/internal/models"
)

// WalkedEndpoint names the SSH server and credentials a remote walk reads
// through instance, and is empty for any other mode. The account is part of it
// because a username change keeps the pin and can see other files. A digest of
// the stored pin is part of it because a host key replaced at the same address
// is another server, and a digest of the stored private key because another
// key can log in to another view of the same server. Both are sealed with a
// random nonce, so any save of either changes the digest, even of the same key.
func WalkedEndpoint(instance *models.Instance) string {
	if models.FilesystemAccessMode(instance) != models.FilesystemModeRemote {
		return ""
	}
	pin := sha256.Sum256([]byte(instance.SSHHostKeyEncrypted))
	key := sha256.Sum256([]byte(instance.SSHKeyEncrypted))
	return instance.SSHUsername + "@" + net.JoinHostPort(instance.SSHHost, strconv.Itoa(instance.SSHPort)) +
		"#" + hex.EncodeToString(pin[:8]) + "." + hex.EncodeToString(key[:8])
}
