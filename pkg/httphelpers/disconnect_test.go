// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package httphelpers

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIsClientDisconnect(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{name: "nil", err: nil, want: false},
		{name: "epipe", err: syscall.EPIPE, want: true},
		{name: "wrapped epipe", err: fmt.Errorf("write: %w", &net.OpError{Op: "write", Err: syscall.EPIPE}), want: true},
		{name: "wrapped econnreset", err: fmt.Errorf("write: %w", syscall.ECONNRESET), want: true},
		{name: "net closed", err: fmt.Errorf("write: %w", net.ErrClosed), want: true},
		{name: "context canceled", err: fmt.Errorf("write: %w", context.Canceled), want: true},
		{name: "context deadline", err: fmt.Errorf("write: %w", context.DeadlineExceeded), want: true},
		{name: "flattened context canceled", err: errors.New("Get: context canceled"), want: true},
		{name: "windows aborted", err: errors.New("wsasend: An established connection was aborted by the software in your host machine."), want: true},
		{name: "windows forcibly closed", err: errors.New("write tcp 127.0.0.1:1->127.0.0.1:2: wsasend: An existing connection was forcibly closed by the remote host."), want: true},
		{name: "unrelated", err: io.ErrShortWrite, want: false},
		{name: "unsupported json value", err: errors.New("json: unsupported type: chan int"), want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsClientDisconnect(tt.err))
		})
	}
}
