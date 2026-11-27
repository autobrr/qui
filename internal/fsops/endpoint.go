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

func WalkedEndpoint(instance *models.Instance) string {
	if models.FilesystemAccessMode(instance) != models.FilesystemModeRemote {
		return ""
	}
	pin := sha256.Sum256([]byte(instance.SSHHostKeyEncrypted))
	key := sha256.Sum256([]byte(instance.SSHKeyEncrypted))
	return instance.SSHUsername + "@" + net.JoinHostPort(instance.SSHHost, strconv.Itoa(instance.SSHPort)) +
		"#" + hex.EncodeToString(pin[:8]) + "." + hex.EncodeToString(key[:8])
}

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

func SameSSHServer(a *models.Instance, aPin []byte, b *models.Instance, pinOf func(*models.Instance) []byte) bool {
	if a.SSHHost == "" || b.SSHHost == "" {
		return false
	}
	if a.SSHHost == b.SSHHost && a.SSHPort == b.SSHPort {
		return true
	}
	return len(aPin) > 0 && bytes.Equal(aPin, pinOf(b))
}

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
	FilesUnknown FileRelation = iota
	FilesSame
	FilesDifferent
)

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
