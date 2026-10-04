// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import (
	"context"
	"errors"
	"net"
	"strings"
	"syscall"
)

// IsClientDisconnect reports whether err is an expected client-side socket or
// request-lifetime failure: the peer went away, the connection was closed, or
// the request context was canceled or timed out.
//
// Detection goes through errors.Is for wrapped errors and also matches on the
// message, because the Windows forms (connection aborted, connection forcibly
// closed by the remote host) do not map to syscall.EPIPE or syscall.ECONNRESET.
func IsClientDisconnect(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EPIPE) || errors.Is(err, syscall.ECONNRESET) || errors.Is(err, net.ErrClosed) {
		return true
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}

	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context canceled") ||
		strings.Contains(msg, "context deadline exceeded") ||
		strings.Contains(msg, "connection reset") ||
		strings.Contains(msg, "broken pipe") ||
		strings.Contains(msg, "use of closed network connection") ||
		strings.Contains(msg, "forcibly closed by the remote host") ||
		strings.Contains(msg, "connection was aborted") ||
		strings.Contains(msg, "wsasend:")
}
