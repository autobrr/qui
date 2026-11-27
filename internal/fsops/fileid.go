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

type FileKey struct {
	id    hardlink.FileID
	scope [4]byte
}

func FileKeyOf(id hardlink.FileID, instance *models.Instance) (key FileKey, ok bool) {
	if id.Kind() != hardlink.KindRemote {
		return FileKey{id: id}, !id.IsZero()
	}
	return remoteFileKey(id, instance)
}

func remoteFileKey(id hardlink.FileID, instance *models.Instance) (FileKey, bool) {
	if instance == nil || instance.SSHHost == "" {
		return FileKey{}, false
	}
	key := FileKey{id: id}
	binary.LittleEndian.PutUint32(key.scope[:], remoteScope(net.JoinHostPort(instance.SSHHost, strconv.Itoa(instance.SSHPort))))
	return key, true
}

func SameFile(a hardlink.FileID, aInstance *models.Instance, b hardlink.FileID, bInstance *models.Instance) bool {
	aKey, aOK := FileKeyOf(a, aInstance)
	bKey, bOK := FileKeyOf(b, bInstance)
	return aOK && bOK && aKey == bKey
}

var remoteScopes = struct {
	sync.Mutex
	ids map[string]uint32
}{ids: map[string]uint32{}}

func remoteScope(hostPort string) uint32 {
	remoteScopes.Lock()
	defer remoteScopes.Unlock()
	id, ok := remoteScopes.ids[hostPort]
	if !ok {
		id = uint32(len(remoteScopes.ids)) + 1
		remoteScopes.ids[hostPort] = id
	}
	return id
}
