// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package jackett

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
)

func HiddenGUID(indexerID int, guid, downloadURL string) string {
	sum := sha256.Sum256([]byte(strconv.Itoa(indexerID) + "\x00" + cmp.Or(guid, downloadURL)))
	return hex.EncodeToString(sum[:])
}
