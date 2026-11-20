// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"net"
	"strconv"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
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

// WalkedEndpointTag is an 8-byte digest of WalkedEndpoint, for a store that
// keeps one in each row. It is nil for a nil instance and for any mode but
// remote. Dir Scan stores it, so it never changes for the same endpoint.
func WalkedEndpointTag(instance *models.Instance) []byte {
	if instance == nil {
		return nil
	}
	endpoint := WalkedEndpoint(instance)
	if endpoint == "" {
		return nil
	}
	sum := sha256.Sum256([]byte(endpoint))
	return sum[:8]
}

// SameSSHServer reports whether a and b reach one SSH server: the same host
// and port, or the same pinned host key under any name or port, such as an IP
// and a hostname for one seedbox. aPin is a's host key pin, and pinOf reads
// b's, only when the host and port differ and aPin is set. A pin only ever
// adds a match, so a missing or unreadable pin (nil) still pairs by host and port.
func SameSSHServer(a *models.Instance, aPin []byte, b *models.Instance, pinOf func(*models.Instance) []byte) bool {
	if a.SSHHost == "" || b.SSHHost == "" {
		return false
	}
	if a.SSHHost == b.SSHHost && a.SSHPort == b.SSHPort {
		return true
	}
	return len(aPin) > 0 && bytes.Equal(aPin, pinOf(b))
}

// Endpoint is where qui reaches an instance: its qBittorrent URL, its
// filesystem access mode, and the WalkedEndpoint of a remote instance. Work
// that must not go on against another server stores EndpointOf with its first
// step and asks Changed before each later one.
type Endpoint struct {
	URL    string
	Mode   models.FilesystemMode
	Walked string
}

func EndpointOf(instance *models.Instance) Endpoint {
	return Endpoint{URL: instance.Host, Mode: models.FilesystemAccessMode(instance), Walked: WalkedEndpoint(instance)}
}

func (e Endpoint) Changed(current *models.Instance) bool {
	return current == nil || EndpointOf(current) != e
}

type FileRelation int

const (
	// FilesUnknown is every pair with an instance that has no local
	// filesystem access, such as an SSH instance, and every pair with a file
	// that has no identity. A file identity that a remote host reports never
	// widens a delete (ADR 0033).
	FilesUnknown FileRelation = iota
	FilesSame
	FilesDifferent
)

// CompareFiles answers whether the file with identity aID on instance a is
// the file with identity bID on instance b. Each identity comes from an Lstat
// through the backend of its instance.
func CompareFiles(a *models.Instance, aID hardlink.FileID, b *models.Instance, bID hardlink.FileID) FileRelation {
	if !trustedFileID(a, aID) || !trustedFileID(b, bID) {
		return FilesUnknown
	}
	if aID == bID {
		return FilesSame
	}
	return FilesDifferent
}

func trustedFileID(instance *models.Instance, id hardlink.FileID) bool {
	return models.FilesystemCapabilitiesOf(instance).TrustedIdentity && !id.IsZero()
}
