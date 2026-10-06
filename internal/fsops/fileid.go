// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package fsops

import (
	"encoding/binary"
	"net"
	"strconv"
	"sync"

	"github.com/autobrr/qui/internal/models"
	"github.com/autobrr/qui/pkg/hardlink"
)

// FileKey is a FileID bound to the filesystem it came from. Two keys are
// equal exactly when SameFile holds, so an index that mixes instances keys
// on it instead of on the bare FileID.
type FileKey struct {
	id hardlink.FileID
	// scope is an interned host:port. Not the string, and not a uint32: a
	// pointer-free key with no padding hashes as one memhash call, and
	// orphan scan and the hardlink index key hundreds of thousands of these.
	scope [4]byte
}

// FileKeyOf binds id to the instance that reported it. Local kinds all live
// on the qui host, so they share one scope; a remote kind is scoped to the
// SSH host and port, the same-filesystem rule orphan scan merges by. A nil
// instance is the qui host itself.
func FileKeyOf(id hardlink.FileID, instance *models.Instance) FileKey {
	// Kept to one branch so the local path inlines; orphan scan calls this per file.
	if id.Kind() != hardlink.KindRemote || instance == nil {
		return FileKey{id: id}
	}
	return remoteFileKey(id, instance)
}

func remoteFileKey(id hardlink.FileID, instance *models.Instance) FileKey {
	key := FileKey{id: id}
	binary.LittleEndian.PutUint32(key.scope[:], remoteScope(net.JoinHostPort(instance.SSHHost, strconv.Itoa(instance.SSHPort))))
	return key
}

// SameFile reports whether two IDs name one file. IDs of different kinds
// never match, and a zero ID matches nothing.
func SameFile(a hardlink.FileID, aInstance *models.Instance, b hardlink.FileID, bInstance *models.Instance) bool {
	return !a.IsZero() && FileKeyOf(a, aInstance) == FileKeyOf(b, bInstance)
}

var remoteScopes = struct {
	sync.Mutex
	ids map[string]uint32
}{ids: map[string]uint32{}}

// remoteScope interns one host:port per process. 0 stays the qui host, so a
// remote identity never shares a scope with a local one.
func remoteScope(hostPort string) uint32 {
	remoteScopes.Lock()
	defer remoteScopes.Unlock()
	id, ok := remoteScopes.ids[hostPort]
	if !ok {
		id = uint32(len(remoteScopes.ids)) + 1 //nolint:gosec // a handful of instances, never 4 billion
		remoteScopes.ids[hostPort] = id
	}
	return id
}
