// Copyright (c) 2025-2026, s0up and the autobrr contributors.
// SPDX-License-Identifier: GPL-2.0-or-later

package models

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestArrInstanceStoreUpdateNilParams(t *testing.T) {
	store, err := NewArrInstanceStore(nil, make([]byte, 32))
	require.NoError(t, err)

	_, err = store.Update(context.Background(), 123, nil)
	require.EqualError(t, err, "params cannot be nil")
}
