// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

// Package executor handles dispatching protocol commands to the appropriate
// filesystem operation. In this initial scaffold, only diag.echo is supported.
package executor

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/autobrr/qui/pkg/agent/proto"
)

// Executor processes incoming commands and returns results.
type Executor struct{}

// NewExecutor creates a new command executor.
func NewExecutor() *Executor { return &Executor{} }

// Execute dispatches a command to the appropriate handler and returns the result.
func (e *Executor) Execute(ctx context.Context, cmd proto.Command) proto.Result {
	switch cmd.Op {
	case proto.OpDiagEcho:
		return e.diagEcho(cmd)
	default:
		return proto.Result{
			RequestID: cmd.RequestID,
			OK:        false,
			Code:      proto.CodeInternal,
			Error:     fmt.Sprintf("unsupported op: %s", cmd.Op),
			Done:      true,
		}
	}
}

func (e *Executor) diagEcho(cmd proto.Command) proto.Result {
	var req proto.DiagEchoRequest
	if err := json.Unmarshal(cmd.Args, &req); err != nil {
		return proto.Result{
			RequestID: cmd.RequestID,
			OK:        false,
			Code:      proto.CodeInternal,
			Error:     fmt.Sprintf("unmarshal diag.echo args: %v", err),
			Done:      true,
		}
	}

	payload, _ := json.Marshal(proto.DiagEchoResponse{Message: req.Message})
	return proto.Result{
		RequestID: cmd.RequestID,
		OK:        true,
		Payload:   payload,
		Done:      true,
	}
}
